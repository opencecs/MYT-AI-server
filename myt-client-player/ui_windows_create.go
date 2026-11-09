//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// --- 窗口类注册、窗口创建、GDI 缓存 ---

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

func registerClass(name string, wndProc uintptr, hInst, hCursor, hBg uintptr, style uint32, hIcon uintptr) {
	namePtr, _ := windows.UTF16PtrFromString(name)
	wc := wndClassExW{
		Size:       uint32(unsafe.Sizeof(wndClassExW{})),
		Style:      style,
		WndProc:    wndProc,
		Instance:   windows.Handle(hInst),
		Icon:       windows.Handle(hIcon),
		Cursor:     windows.Handle(hCursor),
		Background: windows.Handle(hBg),
		ClassName:  namePtr,
		IconSm:     windows.Handle(hIcon),
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
}

func createWnd(exStyle uintptr, className string, style, x, y, w, h, parent, hInst uintptr) uintptr {
	cls, _ := windows.UTF16PtrFromString(className)
	hw, _, _ := procCreateWindowExW.Call(exStyle,
		uintptr(unsafe.Pointer(cls)), 0, style,
		x, y, w, h, parent, 0, hInst, 0)
	return hw
}

func (ui *WindowsUI) createWindows() {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	hCursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	var hIcon uintptr
	iconName, _ := windows.UTF16PtrFromString("APP")
	if h, _, _ := procLoadImageW.Call(hInst, uintptr(unsafe.Pointer(iconName)),
		IMAGE_ICON, 0, 0, LR_DEFAULTSIZE); h != 0 {
		hIcon = h
	}
	darkBrush, _, _ := procCreateSolidBrush.Call(0x00F0F0F0)
	ui.hBrushBlack, _, _ = procCreateSolidBrush.Call(0x00000000)

	// 矩阵投屏窗口用独立类名，ArrangePlayerWindows 只排列矩阵窗口，不影响普通投屏
	clsName := legacyClassName
	if ui.app.Controlled {
		clsName = matrixClassName
	}
	registerClass(clsName, func() uintptr {
		wndProcCB = windows.NewCallback(ui.wndProc)
		return wndProcCB
	}(), hInst, hCursor, darkBrush, 0, hIcon)

	titlePtr, _ := windows.UTF16PtrFromString(*flagTitle)
	var winRect struct{ Left, Top, Right, Bottom int32 }
	winRect.Right = ui.baseWinW
	winRect.Bottom = ui.baseWinH
	dwStyle := uintptr(WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN)
	if ui.app.Controlled {
		// 批量投屏：保留标题栏，允许缩放和最大化
		dwStyle = uintptr(WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_CLIPCHILDREN | WS_MINIMIZEBOX | WS_THICKFRAME | WS_MAXIMIZEBOX)
	}
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&winRect)), dwStyle, 0, 0)
	winW := winRect.Right - winRect.Left
	winH := winRect.Bottom - winRect.Top
	screenW, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
	screenH, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
	// 批量投屏：主程序预计算后传入 -winx/-winy/-winw/-winh，窗口一创建就在网格位置，避免堆叠重排。
	// 默认 0=居中（兼容单台投屏）。尺寸为 0 时用 baseWinW/H 默认值。
	if *flagWinW > 0 {
		winW = int32(*flagWinW)
	}
	if *flagWinH > 0 {
		winH = int32(*flagWinH)
	}
	winX := (int32(screenW) - winW) / 2
	winY := (int32(screenH) - winH) / 2
	// 批量投屏：主程序预计算后传入 -winx/-winy/-winw/-winh，窗口一创建就在网格位置，避免堆叠后重排。
	// 哨兵 -32000 表示未传（居中）。0 是合法位置（第一列/第一行），必须用哨兵判断。
	const winPosUnset = -32000
	if *flagWinX != winPosUnset {
		winX = int32(*flagWinX)
	}
	if *flagWinY != winPosUnset {
		winY = int32(*flagWinY)
	}
	if winX < 0 { winX = 0 }
	if winY < 0 { winY = 0 }
	clsNamePtr, _ := windows.UTF16PtrFromString(clsName)
	h, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(clsNamePtr)), uintptr(unsafe.Pointer(titlePtr)),
		dwStyle,
		uintptr(winX), uintptr(winY), uintptr(winW), uintptr(winH),
		0, 0, hInst, 0)
	ui.hParent = windows.HWND(h)

	// 批量投屏传了 -winw/-winh 时，用实际窗口尺寸同步 baseWinW/H（客户区=整窗-边框）。
	// 否则 baseWinW 仍是 464×scale 默认值，与主程序传的窗口尺寸不一致，导致 SDL 区/侧边栏
	// 布局错位、overlay 位置偏移。同步后内部布局与窗口尺寸一致。
	if *flagWinW > 0 || *flagWinH > 0 {
		var cc struct{ Left, Top, Right, Bottom int32 }
		procGetClientRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&cc)))
		if *flagWinW > 0 {
			ui.baseWinW = cc.Right - cc.Left
		}
		if *flagWinH > 0 {
			ui.baseWinH = cc.Bottom - cc.Top
		}
	}

	leftW := ui.baseWinW - ui.sideCollapsedW
	sdlH := ui.baseWinH - ui.navBarHeight

	registerClass("LgPlayerLegacySDL", func() uintptr {
		sdlAreaProcCB = windows.NewCallback(ui.sdlAreaWndProc)
		return sdlAreaProcCB
	}(), hInst, hCursor, darkBrush, 0x0008, 0)
	sh := createWnd(0, "LgPlayerLegacySDL",
		WS_CHILD|WS_VISIBLE|WS_CLIPSIBLINGS,
		0, 0, uintptr(leftW), uintptr(sdlH), uintptr(ui.hParent), hInst)
	ui.hSdlArea = windows.HWND(sh)

	registerClass("LgPlayerLegacyNav", func() uintptr {
		navWndProcCB = windows.NewCallback(ui.navWndProc)
		return navWndProcCB
	}(), hInst, hCursor, darkBrush, 0, 0)
	nh := createWnd(0, "LgPlayerLegacyNav",
		WS_CHILD|WS_VISIBLE|WS_CLIPSIBLINGS,
		0, uintptr(sdlH), uintptr(leftW), uintptr(ui.navBarHeight), uintptr(ui.hParent), hInst)
	ui.hNavBar = windows.HWND(nh)

	registerClass("LgPlayerLegacySide", func() uintptr {
		sideWndProcCB = windows.NewCallback(ui.sideWndProc)
		return sideWndProcCB
	}(), hInst, hCursor, darkBrush, 0, 0)
	if !ui.app.NoSidebar {
		sph := createWnd(0, "LgPlayerLegacySide",
			WS_CHILD|WS_VISIBLE|WS_CLIPSIBLINGS,
			uintptr(leftW), 0, uintptr(ui.sideCollapsedW), uintptr(ui.baseWinH), uintptr(ui.hParent), hInst)
		ui.hSidePanel = windows.HWND(sph)
	}

	registerClass("LgPlayerLegacyOverlay", func() uintptr {
		overlayWndProcCB = windows.NewCallback(ui.overlayWndProc)
		return overlayWndProcCB
	}(), hInst, hCursor, 0, 0, 0)
	oh, _, _ := procCreateWindowExW.Call(
		WS_EX_TOOLWINDOW|WS_EX_LAYERED,
		uintptr(unsafe.Pointer(func() *uint16 {
			p, _ := windows.UTF16PtrFromString("LgPlayerLegacyOverlay"); return p
		}())), 0, WS_POPUP, 0, 0, 400, 80, uintptr(ui.hParent), 0, hInst, 0)
	ui.hOverlay = windows.HWND(oh)
	procSetLayeredWindowAttributes.Call(oh, 0, 210, LWA_ALPHA)

	registerClass("LgPlayerToast", func() uintptr {
		toastWndProcCB = windows.NewCallback(ui.toastWndProc)
		return toastWndProcCB
	}(), hInst, hCursor, 0, 0, 0)
	th, _, _ := procCreateWindowExW.Call(
		WS_EX_TOOLWINDOW|WS_EX_LAYERED,
		uintptr(unsafe.Pointer(func() *uint16 {
			p, _ := windows.UTF16PtrFromString("LgPlayerToast"); return p
		}())), 0, WS_POPUP, 0, 0, 380, 110, uintptr(ui.hParent), 0, hInst, 0)
	ui.hToast = windows.HWND(th)
	procSetLayeredWindowAttributes.Call(th, 0, 255, LWA_ALPHA)

	// 键位映射编辑覆盖层(色键透明,默认隐藏)
	ui.createKeymapOverlay(hInst, hCursor)

	// F9 切换回调:经 PostMessage 转 UI 线程弹 toast
	ui.app.Keymap.OnToggle = func(enabled bool) {
		w := uintptr(0)
		if enabled {
			w = 1
		}
		procPostMessageW.Call(uintptr(ui.hParent), WM_APP_KM_TOGGLE, w, 0)
	}

	procDragAcceptFiles.Call(uintptr(ui.hParent), 1)
}

