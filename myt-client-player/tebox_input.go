package main

import (
	"encoding/binary"
	"sync/atomic"

	"go_legacy/teboxcast"
)

// controlSink 控制事件出口抽象：RTSP 模式为 ControlChannel（legacy 二进制协议直发），
// tebox 模式为 TeboxInput（把 legacy 消息翻译成 tebox JSON 事件）。App.Ctrl 持有。
type controlSink interface {
	Send(msg []byte)
	Close()
}

// tebox 模式用 SDL scancode（USB HID usage，平台无关）映射 Android 键码。
// 不引用 keys_sdl.go 的 SC_*：tebox 仅需十几个键码，独立 tbScan* 常量便于对照
// 上游协议文档逐值核对。
const (
	tbScanEscape = 41
	tbScanBacksp = 42
	tbScanTab    = 43
	tbScanEnter  = 40
	tbScanLeft   = 80
	tbScanRight  = 79
	tbScanUp     = 82
	tbScanDown   = 81
	tbScanHome   = 74
	tbScanEnd    = 77
	tbScanPgUp   = 75
	tbScanPgDn   = 78
	tbScanDelete = 76
	tbScanF1     = 58
	tbScanF2     = 59
	tbScanF5     = 62
	tbScanA      = 4 // 字母 A（4..29 = A..Z）
)

// Android KEYCODE_*（标准 Android 键码值）
const (
	kcDpadUp     = 19
	kcDpadDown   = 20
	kcDpadLeft   = 21
	kcDpadRight  = 22
	kcA          = 29
	kcTab        = 61
	kcPageUp     = 92
	kcPageDown   = 93
	kcBackspace  = 67
	kcEnter      = 66
	kcMoveHome   = 122
	kcMoveEnd    = 123
	kcForwardDel = 112
	kcAppSwitch  = 187
	metaShift    = 0x41   // META_SHIFT_ON | META_SHIFT_LEFT_ON
	metaCtrl     = 0x3000 // META_CTRL_ON | META_CTRL_LEFT_ON
)

// TeboxInput tebox 控制事件适配器：实现 controlSink，把既有输入路径产出的
// legacy 二进制消息（event.go）反向解析后翻译成 tebox UDP JSON 事件。
// 好处：所有输入调用点（鼠标/键盘/键位映射/侧边栏按钮/文本输入）零改动。
type TeboxInput struct {
	ctrl *teboxcast.Control

	closed atomic.Bool
}

// NewTeboxInput 创建 tebox 输入适配器并启动控制通道。
func NewTeboxInput(host string, ctrlPort int, instanceID string) (*TeboxInput, error) {
	c := teboxcast.NewControl(host, ctrlPort, instanceID)
	c.Logf = logf
	t := &TeboxInput{ctrl: c}
	if err := c.Start(); err != nil {
		return nil, err
	}
	return t, nil
}

// Close 停止控制通道（幂等）。
func (t *TeboxInput) Close() {
	if t.closed.CompareAndSwap(false, true) {
		t.ctrl.Close()
	}
}

// PushClipboard 本机剪贴板 → guest。
// paste=true 时同时粘贴（Ctrl+V 语义）。
func (t *TeboxInput) PushClipboard(text string, paste bool) {
	t.ctrl.Clipboard(text, paste)
}

// Send 实现 controlSink：解析 legacy 二进制消息并翻译为 tebox 事件。
func (t *TeboxInput) Send(msg []byte) {
	if t.closed.Load() || len(msg) < 4 {
		return
	}
	wireLen := int(binary.BigEndian.Uint16(msg))
	if wireLen < 4 || wireLen > len(msg) {
		return
	}
	switch msg[2] {
	case MsgMouseKey:
		t.onMouseKey(msg)
	case MsgMouseMotion:
		t.onMouseMotion(msg)
	case MsgMouseClick:
		t.onMouseClick(msg)
	case MsgMouseWheel:
		// tebox 协议无滚轮事件，阶段一忽略（日志见 onMouseWheel）
	case MsgKeyboard:
		t.onKeyboard(msg)
	case MsgKeyText:
		t.onText(msg)
	case MsgKeyCmd:
		t.onKeyCmd(msg)
	}
}

