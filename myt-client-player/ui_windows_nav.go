//go:build windows

package main

import (
	"unsafe"
)

// navPillLayout computes button geometry for the navigation bar.
func navPillLayout(w, h int32) (pillW, pillH, pillY, gap, startX, iconSz, pillRad int32) {
	pillW = int32(float32(h) * 1.6)
	if pillW > w/3 {
		pillW = w / 3
	}
	pillH = int32(float32(h) * 0.72)
	pillY = (h - pillH) / 2
	gap = int32(float32(h) * 0.7)
	totalW := pillW*3 + gap*2
	startX = (w - totalW) / 2
	iconSz = pillH / 3
	if iconSz < 6 {
		iconSz = 6
	}
	pillRad = pillH / 3
	if pillRad > 14 {
		pillRad = 14
	}
	return
}

// navHitTest returns the button index (0-2) at coordinates (x,y), or -1.
func (ui *WindowsUI) navHitTest(x, y, w, h int32) int {
	pillW, pillH, pillY, gap, startX, _, _ := navPillLayout(w, h)
	for i := 0; i < 3; i++ {
		px := startX + int32(i)*(pillW+gap)
		if x >= px && x < px+pillW && y >= pillY && y < pillY+pillH {
			return i
		}
	}
	return -1
}

// navEnsureBuffer creates or resizes the offscreen DC/bitmap for double-buffered painting.
func (ui *WindowsUI) navEnsureBuffer(hdc uintptr, w, h int32) uintptr {
	if ui.navMemDC != 0 && ui.navBufW == w && ui.navBufH == h {
		return ui.navMemDC
	}
	if ui.navMemDC != 0 {
		procSelectObject.Call(ui.navMemDC, ui.navOldBmp)
		procDeleteObject.Call(ui.navMemBmp)
		procDeleteDC.Call(ui.navMemDC)
	}
	ui.navMemDC, _, _ = procCreateCompatibleDC.Call(hdc)
	bmi := struct {
		Size uint32; Width int32; Height int32; Planes uint16; BPP uint16
		Compression uint32; SizeImage uint32; PPI_X int32; PPI_Y int32
		ColorsUsed uint32; ColorsImp uint32
	}{
		Size: 40, Width: w, Height: -h, Planes: 1, BPP: 32,
	}
	ui.navMemBmp, _, _ = procCreateDIBSection.Call(ui.navMemDC, uintptr(unsafe.Pointer(&bmi)), 0, 0, 0, 0)
	ui.navOldBmp, _, _ = procSelectObject.Call(ui.navMemDC, ui.navMemBmp)
	ui.navBufW, ui.navBufH = w, h
	return ui.navMemDC
}

