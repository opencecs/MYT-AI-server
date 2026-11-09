package main

import (
	"encoding/binary"
	"sync"
	"unicode/utf8"
)

// ─── Wire protocol message types (big-endian, packed) ────────────────────────

const (
	MsgKeyboard    = 1
	MsgMouseKey    = 2
	MsgMouseMotion = 3
	MsgMouseWheel  = 4
	MsgKeyText     = 5
	MsgKeyCmd      = 6
	MsgMouseClick  = 7
	MsgPointsClick = 8
	MsgDevCmd      = 9
)

// Buffer pools for high-frequency messages (mouse events fire 100s of times/sec).
// Avoids per-event heap allocation and GC pressure.
var (
	pool20 = sync.Pool{New: func() any { buf := make([]byte, 20); return &buf }}
	pool32 = sync.Pool{New: func() any { buf := make([]byte, 32); return &buf }}
	pool66 = sync.Pool{New: func() any { buf := make([]byte, 66); return &buf }}
)

// Action constants (for PointsClick)
const (
	ActionDown        = 0
	ActionUp          = 1
	ActionMove        = 2
	ActionPointerDown = 5
	ActionPointerUp   = 6
)

// KeyCmd command values
const (
	CmdAndroidKey = 0x101
	CmdFuncKey    = 0x102
)

// Android key parameters
const (
	KeyBack     = 5
	KeyHome     = 6
	KeyVolUp    = 8
	KeyVolDown  = 9
	KeyRecent   = 11
	KeySnapshot = 12
)

// FuncKey parameters
const (
	FuncBackspace = 1
	FuncEnter     = 2
	FuncRight     = 3
	FuncLeft      = 4
	FuncUp        = 5
	FuncDown      = 6
	FuncTab       = 7
)

// Mouse buttons
const (
	MouseLeft   = 1
	MouseMiddle = 2
	MouseRight  = 3
	MouseWheelUp = 4
	MouseWheelDown = 5
)

// SDL modifier bits
const (
	ModShift = 0x0001
	ModCtrl  = 0x0040
	ModAlt   = 0x0100
)

// ─── Message builders ─────────────────────────────────────────────────────────

// BuildKeyboard creates a keyboard event message (18 bytes wire, padded to 20 for pool reuse).
func BuildKeyboard(pressed bool, scancode uint16, sdlkey int32, unicode uint32, sdlmod uint16) []byte {
	buf := *pool20.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 18)
	buf[2] = MsgKeyboard
	buf[3] = 0
	if pressed {
		buf[4] = 1
	} else {
		buf[4] = 0
	}
	buf[5] = 0
	binary.BigEndian.PutUint16(buf[6:], scancode)
	binary.BigEndian.PutUint32(buf[8:], uint32(sdlkey))
	binary.BigEndian.PutUint32(buf[12:], unicode)
	binary.BigEndian.PutUint16(buf[16:], sdlmod)
	buf[18] = 0
	buf[19] = 0
	return buf
}

// BuildMouseKey creates a mouse button event message (20 bytes).
func BuildMouseKey(pressed bool, button uint8, x, y, screenW, screenH uint16) []byte {
	buf := *pool20.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 20)
	buf[2] = MsgMouseKey
	if pressed {
		buf[4] = 1
	} else {
		buf[4] = 0
	}
	buf[5] = button
	buf[6] = 0
	buf[7] = 0
	binary.BigEndian.PutUint16(buf[8:], x)
	binary.BigEndian.PutUint16(buf[10:], y)
	binary.BigEndian.PutUint16(buf[12:], 0)
	binary.BigEndian.PutUint16(buf[14:], 0)
	binary.BigEndian.PutUint16(buf[16:], screenW)
	binary.BigEndian.PutUint16(buf[18:], screenH)
	return buf
}

// BuildMouseMotion creates a mouse motion event message (20 bytes).
// C struct layout: offset 6 = mousestate, offset 7 = relativeMouseMode
func BuildMouseMotion(state uint8, relativeMode uint8, x, y, relX, relY, screenW, screenH uint16) []byte {
	buf := *pool20.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 20)
	buf[2] = MsgMouseMotion
	buf[3] = 0
	buf[4] = 0
	buf[5] = 0
	buf[6] = state
	buf[7] = relativeMode
	binary.BigEndian.PutUint16(buf[8:], x)
	binary.BigEndian.PutUint16(buf[10:], y)
	binary.BigEndian.PutUint16(buf[12:], relX)
	binary.BigEndian.PutUint16(buf[14:], relY)
	binary.BigEndian.PutUint16(buf[16:], screenW)
	binary.BigEndian.PutUint16(buf[18:], screenH)
	return buf
}