// mouseCoords 解析 legacy 鼠标消息坐标（BE16: x@8 y@10 w@16 h@18），返回 0–1 比例。
func mouseCoords(msg []byte) (x, y float64, ok bool) {
	w := int(binary.BigEndian.Uint16(msg[16:]))
	h := int(binary.BigEndian.Uint16(msg[18:]))
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return float64(binary.BigEndian.Uint16(msg[8:])) / float64(w),
		float64(binary.BigEndian.Uint16(msg[10:])) / float64(h), true
}

func (t *TeboxInput) onMouseKey(msg []byte) {
	pressed := msg[4] == 1
	button := msg[5]
	x, y, ok := mouseCoords(msg)
	if !ok {
		return
	}
	switch button {
	case MouseLeft:
		if pressed {
			t.ctrl.Touch("down", x, y)
		} else {
			t.ctrl.Touch("up", x, y) // Control 内部连发两次
		}
	case MouseRight:
		// cast_view.c：右键 = back（按下沿触发一次）
		if pressed {
			t.ctrl.Key("back")
		}
	default:
		// 中键等无对应 tebox 事件，忽略
	}
}

func (t *TeboxInput) onMouseMotion(msg []byte) {
	if msg[6] == 0 {
		return // 无按键按下时的悬停移动，不是拖动
	}
	x, y, ok := mouseCoords(msg)
	if ok {
		t.ctrl.Touch("move", x, y)
	}
}

func (t *TeboxInput) onMouseClick(msg []byte) {
	// 双击 = 两次快速点按（touch down/up 由服务端合成 tap）
	x, y, ok := mouseCoords(msg)
	if !ok {
		return
	}
	t.ctrl.Touch("down", x, y)
	t.ctrl.Touch("up", x, y)
	t.ctrl.Touch("down", x, y)
	t.ctrl.Touch("up", x, y)
}

// onKeyboard legacy 键盘消息 → tebox 按键/键码。
// 可打印字符经 WM_CHAR → SendText（tebox text 事件）输入，这里只翻译
// 特殊键（导航/编辑键），避免双重出字。
func (t *TeboxInput) onKeyboard(msg []byte) {
	pressed := msg[4] == 1
	if !pressed {
		return // tebox keycode 用 press 语义，按下沿触发即可
	}
	sc := binary.BigEndian.Uint16(msg[6:])
	mod := binary.BigEndian.Uint16(msg[16:])

	// Ctrl+字母 → Android Ctrl+key（cast_view.c 快捷键同款）
	if mod&ModCtrl != 0 && sc >= tbScanA && sc <= tbScanA+25 {
		meta := metaCtrl
		if mod&ModShift != 0 {
			meta |= metaShift
		}
		t.ctrl.Keycode(kcA+int(sc-tbScanA), meta)
		return
	}
	// 导航键（cast_view.c nav_key_for 同款）
	switch sc {
	case tbScanEscape:
		t.ctrl.Key("back")
	case tbScanF1:
		t.ctrl.Key("home")
	case tbScanF2:
		// menu 不走 key:"menu"（QEMU qcode menu → KEYCODE_MENU，Android 3.0+ 上
		// 无菜单栏应用中无反应）；实测 keycode 187（APP_SWITCH）弹出最近任务
		// （与侧边栏 KeyRecent 同语义），改发 keycode。
		t.ctrl.Keycode(kcAppSwitch, 0)
	case tbScanF5:
		t.ctrl.Key("power")
	case tbScanPgUp:
		t.ctrl.Key("volume_up")
	case tbScanPgDn:
		t.ctrl.Key("volume_down")
	default:
		// 编辑键（cast_view.c android_keycode_for 同款）
		if code, ok := keycodeForScan(sc); ok {
			meta := 0
			if mod&ModShift != 0 {
				meta = metaShift
			}
			t.ctrl.Keycode(code, meta)
		}
		// 其余（字母/数字等）由 text 路径处理，此处丢弃
	}
}

