//go:build windows

package main

import (
	"unsafe"
)

// sdlAreaWndProc handles mouse events in the video rendering area.
func (ui *WindowsUI) sdlAreaWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if uint32(msg) == WM_ERASEBKGND {
		return 1
	}

	// WebView2 模式：resize player WebView
	if uint32(msg) == WM_SIZE {
		if ui.webChromium != nil {
			ui.webChromium.Resize()
		}
		return 0
	}

	switch uint32(msg) {
	case WM_PAINT:
		if d3d := ui.app.D3D11Ren.Load(); d3d != nil {
			// D3D11 owns this window — do NOT paint with GDI (causes green flash).
			// Just validate the rect so WM_PAINT stops firing.
			procValidateRect.Call(hwnd, 0)
		} else if ren := ui.app.Renderer.Load(); ren != nil {
			ren.RenderFrame()
			procValidateRect.Call(hwnd, 0)
		} else {
			// No renderer yet — paint background on SDL area. 本地模式等视频前用浅色底；
			// 公网 Web 模式没有本地 renderer，WebView 子窗口就位前这块会亮一下浅灰(≈白)，
			// 直接铺黑底，与加载动画深色背景无缝衔接，消除"先白"闪烁。
			bg := ui.hBrushLight
			if ui.app.WebURL != "" {
				bg = ui.hBrushBlack
			}
			type psStruct struct {
				Hdc uintptr; Erase int32; RcPaint struct{ Left, Top, Right, Bottom int32 }
				Restored int32; IncUpdate int32; RgbReserved [32]byte
			}
			var ps psStruct
			procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			hdc := ps.Hdc
			if hdc != 0 {
				var rc struct{ Left, Top, Right, Bottom int32 }
				procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
				procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bg)
				procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			} else {
				procValidateRect.Call(hwnd, 0)
			}
		}
		return 0
	}

	app := ui.app
	if !app.CanControlInput() {
		switch uint32(msg) {
		case WM_LBUTTONDOWN, WM_MBUTTONDOWN, WM_RBUTTONDOWN, WM_LBUTTONDBLCLK:
			procSetFocus.Call(hwnd)
			return 0
		case WM_LBUTTONUP, WM_MBUTTONUP, WM_RBUTTONUP, WM_MOUSEMOVE, WM_MOUSELEAVE,
			WM_MOUSEWHEEL, WM_KEYDOWN, WM_KEYUP, WM_CHAR:
			return 0
		}
	}
	scaler := ui.activeScaler()
	if scaler == nil {
		ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return ret
	}

	switch uint32(msg) {
	case WM_LBUTTONDOWN, WM_MBUTTONDOWN:
		if ctrl := app.Ctrl.Load(); ctrl != nil {
			x := int32(int16(lParam & 0xFFFF))
			y := int32(int16((lParam >> 16) & 0xFFFF))
			sx, sy, srcW, srcH, ok := scaler.ScaleMouseCoords(x, y)
			if ok {
				var btn uint8
				switch uint32(msg) {
				case WM_LBUTTONDOWN:
					btn = MouseLeft
				case WM_MBUTTONDOWN:
					btn = MouseMiddle
				}
				(*ctrl).Send(BuildMouseKey(true, btn, sx, sy, uint16(srcW), uint16(srcH)))
			}
		}
		procSetFocus.Call(hwnd)
		return 0

	case WM_LBUTTONUP, WM_MBUTTONUP:
		if ctrl := app.Ctrl.Load(); ctrl != nil {
			x := int32(int16(lParam & 0xFFFF))
			y := int32(int16((lParam >> 16) & 0xFFFF))
			sx, sy, srcW, srcH, ok := scaler.ScaleMouseCoords(x, y)
			// Always send mouse-up, even if outside video area.
			if !ok && srcW > 0 && srcH > 0 {
				g := scaler.LBGeo()
				if g != nil {
					relX := x - g.offX
					relY := y - g.offY
					if relX < 0 { relX = 0 } else if relX > g.drawW { relX = g.drawW }
					if relY < 0 { relY = 0 } else if relY > g.drawH { relY = g.drawH }
					if scaler.IsRotated() {
						sx = uint16(relY * srcW / g.drawH)
						sy = uint16((g.drawW - relX) * srcH / g.drawW)
					} else {
						sx = uint16(relX * srcW / g.drawW)
						sy = uint16(relY * srcH / g.drawH)
					}
				}
				ok = srcW > 0 && srcH > 0
			}
			if ok {
				var btn uint8
				switch uint32(msg) {
				case WM_LBUTTONUP:
					btn = MouseLeft
				case WM_MBUTTONUP:
					btn = MouseMiddle
				}
				(*ctrl).Send(BuildMouseKey(false, btn, sx, sy, uint16(srcW), uint16(srcH)))
			}
		}
		return 0

	case WM_RBUTTONDOWN:
		procSetFocus.Call(hwnd)
		return 0

	case WM_RBUTTONUP:
		// Right-click = Android Back
		if ctrl := app.Ctrl.Load(); ctrl != nil {
			(*ctrl).Send(BuildKeyCmd(CmdAndroidKey, KeyBack))
		}
		return 0

	case WM_LBUTTONDBLCLK:
		if ctrl := app.Ctrl.Load(); ctrl != nil {
			x := int32(int16(lParam & 0xFFFF))
			y := int32(int16((lParam >> 16) & 0xFFFF))
			sx, sy, srcW, srcH, ok := scaler.ScaleMouseCoords(x, y)
			if ok {
				sw, sh := uint16(srcW), uint16(srcH)
				(*ctrl).Send(BuildMouseKey(true, MouseLeft, sx, sy, sw, sh))
				(*ctrl).Send(BuildMouseKey(false, MouseLeft, sx, sy, sw, sh))
				(*ctrl).Send(BuildMouseKey(true, MouseLeft, sx, sy, sw, sh))
				(*ctrl).Send(BuildMouseKey(false, MouseLeft, sx, sy, sw, sh))
			}
		}
		procSetFocus.Call(hwnd)
		return 0

	case WM_MOUSELEAVE:
		ui.sdlMouseTracking = false
		if ctrl := app.Ctrl.Load(); ctrl != nil {
			srcW, srcH := scaler.SourceSize()
			if srcW > 0 && srcH > 0 {
				(*ctrl).Send(BuildMouseKey(false, MouseLeft, 0, 0, uint16(srcW), uint16(srcH)))
			}
		}
		return 0

	case WM_MOUSEMOVE:
		if !ui.sdlMouseTracking {
			type trackMouseEvent struct {
				CbSize uint32
				DwFlags uint32
				HwndTrack uintptr
				DwHoverTime uint32
			}
			var tme trackMouseEvent
			tme.CbSize = uint32(unsafe.Sizeof(tme))
			tme.DwFlags = 0x0002 // TME_LEAVE
			tme.HwndTrack = hwnd
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			ui.sdlMouseTracking = true
		}
		if ctrl := app.Ctrl.Load(); ctrl != nil {
			x := int32(int16(lParam & 0xFFFF))
			y := int32(int16((lParam >> 16) & 0xFFFF))
			sx, sy, srcW, srcH, ok := scaler.ScaleMouseCoords(x, y)
			if !ok {
				return 0
			}
			state := uint8(wParam & 0xFF)
			(*ctrl).Send(BuildMouseMotion(state, 0, sx, sy, 0, 0, uint16(srcW), uint16(srcH)))
		}
		return 0

	case WM_MOUSEWHEEL:
		ui.handleMouseWheel(wParam, lParam)
		return 0

	case WM_KEYDOWN, WM_KEYUP:
		ui.handleWin32Keyboard(uint32(msg), wParam, lParam)
		return 0

	case WM_CHAR:
		ui.handleWin32Char(wParam)
		return 0

	case WM_SETCURSOR:
		hCursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
		procSetCursor.Call(hCursor)
		return 1
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

// wndClassExW/registerClass/createWnd/createWindows/initGDICache/sideWndProc
// 已拆分到 ui_windows_create.go