func (ui *WindowsUI) navWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_ERASEBKGND:
		return 1
	case WM_PAINT:
		var ps struct {
			Hdc       uintptr
			Erase     int32
			RcPaint   struct{ Left, Top, Right, Bottom int32 }
			Restored  int32
			IncUpdate int32
			RgbReserved [32]byte
		}
		procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		hdc := ps.Hdc
		var rc struct{ Left, Top, Right, Bottom int32 }
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		w := rc.Right - rc.Left
		h := rc.Bottom - rc.Top
		memDC := ui.navEnsureBuffer(hdc, w, h)
		procFillRect.Call(memDC, uintptr(unsafe.Pointer(&rc)), ui.hBrushLight)
		pillW, pillH, pillY, gap, startX, iconSz, pillRad := navPillLayout(w, h)
		pressedBtn := ui.navPressedBtn
		for i := 0; i < 3; i++ {
			px := startX + int32(i)*(pillW+gap)
			cx := px + pillW/2
			cy := h / 2
			if int32(i) == pressedBtn {
				oldBrush, _, _ := procSelectObject.Call(memDC, ui.hBrushPillAct)
				procSelectObject.Call(memDC, uintptr(5))
				procRoundRect.Call(memDC,
					uintptr(px), uintptr(pillY),
					uintptr(px+pillW), uintptr(pillY+pillH),
					uintptr(pillRad), uintptr(pillRad))
				procSelectObject.Call(memDC, oldBrush)
			} else if int32(i) == ui.navHoverBtn {
				oldBrush, _, _ := procSelectObject.Call(memDC, ui.hBrushPill)
				procSelectObject.Call(memDC, uintptr(5))
				procRoundRect.Call(memDC,
					uintptr(px), uintptr(pillY),
					uintptr(px+pillW), uintptr(pillY+pillH),
					uintptr(pillRad), uintptr(pillRad))
				procSelectObject.Call(memDC, oldBrush)
			}
			isActive := int32(i) == ui.navHoverBtn || int32(i) == pressedBtn
			iconBrush := ui.hBrushIcon
			iconPen := ui.hPenIcon
			if isActive {
				iconBrush = ui.hBrushIconHov
				iconPen = ui.hPenIconHov
			}
			half := iconSz
			switch i {
			case 0: // Back: ◀ filled triangle
				procSelectObject.Call(memDC, iconBrush)
				procSelectObject.Call(memDC, uintptr(5))
				pts := [3]struct{ X, Y int32 }{
					{cx + half, cy - half},
					{cx - half, cy},
					{cx + half, cy + half},
				}
				procPolygon.Call(memDC, uintptr(unsafe.Pointer(&pts[0])), 3)
			case 1: // Home: ○ thick circle outline
				procSelectObject.Call(memDC, iconPen)
				procSelectObject.Call(memDC, uintptr(5))
				procEllipse.Call(memDC,
					uintptr(cx-half), uintptr(cy-half),
					uintptr(cx+half), uintptr(cy+half))
			case 2: // Recent: □ thick rounded rectangle outline
				rsz := int32(float32(iconSz) * 0.9)
				procSelectObject.Call(memDC, iconPen)
				procSelectObject.Call(memDC, uintptr(5))
				procRoundRect.Call(memDC,
					uintptr(cx-rsz), uintptr(cy-rsz),
					uintptr(cx+rsz), uintptr(cy+rsz),
					4, 4)
			}
		}
		procBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), memDC, 0, 0, 0x00CC0020)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case WM_MOUSEMOVE:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		var rc struct{ Left, Top, Right, Bottom int32 }
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		ww := rc.Right - rc.Left
		hh := rc.Bottom - rc.Top
		oldHover := ui.navHoverBtn
		ui.navHoverBtn = int32(ui.navHitTest(x, y, ww, hh))
		if ui.navHoverBtn != oldHover {
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		if !ui.navMouseTrack {
			ui.navMouseTrack = true
			tme := struct {
				CbSize uint32; DwFlags uint32; HwndTrack uintptr; DwHoverTime uint32
			}{CbSize: 16, DwFlags: 2, HwndTrack: hwnd}
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
		}
		return 0
	case WM_MOUSELEAVE:
		ui.navMouseTrack = false
		if ui.navHoverBtn != -1 {
			ui.navHoverBtn = -1
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case WM_LBUTTONDOWN:
		ui.navPressedBtn = ui.navHoverBtn
		if ui.navPressedBtn >= 0 {
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case WM_LBUTTONUP:
		ui.navPressedBtn = -1
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		var rc struct{ Left, Top, Right, Bottom int32 }
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		idx := ui.navHitTest(x, y, rc.Right-rc.Left, rc.Bottom-rc.Top)
		// WebURL（公网）模式无 RTSP 控制通道，按键转发给 player 页面，
		// 由页面内 h5_lgair.sendCmdEvent 经 WebSocket 下发（与侧边栏 back/home/recent 同链路）。
		if ui.app.WebURL != "" && ui.app.WebCmdForward != nil {
			switch idx {
			case 0:
				ui.app.WebCmdForward("goBack")
			case 1:
				ui.app.WebCmdForward("goHome")
			case 2:
				ui.app.WebCmdForward("goClean")
			}
			return 0
		}
		switch idx {
		case 0:
			ui.app.SendCmd(CmdAndroidKey, KeyBack)
		case 1:
			ui.app.SendCmd(CmdAndroidKey, KeyHome)
		case 2:
			ui.app.SendCmd(CmdAndroidKey, KeyRecent)
		}
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}