// keycodeForScan SDL scancode → Android KEYCODE_*。
func keycodeForScan(sc uint16) (int, bool) {
	switch sc {
	case tbScanBacksp:
		return kcBackspace, true
	case tbScanEnter:
		return kcEnter, true
	case tbScanTab:
		return kcTab, true
	case tbScanUp:
		return kcDpadUp, true
	case tbScanDown:
		return kcDpadDown, true
	case tbScanLeft:
		return kcDpadLeft, true
	case tbScanRight:
		return kcDpadRight, true
	case tbScanHome:
		return kcMoveHome, true
	case tbScanEnd:
		return kcMoveEnd, true
	case tbScanDelete:
		return kcForwardDel, true
	}
	return 0, false
}

// onText legacy 文本消息 → tebox text 事件（SendText 已按 rune 边界分段）。
func (t *TeboxInput) onText(msg []byte) {
	n := int(binary.BigEndian.Uint16(msg[4:]))
	if n <= 0 || 6+n > len(msg) {
		return
	}
	t.ctrl.Text(string(msg[6 : 6+n]))
}

// onKeyCmd legacy 命令消息（侧边栏按钮/功能键）→ tebox key/keycode。
func (t *TeboxInput) onKeyCmd(msg []byte) {
	cmd := binary.BigEndian.Uint32(msg[4:])
	para := binary.BigEndian.Uint32(msg[8:])
	switch cmd {
	case CmdAndroidKey:
		switch para {
		case KeyBack:
			t.ctrl.Key("back")
		case KeyHome:
			t.ctrl.Key("home")
		case KeyVolUp:
			t.ctrl.Key("volume_up")
		case KeyVolDown:
			t.ctrl.Key("volume_down")
		case KeyRecent:
			t.ctrl.Keycode(kcAppSwitch, 0) // Android APP_SWITCH
		case KeySnapshot:
			// tebox 无截图键，忽略
		}
	case CmdFuncKey:
		switch para {
		case FuncBackspace:
			t.ctrl.Keycode(kcBackspace, 0)
		case FuncEnter:
			t.ctrl.Keycode(kcEnter, 0)
		case FuncRight:
			t.ctrl.Keycode(kcDpadRight, 0)
		case FuncLeft:
			t.ctrl.Keycode(kcDpadLeft, 0)
		case FuncUp:
			t.ctrl.Keycode(kcDpadUp, 0)
		case FuncDown:
			t.ctrl.Keycode(kcDpadDown, 0)
		case FuncTab:
			t.ctrl.Keycode(kcTab, 0)
		}
	}
}

// PasteClipboard pushes text into the guest: tebox uses clipboard+paste events (one-way push, no echo-back),
// RTSP 群控（有扇出目标）走控制通道文本广播，其余保持原设备 API cmd=5 路径。
func (a *App) PasteClipboard(text string) {
	if a.CastMode == CastModeTebox {
		if in := a.teboxIn.Load(); in != nil {
			in.PushClipboard(text, true)
		}
		return
	}
	// 群控：cmd=5 是 HTTP 单播，只到本窗口设备，被控设备收不到（复制后不能批量粘贴的根因）。
	// 改走 ControlChannel 的 SendText —— 与键盘/文本同一条扇出路径，逐目标送达。
	if a.hasBroadcastTargets() {
		a.SendText(text)
		return
	}
	a.pasteText(text)
}

// hasBroadcastTargets 本窗口是否有扇出目标（群控主控窗口 -list 非空）。
// 有目标时输入应走 ControlChannel 广播，而不是设备 API 单播。
func (a *App) hasBroadcastTargets() bool {
	if ctrl := a.Ctrl.Load(); ctrl != nil {
		if cc, ok := (*ctrl).(*ControlChannel); ok {
			return cc.TargetCount() > 1
		}
	}
	return false
}