func (ui *WindowsUI) initGDICache() {
	fontName, _ := syscall.UTF16PtrFromString("Microsoft YaHei")
	ui.hFontOverlay, _, _ = procCreateFontW.Call(20, 0, 0, 0, 400, 0, 0, 0,
		1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	ui.hFontUpload14, _, _ = procCreateFontW.Call(18, 0, 0, 0, 400, 0, 0, 0,
		1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	ui.hFontUpload11, _, _ = procCreateFontW.Call(14, 0, 0, 0, 400, 0, 0, 0,
		1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	ui.hFontToast, _, _ = procCreateFontW.Call(24, 0, 0, 0, 400, 0, 0, 0,
		1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	ui.hFontToastBtn, _, _ = procCreateFontW.Call(16, 0, 0, 0, 400, 0, 0, 0,
		1, 0, 0, 0, 0, uintptr(unsafe.Pointer(fontName)))
	ui.hBrushLight, _, _ = procCreateSolidBrush.Call(0x00F0F0F0)
	ui.hBrushOverlay, _, _ = procCreateSolidBrush.Call(0x00FFFFFF)
	ui.hBrushBarBg, _, _ = procCreateSolidBrush.Call(0x00E0E0E0)
	ui.hBrushBarFill, _, _ = procCreateSolidBrush.Call(0x00D9904A)
	ui.hBrushPill, _, _ = procCreateSolidBrush.Call(0x00E0E0E0)
	ui.hBrushPillAct, _, _ = procCreateSolidBrush.Call(0x00D0D0D0)
	ui.hBrushIcon, _, _ = procCreateSolidBrush.Call(0x00707070)
	ui.hBrushIconHov, _, _ = procCreateSolidBrush.Call(0x00404040)
	ui.hPenIcon, _, _ = procCreatePen.Call(0, 3, 0x00707070)
	ui.hPenIconHov, _, _ = procCreatePen.Call(0, 3, 0x00404040)
	ui.hPenGray, _, _ = procCreatePen.Call(0, 2, 0x00808080)
	ui.hPenSep, _, _ = procCreatePen.Call(0, 1, 0x00404040)
}

func (ui *WindowsUI) sideWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if uint32(msg) == WM_SIZE {
		if ui.chromium != nil {
			ui.chromium.Resize()
		}
		return 0
	}
	// 主控窗口侧边栏区域允许拖拽窗口（通过 HTCAPTION 传递给父窗口）
	if uint32(msg) == WM_NCHITTEST && ui.app.Controlled && len(ui.app.CtrlItems) > 0 {
		return HTCAPTION
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}
