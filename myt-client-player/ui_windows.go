//go:build windows

package main

import (
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

type WindowsUI struct {
	app         *App
	hParent     windows.HWND
	hSdlArea    windows.HWND
	hNavBar     windows.HWND
	hSidePanel  windows.HWND
	hOverlay    windows.HWND
	chromium    *edge.Chromium
	webChromium *edge.Chromium // WebView2 模式：加载 player URL

	hFontOverlay  uintptr
	hFontUpload14 uintptr
	hFontUpload11 uintptr
	hFontToast    uintptr
	hFontToastBtn uintptr
	hBrushLight   uintptr
	hBrushOverlay uintptr
	hBrushBarBg   uintptr
	hBrushBarFill uintptr
	hBrushPill    uintptr
	hBrushPillAct uintptr
	hBrushIcon    uintptr
	hBrushIconHov uintptr
	hPenIcon      uintptr
	hPenIconHov   uintptr
	hPenGray      uintptr
	hPenSep       uintptr
	hBrushBlack   uintptr

	navMemDC  uintptr
	navMemBmp uintptr
	navOldBmp uintptr
	navBufW   int32
	navBufH   int32

	mu              sync.Mutex
	panelExpanded   bool
	baseWinW        int32
	baseWinH        int32
	sideCollapsedW  int32
	sideExpandedW   int32
	navBarHeight    int32
	navHoverBtn     int32
	navPressedBtn   int32
	navMouseTrack   bool
	overlayText     string
	pendingJS       string
	sidebarInited   bool // 侧边栏已初始化（仅 UI 线程读写，见 ui_windows_sidebar.go）
	matrixEvalQ     []string // 矩阵回放 JS 队列（跨线程 → UI 线程投递，见 WM_APP_MATRIX_EVAL）
	smsResultJS     string
	contactResultJS string
	uploadJS        string // 侧边栏上传状态/进度 JS（WM_APP_UPLOAD_SIDEBAR 消费，高频覆盖合并）
	hToast          windows.HWND
	toastLine1      string
	toastLine2      string
	toastBtnText    string
	toastBtnClick   func()
	wheelCount      int
	wheelDir        int32
	wheelLastTick   time.Time
	wheelTimer      *time.Timer

	overlayLastX     int32
	overlayLastY     int32
	sdlMouseTracking bool
	lastRepresentAt  time.Time // 上次兜底重绘时间（500ms 节流）
	rotating         bool
	fullscreen       bool
	blackBars        bool // 公网投屏全屏/最大化时，WebView 视频区等比例 letterbox，四周露黑
	fsRestoreRect    [4]int32
	fsRestoreRotated bool // 进入全屏时的旋转态：全屏中旋转过则还原尺寸需按视频方向重算
	wasZoomed        bool // 系统最大化跟踪：WM_SIZE 检测 Zoomed 翻转（还原时校正形状）
	zoomedRotated    bool // 进入最大化时的旋转态：最大化中旋转过则还原后需互换形状
	frameChromeW     int32
	frameChromeH     int32
	// 公网投屏模式（WebURL，无本地 scaler）：页面汇报的视频真实尺寸，用于窗口自适应比例
	webVideoW atomic.Int32
	webVideoH atomic.Int32

	// 键位映射编辑覆盖层(keymap_overlay_windows.go)
	hKMOverlay    windows.HWND
	kmEditing     bool
	kmWork        *KeymapProfile // 编辑中的工作副本
	kmSelID       string         // 编辑器选中条目
	kmDragIdx     int            // 拖动中的条目下标(-1=无)
	kmDragB       bool           // 拖动的是滑动终点手柄
	kmDragResize  bool           // 拖动的是 dpad 环缩放手柄
	kmCaptureID   string         // 改键捕获中的条目 id(空=无;下一按键直接绑定)
	kmCaptureSlot int            // dpad 捕获槽位(-1=自动/单键)
	// 覆盖窗上次几何(变化检测:pump 高频调用,几何没变零开销)
	kmOvlLastX, kmOvlLastY, kmOvlLastW, kmOvlLastH int32
	// lastFitSrc 记录最近一次已据此完成 fit 测量的源分辨率，供泵轮询检测变化重触发
	lastFitSrc struct {
		w, h    int32
		rotated bool
	}
	// teboxInitialFitDone tebox 一次性初始 fit 标记：首帧/泵在源尺寸就绪时按流比例
	// 设定一次初始窗口尺寸（消除默认窗口比例黑边），之后永不再 fit（用户自管尺寸，
	// 拖动不回弹）。仅 tebox 模式读写（fitWindowToVideo / tryRefitOnSourceChange）。
	teboxInitialFitDone bool
	camPollOnce sync.Once
}

// minSdl* 常量已移至 layout_constants.go（统一尺寸标准）

func NewWindowsUI(app *App) *WindowsUI {
	ui := &WindowsUI{
		app:           app,
		navBarHeight:  navBarHeightDefault,
		navHoverBtn:   -1,
		navPressedBtn: -1,
	}
	if app.NoSidebar {
		ui.sideCollapsedW = sideWidthNoSide
		ui.sideExpandedW = sideWidthNoSide
	} else {
		ui.sideCollapsedW = sideCollapsedDefault
		ui.sideExpandedW = sideExpandedDefault
	}
	return ui
}
func (ui *WindowsUI) gdiObjects() []uintptr {
	return []uintptr{
		ui.hFontOverlay, ui.hFontUpload14, ui.hFontUpload11,
		ui.hFontToast, ui.hFontToastBtn,
		ui.hBrushLight, ui.hBrushOverlay, ui.hBrushBarBg, ui.hBrushBarFill,
		ui.hBrushPill, ui.hBrushPillAct,
		ui.hBrushIcon, ui.hBrushIconHov,
		ui.hPenIcon, ui.hPenIconHov,
		ui.hPenGray, ui.hPenSep,
		ui.hBrushBlack,
	}
}
func (ui *WindowsUI) ensureFrameChrome() {
	if ui.frameChromeW != 0 {
		return
	}
	var frameRect struct{ Left, Top, Right, Bottom int32 }
	frameRect.Right = 100
	frameRect.Bottom = 100
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&frameRect)), WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN, 0, 0)
	ui.frameChromeW = (frameRect.Right - frameRect.Left) - 100
	ui.frameChromeH = (frameRect.Bottom - frameRect.Top) - 100
}

