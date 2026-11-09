//go:build windows

package main

import (
	"time"
	"unsafe"
)

func (ui *WindowsUI) wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_SIZING:
		ui.handleSizing(wParam, lParam)
		ui.layoutChildrenResize(false) // 拖动中轻量更新，不 ResizeBuffers，避免闪黑
		ui.repositionOverlay()
		return 1
	case WM_EXITSIZEMOVE:
		// 拖动/移动结束：执行完整 resize，让 swapchain 尺寸与窗口匹配（拖动中是轻量更新）
		ui.layoutChildren()
		ui.repositionOverlay()
		return 0
	case WM_GETMINMAXINFO:
		mmi := (*struct {
			Reserved             uintptr
			MaxSizeX, MaxSizeY   int32
			MaxPos               [2]int32
			MinTrackX, MinTrackY int32
			MaxTrack             [2]int32
		})(unsafe.Pointer(lParam))
		if ui.app.Controlled {
			// 矩阵投屏需要允许更小窗口，支持大量设备时自动缩小
			mmi.MinTrackX = 100
			mmi.MinTrackY = 140
			return 0
		}
		// 非controlled 单台：统一最小值与 controlled 一致（用户要求手动/排列最小值统一），
		// 支持用户拖到很小；minFrameW/H 基准仍用于布局计算，不限制下限。
		mmi.MinTrackX = 100
		mmi.MinTrackY = 140
		return 0
	case WM_SIZE:
		// 系统最大化→还原的形状校正：最大化期间旋转过（zoomedRotated ≠ 当前）
		// 则还原形状与视频方向失配（letterbox 大黑边）——与 ToggleFullscreen 还原
		// 同源问题（2026-09-08 用户报障：标题栏双击最大化→旋转→还原→黑边）。
		if z, _, _ := procIsZoomed.Call(hwnd); z != 0 {
			if !ui.wasZoomed {
				ui.zoomedRotated = ui.app.Rotated.Load() // 进入最大化：快照旋转态
			}
			ui.wasZoomed = true
		} else if ui.wasZoomed {
			ui.wasZoomed = false
			ui.correctWindowShapeToVideo(ui.zoomedRotated != ui.app.Rotated.Load(), "zoomRestore")
		}
		ui.layoutChildren()
		fallthrough
	case WM_MOVE:
		ui.repositionOverlay()
		ui.repositionToast()
		return 0
	case WM_NCHITTEST:
		// 窗口样式不变（仍带 WS_THICKFRAME 创建），主程序排版为无缝贴合会临时去掉
		// WS_THICKFRAME。去掉后系统不再返回边缘缩放 hit-test，这里接管：仅当
		// DefWindowProc 判定为 HTCLIENT（客户区）且鼠标落在窗口边缘时，返回缩放
		// hit-test，实现无边框可缩放。标题栏拖动与系统按钮仍由系统处理。
		return ui.nchHitTest(lParam)
	case WM_SYSCOMMAND:
		// 不拦截系统命令，保留标题栏关闭和最小化。
	case WM_DPICHANGED:
		ui.handleDpiChanged(lParam)
		return 0
	case WM_COMMAND:
		cmdID := int(wParam & 0xFFFF)
		notifyCode := int((wParam >> 16) & 0xFFFF)
		if notifyCode == BN_CLICKED {
			switch cmdID {
			case IDC_NAV_BACK:
				ui.app.SendCmd(CmdAndroidKey, KeyBack)
			case IDC_NAV_HOME:
				ui.app.SendCmd(CmdAndroidKey, KeyHome)
			case IDC_NAV_RECENT:
				ui.app.SendCmd(CmdAndroidKey, KeyRecent)
			}
		}
		return 0
	case WM_TIMER:
		ui.handleTimer(wParam)
		return 0
	case WM_PARENTNOTIFY:
		if wParam == WM_LBUTTONDOWN || wParam == WM_RBUTTONDOWN || wParam == WM_MBUTTONDOWN {
			if ui.hSdlArea != 0 {
				procSetFocus.Call(uintptr(ui.hSdlArea))
			}
		}
		return 0
	case WM_KEYDOWN, WM_KEYUP:
		focusH, _, _ := procGetFocus.Call()
		// 键盘路由:焦点在视频区/主窗口时进键位引擎(F9 全局开关必须可用);
		// 焦点在 WebView2 侧栏时不拦(输入框打字优先,此时开关走侧栏按钮)。
		if focusH == uintptr(ui.hSdlArea) || focusH == hwnd || focusH == 0 {
			ui.handleWin32Keyboard(uint32(msg), wParam, lParam)
			return 0
		}
	case WM_CHAR:
		focusH, _, _ := procGetFocus.Call()
		if focusH == uintptr(ui.hSdlArea) {
			ui.handleWin32Char(wParam)
			return 0
		}
	case WM_MOUSEWHEEL:
		ui.handleMouseWheel(wParam, lParam)
		return 0
	case WM_COPYDATA:
		return ui.handleCopyData(lParam)
	case WM_APP_SYNC_STATE:
		ui.doSyncStateToPanel()
		return 0
	case WM_APP_PUBFIT:
		// 公网投屏：拿到视频真实尺寸后，把窗口横屏适配到内容比例（UI 线程）。
		// 处于全屏/最大化填充态时窗口尺寸不能动，改为重排 WebView 的 letterbox。
		if ui.fillScreen() {
			ui.layoutChildren()
		} else {
			ui.tryFitPublicWindow()
		}
		return 0
	case WM_APP_TEBOX_REFIT:
		// tebox 方向自适应翻转（竖↔横，tebox_orientation.go）：重置一次性初始
		// fit 标记，泵下一拍 tryRefitOnSourceChange 按新源尺寸/方向 refit。
		// 全屏/最大化时窗口尺寸不能动，跳过（画面由 letterbox 自适应）。
		if !ui.fillScreen() {
			ui.teboxInitialFitDone = false
		}
		return 0
	case WM_APP_RENDER:
		// 新帧就绪：立即 Present，不等 WM_TIMER 16ms tick（帧到即渲染，消除抖动）。
		// WM_TIMER 仍作为兜底（万一 PostMessage 丢失或重连场景）。
		if d3d := ui.app.D3D11Ren.Load(); d3d != nil {
			d3d.Present()
			// 首帧 Present 后延迟隐藏 overlay：首帧可能是 D3D11VA 过渡期 UV=0 黑帧（shader 用 prevTex 显示黑），
			// 立即隐藏 overlay 会露出黑帧。延迟 500ms 让过渡期过去（硬件就绪出有效画面）再隐藏，
			// 期间 overlay 遮住黑帧，用户看到“加载中”。有帧就渲染不丢帧，只是 overlay 延迟隐藏。
			if ui.hOverlay != 0 && ui.app.firstFramePresented.CompareAndSwap(false, true) {
				go func() {
					time.Sleep(500 * time.Millisecond)
					procPostMessageW.Call(uintptr(ui.hParent), WM_APP_HIDE_OVERLAY, 0, 0)
				}()
			}
		}
		return 0
	case WM_APP_HIDE_OVERLAY:
		// 首帧后延迟 500ms 隐藏 overlay（遮住过渡期黑帧，出有效画面后再隐藏）
		if ui.hOverlay != 0 {
			procShowWindow.Call(uintptr(ui.hOverlay), 0)
		}
		// 首帧稳定后收敛键位覆盖层运行态显隐：源尺寸就绪，kmPctToClient 可用；
		// 未编辑但 F9+按键提醒开着时把运行态键帽标记显示出来（启动收敛唯一入口）。
		ui.syncKeymapOverlayVis()
		return 0
	case WM_APP_MATRIX_EVAL:
		// 整体换出队列后在锁外执行 Eval（已在 UI 线程，调用安全）
		ui.mu.Lock()
		q := ui.matrixEvalQ
		ui.matrixEvalQ = nil
		ui.mu.Unlock()
		for _, script := range q {
			if ui.webChromium != nil {
				ui.webChromium.Eval(script)
			}
		}
		return 0
	case WM_APP_KM_TOGGLE:
		// F9 切换键盘映射:toast 提示 + 侧边栏状态同步
		ui.onKeymapToggled(wParam == 1)
		return 0
	case WM_APP_OVERLAY:
		ui.mu.Lock()
		text := ui.overlayText
		ui.mu.Unlock()
		ui.showOverlay(text)
		return 0
	case WM_APP_CLARITY_TOAST:
		// 清晰度切换成功反馈：码率档位无视觉信号（分辨率不变），toast 是唯一可感知确认
		ui.showClarityToast(int(wParam))
		return 0
	case WM_APP_SIDEBAR_INIT:
		// 首帧上屏后初始化侧边栏（或超时兜底触发）。同步执行：WebView2 必须在
		// 创建它的 STA 线程初始化；Embed 自带消息泵，期间主窗口消息仍被派发。
		ui.doSidebarInit()
		return 0
	case WM_APP_FIRST_FRAME:
		ui.handleFirstFrame()
		return 0
	case WM_APP_D3D11_REBUILD:
		// D3D11 设备丢失（DEVICE_REMOVED/RESET）：重建渲染器+解码器管线。
		// RTSP 连接保持不动，重建管线恢复画面。
		ui.rebuildVideoPipeline()
		return 0
	case WM_APP_IPLOC:
		ui.doIPLocUpdate()
		return 0
	case WM_APP_SMS_RESULT:
		ui.doSMSResultUpdate()
		return 0
	case WM_APP_CONTACT_RESULT:
		ui.doContactResultUpdate()
		return 0
	case WM_APP_SCREENSHOT:
		ui.handleScreenshotResult()
		return 0
	case WM_APP_UPLOAD:
		ui.handleUploadFilePick()
		return 0
	case WM_APP_UPLOAD_SIDEBAR:
		ui.doUploadResultUpdate()
		return 0
	case WM_DROPFILES:
		ui.handleDropFiles(wParam)
		return 0
	case WM_PAINT:
		if ui.blackBars {
			// 公网全屏 letterbox：把 WebView 视频区四周露出的区域涂黑
			var ps struct {
				Hdc         uintptr
				Erase       int32
				RcPaint     struct{ Left, Top, Right, Bottom int32 }
				Restored    int32
				IncUpdate   int32
				RgbReserved [32]byte
			}
			procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			if ps.Hdc != 0 {
				var rc struct{ Left, Top, Right, Bottom int32 }
				procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
				procFillRect.Call(ps.Hdc, uintptr(unsafe.Pointer(&rc)), ui.hBrushBlack)
			}
			procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			return 0
		}
		procValidateRect.Call(hwnd, 0)
		return 0
	case WM_ERASEBKGND, WM_NCACTIVATE:
		if ui.blackBars {
			var rc struct{ Left, Top, Right, Bottom int32 }
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
			procFillRect.Call(wParam, uintptr(unsafe.Pointer(&rc)), ui.hBrushBlack)
			return 1
		}
		return 1
	case WM_NCPAINT:
		ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return ret
	case WM_CONTEXTMENU, 0x00A4, 0x00A5, 0x00A6:
		return 0
	case WM_DESTROY:
		ui.handleDestroy()
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}
