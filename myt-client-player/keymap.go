package main

// keymap.go — 键盘映射数据模型、按键名换算、存储路径。
// 坐标一律用万分比(0~10000)相对设备源分辨率,转屏/换分辨率/多坑位通吃。

import (
	"net/url"
	"os"
	"strconv"
)

// KeymapEntry 单条映射。dpad 的 Keys 固定为 [上,左,下,右] 四键。
type KeymapEntry struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`              // tap|dpad|swipe|android
	Keys    []string `json:"keys"`              // dpad 4 键;单键类型取 [0]
	X       float64  `json:"x"`                 // 万分比 0~10000
	Y       float64  `json:"y"`                 // 万分比 0~10000
	X2      float64  `json:"x2,omitempty"`      // swipe 终点
	Y2      float64  `json:"y2,omitempty"`      // swipe 终点
	R       float64  `json:"r,omitempty"`       // dpad 半径(万分比,相对 min(srcW,srcH))
	Android string   `json:"android,omitempty"` // back|home|recent|volup|voldown
	TapMode string   `json:"tapMode,omitempty"` // hold|click
	Scale   float64  `json:"scale,omitempty"`   // 标记缩放(0.6~3.0,缺省 1;滚轮调整)
}

// KeymapProfile 一个可保存的键位方案(预设)。
type KeymapProfile struct {
	Version int           `json:"version"`
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Updated int64         `json:"updated"`
	Entries []KeymapEntry `json:"entries"`
}

// DeviceBinding 设备↔预设激活关系 + 开关状态。
// Hints 运行态按键提醒(雷电「在游戏中显示按键提醒」同款):零值文件(旧版无此字段)
// 默认开启——JSON 反序列化零值 false 会关掉提醒,loadBinding 后需归一化。
type DeviceBinding struct {
	PresetID string `json:"presetId"`
	Enabled  bool   `json:"enabled"`
	Hints    *bool  `json:"hints,omitempty"`
}

// HintsOn 运行态按键提醒是否开启(缺省 true)。
func (b DeviceBinding) HintsOn() bool { return b.Hints == nil || *b.Hints }

const (
	kmTypeTap     = "tap"
	kmTypeDpad    = "dpad"
	kmTypeSwipe   = "swipe"
	kmTypeAndroid = "android"
)

var kmAndroidKeys = []string{"back", "home", "recent", "volup", "voldown"}

// validateEntry 规范化并校验单条映射;返回 false 表示丢弃。
func validateEntry(e *KeymapEntry) bool {
	switch e.Type {
	case kmTypeTap:
		if len(e.Keys) < 1 || e.Keys[0] == "" {
			return false
		}
		e.Keys = e.Keys[:1]
		if e.TapMode != "click" {
			e.TapMode = "hold"
		}
		e.X2, e.Y2, e.R = 0, 0, 0
		e.Android = ""
	case kmTypeDpad:
		if len(e.Keys) != 4 {
			return false
		}
		for _, k := range e.Keys {
			if k == "" {
				return false
			}
		}
		if e.R <= 0 {
			e.R = 700
		}
		e.X2, e.Y2, e.Android, e.TapMode = 0, 0, "", ""
	case kmTypeSwipe:
		if len(e.Keys) < 1 || e.Keys[0] == "" {
			return false
		}
		e.Keys = e.Keys[:1]
		e.R, e.Android, e.TapMode = 0, "", ""
	case kmTypeAndroid:
		if len(e.Keys) < 1 || e.Keys[0] == "" {
			return false
		}
		ok := false
		for _, a := range kmAndroidKeys {
			if e.Android == a {
				ok = true
			}
		}
		if !ok {
			return false
		}
		e.Keys = e.Keys[:1]
		e.X2, e.Y2, e.R, e.TapMode = 0, 0, 0, ""
	default:
		return false
	}
	clampPct(&e.X)
	clampPct(&e.Y)
	clampPct(&e.X2)
	clampPct(&e.Y2)
	if e.Scale < 0.6 || e.Scale > 3.0 {
		if e.Scale == 0 {
			e.Scale = 1
		} else if e.Scale < 0.6 {
			e.Scale = 0.6
		} else {
			e.Scale = 3.0
		}
	}
	return true
}

func clampPct(v *float64) {
	if *v < 0 {
		*v = 0
	}
	if *v > 10000 {
		*v = 10000
	}
}

// keyDisplayName 按键展示名(KeyW→W、Digit1→1、Space→空格)。
func keyDisplayName(code string) string {
	switch {
	case len(code) == 4 && code[:3] == "Key":
		return code[3:]
	case len(code) == 6 && code[:5] == "Digit":
		return code[5:]
	}
	switch code {
	case "Space":
		return "空格"
	case "Backquote":
		return "`"
	case "Minus":
		return "-"
	case "Equal":
		return "="
	case "BracketLeft":
		return "["
	case "BracketRight":
		return "]"
	case "Backslash":
		return "\\"
	case "Semicolon":
		return ";"
	case "Quote":
		return "'"
	case "Comma":
		return ","
	case "Period":
		return "."
	case "Slash":
		return "/"
	}
	return code
}

