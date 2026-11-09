//go:build linux

package main

// ui_linux_input.go — Linux 输入事件处理：鼠标点击/移动/滚轮手势/键盘 SDL 发送。
// GDK 回调（ui_linux.go 尾部 //export）经本文件函数转换为设备控制消息
//（对照 ui_darwin_input.go 同语义；keyval 与 X keysym 同值域，linux_keys.go 映射表直接复用）。

/*
#cgo pkg-config: x11

#include <X11/Xlib.h>

// 本文件仅引用 Xlib 数值宏（Button1-3/ShiftMask/ControlMask/Mod1Mask，与 GDK
// 掩码同值域），无需 static helper；头文件由 pkg-config x11 的 -I 提供。
*/
import "C"

import (
	"os/exec"
	"strings"
	"time"
)

// ─── 输入处理（对照 ui_darwin_input.go 同语义）────────────────────────────

func (ui *LinuxUI) handleMouseClick(pressed bool, button int, x, y int) {
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
	case C.Button1:
		btn = MouseLeft
	case C.Button2:
		btn = MouseMiddle
	case C.Button3:
		btn = MouseRight
	default:
		return
	}
	if !pressed && btn == MouseRight {
		app.SendCmd(CmdAndroidKey, KeyBack)
		return
	}
	app.OnMouseEvent(buildMouseKeyClamped(pressed, btn, sx, sy, sw, sh, scaler))
}

// buildMouseKeyClamped mouse-up 时坐标 clamp 到视频边界（对照 DarwinUI 同款）。
func buildMouseKeyClamped(pressed bool, btn uint8, sx, sy uint16, sw, sh int32, scaler inputScaler) []byte {
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
	return BuildMouseKey(pressed, btn, sx16, sy16, sw16, sh16)
}

func (ui *LinuxUI) handleMotion(x, y int, buttonState int) {
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

func (ui *LinuxUI) queueWheelGesture(x, y int, dir int32) {
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

// sendWheelGesture 滚轮 → 模拟拖拽手势（对照 DarwinUI.sendWheelGesture 同构）。
func (ui *LinuxUI) sendWheelGesture(wheelX, wheelY int32, dir int32, count int) {
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

// xmodsToSDL X11 KeyButMask → SDL 修饰位。
func xmodsToSDL(state uint32) int {
	mod := 0
	if state&C.ShiftMask != 0 {
		mod |= ModShift
	}
	if state&C.ControlMask != 0 {
		mod |= ModCtrl
	}
	if state&(C.Mod1Mask) != 0 {
		mod |= ModAlt
	}
	return mod
}

// ─── 剪贴板（Linux 用 xclip，未安装时静默失败）────────────────────────────

func linuxWriteClipboard(text string) {
	cmd := exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
}
