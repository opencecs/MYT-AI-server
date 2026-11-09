package main

// keymap_gesture.go — 键位映射手势实现:单指点按、定向滑动、虚拟摇杆、系统键。
//
// 全部走已真机验证的 MsgMouseKey / MsgMouseMotion 路径(与滚轮手势同构),
// 设备端零改动。坐标:万分比 → 源像素 → BuildMouseKey/Motion。

import "time"

// sendMouse 单指按下/抬起。
func (e *KeymapEngine) sendMouse(down bool, x, y uint16, sw, sh uint16) {
	e.app.OnMouseEvent(BuildMouseKey(down, MouseLeft, x, y, sw, sh))
}

// sendMove 按下状态的移动事件。
func (e *KeymapEngine) sendMove(x, y uint16, sw, sh uint16) {
	e.app.OnMouseEvent(BuildMouseMotion(1, 0, x, y, 0, 0, sw, sh))
}

// gestureTap 点击:hold=按住跟随(技能长按),click=按下即自动短点(UI 点击)。
func (e *KeymapEngine) gestureTap(en KeymapEntry, pressed bool) {
	w, h, ok := e.srcSize()
	if !ok {
		return
	}
	x, y := en.pixels(w, h)
	if pressed {
		e.sendMouse(true, x, y, uint16(w), uint16(h))
		if en.TapMode == "click" {
			time.Sleep(20 * time.Millisecond)
			e.sendMouse(false, x, y, uint16(w), uint16(h))
		}
		return
	}
	if en.TapMode == "hold" {
		e.sendMouse(false, x, y, uint16(w), uint16(h))
	}
}

// gestureSwipe 定向滑动:按下 down@A,松开时 3 步插值滑到 B 再抬起(放技能)。
func (e *KeymapEngine) gestureSwipe(en KeymapEntry, pressed bool) {
	w, h, ok := e.srcSize()
	if !ok {
		return
	}
	ax, ay := pxOf(en.X, w), pxOf(en.Y, h)
	bx, by := pxOf(en.X2, w), pxOf(en.Y2, h)
	if pressed {
		e.sendMouse(true, ax, ay, uint16(w), uint16(h))
		return
	}
	for i := 1; i <= 3; i++ {
		e.sendMove(ax+(bx-ax)*uint16(i)/3, ay+(by-ay)*uint16(i)/3, uint16(w), uint16(h))
		time.Sleep(10 * time.Millisecond)
	}
	e.sendMouse(false, bx, by, uint16(w), uint16(h))
}

// dpadStep 虚拟摇杆状态机(dpadMu 串行化,防双键竞态):
//   - 首键按下:down @ 圆心+半径×方向(触点直接落在偏移位,游戏摇杆即识别方向)
//   - 方向变化(W→WD):2 步插值滑向新方向(10ms/步,与滚轮手势同节奏)
//   - 全部松开:up @ 最近触点
func (e *KeymapEngine) dpadStep(en KeymapEntry, pressed bool) {
	e.dpadMu.Lock()
	defer e.dpadMu.Unlock()

	w, h, ok := e.srcSize()
	if !ok {
		return
	}
	rPx := en.R / kmRuneScale * float64(min32(w, h))
	cx, cy := float64(pxOf(en.X, w)), float64(pxOf(en.Y, h))

	e.mu.Lock()
	run := e.dpadRun[en.ID]
	if run == nil {
		run = &dpadRun{}
		e.dpadRun[en.ID] = run
	}
	dirX, dirY, any := e.dpadDirLocked(&en)
	e.mu.Unlock()

	sw, sh := uint16(w), uint16(h)
	if !any {
		// 全部方向键已松开 → 抬起
		if run.down {
			run.down = false
			e.sendMouse(false, uint16(run.lastX), uint16(run.lastY), sw, sh)
		}
		return
	}

	// 目标点 = 圆心 + 归一化方向 × 半径(斜向自动 45°,业界八方向摇杆语义)
	length := sqrtF(float64(dirX*dirX + dirY*dirY))
	var tx, ty float64
	if length == 0 {
		// 对向键同按(A+D/W+S)相互抵消 → 触点回圆心(中立,保持按下)。
		// 不能除零:NaN 经 uint16() 转换是未定义值,触点会跳飞。
		tx, ty = cx, cy
	} else {
		tx = cx + float64(dirX)/length*rPx
		ty = cy + float64(dirY)/length*rPx
	}

	if !run.down {
		run.down = true
		run.lastX, run.lastY = tx, ty
		e.sendMouse(true, uint16(tx), uint16(ty), sw, sh)
		return
	}
	// 方向变化:2 步插值滑向新方向
	lx, ly := run.lastX, run.lastY
	for i := 1; i <= 2; i++ {
		e.sendMove(uint16(lx+(tx-lx)*float64(i)/2), uint16(ly+(ty-ly)*float64(i)/2), sw, sh)
		time.Sleep(10 * time.Millisecond)
	}
	run.lastX, run.lastY = tx, ty
}

