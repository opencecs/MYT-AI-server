package main

import (
	"encoding/json"
	"net/url"
	"strings"
)

// ─── SDL scancode and sym constants ───────────────────────────────────────────

const SDLScanMask = 0x40000000

// jsKeyToSDL maps JS key names to SDL sym values.
func jsKeyToSDL(key string) int {
	switch key {
	case "Backspace":
		return 8
	case "Tab":
		return 9
	case "Enter":
		return 13
	case "Escape":
		return 27
	case " ":
		return 32
	case "Delete":
		return 127
	case "ArrowUp":
		return SDLScanMask | 82
	case "ArrowDown":
		return SDLScanMask | 81
	case "ArrowRight":
		return SDLScanMask | 79
	case "ArrowLeft":
		return SDLScanMask | 80
	case "Insert":
		return SDLScanMask | 73
	case "Home":
		return SDLScanMask | 74
	case "End":
		return SDLScanMask | 77
	case "PageUp":
		return SDLScanMask | 75
	case "PageDown":
		return SDLScanMask | 78
	case "F1":
		return SDLScanMask | 58
	case "F2":
		return SDLScanMask | 59
	case "F3":
		return SDLScanMask | 60
	case "F4":
		return SDLScanMask | 61
	case "F5":
		return SDLScanMask | 62
	case "F6":
		return SDLScanMask | 63
	case "F7":
		return SDLScanMask | 64
	case "F8":
		return SDLScanMask | 65
	case "F9":
		return SDLScanMask | 66
	case "F10":
		return SDLScanMask | 67
	case "F11":
		return SDLScanMask | 68
	case "F12":
		return SDLScanMask | 69
	case "Shift":
		return SDLScanMask | 225
	case "Control":
		return SDLScanMask | 224
	case "Alt":
		return SDLScanMask | 226
	case "Meta":
		return SDLScanMask | 227
	}
	return 0
}

// jsCodeToSDLScancode maps JS code strings to SDL scancodes.
func jsCodeToSDLScancode(code string) uint16 {
	v, ok := jsCodeToSDLMap[code]
	if ok {
		return v
	}
	return 0
}

var jsCodeToSDLMap = map[string]uint16{
	"KeyA": 4, "KeyB": 5, "KeyC": 6, "KeyD": 7, "KeyE": 8,
	"KeyF": 9, "KeyG": 10, "KeyH": 11, "KeyI": 12, "KeyJ": 13,
	"KeyK": 14, "KeyL": 15, "KeyM": 16, "KeyN": 17, "KeyO": 18,
	"KeyP": 19, "KeyQ": 20, "KeyR": 21, "KeyS": 22, "KeyT": 23,
	"KeyU": 24, "KeyV": 25, "KeyW": 26, "KeyX": 27, "KeyY": 28,
	"KeyZ": 29,
	"Digit1": 30, "Digit2": 31, "Digit3": 32, "Digit4": 33,
	"Digit5": 34, "Digit6": 35, "Digit7": 36, "Digit8": 37,
	"Digit9": 38, "Digit0": 39,
	"Enter": 40, "Escape": 41, "Backspace": 42, "Tab": 43,
	"Space": 44, "Minus": 45, "Equal": 46,
	"BracketLeft": 47, "BracketRight": 48, "Backslash": 49,
	"Semicolon": 51, "Quote": 52, "Backquote": 53,
	"Comma": 54, "Period": 55, "Slash": 56,
	"CapsLock": 57,
	"F1": 58, "F2": 59, "F3": 60, "F4": 61, "F5": 62,
	"F6": 63, "F7": 64, "F8": 65, "F9": 66, "F10": 67,
	"F11": 68, "F12": 69,
	"Insert": 73, "Home": 74, "PageUp": 75,
	"Delete": 76, "End": 77, "PageDown": 78,
	"ArrowRight": 79, "ArrowLeft": 80,
	"ArrowDown": 81, "ArrowUp": 82,
	"ControlLeft": 224, "ShiftLeft": 225,
	"AltLeft": 226, "MetaLeft": 227,
	"ControlRight": 228, "ShiftRight": 229,
	"AltRight": 230, "MetaRight": 231,
}

// funcKeyCmdID maps functional keys to KEYCMD parameter IDs.
func funcKeyCmdID(key string) int {
	switch key {
	case "Backspace", "Delete":
		return 1
	case "Enter":
		return 2
	case "ArrowRight":
		return 3
	case "ArrowLeft":
		return 4
	case "ArrowUp":
		return 5
	case "ArrowDown":
		return 6
	case "Tab":
		return 7
	}
	return 0
}

// vkToSDLSym maps Win32 VK codes to SDL sym values.
func vkToSDLSym(vk int) int {
	if vk >= 0x41 && vk <= 0x5A {
		return vk + 32
	}
	if vk >= 0x30 && vk <= 0x39 {
		return vk
	}
	return vkToSDLSymMap[vk]
}

var vkToSDLSymMap = map[int]int{
	0x08: 8, 0x09: 9, 0x0D: 13, 0x1B: 27, 0x20: 32, 0x2E: 127,
	0x26: SDLScanMask | 82, 0x28: SDLScanMask | 81,
	0x27: SDLScanMask | 79, 0x25: SDLScanMask | 80,
	0x2D: SDLScanMask | 73, 0x24: SDLScanMask | 74,
	0x23: SDLScanMask | 77, 0x21: SDLScanMask | 75,
	0x22: SDLScanMask | 78,
	0x70: SDLScanMask | 58, 0x71: SDLScanMask | 59,
	0x72: SDLScanMask | 60, 0x73: SDLScanMask | 61,
	0x74: SDLScanMask | 62, 0x75: SDLScanMask | 63,
	0x76: SDLScanMask | 64, 0x77: SDLScanMask | 65,
	0x78: SDLScanMask | 66, 0x79: SDLScanMask | 67,
	0x7A: SDLScanMask | 68, 0x7B: SDLScanMask | 69,
}

// ─── URL helpers ──────────────────────────────────────────────────────────────

func parseRTSPHost(rtspURL string) string {
	u, err := url.Parse(rtspURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func parseRTSPPort(rtspURL string) string {
	u, err := url.Parse(rtspURL)
	if err != nil {
		return ""
	}
	return u.Port()
}

// ─── CtrlList JSON ────────────────────────────────────────────────────────────

type ctrlItem struct {
	Addr string `json:"addr"`
	On   bool   `json:"on"`
}

func ctrlListJSON(items []ctrlItem) string {
	if len(items) == 0 {
		return "[]"
	}
	type ctrlItemJSON struct {
		Addr string `json:"addr"`
		On   bool   `json:"on"`
	}
	out := make([]ctrlItemJSON, len(items))
	for i, it := range items {
		out[i] = ctrlItemJSON{Addr: it.Addr, On: it.On}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func initCtrlItems(list string) []ctrlItem {
	if list == "" {
		return nil
	}
	var items []ctrlItem
	for _, s := range strings.Split(list, "#") {
		s = strings.TrimSpace(s)
		if s != "" {
			items = append(items, ctrlItem{Addr: s, On: true})
		}
	}
	return items
}

func applyCtrlList(items []ctrlItem, ctrl *ControlChannel) {
	var parts []string
	for _, it := range items {
		if it.On {
			parts = append(parts, it.Addr)
		}
	}
	ctrl.SetTargetList(strings.Join(parts, "#"))
}
