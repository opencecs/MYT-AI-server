//go:build windows

package main

import (
	"runtime"

	"golang.org/x/sys/windows"
)

func init() {
	runtime.LockOSThread()

	// 提升系统定时器精度到 1ms（默认 ~15.6ms），让 WM_TIMER 更精确
	winmm := windows.NewLazyDLL("winmm.dll")
	procTimeBeginPeriod := winmm.NewProc("timeBeginPeriod")
	procTimeBeginPeriod.Call(1)

	// Enable per-monitor DPI awareness for crisp rendering on HiDPI
	shcore := windows.NewLazyDLL("shcore.dll")
	procSetProcessDpiAwareness := shcore.NewProc("SetProcessDpiAwareness")
	procSetProcessDpiAwareness.Call(2) // PROCESS_PER_MONITOR_DPI_AWARE

	_newPlatformUIImpl = func(app *App) PlatformUI {
		return NewWindowsUI(app)
	}
}

// ─── Win32 API ────────────────────────────────────────────────────────────────

var (
	user32   = windows.NewLazyDLL("user32.dll")
	kernel32 = windows.NewLazyDLL("kernel32.dll")
	gdi32    = windows.NewLazyDLL("gdi32.dll")
	shell32  = windows.NewLazyDLL("shell32.dll")
	dwmapi   = windows.NewLazyDLL("dwmapi.dll")

	// Store callback pointers to prevent GC from collecting them
	// (windows.NewCallback returns uintptr to Go closures that must stay reachable)
	wndProcCB        uintptr
	sdlAreaProcCB    uintptr
	navWndProcCB     uintptr
	sideWndProcCB    uintptr
	overlayWndProcCB uintptr
	toastWndProcCB   uintptr

	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procMoveWindow                 = user32.NewProc("MoveWindow")
	procGetClientRect              = user32.NewProc("GetClientRect")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procSetWindowRgn               = user32.NewProc("SetWindowRgn")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procSetFocus                   = user32.NewProc("SetFocus")
	procGetWindowLongW             = user32.NewProc("GetWindowLongW")
	procSetWindowLongW             = user32.NewProc("SetWindowLongW")
	procIsZoomed                   = user32.NewProc("IsZoomed")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procMonitorFromWindow          = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW            = user32.NewProc("GetMonitorInfoW")
	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	procLoadCursorW                = user32.NewProc("LoadCursorW")
	procSetCursor                  = user32.NewProc("SetCursor")
	procLoadImageW                 = user32.NewProc("LoadImageW")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
	procSetConsoleOutputCP         = kernel32.NewProc("SetConsoleOutputCP")
	procSetConsoleCP               = kernel32.NewProc("SetConsoleCP")
	procCreateFontW                = gdi32.NewProc("CreateFontW")
	procFillRect                   = user32.NewProc("FillRect")
	procDrawTextW                  = user32.NewProc("DrawTextW")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procValidateRect               = user32.NewProc("ValidateRect")
	procUpdateWindow               = user32.NewProc("UpdateWindow")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procEnumWindows                = user32.NewProc("EnumWindows")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
	procSendMessageW               = user32.NewProc("SendMessageW")
	procSystemParametersInfoW      = user32.NewProc("SystemParametersInfoW")
	procGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	procIsWindow                   = user32.NewProc("IsWindow")
	procGetKeyState                = user32.NewProc("GetKeyState")
	procGetFocus                   = user32.NewProc("GetFocus")
	procPostMessageW               = user32.NewProc("PostMessageW")
	procScreenToClient             = user32.NewProc("ScreenToClient")
	procTrackMouseEvent            = user32.NewProc("TrackMouseEvent")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procCreateSolidBrush           = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject               = gdi32.NewProc("DeleteObject")
	procCreateRoundRectRgn         = gdi32.NewProc("CreateRoundRectRgn")
	procSelectClipRgn              = gdi32.NewProc("SelectClipRgn")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procClientToScreen             = user32.NewProc("ClientToScreen")
	procSetBkMode                  = gdi32.NewProc("SetBkMode")
	procSetTextColor               = gdi32.NewProc("SetTextColor")
	procSelectObject               = gdi32.NewProc("SelectObject")
	procPolygon                    = gdi32.NewProc("Polygon")

	// GDI rendering procs (used by render.go)
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procStretchBlt         = gdi32.NewProc("StretchBlt")
	procSetStretchBltMode  = gdi32.NewProc("SetStretchBltMode")
	procSetBrushOrgEx      = gdi32.NewProc("SetBrushOrgEx")
	procEllipse            = gdi32.NewProc("Ellipse")
	procRoundRect          = gdi32.NewProc("RoundRect")
	procCreatePen          = gdi32.NewProc("CreatePen")
	procMoveToEx           = gdi32.NewProc("MoveToEx")
	procLineTo             = gdi32.NewProc("LineTo")
	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procAdjustWindowRect   = user32.NewProc("AdjustWindowRect")
	procGetStockObject     = gdi32.NewProc("GetStockObject")
	procSetCapture         = user32.NewProc("SetCapture")
	procReleaseCapture     = user32.NewProc("ReleaseCapture")

	// Drag-and-drop
	procDragAcceptFiles = shell32.NewProc("DragAcceptFiles")
	procDragQueryFileW  = shell32.NewProc("DragQueryFileW")
	procDragFinish      = shell32.NewProc("DragFinish")
	procShellExecuteW   = shell32.NewProc("ShellExecuteW")

	// 低级鼠标钩子(键位编辑态滚轮转发用)
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx     = user32.NewProc("CallNextHookEx")
	procWindowFromPoint    = user32.NewProc("WindowFromPoint")

	// DWM (Desktop Window Manager)
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

// ─── Win32 constants ─────────────────────────────────────────────────────────

const (
	// Window styles
	WS_OVERLAPPED       = 0x00000000
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_THICKFRAME       = 0x00040000
	WS_MINIMIZEBOX      = 0x00020000
	WS_MAXIMIZEBOX      = 0x00010000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000
	WS_POPUP            = 0x80000000

	// 负值 nIndex 必须用符号扩展形态（见 GWL_EXSTYLE 处注释；-16 零扩展实测
	// 恰好可用但无文档保证，统一符号扩展防踩同坑）
	GWL_STYLE                = ^uintptr(15) // -16，符号扩展
	MONITOR_DEFAULTTOPRIMARY = 1

	// Window messages
	WM_CLOSE       = 0x0010
	WM_DESTROY     = 0x0002
	WM_SIZE        = 0x0005
	WM_MOVE        = 0x0003
	WM_ACTIVATE    = 0x0006
	WM_DPICHANGED  = 0x02E0
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	WM_CHAR        = 0x0102
	WM_TIMER       = 0x0113
	WM_COMMAND     = 0x0111
	WM_PAINT       = 0x000F
	WM_CTLCOLORBTN = 0x0135
	WM_ERASEBKGND  = 0x0014
	WM_USER        = 0x0400
	WM_COPYDATA    = 0x004A

	// Mouse messages
	WM_LBUTTONDOWN   = 0x0201
	WM_LBUTTONUP     = 0x0202
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONDOWN   = 0x0204
	WM_RBUTTONUP     = 0x0205
	WM_MBUTTONDOWN   = 0x0207
	WM_MBUTTONUP     = 0x0208
	WM_PARENTNOTIFY  = 0x0210
	WM_MOUSEMOVE     = 0x0200
	WM_MOUSELEAVE    = 0x02A3
	WM_MOUSEWHEEL    = 0x020A
	WM_SETCURSOR     = 0x0020
	WM_CONTEXTMENU   = 0x007B
	WM_NCHITTEST     = 0x0084
	WM_SYSCOMMAND    = 0x0112
	WM_NCPAINT       = 0x0085
	WM_NCACTIVATE    = 0x0086
	WM_SIZING        = 0x0214
	WM_EXITSIZEMOVE  = 0x0232
	WM_GETMINMAXINFO = 0x0024

	// WM_SIZING edge flags
	WMSZ_LEFT        = 1
	WMSZ_RIGHT       = 2
	WMSZ_TOP         = 3
	WMSZ_TOPLEFT     = 4
	WMSZ_TOPRIGHT    = 5
	WMSZ_BOTTOM      = 6
	WMSZ_BOTTOMLEFT  = 7
	WMSZ_BOTTOMRIGHT = 8

	// Hit-test values for WM_NCHITTEST
	HTCLIENT      = 1
	HTCAPTION     = 2
	HTTOP         = 12
	HTBOTTOM      = 15
	HTLEFT        = 10
	HTRIGHT       = 11
	HTTOPLEFT     = 13
	HTTOPRIGHT    = 14
	HTBOTTOMLEFT  = 16
	HTBOTTOMRIGHT = 17
	HTMINBUTTON   = 8
	HTMAXBUTTON   = 9
	HTCLOSE       = 20

	// Show window / position flags
	SW_SHOW          = 5
	SW_SHOWNORMAL    = 1
	CW_USEDEFAULT    = 0x80000000
	SWP_NOZORDER     = 0x0004
	SWP_NOMOVE       = 0x0002
	SWP_NOSIZE       = 0x0001
	SWP_SHOWWINDOW   = 0x0040
	SWP_NOCOPYBITS   = 0x0100
	SWP_NOACTIVATE   = 0x0010
	SWP_FRAMECHANGED = 0x0020

	HWND_TOPMOST   = ^uintptr(0)
	HWND_NOTOPMOST = ^uintptr(1)
	HWND_TOP       = 0

	// System
	IDC_ARROW       = 32512
	IMAGE_ICON      = 1
	LR_LOADFROMFILE = 0x00000010
	LR_DEFAULTSIZE  = 0x00000040
	COLOR_BTNFACE   = 15
	COLOR_WINDOW    = 5
	COLOR_HIGHLIGHT = 13
	SM_CXSCREEN     = 0
	SM_CYSCREEN     = 1
	SM_CXFULLSCREEN = 17
	SPI_GETWORKAREA = 0x0030

	// 可缩放边框厚度（DPI 感知），用于无边框窗口的边缘 hit-test 判定
	SM_CXSIZEFRAME  = 32
	SM_CYSIZEFRAME  = 33
	SM_CXFIXEDFRAME = 7
	SM_CYFIXEDFRAME = 8

	// Clipboard
	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002

	// Drawing
	DT_CENTER       = 0x00000001
	DT_LEFT         = 0x00000000
	DT_END_ELLIPSIS = 0x00008000
	DT_VCENTER      = 0x00000004
	DT_SINGLELINE   = 0x00000020
	DT_CALCRECT     = 0x00000400
	BN_CLICKED      = 0

	// Timers（TIMER_UPLOAD_HIDE=3 已随上传 overlay 移除，编号不复用）
	TIMER_SDL_PUMP     = 1
	TIMER_OVERLAY_HIDE = 2
	TIMER_INIT_LAYOUT  = 4
	TIMER_TOAST_HIDE   = 5
	TIMER_CONN_TIMEOUT = 6
	TIMER_LAYOUT_RELAY = 7
	TIMER_KM_EVAL      = 8 // 键位面板状态防抖推送(滚轮缩放高频路径,见 kmOnWheel)

	// Navbar button IDs
	// Traffic light button hit-test IDs
	HT_TL_CLOSE    = 100
	HT_TL_MAXIMIZE = 101
	HT_TL_MINIMIZE = 102

	IDC_NAV_BACK   = 1001
	IDC_NAV_HOME   = 1002
	IDC_NAV_RECENT = 1003

	// Custom messages
	WM_APP_SYNC_STATE      = WM_USER + 1
	WM_APP_OVERLAY         = WM_USER + 2
	WM_APP_IPLOC           = WM_USER + 3
	WM_APP_UPLOAD          = WM_USER + 4
	WM_APP_SMS_RESULT      = WM_USER + 6
	WM_APP_CONTACT_RESULT  = WM_USER + 7
	WM_APP_TIMEOUT         = WM_USER + 8
	WM_APP_SCREENSHOT      = WM_USER + 9
	WM_APP_FIRST_FRAME     = WM_USER + 11
	WM_APP_D3D11_REBUILD   = WM_USER + 12
	WM_APP_MATRIX_EVAL     = WM_USER + 13
	WM_APP_PUBFIT          = WM_USER + 14
	WM_APP_HIDE_OVERLAY    = WM_USER + 16 // 首帧后延迟隐藏 overlay（遮住过渡期黑帧）
	WM_APP_RENDER          = WM_USER + 15 // 新帧就绪，UI 线程立即 Present（帧到即渲染，消除 WM_TIMER 抖动）
	WM_APP_CLOSE_RENDERER  = WM_USER + 18 // UI 线程关闭渲染器（避免跨线程 D3D11R_Destroy 竞态）
	WM_APP_UPLOAD_SIDEBAR  = WM_USER + 19 // 上传状态/进度同步侧边栏（uploadJS，高频覆盖合并）
	WM_APP_CLARITY_TOAST   = WM_USER + 20 // 清晰度切换成功 toast（wParam=档位）
	WM_APP_SIDEBAR_INIT    = WM_USER + 21 // 首帧上屏后初始化侧边栏 WebView2（见 ui_windows_sidebar.go）
	WM_APP_TEBOX_REFIT     = WM_USER + 22 // tebox 方向翻转（竖↔横）后重置一次性初始 fit（tebox_orientation.go）
	// 注:WM_USER+17 已被 keymap_overlay_windows.go 的 WM_APP_KM_TOGGLE 占用
	// (+16 曾与本常量撞号,恰好后者未被引用才未炸)——新增消息从这里顺延,勿复用。
	// （旧的 WM_USER+5 = WM_APP_UPLOAD_PROGRESS 已随 progressReader 一起移除）

	// Drag-and-drop
	WM_DROPFILES = 0x0233

	// Layered window
	WS_EX_LAYERED    = 0x00080000
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000
	WS_EX_TRANSPARENT = 0x00000020 // 点击穿透(LWA_COLORKEY 只做视觉透明,不穿透点击)
	LWA_ALPHA        = 0x02
	LWA_COLORKEY     = 0x01
	NULL_BRUSH       = 5 // GetStockObject: 空刷(仅描边)

	// GWL_EXSTYLE 必须是 -20 的**符号扩展**形态（^uintptr(19) = 0xFFFFFFFFFFFFFFF4）。
	// 64 位 Windows 的 Get/SetWindowLongW 读的是 nIndex 完整 64 位寄存器值：
	// 零扩展形态 uintptr(0xFFFFFFF4)（旧定义）会令 nIndex≠-20 → Get 恒返回 0、
	// Set 报 Invalid menu handle——键位覆盖层的穿透/隐藏静默失效（吞鼠标），
	// 2026-09-08 最小复现实测；同文件 GWL_STYLE 的调用方用 uintptr(int 常量)
	// （符号扩展）所以一直正常。
	GWL_EXSTYLE = ^uintptr(19) // -20，符号扩展

	legacyClassName = "LgPlayerLegacy"
	matrixClassName = "LgPlayerMatrix"
)