// BuildMouseWheel creates a mouse wheel event message (20 bytes).
func BuildMouseWheel(deltaX, deltaY, screenW, screenH uint16) []byte {
	buf := *pool20.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 20)
	buf[2] = MsgMouseWheel
	buf[3] = 0
	buf[4] = 0
	buf[5] = 0
	buf[6] = 0
	buf[7] = 0
	binary.BigEndian.PutUint16(buf[8:], deltaX)
	binary.BigEndian.PutUint16(buf[10:], deltaY)
	binary.BigEndian.PutUint16(buf[12:], 0)
	binary.BigEndian.PutUint16(buf[14:], 0)
	// Note: C++ original does NOT set screenW/screenH for wheel events (offsets 16-19 stay 0)
	binary.BigEndian.PutUint16(buf[16:], 0)
	binary.BigEndian.PutUint16(buf[18:], 0)
	return buf
}

// BuildKeyText creates a text input event message (66 bytes).
func BuildKeyText(text string) []byte {
	if len(text) > 59 {
		// Truncate at rune boundary to avoid splitting multi-byte UTF-8
		for len(text) > 59 {
			r, size := utf8.DecodeLastRuneInString(text)
			if r == utf8.RuneError {
				break
			}
			text = text[:len(text)-size]
		}
		if len(text) > 59 {
			text = text[:59]
		}
	}
	buf := *pool66.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 66)
	buf[2] = MsgKeyText
	buf[3] = 0
	binary.BigEndian.PutUint16(buf[4:], uint16(len(text)))
	copy(buf[6:], text)
	return buf
}

// BuildKeyCmd creates a command event message (12 bytes wire, padded to 20 for pool reuse).
func BuildKeyCmd(cmd, para uint32) []byte {
	buf := *pool20.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 12)
	buf[2] = MsgKeyCmd
	buf[3] = 0
	binary.BigEndian.PutUint32(buf[4:], cmd)
	binary.BigEndian.PutUint32(buf[8:], para)
	buf[12] = 0
	buf[13] = 0
	buf[14] = 0
	buf[15] = 0
	buf[16] = 0
	buf[17] = 0
	buf[18] = 0
	buf[19] = 0
	return buf
}

// BuildMouseClick creates a double-click event message (20 bytes).
func BuildMouseClick(button uint8, x, y, screenW, screenH uint16) []byte {
	buf := *pool20.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 20)
	buf[2] = MsgMouseClick
	buf[3] = 0
	buf[4] = 1 // double-click flag
	buf[5] = button
	buf[6] = 0
	buf[7] = 0
	binary.BigEndian.PutUint16(buf[8:], x)
	binary.BigEndian.PutUint16(buf[10:], y)
	binary.BigEndian.PutUint16(buf[12:], 0)
	binary.BigEndian.PutUint16(buf[14:], 0)
	binary.BigEndian.PutUint16(buf[16:], screenW)
	binary.BigEndian.PutUint16(buf[18:], screenH)
	return buf
}

// BuildPointsClick creates a multi-touch event message (32 bytes).
func BuildPointsClick(points int, x, y [5]uint16, action uint16, screenW, screenH uint16) []byte {
	buf := *pool32.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 32)
	buf[2] = MsgPointsClick
	binary.BigEndian.PutUint16(buf[4:], uint16(points))
	for i := 0; i < 5; i++ {
		binary.BigEndian.PutUint16(buf[6+i*2:], x[i])
		binary.BigEndian.PutUint16(buf[16+i*2:], y[i])
	}
	binary.BigEndian.PutUint16(buf[26:], action)
	binary.BigEndian.PutUint16(buf[28:], screenW)
	binary.BigEndian.PutUint16(buf[30:], screenH)
	return buf
}

// BuildDevCmd creates a device command event message (20 bytes).
func BuildDevCmd(cmd, arg1, arg2, arg3 uint32) []byte {
	buf := *pool20.Get().(*[]byte)
	binary.BigEndian.PutUint16(buf[0:], 20)
	buf[2] = MsgDevCmd
	buf[3] = 0
	binary.BigEndian.PutUint32(buf[4:], cmd)
	binary.BigEndian.PutUint32(buf[8:], arg1)
	binary.BigEndian.PutUint32(buf[12:], arg2)
	binary.BigEndian.PutUint32(buf[16:], arg3)
	return buf
}