// dpadDirLocked 当前按下的方向向量(±1 组合;调用方须持 e.mu)。
// Keys 固定顺序 [上,左,下,右]。
func (e *KeymapEngine) dpadDirLocked(en *KeymapEntry) (int, int, bool) {
	if en.Type != kmTypeDpad {
		return 0, 0, false
	}
	var dx, dy int
	any := false
	for idx, code := range en.Keys {
		if !e.pressed[code] {
			continue
		}
		any = true
		switch idx {
		case 0:
			dy -= 1 // 上
		case 1:
			dx -= 1 // 左
		case 2:
			dy += 1 // 下
		case 3:
			dx += 1 // 右
		}
	}
	return dx, dy, any
}

// gestureAndroid 系统键:按下时发 Android 系统键命令。
func (e *KeymapEngine) gestureAndroid(en KeymapEntry) {
	var para uint32
	switch en.Android {
	case "back":
		para = KeyBack
	case "home":
		para = KeyHome
	case "recent":
		para = KeyRecent
	case "volup":
		para = KeyVolUp
	case "voldown":
		para = KeyVolDown
	default:
		return
	}
	e.app.SendCmd(CmdAndroidKey, para)
}

// runGesture 手势分发(tap/swipe 异步执行,android 同步发)。
func (e *KeymapEngine) runGesture(en KeymapEntry, pressed bool) {
	switch en.Type {
	case kmTypeTap:
		e.gestureTap(en, pressed)
	case kmTypeSwipe:
		e.gestureSwipe(en, pressed)
	case kmTypeAndroid:
		if pressed {
			e.gestureAndroid(en)
		}
	}
}

// releaseAll 抬起全部活动触点(断连/切预设防粘滞)。
func (e *KeymapEngine) releaseAll() {
	e.mu.Lock()
	runs := make([]*dpadRun, 0, len(e.dpadRun))
	for _, r := range e.dpadRun {
		runs = append(runs, r)
	}
	e.mu.Unlock()
	w, h, ok := e.srcSize()
	for _, r := range runs {
		if r.down {
			r.down = false
			if ok {
				e.sendMouse(false, uint16(r.lastX), uint16(r.lastY), uint16(w), uint16(h))
			}
		}
	}
}

// ─── 工具函数 ────────────────────────────────────────────────────────────────

// entryHasCode 条目是否绑定某键(dpad 匹配四键,其余匹配首键)。
func entryHasCode(en *KeymapEntry, code string) bool {
	if len(en.Keys) == 0 {
		return false
	}
	if en.Type == kmTypeDpad {
		for _, k := range en.Keys {
			if k == code {
				return true
			}
		}
		return false
	}
	return en.Keys[0] == code
}

// pixels 万分比坐标 → 源像素。
func (en *KeymapEntry) pixels(w, h int32) (uint16, uint16) {
	return pxOf(en.X, w), pxOf(en.Y, h)
}

// pxOf 万分比 → 源像素(带边界钳制)。
func pxOf(pct float64, size int32) uint16 {
	v := int32(pct / kmRuneScale * float64(size))
	if v < 0 {
		v = 0
	}
	if v > size-1 {
		v = size - 1
	}
	return uint16(v)
}

// jsCodeToVK JS code → Win32 VK(吞 WM_CHAR 用)。
func jsCodeToVK(code string) int {
	for vk, c := range vkToJSCode {
		if c == code {
			return vk
		}
	}
	return 0
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func sqrtF(v float64) float64 {
	// 牛顿迭代开方,避免引入 math 依赖(此路径每按键一次,性能无关紧要)
	if v <= 0 {
		return 0
	}
	x := v
	for i := 0; i < 24; i++ {
		x = (x + v/x) / 2
	}
	return x
}
