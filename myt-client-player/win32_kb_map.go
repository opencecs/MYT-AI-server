//go:build windows

package main

// VK→SDL 符号/scancode/键名 映射表（纯查表，零依赖）

func vkToSDLSymWin(vk int) int {
	switch vk {
	case 0x08:
		return K_BACKSPACE
	case 0x09:
		return K_TAB
	case 0x0D:
		return K_RETURN
	case 0x1B:
		return K_ESCAPE
	case 0x20:
		return K_SPACE
	case 0x21:
		return K_PAGEUP
	case 0x22:
		return K_PAGEDOWN
	case 0x23:
		return K_END
	case 0x24:
		return K_HOME
	case 0x25:
		return K_LEFT
	case 0x26:
		return K_UP
	case 0x27:
		return K_RIGHT
	case 0x28:
		return K_DOWN
	case 0x2D:
		return K_INSERT
	case 0x2E:
		return K_DELETE
	case 0x2C:
		return K_PRINTSCREEN
	case 0x13:
		return K_PAUSE
	// OEM keys
	case 0xBA:
		return K_SEMICOLON
	case 0xBB:
		return K_EQUALS
	case 0xBD:
		return K_MINUS
	case 0xBE:
		return K_PERIOD
	case 0xBF:
		return K_SLASH
	case 0xC0:
		return K_BACKQUOTE
	case 0xDB:
		return K_LEFTBRACKET
	case 0xDC:
		return K_BACKSLASH
	case 0xDD:
		return K_RIGHTBRACKET
	case 0xDE:
		return K_QUOTE
	// Lock keys
	case 0x14:
		return K_CAPSLOCK
	case 0x90:
		return K_NUMLOCK
	case 0x91:
		return K_SCROLLLOCK
	// Function keys
	case 0x70:
		return K_F1
	case 0x71:
		return K_F2
	case 0x72:
		return K_F3
	case 0x73:
		return K_F4
	case 0x74:
		return K_F5
	case 0x75:
		return K_F6
	case 0x76:
		return K_F7
	case 0x77:
		return K_F8
	case 0x78:
		return K_F9
	case 0x79:
		return K_F10
	case 0x7A:
		return K_F11
	case 0x7B:
		return K_F12
	case 0x7C:
		return K_F13
	case 0x7D:
		return K_F14
	case 0x7E:
		return K_F15
	// Numpad
	case 0x60:
		return K_KP0
	case 0x61:
		return K_KP1
	case 0x62:
		return K_KP2
	case 0x63:
		return K_KP3
	case 0x64:
		return K_KP4
	case 0x65:
		return K_KP5
	case 0x66:
		return K_KP6
	case 0x67:
		return K_KP7
	case 0x68:
		return K_KP8
	case 0x69:
		return K_KP9
	case 0x6E:
		return K_KP_PERIOD
	case 0x6F:
		return K_KP_DIVIDE
	case 0x6A:
		return K_KP_MULTIPLY
	case 0x6D:
		return K_KP_MINUS
	case 0x6B:
		return K_KP_PLUS
	// Modifier keys
	case 0xA0:
		return K_LSHIFT
	case 0xA1:
		return K_RSHIFT
	case 0xA2:
		return K_LCTRL
	case 0xA3:
		return K_RCTRL
	case 0xA4:
		return K_LALT
	case 0xA5:
		return K_RALT
	case 0x5B:
		return K_LMETA
	case 0x5C:
		return K_RMETA
	}
	if vk >= 'A' && vk <= 'Z' {
		return vk + 32
	}
	if vk >= '0' && vk <= '9' {
		return vk
	}
	return 0
}

