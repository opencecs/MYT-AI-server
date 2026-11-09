//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

type paintStruct struct {
	Hdc         uintptr
	Erase       int32
	RcPaint     struct{ Left, Top, Right, Bottom int32 }
	Restored    int32
	IncUpdate   int32
	RgbReserved [32]byte
}

// overlayFixedW is the fixed pill width: the largest text width seen so far in
// this process, never shrinking back. A fixed width removes the two-edge jitter
// when the text switches (connecting -> waiting -> retrying).
// Only one WindowsUI instance exists per process (built once in runWindowsUI),
// so a package-level memo is safe.
// showOverlay/repositionOverlay/overlaySize must be called on the UI thread
// only: overlayFixedW and overlayLastX/LastY are intentionally lock-free.
var overlayFixedW int32

// screenPillMaxW caps the pill width at the primary screen usable width
// (SM_CXFULLSCREEN) minus a safety margin, so an extreme text can never
// overflow the screen.
func screenPillMaxW() int32 {
	r1, _, _ := procGetSystemMetrics.Call(SM_CXFULLSCREEN)
	maxW := int32(r1) - 40
	if maxW < 120 {
		maxW = 120
	}
	return maxW
}

// overlaySize returns the fixed pill size (w, h): width = max text width seen,
// height constant; paintOverlay centers the text with DT_CENTER so any slack
// is invisible.
func (ui *WindowsUI) overlaySize(text string) (int32, int32) {
	const padX int32 = 40 // padding both sides
	const ovlH int32 = 36 // pill height (compact)
	if overlayFixedW == 0 {
		overlayFixedW = 120 // baseline minimum width
	}
	if text != "" && ui.hFontOverlay != 0 {
		hdc, _, _ := procGetDC.Call(0)
		procSelectObject.Call(hdc, ui.hFontOverlay)
		textPtr, _ := syscall.UTF16PtrFromString(text)
		calcRc := struct{ Left, Top, Right, Bottom int32 }{0, 0, 0, 0}
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(textPtr)), ^uintptr(0),
			uintptr(unsafe.Pointer(&calcRc)), DT_CALCRECT|DT_SINGLELINE)
		procReleaseDC.Call(0, hdc)
		textW := calcRc.Right + padX
		if maxW := screenPillMaxW(); textW > maxW {
			textW = maxW // cap: an extreme text must never overflow the screen
		}
		if textW > overlayFixedW {
			overlayFixedW = textW // grow-only: short texts no longer shrink the pill
		}
	}
	return overlayFixedW, ovlH
}

func (ui *WindowsUI) overlayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_ERASEBKGND:
		return 1
	case WM_PAINT:
		ui.paintOverlay(hwnd)
		return 0
	case WM_LBUTTONDOWN:
		if ui.app.kicked.Load() {
			go ui.app.ManualReconnect()
		}
		return 0
	case WM_TIMER:
		if wParam == TIMER_OVERLAY_HIDE {
			if ui.app.Connected.Load() {
				procShowWindow.Call(hwnd, 0)
			}
			procKillTimer.Call(hwnd, TIMER_OVERLAY_HIDE)
		}
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

