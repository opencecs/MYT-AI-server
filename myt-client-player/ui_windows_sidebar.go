//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jchv/go-webview2/pkg/edge"
)

// ui_windows_sidebar.go — 侧边栏 WebView2 初始化（从 ui_windows.go 抽出）。
//
// 时序约定（为什么不能更早、也不能丢进 goroutine）：
//  1. WebView2 必须在创建它的 STA 线程上初始化——edge 包的 init() 在主线程
//     LockOSThread + CoInitializeEx(STA)，且 Eval/Resize 都是裸 vtable 调用，
//     跨线程会静默失败（同款结论见 ui_windows_webview.go 矩阵 Eval 的注释）。
//     所以 Embed 不能放进独立 goroutine。
//  2. Embed 内部自带 GetMessageW 消息泵（vendor/.../edge/chromium.go:95-111），
//     但那要等 environment 创建完成才进泵；environment 创建本身是同步阻塞调用，
//     这期间主窗口消息不派发、画面不更新。
// 因此策略是「等首帧上屏后再初始化」：把 WebView2 完全排除在视频首帧路径之外，
// 用户先看到画面，侧边栏随后出现；迟迟没有首帧时超时兜底，侧边栏功能不丢失。

const sidebarInitTimeout = 8 * time.Second

// scheduleSidebarInit 在首帧上屏（或超时）后把侧边栏初始化投递到 UI 线程。
// 调用点必须在 app.Connect() 已经启动之后。
func (ui *WindowsUI) scheduleSidebarInit(app *App) {
	if app.NoSidebar {
		logf("[ui] sidebar disabled (nosidebar mode), skipping WebView2\n")
		return
	}
	go func() {
		deadline := time.Now().Add(sidebarInitTimeout)
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		firstFrame := false
		for {
			if app.firstFramePresented.Load() {
				firstFrame = true
				break
			}
			if time.Now().After(deadline) {
				break
			}
			select {
			case <-app.shutdownCh:
				// 窗口已关闭：不再等首帧，直接退出（避免关窗后残留 goroutine）。
				logf("[ui] sidebar init skipped: shutting down\n")
				return
			case <-tick.C:
			}
			if r, _, _ := procIsWindow.Call(uintptr(ui.hParent)); r == 0 {
				logf("[ui] sidebar init skipped: host window gone\n")
				return
			}
		}
		if firstFrame {
			logf("[ui] sidebar init: first frame on screen, starting WebView2\n")
		} else {
			logf("[ui] sidebar init: no first frame within %v, starting WebView2 anyway\n", sidebarInitTimeout)
		}
		if r, _, _ := procIsWindow.Call(uintptr(ui.hParent)); r == 0 {
			logf("[ui] sidebar init skipped: host window gone\n")
			return
		}
		procPostMessageW.Call(uintptr(ui.hParent), WM_APP_SIDEBAR_INIT, 0, 0)
	}()
}

// doSidebarInit 在 UI 线程执行真正的初始化（WM_APP_SIDEBAR_INIT 处理）。
// sidebarInited 只在 UI 线程读写，无需加锁。
func (ui *WindowsUI) doSidebarInit() {
	if ui.sidebarInited {
		return
	}
	ui.sidebarInited = true
	ui.initSidebar(ui.app)
}

// initSidebar 创建侧边栏 WebView2。必须在 UI 线程同步执行（STA 约束）。
func (ui *WindowsUI) initSidebar(app *App) {
	start := time.Now()
	logf("[ui] sidebar WebView2 init start\n")

	// 先建局部实例，Embed 成功后才发布到 ui.chromium：初始化期间 Embed 的消息泵
	// 会 Dispatch 主窗口消息，doSyncStateToPanel 等路径对 ui.chromium 的 Eval 靠
	// nil 判断提前返回；若提前发布，webview 尚未创建时裸 vtable 调用会崩溃。
	ch := edge.NewChromium()
	ch.DataPath = filepath.Join(os.TempDir(), "lgplayer_legacy_wv2")
	ch.MessageCallback = func(raw string) { app.dispatchMessage(raw, ui) }
	// 页面加载完成后再补推一次状态：侧边栏晚于 Connect 创建，此前 connState /
	// clarity 等变化发生时页面还不存在，那些推送已被丢弃。
	ch.NavigationCompletedCallback = func(*edge.ICoreWebView2, *edge.ICoreWebView2NavigationCompletedEventArgs) {
		logf("[ui] sidebar navigation completed (%dms)\n", time.Since(start).Milliseconds())
		if ui.hParent != 0 {
			procPostMessageW.Call(uintptr(ui.hParent), WM_APP_SYNC_STATE, 0, 0)
		}
	}

	ok := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				logf("[ui] sidebar WebView2 init failed (recovered): %v\n", r)
			}
		}()
		ok = ch.Embed(uintptr(ui.hSidePanel))
	}()
	if !ok {
		// 原来 if 无 else，初始化失败完全静默；侧边栏留黑底空白即可，不影响视频。
		logf("[ui] sidebar WebView2 embed failed (%dms), sidebar stays blank\n", time.Since(start).Milliseconds())
		return
	}
	// Embed 期间窗口可能已被销毁（WM_DESTROY 被泵派发 → PostQuitMessage）：
	// 此时不要再发布实例/导航，避免操作已释放的 HWND。
	if r, _, _ := procIsWindow.Call(uintptr(ui.hParent)); r == 0 || ui.hSidePanel == 0 {
		logf("[ui] sidebar init aborted: host window gone\n")
		return
	}
	ui.chromium = ch

	// 文件拖放穿透给宿主窗口（WM_DROPFILES）：dropzone 拖拽与视频区拖拽
	// 同路径走 Win32 文件名 + 流式上传，无 base64 大小限制。
	// 需 WebView2 Runtime ≥100.0.1185.39；旧运行时 no-op（JS drop 兜底）。
	// 实现见 webview_dropext.go（依赖官方包无此方法，自带 QueryInterface）
	setAllowExternalDrop(ch, false)
	ch.Resize()
	ch.Show()
	html := composeSidePanelHTML()
	if list := ctrlListJSON(app.CtrlItems); list != "[]" {
		html = strings.Replace(html, "ctrlList:[]", "ctrlList:"+list, 1)
	}
	ch.NavigateToString(html)
	logf("[ui] sidebar ready (%dms)\n", time.Since(start).Milliseconds())
}