// webVideoRe 匹配 fitprobe 里的真实视频尺寸 "vr":"1920x1088"
var webVideoRe = regexp.MustCompile(`"vr":"(\d+)x(\d+)"`)

// updateWebVideoSize 解析页面 fitprobe 上报的视频分辨率。首次拿到真实尺寸时
// 通知 UI 线程做横屏适配（公网模式无消息泵，故用 PostMessage 驱动）。
// 在 WebView2 回调线程调用，仅写 atomic 字段并从非 UI 线程 PostMessage，安全。
func (ui *WindowsUI) updateWebVideoSize(raw string) {
	m := webVideoRe.FindStringSubmatch(raw)
	if m == nil {
		return
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	if w <= 0 || h <= 0 {
		return
	}
	if ui.webVideoW.Swap(int32(w)) == 0 {
		ui.webVideoH.Store(int32(h))
		if ui.hParent != 0 {
			procPostMessageW.Call(uintptr(ui.hParent), WM_APP_PUBFIT, 0, 0)
			logf("[pubfit] 触发横屏适配: %dx%d\n", w, h)
		}
	} else {
		ui.webVideoH.Store(int32(h))
	}
}

func (ui *WindowsUI) activeScaler() inputScaler {
	if d3d := ui.app.D3D11Ren.Load(); d3d != nil {
		return d3d
	}
	if ren := ui.app.Renderer.Load(); ren != nil {
		return ren
	}
	return nil
}
func (ui *WindowsUI) sdlAreaCenter() (cx, cy, areaH int32) {
	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(uintptr(ui.hSdlArea), uintptr(unsafe.Pointer(&rc)))
	var pt struct{ X, Y int32 }
	procClientToScreen.Call(uintptr(ui.hSdlArea), uintptr(unsafe.Pointer(&pt)))
	cx = (rc.Right-rc.Left)/2 + pt.X
	cy = (rc.Bottom-rc.Top)/2 + pt.Y
	areaH = rc.Bottom - rc.Top
	return
}
func (ui *WindowsUI) Run() {
	procSetConsoleOutputCP.Call(65001)
	procSetConsoleCP.Call(65001)
	app := ui.app
	if app.Controlled {
		// 批量投屏传了 -winw/-winh：baseWinW/H 已在 createWindows 用实际客户区尺寸同步，
		// 这里不再用 464×scale 默认值覆盖（否则同步失效，SDL 区/overlay 布局错位）。
		if *flagWinW > 0 || *flagWinH > 0 {
			// baseWinW/H 已由 createWindows 同步，跳过默认计算
		} else if ui.countMatrixWindows() > 1 {
			// 多台矩阵投屏基准：固定小窗口（464×780 竖屏基准 × matrixScale），不再按屏幕 70% 撑大。
			// 首帧后由 fitWindowToVideo 调到各自比例、主程序 ArrangePlayerWindows 轮询定最终网格大小。
			ms := matrixScale
			if ms <= 0 {
				ms = 0.6
			}
			ui.baseWinH = int32(float64(baseClientH) * ms)
			ui.baseWinW = int32(float64(baseClientW) * ms)
		} else {
			// 单台受控投屏（host 页点开）：固定小窗口，不跟随屏幕尺寸
			if app.Landscape {
				ui.baseWinW = 1000 + ui.sideCollapsedW
				ui.baseWinH = 660
			} else {
				ui.baseWinW = 480 + ui.sideCollapsedW
				ui.baseWinH = 880
			}
			if matrixScale > 0 && matrixScale < 1.0 {
				ui.baseWinW = int32(float64(ui.baseWinW) * matrixScale)
				ui.baseWinH = int32(float64(ui.baseWinH) * matrixScale)
			}
		}
	} else {
		if app.Landscape {
			ui.baseWinW = 1000 + ui.sideCollapsedW
			ui.baseWinH = 660
		} else {
			ui.baseWinW = 480 + ui.sideCollapsedW
			ui.baseWinH = 880
		}
		if matrixScale > 0 && matrixScale < 1.0 {
			ui.baseWinW = int32(float64(ui.baseWinW) * matrixScale)
			ui.baseWinH = int32(float64(ui.baseWinH) * matrixScale)
		}
	}
	ui.createWindows()
	ui.initGDICache()

	// WebURL 模式：WebView2 填满整个窗口，跳过 RTSP/D3D11/侧边栏
	if app.WebURL != "" {
		ui.runWebViewMode(app)
		return
	}

	ui.mu.Lock()
	ui.overlayText = "▶ 连接中..."
	ui.mu.Unlock()
	ui.layoutChildren()
	if !app.Hidden {
		procShowWindow.Call(uintptr(ui.hParent), SW_SHOW)
	}
	ui.showOverlay("▶ 连接中...")

	app.SetPlayerHwnd(uintptr(ui.hParent))
	app.OnSignalShutdown = func() {
		procPostMessageW.Call(uintptr(ui.hParent), WM_DESTROY, 0, 0)
	}
	// control server POST /close 的关闭通道（同款 PostMessage 优雅退出）
	app.RequestClose = func() {
		procPostMessageW.Call(uintptr(ui.hParent), WM_DESTROY, 0, 0)
	}
	// 同设备方案：渲染器创建时自建 device，解码器 Initialize 前通过 SetD3D11Device
	// 复用渲染器 device，无需 rebuild。
	leftW := ui.baseWinW - ui.sideCollapsedW
	sdlH := ui.baseWinH - ui.navBarHeight
	// -hwaccel=false 时完全走 CPU 路线：CPU 软解 + GDI 渲染（跳过 D3D11 GPU 渲染器）
	// 这样即使 d3d11render.dll 有问题（如 D3DCompile 失败）也不会崩溃
	if app.HardwareDecode {
		if d3d, err := NewD3D11RendererWithDevice(uintptr(ui.hSdlArea), int(leftW), int(sdlH), app.Rotated.Load(), *flagFill, 0); err == nil {
			app.D3D11Ren.Store(d3d)
			d3d.SetInputHandler(app)
			d3d.SetOnFrame(func() { // 帧到即渲染：通知 UI 线程立即 Present
				procPostMessageW.Call(uintptr(ui.hParent), WM_APP_RENDER, 0, 0)
			})
			d3d.SetOnDeviceLost(func() { // 设备丢失：重建渲染+解码管线
				procPostMessageW.Call(uintptr(ui.hParent), WM_APP_D3D11_REBUILD, 0, 0)
			})
			defer d3d.Close()
			logf("[ui] D3D11 renderer created\n")
		} else {
			logf("[ui] D3D11 renderer failed: %v, falling back to GDI\n", err)
			renderer, err := NewRenderer(uintptr(ui.hSdlArea), int(leftW), int(sdlH), app.Rotated.Load(), *flagFill)
			if err != nil {
				logf("[ui] GDI init failed: %v\n", err)
				return
			}
			app.Renderer.Store(renderer)
			renderer.SetInputHandler(app)
			defer renderer.Close()
		}
	} else {
		logf("[ui] GPU acceleration disabled, using GDI renderer (CPU-only path)\n")
		renderer, err := NewRenderer(uintptr(ui.hSdlArea), int(leftW), int(sdlH), app.Rotated.Load(), *flagFill)
		if err != nil {
			logf("[ui] GDI init failed: %v\n", err)
			return
		}
		app.Renderer.Store(renderer)
		renderer.SetInputHandler(app)
		defer renderer.Close()
	}

	// 16ms（~60fps）定时器驱动 Present；renderPending 只在有新帧时才上传 GPU
	pumpInterval := uintptr(16)
	procSetTimer.Call(uintptr(ui.hParent), TIMER_SDL_PUMP, pumpInterval, 0)
	procSetTimer.Call(uintptr(ui.hParent), TIMER_INIT_LAYOUT, 50, 0)
	procSetTimer.Call(uintptr(ui.hParent), TIMER_CONN_TIMEOUT, 4000, 0)
	ui.setupCallbacks(app)

	if app.RTSPURL != "" || app.CastMode == CastModeTebox {
		go func() {
			if err := app.Connect(); err != nil {
				logf("[ui] connect failed: %v\n", err)
			}
		}()
	}

	// 侧边栏 WebView2 不再阻塞启动：等首帧上屏后再在 UI 线程初始化（见
	// ui_windows_sidebar.go）。冷启动慢的机器上 Embed 要 10s+，原来排在 Connect
	// 之前会把整个播放器卡住——窗口不重绘、"连接中"提示都出不来。
	ui.scheduleSidebarInit(app)

	var m struct {
		Hwnd    windows.HWND
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
	}
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	app.Disconnect()
}
func (ui *WindowsUI) setupCallbacks(app *App) {
	app.ClipboardWriter = func(text string) {
		ui.writeClipboardText(text)
	}
	app.OnConnStateChange = func(state int, retry int) {
		ui.syncStateToPanel()
		if state == ConnKicked {
			procKillTimer.Call(uintptr(ui.hParent), TIMER_CONN_TIMEOUT)
		}
		if state == ConnConnected && !app.overlayQuiet.Load() {
			logf("[overlay] state=Connected, showing 'waiting for frame'\n")
			ui.mu.Lock()
			ui.overlayText = "▶ 已连接，等待画面..."
			ui.mu.Unlock()
			procPostMessageW.Call(uintptr(ui.hParent), WM_APP_OVERLAY, 0, 0)
		} else if state == ConnConnected && app.overlayQuiet.Load() {
			// 清晰度切换重连：清掉"⟳ 切换清晰度..."提示，画面由旧帧定格过渡到新流
			logf("[overlay] state=Connected (quiet), clearing overlay\n")
			ui.mu.Lock()
			ui.overlayText = ""
			ui.mu.Unlock()
			procPostMessageW.Call(uintptr(ui.hParent), WM_APP_OVERLAY, 0, 0)
		} else {
			text := connStateText(state, retry)
			if text != "" {
				ui.mu.Lock()
				ui.overlayText = text
				ui.mu.Unlock()
				procPostMessageW.Call(uintptr(ui.hParent), WM_APP_OVERLAY, 0, 0)
			}
		}
	}
	app.OnFirstFrame = func() {
		logf("[overlay] OnFirstFrame fired, posting hide\n")
		procPostMessageW.Call(uintptr(ui.hParent), WM_APP_FIRST_FRAME, 0, 0)
	}
	// tebox 方向自适应翻转（竖↔横）：经 PostMessage 转 UI 线程重置一次性初始
	// fit 标记（teboxInitialFitDone 是普通 bool，须在 UI 线程读写），泵下一拍
	// tryRefitOnSourceChange 按新源尺寸/方向 refit 窗口（tebox_orientation.go）。
	app.OnTeboxRefit = func() {
		procPostMessageW.Call(uintptr(ui.hParent), WM_APP_TEBOX_REFIT, 0, 0)
	}
	app.OnClaritySwitched = func(level int) {
		// UI 线程转发（showToast 须在 UI 线程；reconnectRTSP 是 goroutine）
		procPostMessageW.Call(uintptr(ui.hParent), WM_APP_CLARITY_TOAST, uintptr(level), 0)
	}
	app.OnScreenshot = func() {
		procPostMessageW.Call(uintptr(ui.hParent), WM_APP_SCREENSHOT, 0, 0)
	}
}

// wndProc 已拆分到 ui_windows_wndproc.go
// runWebViewMode 已拆分到 ui_windows_webview.go