func (ui *WindowsUI) paintOverlay(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top
	hRgn, _, _ := procCreateRoundRectRgn.Call(
		0, 0, uintptr(w), uintptr(h),
		uintptr(h), uintptr(h))
	procSetWindowRgn.Call(hwnd, hRgn, 1)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), ui.hBrushOverlay)

	ui.mu.Lock()
	text := ui.overlayText
	ui.mu.Unlock()
	if text == "" {
		return
	}
	if ui.hFontOverlay != 0 {
		procSelectObject.Call(hdc, ui.hFontOverlay)
	}
	procSetBkMode.Call(hdc, 1)
	procSetTextColor.Call(hdc, 0x00707070)
	textRc := struct{ Left, Top, Right, Bottom int32 }{rc.Left + 20, rc.Top, rc.Right - 20, rc.Bottom}
	textPtr, _ := syscall.UTF16PtrFromString(text)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(textPtr)), ^uintptr(0),
		uintptr(unsafe.Pointer(&textRc)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func (ui *WindowsUI) showOverlay(text string) {
	if ui.hOverlay == 0 {
		return
	}
	ui.mu.Lock()
	ui.overlayText = text
	ui.mu.Unlock()
	// Empty text -> hide (the old impl showed an empty pill: overlaySize("") gave
	// the minimum size and paintOverlay filled before the text check -- hit by the
	// clarity-switch quiet path).
	if text == "" {
		procShowWindow.Call(uintptr(ui.hOverlay), 0)
		return
	}
	// Fixed pill: size is stable (grow-only fixed width), so re-showing with new
	// text does not move or resize the pill horizontally. Position is re-derived
	// from the video-area center each time -- follow-up while visible is handled
	// by repositionOverlay (WM_MOVE / WM_SIZE / pump), not by text changes.
	ovlW, ovlH := ui.overlaySize(text)
	cx, cy, _ := ui.sdlAreaCenter()
	cx -= ovlW / 2
	cy -= ovlH / 2
	ui.overlayLastX = cx
	ui.overlayLastY = cy
	zOrder := uintptr(HWND_NOTOPMOST)
	if ui.app.Topmost.Load() {
		zOrder = uintptr(HWND_TOPMOST)
	}
	procSetWindowPos.Call(uintptr(ui.hOverlay), zOrder,
		uintptr(cx), uintptr(cy), uintptr(ovlW), uintptr(ovlH), SWP_SHOWWINDOW|SWP_NOACTIVATE)
}

func (ui *WindowsUI) repositionOverlay() {
	// The keymap overlay still follows the window; it runs before the pill guard
	// because a failed keymap window must not take the pill down with it.
	ui.repositionKeymapOverlay()
	if ui.hOverlay == 0 {
		return
	}
	// The pill follows the video-area center on every window move/resize/refit
	// (WM_MOVE / WM_SIZE / WM_EXITSIZEMOVE / pump), so it can never be stranded
	// on the desktop when the parent window is dragged away. Text changes do
	// NOT go through here -- the width is the grow-only overlayFixedW and the
	// position only depends on the window, so the pill is jitter-free while
	// following.
	visRet, _, _ := procIsWindowVisible.Call(uintptr(ui.hOverlay))
	if int32(visRet&0xFFFF) == 0 {
		return
	}
	zOrder := uintptr(HWND_NOTOPMOST)
	if ui.app.Topmost.Load() {
		zOrder = uintptr(HWND_TOPMOST)
	}
	// Current pill size: overlaySize("") returns the memoized fixed width
	// without measuring (and height is constant), so the follow path never
	// resizes the pill.
	ovlW, ovlH := ui.overlaySize("")
	cx, cy, _ := ui.sdlAreaCenter()
	x := cx - ovlW/2
	y := cy - ovlH/2
	if x != ui.overlayLastX || y != ui.overlayLastY {
		ui.overlayLastX = x
		ui.overlayLastY = y
		procSetWindowPos.Call(uintptr(ui.hOverlay), zOrder,
			uintptr(x), uintptr(y), 0, 0, SWP_NOSIZE|SWP_NOACTIVATE)
		return
	}
	// Position unchanged: only refresh z-order in case Topmost toggled while
	// visible -- no move/size, no flicker.
	procSetWindowPos.Call(uintptr(ui.hOverlay), zOrder,
		0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
}

func (ui *WindowsUI) toastWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_ERASEBKGND:
		return 1
	case WM_PAINT:
		ui.paintToast(hwnd)
		return 0
	case WM_LBUTTONDOWN:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		ui.mu.Lock()
		btnClick := ui.toastBtnClick
		ui.mu.Unlock()
		if btnClick != nil && x >= 270 && y >= 60 {
			btnClick()
		}
		return 0
	case WM_TIMER:
		if wParam == TIMER_TOAST_HIDE {
			procShowWindow.Call(hwnd, 0)
			procKillTimer.Call(hwnd, TIMER_TOAST_HIDE)
		}
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

func (ui *WindowsUI) paintToast(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top

	// 深色卡片(与键位标记/侧边栏 accent 同套配色):深底白字,任何视频画面上都清晰
	const (
		toastBg     = 0x2E2323   // #23232E
		toastBorder = 0x463A3A   // #3A3A46
		toastTitle  = 0x00FAF5F5 // #F5F5FA 白
		toastSub    = 0x00C4B8B8 // #B8B8C4 浅灰
		toastAccent = 0x00D9904A // #4A90D9 蓝
	)

	hRgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(w), uintptr(h), 14, 14)
	procSetWindowRgn.Call(hwnd, hRgn, 1)
	bgBrush, _, _ := procCreateSolidBrush.Call(toastBg)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bgBrush)
	procDeleteObject.Call(bgBrush)

	// 左侧强调条
	accentBrush, _, _ := procCreateSolidBrush.Call(toastAccent)
	accentRect := struct{ Left, Top, Right, Bottom int32 }{0, 0, 4, h}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&accentRect)), accentBrush)
	procDeleteObject.Call(accentBrush)

	// 细边框提层次
	borderPen, _, _ := procCreatePen.Call(0, 1, toastBorder)
	oldPen, _, _ := procSelectObject.Call(hdc, borderPen)
	procSelectObject.Call(hdc, uintptr(5)) // NULL_BRUSH
	procRoundRect.Call(hdc, 0, 0, uintptr(w), uintptr(h), 14, 14)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(borderPen)

	ui.mu.Lock()
	line1, line2, btnText := ui.toastLine1, ui.toastLine2, ui.toastBtnText
	ui.mu.Unlock()

	procSetBkMode.Call(hdc, 1)
	procSelectObject.Call(hdc, ui.hFontToast)
	procSetTextColor.Call(hdc, toastTitle)
	if line1 != "" {
		line1Rect := struct{ Left, Top, Right, Bottom int32 }{16, 10, w - 16, 38}
		ptr, _ := syscall.UTF16PtrFromString(line1)
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(ptr)), ^uintptr(0),
			uintptr(unsafe.Pointer(&line1Rect)), DT_LEFT|DT_SINGLELINE)
	}

	procSelectObject.Call(hdc, ui.hFontUpload14)
	procSetTextColor.Call(hdc, toastSub)
	if line2 != "" {
		line2Rect := struct{ Left, Top, Right, Bottom int32 }{16, 36, w - 16, 58}
		ptr, _ := syscall.UTF16PtrFromString(line2)
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(ptr)), ^uintptr(0),
			uintptr(unsafe.Pointer(&line2Rect)), DT_LEFT|DT_END_ELLIPSIS|DT_SINGLELINE)
	}

	if btnText != "" {
		btnW, btnH := int32(88), int32(30)
		btnX := w - btnW - 14
		btnY := h - btnH - 12
		btnRgn, _, _ := procCreateRoundRectRgn.Call(uintptr(btnX), uintptr(btnY), uintptr(btnX+btnW), uintptr(btnY+btnH), 6, 6)
		procSelectClipRgn.Call(hdc, btnRgn)
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&struct{ Left, Top, Right, Bottom int32 }{btnX, btnY, btnX + btnW, btnY + btnH})), ui.hBrushBarFill)
		procSelectClipRgn.Call(hdc, 0)
		procDeleteObject.Call(btnRgn)
		procSelectObject.Call(hdc, ui.hFontToastBtn)
		procSetTextColor.Call(hdc, 0x00FFFFFF)
		btnTextRect := struct{ Left, Top, Right, Bottom int32 }{btnX, btnY, btnX + btnW, btnY + btnH}
		ptr, _ := syscall.UTF16PtrFromString(btnText)
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(ptr)), ^uintptr(0),
			uintptr(unsafe.Pointer(&btnTextRect)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

// clarityLevelNames 档位名（与侧栏 side_body.html 的 data-clarity 标签一致）。
// 码率为 202 hwcodec 实测档位表（进程内存逆向 + 设备日志双验证）。
var clarityLevelNames = map[int]struct{ name, desc string }{
	1: {"高清", "1.5 Mbps"},
	2: {"标清", "1.0 Mbps"},
	3: {"流畅", "0.8 Mbps"},
	4: {"原画", "0.9 Mbps"},
}

// showClarityToast 清晰度切换成功提示（UI 线程，经 WM_APP_CLARITY_TOAST 转发）。
// 档位只改码率不改分辨率，画面无视觉信号——toast 是用户唯一的切换确认。
func (ui *WindowsUI) showClarityToast(level int) {
	info, ok := clarityLevelNames[level]
	if !ok {
		return
	}
	ui.showToast("清晰度已切换", info.name+" · "+info.desc, "", nil)
}

func (ui *WindowsUI) showToast(line1, line2, btnText string, btnClick func()) {
	if ui.hToast == 0 {
		return
	}
	ui.mu.Lock()
	ui.toastLine1, ui.toastLine2, ui.toastBtnText = line1, line2, btnText
	ui.toastBtnClick = btnClick
	ui.mu.Unlock()
	cx, cy, areaH := ui.sdlAreaCenter()
	toastX := cx - 190
	toastY := cy + areaH/2 - 100
	procKillTimer.Call(uintptr(ui.hToast), TIMER_TOAST_HIDE)
	procSetWindowPos.Call(uintptr(ui.hToast), HWND_TOP, uintptr(toastX), uintptr(toastY), 380, 110, SWP_SHOWWINDOW|SWP_NOACTIVATE)
	procSetTimer.Call(uintptr(ui.hToast), TIMER_TOAST_HIDE, 4000, 0)
}

func (ui *WindowsUI) repositionToast() {
	if ui.hToast == 0 {
		return
	}
	cx, cy, areaH := ui.sdlAreaCenter()
	toastX := cx - 190
	toastY := cy + areaH/2 - 100
	procSetWindowPos.Call(uintptr(ui.hToast), HWND_TOP, uintptr(toastX), uintptr(toastY), 0, 0, SWP_NOSIZE|SWP_NOACTIVATE)
}
