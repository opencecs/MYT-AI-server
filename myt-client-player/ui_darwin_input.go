//go:build darwin

package main

import (
	"time"
)

// ─── macOS virtual keycode → SDL scancode / sym ────────────────────────────

type macKeyInfo struct {
	scancode uint16
	sym      int32
}

func macVKToSDL(vk int) (scancode uint16, sym int32) {
	if info, ok := macVKMap[vk]; ok {
		return info.scancode, info.sym
	}
	return 0, 0
}

var macVKMap = map[int]macKeyInfo{
	0x00: {4, 'a'}, 0x01: {22, 's'}, 0x02: {7, 'd'}, 0x03: {9, 'f'},
	0x04: {11, 'h'}, 0x05: {10, 'g'}, 0x06: {29, 'z'}, 0x07: {27, 'x'},
	0x08: {6, 'c'}, 0x09: {25, 'v'}, 0x0B: {5, 'b'}, 0x0C: {20, 'q'},
	0x0D: {26, 'w'}, 0x0E: {8, 'e'}, 0x0F: {21, 'r'}, 0x10: {28, 'y'},
	0x11: {23, 't'}, 0x1F: {18, 'o'}, 0x20: {24, 'u'}, 0x22: {12, 'i'},
	0x23: {19, 'p'}, 0x25: {15, 'l'}, 0x26: {13, 'j'}, 0x28: {14, 'k'},
	0x2D: {17, 'n'}, 0x2E: {16, 'm'},
	0x12: {30, '1'}, 0x13: {31, '2'}, 0x14: {32, '3'}, 0x15: {33, '4'},
	0x17: {34, '5'}, 0x16: {35, '6'}, 0x1A: {36, '7'}, 0x1C: {37, '8'},
	0x19: {38, '9'}, 0x1D: {39, '0'},
	0x1B: {45, '-'}, 0x18: {46, '='}, 0x21: {47, '['}, 0x1E: {48, ']'},
	0x2A: {49, '\\'}, 0x29: {51, ';'}, 0x27: {52, '\''}, 0x32: {53, '`'},
	0x2B: {54, ','}, 0x2F: {55, '.'}, 0x2C: {56, '/'},
	0x24: {40, 0x0D}, 0x30: {43, 0x09}, 0x31: {44, 0x20},
	0x33: {42, 0x08}, 0x35: {41, 0x1B},
	0x7B: {80, 0x40000000 | 80}, 0x7C: {79, 0x40000000 | 79},
	0x7D: {81, 0x40000000 | 81}, 0x7E: {82, 0x40000000 | 82},
	0x73: {76, 0x7F}, 0x71: {74, 0x40000000 | 74}, 0x75: {77, 0x40000000 | 77},
	0x72: {75, 0x40000000 | 75}, 0x77: {78, 0x40000000 | 78},
	0x7A: {58, 0x40000000 | 58}, 0x78: {59, 0x40000000 | 59},
	0x63: {60, 0x40000000 | 60}, 0x76: {61, 0x40000000 | 61},
	0x60: {62, 0x40000000 | 62}, 0x61: {63, 0x40000000 | 63},
	0x62: {64, 0x40000000 | 64}, 0x64: {65, 0x40000000 | 65},
	0x65: {66, 0x40000000 | 66}, 0x6D: {67, 0x40000000 | 67},
	0x67: {68, 0x40000000 | 68}, 0x6F: {69, 0x40000000 | 69},
	0x38: {225, 0x40000000 | 225}, 0x3C: {229, 0x40000000 | 229},
	0x3B: {224, 0x40000000 | 224}, 0x3E: {228, 0x40000000 | 228},
	0x3A: {226, 0x40000000 | 226}, 0x3D: {230, 0x40000000 | 230},
	0x37: {227, 0x40000000 | 227}, 0x36: {231, 0x40000000 | 231},
	0x39: {57, 0x40000000 | 57},
}