func vkToSDLScancodeWin(vk int) int {
	switch vk {
	case 0x08:
		return SC_BACKSPACE
	case 0x09:
		return SC_TAB
	case 0x0D:
		return SC_RETURN
	case 0x1B:
		return SC_ESCAPE
	case 0x20:
		return SC_SPACE
	case 0x21:
		return SC_PAGEUP
	case 0x22:
		return SC_PAGEDOWN
	case 0x23:
		return SC_END
	case 0x24:
		return SC_HOME
	case 0x25:
		return SC_LEFT
	case 0x26:
		return SC_UP
	case 0x27:
		return SC_RIGHT
	case 0x28:
		return SC_DOWN
	case 0x2D:
		return SC_INSERT
	case 0x2E:
		return SC_DELETE
	case 0x2C:
		return SC_PRINTSCREEN
	case 0x13:
		return SC_PAUSE
	case 0xBA:
		return SC_SEMICOLON
	case 0xBB:
		return SC_EQUALS
	case 0xBD:
		return SC_MINUS
	case 0xBE:
		return SC_PERIOD
	case 0xBF:
		return SC_SLASH
	case 0xC0:
		return SC_BACKQUOTE
	case 0xDB:
		return SC_LEFTBRACKET
	case 0xDC:
		return SC_BACKSLASH
	case 0xDD:
		return SC_RIGHTBRACKET
	case 0xDE:
		return SC_QUOTE
	// Lock keys
	case 0x14:
		return SC_CAPSLOCK
	case 0x90:
		return SC_NUMLOCK
	case 0x91:
		return SC_SCROLLLOCK
	// Function keys
	case 0x70:
		return SC_F1
	case 0x71:
		return SC_F2
	case 0x72:
		return SC_F3
	case 0x73:
		return SC_F4
	case 0x74:
		return SC_F5
	case 0x75:
		return SC_F6
	case 0x76:
		return SC_F7
	case 0x77:
		return SC_F8
	case 0x78:
		return SC_F9
	case 0x79:
		return SC_F10
	case 0x7A:
		return SC_F11
	case 0x7B:
		return SC_F12
	// Numpad
	case 0x60:
		return SC_KP0
	case 0x61:
		return SC_KP1
	case 0x62:
		return SC_KP2
	case 0x63:
		return SC_KP3
	case 0x64:
		return SC_KP4
	case 0x65:
		return SC_KP5
	case 0x66:
		return SC_KP6
	case 0x67:
		return SC_KP7
	case 0x68:
		return SC_KP8
	case 0x69:
		return SC_KP9
	case 0x6E:
		return SC_KP_PERIOD
	case 0x6F:
		return SC_KP_DIVIDE
	case 0x6A:
		return SC_KP_MULTIPLY
	case 0x6D:
		return SC_KP_MINUS
	case 0x6B:
		return SC_KP_PLUS
	// Modifier keys
	case 0xA0:
		return SC_LSHIFT
	case 0xA1:
		return SC_RSHIFT
	case 0xA2:
		return SC_LCTRL
	case 0xA3:
		return SC_RCTRL
	case 0xA4:
		return SC_LALT
	case 0xA5:
		return SC_RALT
	case 0x5B:
		return SC_LWIN
	case 0x5C:
		return SC_RWIN
	}
	if vk >= 'A' && vk <= 'Z' {
		return SC_A + (vk - 'A')
	}
	if vk >= '0' && vk <= '9' {
		return SC_1 + (vk - '1')
	}
	return 0
}

func vkToKeyName(vk int) string {
	switch vk {
	case 0x08:
		return "Backspace"
	case 0x09:
		return "Tab"
	case 0x0D:
		return "Enter"
	case 0x1B:
		return "Escape"
	case 0x2D:
		return "Insert"
	case 0x2E:
		return "Delete"
	case 0x70:
		return "F1"
	case 0x71:
		return "F2"
	case 0x72:
		return "F3"
	case 0x73:
		return "F4"
	case 0x74:
		return "F5"
	case 0x75:
		return "F6"
	case 0x76:
		return "F7"
	case 0x77:
		return "F8"
	case 0x78:
		return "F9"
	case 0x79:
		return "F10"
	case 0x7A:
		return "F11"
	case 0x7B:
		return "F12"
	case 0x21:
		return "PageUp"
	case 0x22:
		return "PageDown"
	case 0x23:
		return "End"
	case 0x24:
		return "Home"
	}
	return ""
}
