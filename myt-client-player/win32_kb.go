//go:build windows

package main

import (
	"time"
	"unsafe"
)

func (ui *WindowsUI) handleMouseWheel(wParam, lParam uintptr) {
	app := ui.app

	// 键位编辑态优先:光标在覆盖窗矩形内时转发给标记缩放,不透传给设备。
	// 焦点在 WebView2(点过侧栏按钮)时 WM_MOUSEWHEEL 到不了这里——
	// 那条路由 keymap_overlay_wheel_hook.go 的低级钩子兜底,本分支与其共用
	// kmForwardWheelToOverlay(钩子先拦截并吞掉,不会双重缩放)。
	if ui.kmForwardWheelToOverlay(
		int32(int16(int32((wParam>>16)&0xFFFF))),
		int32(int16(lParam&0xFFFF)),
		int32(int16((lParam>>16)&0xFFFF))) {
		return
	}

	if app == nil || app.Ctrl.Load() == nil {
		return
	}
	if app.Renderer.Load() == nil && app.D3D11Ren.Load() == nil {
		return
	}

	var pt struct{ X, Y int32 }
	pt.X = int32(int16(lParam & 0xFFFF))
	pt.Y = int32(int16((lParam >> 16) & 0xFFFF))
	procScreenToClient.Call(uintptr(ui.hSdlArea), uintptr(unsafe.Pointer(&pt)))

	zDelta := int16(int32((wParam >> 16) & 0xFFFF))
	dir := int32(1)
	if zDelta < 0 {
		dir = -1
	}
	// Track consecutive wheel ticks
	ui.mu.Lock()
	now := time.Now()
	if ui.wheelDir != dir || now.Sub(ui.wheelLastTick) > 100*time.Millisecond {
		ui.wheelCount = 0
		ui.wheelDir = dir
	}
	ui.wheelLastTick = now
	ui.wheelCount++
	if ui.wheelTimer != nil {
		ui.wheelTimer.Stop()
	}

	count := ui.wheelCount
	wheelX, wheelY := pt.X, pt.Y
	ui.wheelTimer = time.AfterFunc(50*time.Millisecond, func() {
		ui.mu.Lock()
		ui.wheelTimer = nil
		ui.wheelCount = 0
		ui.mu.Unlock()

		ui.sendWheelGesture(wheelX, wheelY, dir, count)
	})
	ui.mu.Unlock()
}

// sendWheelGesture sends a simulated drag gesture for a wheel scroll event.
func (ui *WindowsUI) sendWheelGesture(wheelX, wheelY int32, dir int32, count int) {
	app := ui.app
	ctrl := app.Ctrl.Load()
	if app == nil || ctrl == nil {
		return
	}
	var scaler inputScaler
	if d3d := app.D3D11Ren.Load(); d3d != nil {
		scaler = d3d
	} else if ren := app.Renderer.Load(); ren != nil {
		scaler = ren
	}
	if scaler == nil {
		return
	}
	sx, sy, srcW, srcH, ok := scaler.ScaleMouseCoords(wheelX, wheelY)
	if !ok || srcW == 0 || srcH == 0 {
		return
	}

	// Distance: 80 + (count-1)*10 in source pixels (matching C++ original)
	dist := int32(80+(count-1)*10) * dir

	// In rotated (portrait) mode, the window's vertical axis maps to
	// the source X axis, so scroll along X instead of Y.
	var ex16, ey16 uint16
	if app.Rotated.Load() {
		toX := int32(sx) + dist
		if toX < 0 {
			toX = 0
		}
		if toX > srcW {
			toX = srcW
		}
		ex16 = uint16(toX)
		ey16 = uint16(sy)
	} else {
		toY := int32(sy) + dist
		if toY < 0 {
			toY = 0
		}
		if toY > srcH {
			toY = srcH
		}
		ex16 = uint16(sx)
		ey16 = uint16(toY)
	}

	sx16, sy16 := uint16(sx), uint16(sy)
	sw, sh := uint16(srcW), uint16(srcH)

	// Mouse down at start position
	(*ctrl).Send(BuildMouseKey(true, MouseLeft, sx16, sy16, sw, sh))
	// 3 interpolation motion steps with 10ms delays (faster than C++ 5x20ms)
	for i := 1; i <= 3; i++ {
		mx := sx16 + (ex16-sx16)*uint16(i)/3
		my := sy16 + (ey16-sy16)*uint16(i)/3
		(*ctrl).Send(BuildMouseMotion(1, 0, mx, my, 0, 0, sw, sh))
		time.Sleep(10 * time.Millisecond)
	}
	// Mouse up at end position
	(*ctrl).Send(BuildMouseKey(false, MouseLeft, ex16, ey16, sw, sh))
}