func macVKToKeyName(vk int) string {
	switch vk {
	case 0x33:
		return "Backspace"
	case 0x24:
		return "Enter"
	case 0x7C:
		return "ArrowRight"
	case 0x7B:
		return "ArrowLeft"
	case 0x7E:
		return "ArrowUp"
	case 0x7D:
		return "ArrowDown"
	case 0x30:
		return "Tab"
	case 0x73:
		return "Delete"
	case 0x71:
		return "Home"
	case 0x75:
		return "End"
	case 0x72:
		return "PageUp"
	case 0x77:
		return "PageDown"
	}
	return ""
}

// ─── Input handlers ────────────────────────────────────────────────────────

func (ui *DarwinUI) handleMouseClick(pressed bool, button int, x, y int) {
	app := ui.app
	if app.Ctrl.Load() == nil {
		return
	}
	scaler := app.activeScaler()
	if scaler == nil {
		return
	}
	sx, sy, sw, sh, ok := scaler.ScaleMouseCoords(int32(x), int32(y))
	if !ok {
		return
	}
	var btn uint8
	switch button {
	case 1:
		btn = MouseLeft
	case 2:
		btn = MouseMiddle
	case 3:
		btn = MouseRight
	default:
		return
	}
	if !pressed && btn == MouseRight {
		app.SendCmd(CmdAndroidKey, KeyBack)
		return
	}
	// Clamp mouse-up coordinates to video boundaries
	sx16, sy16, sw16, sh16 := sx, sy, uint16(sw), uint16(sh)
	if !pressed {
		if g := scaler.LBGeo(); g != nil {
			if sx16 > uint16(sw) {
				sx16 = uint16(sw)
			}
			if sy16 > uint16(sh) {
				sy16 = uint16(sh)
			}
		}
	}
	app.OnMouseEvent(BuildMouseKey(pressed, btn, sx16, sy16, sw16, sh16))
}

func (ui *DarwinUI) handleDoubleClick(x, y int) {
	app := ui.app
	if app.Ctrl.Load() == nil {
		return
	}
	scaler := app.activeScaler()
	if scaler == nil {
		return
	}
	sx, sy, sw, sh, ok := scaler.ScaleMouseCoords(int32(x), int32(y))
	if !ok {
		return
	}
	sw16, sh16 := uint16(sw), uint16(sh)
	app.OnMouseEvent(BuildMouseKey(true, MouseLeft, sx, sy, sw16, sh16))
	app.OnMouseEvent(BuildMouseKey(false, MouseLeft, sx, sy, sw16, sh16))
	app.OnMouseEvent(BuildMouseKey(true, MouseLeft, sx, sy, sw16, sh16))
	app.OnMouseEvent(BuildMouseKey(false, MouseLeft, sx, sy, sw16, sh16))
}

func (ui *DarwinUI) handleMouseMove(x, y int, buttonState int) {
	app := ui.app
	if app.Ctrl.Load() == nil {
		return
	}
	scaler := app.activeScaler()
	if scaler == nil {
		return
	}
	sx, sy, sw, sh, ok := scaler.ScaleMouseCoords(int32(x), int32(y))
	if !ok {
		return
	}
	app.OnMouseEvent(BuildMouseMotion(uint8(buttonState), 0, sx, sy, 0, 0, uint16(sw), uint16(sh)))
}

func (ui *DarwinUI) handleMouseLeave() {
	app := ui.app
	if app.Ctrl.Load() == nil {
		return
	}
	scaler := app.activeScaler()
	if scaler == nil {
		return
	}
	srcW, srcH := scaler.SourceSize()
	if srcW <= 0 || srcH <= 0 {
		return
	}
	app.OnMouseEvent(BuildMouseKey(false, MouseLeft, 0, 0, uint16(srcW), uint16(srcH)))
}