// androidKeyLabel 系统键中文标签。
func androidKeyLabel(a string) string {
	switch a {
	case "back":
		return "返回"
	case "home":
		return "主页"
	case "recent":
		return "多任务"
	case "volup":
		return "音量+"
	case "voldown":
		return "音量-"
	}
	return a
}

// vkToJSCode Win32 VK → JS KeyboardEvent.code。
var vkToJSCode = buildVKToJSCode()

func buildVKToJSCode() map[int]string {
	m := make(map[int]string, 80)
	for i := 0; i < 26; i++ {
		m[0x41+i] = "Key" + string(rune('A'+i))
	}
	for i := 0; i < 10; i++ {
		m[0x30+i] = "Digit" + string(rune('0'+i))
	}
	named := map[int]string{
		0x20: "Space", 0x0D: "Enter", 0x1B: "Escape", 0x08: "Backspace",
		0x09: "Tab", 0x2E: "Delete", 0x2D: "Insert", 0x14: "CapsLock",
		0x21: "PageUp", 0x22: "PageDown", 0x23: "End", 0x24: "Home",
		0x25: "ArrowLeft", 0x26: "ArrowUp", 0x27: "ArrowRight", 0x28: "ArrowDown",
		0xBA: "Semicolon", 0xBB: "Equal", 0xBD: "Minus", 0xBE: "Period",
		0xBF: "Slash", 0xC0: "Backquote", 0xDB: "BracketLeft",
		0xDC: "Backslash", 0xDD: "BracketRight", 0xDE: "Quote", 0xBC: "Comma",
	}
	for vk, code := range named {
		m[vk] = code
	}
	for i := 0; i < 12; i++ {
		m[0x70+i] = "F" + strconv.Itoa(i+1)
	}
	for i := 0; i < 10; i++ {
		m[0x60+i] = "Numpad" + string(rune('0'+i))
	}
	m[0x6A] = "NumpadMultiply"
	m[0x6B] = "NumpadAdd"
	m[0x6D] = "NumpadSubtract"
	m[0x6E] = "NumpadDecimal"
	m[0x6F] = "NumpadDivide"
	return m
}

func vkToJSCodeName(vk int) string { return vkToJSCode[vk] }

// sdlScanToJSCode 反查表:SDL scancode → JS code(macOS 路径复用)。
var sdlScanToJSCode = buildScanToCode()

func buildScanToCode() map[uint16]string {
	m := make(map[uint16]string, len(jsCodeToSDLMap))
	for code, sc := range jsCodeToSDLMap {
		m[sc] = code
	}
	return m
}

// ─── 存储路径 ────────────────────────────────────────────────────────────────

// kmBaseDir 键位配置根目录:%LOCALAPPDATA%\MYT\player\keymap
func kmBaseDir() string {
	base := ""
	if v, ok := os.LookupEnv("LOCALAPPDATA"); ok && v != "" {
		base = v
	} else if v, ok := os.LookupEnv("TEMP"); ok && v != "" {
		base = v
	} else {
		base = "."
	}
	return base + string(os.PathSeparator) + "MYT" + string(os.PathSeparator) + "player" + string(os.PathSeparator) + "keymap"
}

// kmDeviceKey 当前设备的存储键:公网模式 web_<deviceID>,本地 RTSP 用 ip_vport。
func kmDeviceKey() string {
	if *flagWebURL != "" {
		if id := kmWebDeviceID(*flagWebURL); id != "" {
			return "web_" + id
		}
	}
	if *flagIP == "" {
		return "default"
	}
	if *flagVPort > 0 {
		return *flagIP + "_" + strconv.Itoa(*flagVPort)
	}
	return *flagIP
}

func kmWebDeviceID(weburl string) string {
	u, err := url.Parse(weburl)
	if err != nil {
		return ""
	}
	return u.Query().Get("device_id")
}
