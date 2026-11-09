//go:build linux

package main

// linux_keys.go — X11 keysym → SDL sym/scancode 映射（对照 win32_kb_map.go /
// ui_darwin_input.go macVKMap 的表格模式，纯查表零依赖）。
//
// 覆盖范围：字母 A-Z、数字 0-9、常用标点、方向键、Home/End/PageUp/PageDown、
// Insert/Delete、空格、Enter、Esc、Backspace、Tab、F1-F12、Ctrl/Shift/Alt、
// 方向键、小键盘、Lock 键、Meta 键。

// xkeysymToSDL 把 X11 keysym 转成 (scancode, sdlSym)。未映射返 (0,0)。
func xkeysymToSDL(ks uint32) (scancode uint16, sym int32) {
	if info, ok := xKeySymMap[ks]; ok {
		return info.scancode, info.sym
	}
	return 0, 0
}

type xKeyInfo struct {
	scancode uint16
	sym      int32
}

var xKeySymMap = map[uint32]xKeyInfo{
	// ── 字母（lowercase keysym 0x61..0x7A）────────────────────────────
	0x61: {SC_A + 0, 'a'}, 0x62: {SC_A + 5, 'b'}, 0x63: {SC_A + 2, 'c'},
	0x64: {SC_A + 3, 'd'}, 0x65: {SC_A + 8, 'e'}, 0x66: {SC_A + 6, 'f'},
	0x67: {SC_A + 7, 'g'}, 0x68: {SC_A + 11, 'h'}, 0x69: {SC_A + 4, 'i'},
	0x6A: {SC_A + 9, 'j'}, 0x6B: {SC_A + 10, 'k'}, 0x6C: {SC_A + 15, 'l'},
	0x6D: {SC_A + 13, 'm'}, 0x6E: {SC_A + 12, 'n'}, 0x6F: {SC_A + 14, 'o'},
	0x70: {SC_A + 17, 'p'}, 0x71: {SC_A + 16, 'q'}, 0x72: {SC_A + 21, 'r'},
	0x73: {SC_A + 22, 's'}, 0x74: {SC_A + 23, 't'}, 0x75: {SC_A + 18, 'u'},
	0x76: {SC_A + 25, 'v'}, 0x77: {SC_A + 26, 'w'}, 0x78: {SC_A + 27, 'x'},
	0x79: {SC_A + 28, 'y'}, 0x7A: {SC_A + 29, 'z'},
	// ── 数字（0x30..0x39 → SC_1=30 起，Digit1 键位）──────────────────
	0x31: {SC_1 + 0, '1'}, 0x32: {SC_1 + 1, '2'}, 0x33: {SC_1 + 2, '3'},
	0x34: {SC_1 + 3, '4'}, 0x35: {SC_1 + 4, '5'}, 0x36: {SC_1 + 5, '6'},
	0x37: {SC_1 + 6, '7'}, 0x38: {SC_1 + 7, '8'}, 0x39: {SC_1 + 8, '9'},
	0x30: {SC_1 + 9, '0'},
	// ── 标点 ─────────────────────────────────────────────────────────
	0x3B: {SC_SEMICOLON, K_SEMICOLON},     // ;
	0x3D: {SC_EQUALS, K_EQUALS},           // =
	0x2D: {SC_MINUS, K_MINUS},             // -
	0x2E: {SC_PERIOD, K_PERIOD},           // .
	0x2F: {SC_SLASH, K_SLASH},             // /
	0x60: {SC_BACKQUOTE, K_BACKQUOTE},     // `
	0x5B: {SC_LEFTBRACKET, K_LEFTBRACKET}, // [
	0x5C: {SC_BACKSLASH, K_BACKSLASH},     // \
	0x5D: {SC_RIGHTBRACKET, K_RIGHTBRACKET},
	0x27: {SC_QUOTE, K_QUOTE}, // '
	0x2C: {54, 44},            // ,（SDL scancode 54，sym 逗号 0x2C）
	// ── 控制键 ───────────────────────────────────────────────────────
	0x0D:   {SC_RETURN, 0x0D},    // Return → Enter
	0x09:   {SC_TAB, 0x09},       // Tab
	0x20:   {SC_SPACE, 0x20},     // Space
	0xFF08: {SC_BACKSPACE, 0x08}, // Backspace
	0xFF1B: {SC_ESCAPE, 0x1B},    // Escape
	0xFF7F: {SC_NUMLOCK, K_NUMLOCK},
	0xFF14: {SC_SCROLLLOCK, K_SCROLLLOCK},
	0xFFE5: {SC_CAPSLOCK, K_CAPSLOCK},
	// ── 导航键 ───────────────────────────────────────────────────────
	0xFF50: {SC_HOME, K_HOME},
	0xFF51: {SC_LEFT, K_LEFT},
	0xFF52: {SC_UP, K_UP},
	0xFF53: {SC_RIGHT, K_RIGHT},
	0xFF54: {SC_DOWN, K_DOWN},
	0xFF55: {SC_PAGEUP, K_PAGEUP},
	0xFF56: {SC_PAGEDOWN, K_PAGEDOWN},
	0xFF57: {SC_END, K_END},
	0xFF63: {SC_INSERT, K_INSERT},
	0xFFFF: {SC_DELETE, K_DELETE},
	0xFF61: {SC_PRINTSCREEN, K_PRINTSCREEN},
	0xFF13: {SC_PAUSE, K_PAUSE},
	// ── 功能键 F1-F12（XK_F1=0xFFBE .. XK_F12=0xFFC9）───────────────
	0xFFBE: {SC_F1, K_F1},
	0xFFBF: {SC_F2, K_F2},
	0xFFC0: {SC_F3, K_F3},
	0xFFC1: {SC_F4, K_F4},
	0xFFC2: {SC_F5, K_F5},
	0xFFC3: {SC_F6, K_F6},
	0xFFC4: {SC_F7, K_F7},
	0xFFC5: {SC_F8, K_F8},
	0xFFC6: {SC_F9, K_F9},
	0xFFC7: {SC_F10, K_F10},
	0xFFC8: {SC_F11, K_F11},
	0xFFC9: {SC_F12, K_F12},
	// ── 修饰键 ───────────────────────────────────────────────────────
	0xFFE1: {SC_LSHIFT, K_LSHIFT},
	0xFFE2: {SC_RSHIFT, K_RSHIFT},
	0xFFE3: {SC_LCTRL, K_LCTRL},
	0xFFE4: {SC_RCTRL, K_RCTRL},
	0xFFE9: {SC_LALT, K_LALT},
	0xFFEA: {SC_RALT, K_RALT},
	0xFFEB: {227, K_LMETA}, // Super_L（SDL scancode 227）
	0xFFEC: {231, K_RMETA}, // Super_R（SDL scancode 231）
	// ── 小键盘 ───────────────────────────────────────────────────────
	0xFFB0: {SC_KP0, K_KP0},
	0xFFB1: {SC_KP1, K_KP1},
	0xFFB2: {SC_KP2, K_KP2},
	0xFFB3: {SC_KP3, K_KP3},
	0xFFB4: {SC_KP4, K_KP4},
	0xFFB5: {SC_KP5, K_KP5},
	0xFFB6: {SC_KP6, K_KP6},
	0xFFB7: {SC_KP7, K_KP7},
	0xFFB8: {SC_KP8, K_KP8},
	0xFFB9: {SC_KP9, K_KP9},
	0xFFAE: {SC_KP_PERIOD, K_KP_PERIOD},
	0xFFAF: {SC_KP_DIVIDE, K_KP_DIVIDE},
	0xFFAA: {SC_KP_MULTIPLY, K_KP_MULTIPLY},
	0xFFAD: {SC_KP_MINUS, K_KP_MINUS},
	0xFFAB: {SC_KP_PLUS, K_KP_PLUS},
	0xFF8D: {SC_RETURN, 0x0D}, // KP_Enter
}