func (ui *WindowsUI) handleWin32Keyboard(msg uint32, wParam, lParam uintptr) {
	vk := int(wParam & 0xFFFF)
	pressed := msg == WM_KEYDOWN

	sym := vkToSDLSymWin(vk)
	scancode := uint16(vkToSDLScancodeWin(vk))
	mod := getWin32Mods()

	// 入口日志：记录所有带 Ctrl 的按键，定位 Ctrl+C 是否到达、mod/vk 值是否正确
	if pressed && mod&ModCtrl != 0 {
		logf("[kb] Ctrl组合键到达 msg=0x%x vk=0x%x('%c') mod=0x%x scancode=%d sym=%d\n", msg, vk, vk, mod, scancode, sym)
	}

	// Ctrl+C -> copy: 调设备 API cmd=3 复制屏幕选中文字到本地剪贴板。
	// 不再转发 Ctrl+C 按键给 Android（Android 不认 Ctrl+C=复制，转发无效）。
	if pressed && mod&ModCtrl != 0 && vk == 'C' {
		logf("[kb] Ctrl+C 分支命中 mod=0x%x vk=0x%x scancode=%d sym=%d\n", mod, vk, scancode, sym)
		go ui.ClipboardCopy()
		return
	}
	// Ctrl+X -> cut: 调设备 API cmd=4 剪切屏幕选中文字到本地剪贴板。
	if pressed && mod&ModCtrl != 0 && vk == 'X' {
		logf("[kb] Ctrl+X 分支命中 mod=0x%x vk=0x%x\n", mod, vk)
		go ui.ClipboardCut()
		return
	}
	// Ctrl+V -> paste: 读本地剪贴板，调设备 API cmd=5 粘贴到输入框。
	if pressed && mod&ModCtrl != 0 && vk == 'V' {
		logf("[kb] Ctrl+V 分支命中 mod=0x%x vk=0x%x\n", mod, vk)
		go func() {
			text := ui.ReadClipboardText()
			logf("[kb] Ctrl+V 本地剪贴板读到 len=%d text=%q\n", len(text), truncForLog(text, 80))
			if text != "" {
				ui.app.PasteClipboard(text)
			}
		}()
		return
	}
	// ── 键位映射:改键捕获优先——编辑器里点击了标记(或新增条目)后,
	// 下一按键直接绑到该条目,不透传给设备(雷电同款「点击就能改」)。
	// F9 是全局开关保留键,捕获态下跳过(维持开关语义不被绑走)。
	if pressed && ui.app.Keymap != nil && vk != 0 && vk != kmToggleVK {
		if code := vkToJSCodeName(vk); code != "" {
			ui.mu.Lock()
			capID, capSlot, capEditing := ui.kmCaptureID, ui.kmCaptureSlot, ui.kmEditing
			ui.mu.Unlock()
			if capID != "" && !capEditing {
				// 编辑会话已结束但捕获态残留(退出会话本应已清,兜底):
				// 清掉并放行——不能吞游戏按键。
				ui.mu.Lock()
				ui.kmCaptureID = ""
				ui.kmCaptureSlot = -1
				ui.mu.Unlock()
			} else if capID != "" {
				if code == "Escape" {
					ui.mu.Lock()
					ui.kmCaptureID = ""
					ui.kmCaptureSlot = -1
					ui.mu.Unlock()
				} else {
					ui.KeymapBindCode(code, capSlot)
					ui.repaintKeymapOverlay()
				}
				return
			}
		}
	}

	// ── 键位映射引擎:先于 funcKey/原始键透传。
	// F9 开关与命中的映射键在此消费;带 Ctrl/Alt 的按下由引擎内部直通
	// (保证 Ctrl+C/X/V 等系统组合键不受映射影响)。
	if ui.app.Keymap != nil && vk != 0 {
		if ui.app.Keymap.HandleVK(vk, pressed, mod) {
			return
		}
	}

	// Functional keys
	if pressed && mod&(ModCtrl|ModAlt) == 0 {
		keyName := vkToKeyName(vk)
		cmdID := funcKeyCmdID(keyName)
		if cmdID > 0 {
			ui.app.SendCmd(CmdFuncKey, uint32(cmdID))
			return
		}
	}
	// Escape -> Back
	if pressed && vk == 0x1B {
		ui.app.SendCmd(CmdAndroidKey, KeyBack)
		return
	}

	if sym == 0 && scancode == 0 {
		return
	}

	ui.app.SendKeyboard(pressed, scancode, int32(sym), 0, uint16(mod))
}

func (ui *WindowsUI) handleWin32Char(wParam uintptr) {
	ch := rune(wParam)
	if ch < 32 || ch == 127 {
		return
	}
	// 被映射消费的按键:吞掉字符,防止设备端重复出字(键映射已发触控)
	if ui.app.Keymap != nil && ui.app.Keymap.SuppressChar(ch) {
		return
	}
	text := string(ch)
	mod := getWin32Mods()
	if mod&(ModCtrl|ModAlt) != 0 {
		return
	}
	ui.app.SendText(text)
}

// vkToSDLSymWin/vkToSDLScancodeWin/vkToKeyName 已拆分到 win32_kb_map.go

func getWin32Mods() int {
	mod := 0
	ctrl, _, _ := procGetKeyState.Call(0x11) // VK_CONTROL
	if int16(ctrl) < 0 {
		mod |= ModCtrl
	}
	shift, _, _ := procGetKeyState.Call(0x10) // VK_SHIFT
	if int16(shift) < 0 {
		mod |= ModShift
	}
	alt, _, _ := procGetKeyState.Call(0x12) // VK_MENU
	if int16(alt) < 0 {
		mod |= ModAlt
	}
	return mod
}