func (ui *DarwinUI) handleKey(pressed bool, keycode int, mods int, unicode uint32, textChar uint32) {
	app := ui.app
	if app.Ctrl.Load() == nil {
		return
	}
	scancode, sym := macVKToSDL(keycode)

	if pressed && mods&ModCtrl != 0 {
		ch := rune(unicode)
		if ch == 'c' || ch == 'C' {
			go ui.ClipboardCopy()
			return
		}
		if ch == 'x' || ch == 'X' {
			go ui.ClipboardCut()
			return
		}
		if ch == 'v' || ch == 'V' {
			go func() {
				if text := ui.ReadClipboardText(); text != "" {
					app.PasteClipboard(text)
				}
			}()
			return
		}
	}

	if pressed && mods&(ModCtrl|ModAlt) == 0 {
		cmdID := funcKeyCmdID(macVKToKeyName(keycode))
		if cmdID > 0 {
			app.SendCmd(CmdFuncKey, uint32(cmdID))
			return
		}
	}

	if pressed && keycode == 0x35 {
		app.SendCmd(CmdAndroidKey, KeyBack)
		return
	}

	// ── 键位映射引擎:F9 开关与命中的映射键在此消费(与 Win 路径同语义)
	if app.Keymap != nil {
		if code := sdlScanToJSCode[uint16(scancode)]; code != "" {
			if app.Keymap.HandleCode(code, pressed, mods) {
				return
			}
		}
	}

	if sym == 0 && scancode == 0 {
		return
	}
	app.SendKeyboard(pressed, scancode, int32(sym), 0, uint16(mods))

	// Send text for printable characters (matching Windows handleWin32Char)
	if pressed && mods&(ModCtrl|ModAlt) == 0 && textChar >= 32 && textChar != 127 {
		app.SendText(string(rune(textChar)))
	}
}

func (ui *DarwinUI) handleScroll(x, y int, deltaY float32) {
	app := ui.app
	if app.Ctrl.Load() == nil {
		return
	}
	scaler := app.activeScaler()
	if scaler == nil {
		return
	}

	dir := int32(1)
	if deltaY < 0 {
		dir = -1
	}
	if dir == 0 {
		return
	}

	ui.mu.Lock()
	now := time.Now()
	if ui.wheelDir != int(dir) || now.Sub(ui.wheelLast) > 100*time.Millisecond {
		ui.wheelCount = 0
		ui.wheelDir = int(dir)
	}
	ui.wheelLast = now
	ui.wheelCount++
	if ui.wheelTimer != nil {
		ui.wheelTimer.Stop()
	}
	count := ui.wheelCount
	wx, wy := int32(x), int32(y)
	ui.wheelTimer = time.AfterFunc(50*time.Millisecond, func() {
		ui.mu.Lock()
		ui.wheelTimer = nil
		ui.wheelCount = 0
		ui.mu.Unlock()
		ui.sendWheelGesture(wx, wy, dir, count)
	})
	ui.mu.Unlock()
}

func (ui *DarwinUI) sendWheelGesture(wheelX, wheelY int32, dir int32, count int) {
	defer func() {
		if r := recover(); r != nil {
			logf("[wheel] recovered: %v\n", r)
		}
	}()
	app := ui.app
	ctrl := app.Ctrl.Load()
	if ctrl == nil {
		return
	}
	scaler := app.activeScaler()
	if scaler == nil {
		return
	}
	sx, sy, srcW, srcH, ok := scaler.ScaleMouseCoords(wheelX, wheelY)
	if !ok || srcW == 0 || srcH == 0 {
		return
	}

	dist := int32(80+(count-1)*10) * dir
	var ex16, ey16 uint16
	if app.Rotated.Load() {
		toX := int32(sx) + dist
		if toX < 0 {
			toX = 0
		}
		if toX > srcW {
			toX = srcW
		}
		ex16, ey16 = uint16(toX), uint16(sy)
	} else {
		toY := int32(sy) + dist
		if toY < 0 {
			toY = 0
		}
		if toY > srcH {
			toY = srcH
		}
		ex16, ey16 = uint16(sx), uint16(toY)
	}

	sx16, sy16 := uint16(sx), uint16(sy)
	sw, sh := uint16(srcW), uint16(srcH)
	(*ctrl).Send(BuildMouseKey(true, MouseLeft, sx16, sy16, sw, sh))
	for i := 1; i <= 3; i++ {
		mx := sx16 + (ex16-sx16)*uint16(i)/3
		my := sy16 + (ey16-sy16)*uint16(i)/3
		(*ctrl).Send(BuildMouseMotion(1, 0, mx, my, 0, 0, sw, sh))
		time.Sleep(10 * time.Millisecond)
	}
	(*ctrl).Send(BuildMouseKey(false, MouseLeft, ex16, ey16, sw, sh))
}