// xKeySymToKeyName keysym → 功能键名（funcKeyCmdID 查找用，对照 vkToKeyName）。
func xKeySymToKeyName(ks uint32) string {
	switch ks {
	case 0xFF08:
		return "Backspace"
	case 0xFFFF:
		return "Delete"
	case 0xFF0D, 0xFF8D:
		return "Enter"
	case 0xFF51:
		return "ArrowLeft"
	case 0xFF52:
		return "ArrowUp"
	case 0xFF53:
		return "ArrowRight"
	case 0xFF54:
		return "ArrowDown"
	case 0xFF50:
		return "Home"
	case 0xFF57:
		return "End"
	case 0xFF55:
		return "PageUp"
	case 0xFF56:
		return "PageDown"
	case 0xFF09:
		return "Tab"
	case 0xFF63:
		return "Insert"
	case 0xFFBE:
		return "F1"
	case 0xFFBF:
		return "F2"
	case 0xFFC0:
		return "F3"
	case 0xFFC1:
		return "F4"
	case 0xFFC2:
		return "F5"
	case 0xFFC3:
		return "F6"
	case 0xFFC4:
		return "F7"
	case 0xFFC5:
		return "F8"
	case 0xFFC6:
		return "F9"
	case 0xFFC7:
		return "F10"
	case 0xFFC8:
		return "F11"
	case 0xFFC9:
		return "F12"
	}
	return ""
}

// xScanToJSCode SDL scancode → JS code（键位映射引擎 HandleCode 用，linux 上
// 键位映射未接线，但引擎共享代码引用此表时保持可用）。
func xScanToJSCode(sc uint16) string {
	if code, ok := sdlScanToJSCode[sc]; ok {
		return code
	}
	return ""
}
