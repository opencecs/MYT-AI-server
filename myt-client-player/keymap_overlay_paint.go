//go:build windows

package main

// keymap_overlay_paint.go — 键位编辑覆盖层的绘制、几何换算与鼠标交互。
// 视觉规范沿用侧边栏设计 token:accent 蓝 #4A90D9、暗底 #23232E、浅描边 #E8E8F0。

import (
	"encoding/json"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// COLORREF 取值(GDI 为 0x00BBGGRR)。
const (
	kmColFill   = 0x2E2323 // 标记底 #23232E
	kmColBorder = 0xF0E8E8 // 常态描边 #E8E8F0
	kmColAccent = 0xD9904A // 选中描边 #4A90D9(COLORREF=BBGGRR)
	kmColText   = 0xFFFFFF // 白
	kmColHintTx = 0xC8B8B8 // 提示文字 #B8B8C8
	kmColHintBg = 0x1A1414 // 提示条底 #14141A

	// kmShowFlag SW_SHOWNOACTIVATE(4):显示但不抢焦点
	kmShowFlag = 4
)

// createKeymapOverlay 注册窗口类并创建隐藏覆盖窗(createWindows 调用,UI 线程)。
func (ui *WindowsUI) createKeymapOverlay(hInst, hCursor uintptr) {
	kmOverlayProcCB = windows.NewCallback(ui.keymapOvlWndProc)
	registerClass("LgPlayerKeymapOvl", kmOverlayProcCB, hInst, 0, 0, 0, 0)
	h, _, _ := procCreateWindowExW.Call(
		WS_EX_TOOLWINDOW|WS_EX_LAYERED|WS_EX_NOACTIVATE,
		uintptr(unsafe.Pointer(func() *uint16 {
			p, _ := windows.UTF16PtrFromString("LgPlayerKeymapOvl")
			return p
		}())), 0, WS_POPUP, 0, 0, 100, 100,
		uintptr(ui.hParent), 0, hInst, 0)
	ui.hKMOverlay = windows.HWND(h)
	// 色键透明:RGB(1,0,0) 的像素全透明,其余不透明(视频可见,标记悬浮)
	procSetLayeredWindowAttributes.Call(h, kmTransColor, 0, LWA_COLORKEY)
}

// showKeymapOverlay 定位到视频区屏幕矩形并显示。
func (ui *WindowsUI) showKeymapOverlay() {
	if ui.hKMOverlay == 0 {
		return
	}
	ui.setKeymapOverlayPassthrough(false) // 编辑态需要鼠标交互(选中/拖动/删除)
	ui.syncKeymapOverlayGeom()
	procShowWindow.Call(uintptr(ui.hKMOverlay), kmShowFlag)
	procInvalidateRect.Call(uintptr(ui.hKMOverlay), 0, 1)
}

// syncKeymapOverlayGeom 覆盖窗贴齐 sdlArea 客户区(屏幕坐标)。
// 泵(TIMER_SDL_PUMP)每 16ms 无条件调用——几何没变时零开销(只做比较),
// 变化时才 SetWindowPos + 全量重绘(layered 窗口 resize 后旧表面内容保留,
// 系统只 invalidate 新暴露区,不主动刷会留旧位置标记残影)。
func (ui *WindowsUI) syncKeymapOverlayGeom() {
	if ui.hKMOverlay == 0 || ui.hSdlArea == 0 {
		return
	}
	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(uintptr(ui.hSdlArea), uintptr(unsafe.Pointer(&rc)))
	var pt struct{ X, Y int32 }
	procClientToScreen.Call(uintptr(ui.hSdlArea), uintptr(unsafe.Pointer(&pt)))
	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top
	if w <= 0 || h <= 0 {
		return
	}
	x, y := pt.X, pt.Y
	if x == ui.kmOvlLastX && y == ui.kmOvlLastY && w == ui.kmOvlLastW && h == ui.kmOvlLastH {
		return
	}
	ui.kmOvlLastX, ui.kmOvlLastY, ui.kmOvlLastW, ui.kmOvlLastH = x, y, w, h
	procSetWindowPos.Call(uintptr(ui.hKMOverlay), uintptr(HWND_TOP),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		SWP_SHOWWINDOW|SWP_NOACTIVATE)
	procInvalidateRect.Call(uintptr(ui.hKMOverlay), 0, 1)
}

// repositionKeymapOverlay 窗口移动/缩放时跟随(高频 TIMER_SDL_PUMP + WM_SIZE/WM_MOVE)。
// 高频路径只幂等同步几何(SetWindowPos 同位置被系统优化,不触发重绘);
// 显隐翻转收敛在 syncKeymapOverlayVis(状态变化点),这里不做 ShowWindow。
func (ui *WindowsUI) repositionKeymapOverlay() {
	ui.mu.Lock()
	editing := ui.kmEditing
	ui.mu.Unlock()
	if editing || ui.app.Keymap.uiHintsLocked() {
		ui.syncKeymapOverlayGeom()
	}
}

// setKeymapOverlayPassthrough 切换覆盖窗点击穿透。
// LWA_COLORKEY 只做视觉透明,色键像素仍拦截鼠标——运行态标记若不穿透,
// 覆盖窗会吞掉整个视频区的点击(游戏没法玩)。编辑态需要交互,必须接收。
func (ui *WindowsUI) setKeymapOverlayPassthrough(passthrough bool) {
	if ui.hKMOverlay == 0 {
		logf("[km-ovl] passthrough=%v 跳过: hKMOverlay=0\n", passthrough)
		return
	}
	exStyle, _, _ := procGetWindowLongW.Call(uintptr(ui.hKMOverlay), GWL_EXSTYLE)
	if exStyle == 0 {
		logf("[km-ovl] passthrough=%v 跳过: GetWindowLongW 返回 0 (hwnd=0x%X)\n", passthrough, uintptr(ui.hKMOverlay))
		return // 读取失败(bail):0 会把 exstyle 算成只剩 TRANSPARENT,抹掉 LAYERED
	}
	want := exStyle
	if passthrough {
		want = exStyle | uintptr(WS_EX_TRANSPARENT)
	} else {
		want = exStyle & ^uintptr(WS_EX_TRANSPARENT)
	}
	logf("[km-ovl] passthrough=%v hwnd=0x%X ex=0x%X want=0x%X 需变更=%v\n",
		passthrough, uintptr(ui.hKMOverlay), exStyle, want, want != exStyle)
	if want != exStyle {
		prev, _, _ := procSetWindowLongW.Call(uintptr(ui.hKMOverlay), GWL_EXSTYLE, want)
		// SWP_FRAMECHANGED 强制系统重读 exstyle 的 hit-test 语义——
		// 已显示的 layered 窗口改 WS_EX_TRANSPARENT 不刷新则穿透不生效
		// (表现为运行态仍吞点击 + 光标停在覆盖窗的十字上)。
		procSetWindowPos.Call(uintptr(ui.hKMOverlay), 0, 0, 0, 0, 0,
			SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_FRAMECHANGED)
		logf("[km-ovl] SetWindowLongW 返回旧值=0x%X\n", prev)
	}
}

// syncKeymapOverlayVis 覆盖层显隐总控:
//   - 编辑态:显示(编辑样式,含 ×/缩放手柄/提示条),接收鼠标
//   - 运行态:引擎启用且按键提醒开 → 显示(轻量键帽标记),点击穿透
//   - 其余:隐藏
// F9 切换、侧栏开关、编辑进退均经此收敛,避免多处直接 ShowWindow 漏态。
func (ui *WindowsUI) syncKeymapOverlayVis() {
	if ui.hKMOverlay == 0 {
		logf("[km-ovl] sync 跳过: hKMOverlay=0\n")
		return
	}
	ui.mu.Lock()
	editing := ui.kmEditing
	ui.mu.Unlock()
	hints := ui.app.Keymap.uiHintsLocked()
	logf("[km-ovl] sync editing=%v hints=%v hwnd=0x%X\n", editing, hints, uintptr(ui.hKMOverlay))
	if editing || hints {
		ui.setKeymapOverlayPassthrough(!editing) // 运行态穿透让点击落到视频区
		ui.syncKeymapOverlayGeom()
		procShowWindow.Call(uintptr(ui.hKMOverlay), kmShowFlag)
		procInvalidateRect.Call(uintptr(ui.hKMOverlay), 0, 1)
	} else {
		procShowWindow.Call(uintptr(ui.hKMOverlay), 0)
	}
}

// KeymapHintsToggle 侧边栏「运行时显示按键提示」开关。
func (ui *WindowsUI) KeymapHintsToggle(on bool) {
	ui.app.Keymap.setHints(on)
	ui.syncKeymapOverlayVis()
	ui.EvalKeymapUI()
}

func (ui *WindowsUI) repaintKeymapOverlay() {
	if ui.hKMOverlay != 0 {
		procInvalidateRect.Call(uintptr(ui.hKMOverlay), 0, 1)
	}
}

// ─── 几何换算:源万分比 ↔ 覆盖窗客户端像素(含 letterbox/旋转) ────────────────

// kmPctToClient 源万分比 → 覆盖窗客户端像素;同时返回缩放比(源像素→客户端)。
func (ui *WindowsUI) kmPctToClient(pctX, pctY float64) (int32, int32, float64, bool) {
	scaler := ui.app.activeScaler()
	if scaler == nil {
		return 0, 0, 0, false
	}
	sw, sh := scaler.SourceSize()
	g := scaler.LBGeo()
	if g == nil || sw <= 0 || sh <= 0 || g.drawW <= 0 || g.drawH <= 0 {
		return 0, 0, 0, false
	}
	fx := pctX / kmRuneScale * float64(sw)
	fy := pctY / kmRuneScale * float64(sh)
	var relX, relY int32
	var scale float64
	if scaler.IsRotated() {
		// 正向(旋转):x = relY*srcW/drawH;y = (drawW-relX)*srcH/drawW
		relY = int32(fx * float64(g.drawH) / float64(sw))
		relX = int32(float64(g.drawW) - fy*float64(g.drawW)/float64(sh))
		scale = float64(g.drawH) / float64(sw)
	} else {
		relX = int32(fx * float64(g.drawW) / float64(sw))
		relY = int32(fy * float64(g.drawH) / float64(sh))
		scale = float64(g.drawW) / float64(sw)
	}
	return g.offX + relX, g.offY + relY, scale, true
}

// kmClientToPct 覆盖窗客户端像素 → 源万分比。
func (ui *WindowsUI) kmClientToPct(cx, cy int32) (float64, float64, bool) {
	scaler := ui.app.activeScaler()
	if scaler == nil {
		return 0, 0, false
	}
	sx, sy, sw, sh, ok := scaler.ScaleMouseCoords(cx, cy)
	if !ok || sw <= 0 || sh <= 0 {
		return 0, 0, false
	}
	return float64(sx) / float64(sw) * kmRuneScale,
		float64(sy) / float64(sh) * kmRuneScale, true
}

// ─── 鼠标交互 ────────────────────────────────────────────────────────────────

// kmEntryAnchor 条目主锚点(万分比坐标)。
func kmEntryAnchor(en *KeymapEntry) (float64, float64) { return en.X, en.Y }

// kmHitKind 命中类型。
type kmHitKind int

const (
	kmHitNone kmHitKind = iota
	kmHitMain           // 主标记(拖动)
	kmHitSwipeEnd       // 滑动终点手柄
	kmHitDelete         // × 删除钮
	kmHitResize         // dpad 环缩放手柄
)

// kmHitTestLocked 命中检测:× 删除钮 > 缩放手柄 > 滑动终点 > 主标记。
func (ui *WindowsUI) kmHitTestLocked(cx, cy int32) (int, kmHitKind) {
	work := ui.kmWork
	if work == nil {
		return -1, kmHitNone
	}
	// × 删除钮(编辑态可见,优先——避免和主标记重叠区冲突)
	for i := range work.Entries {
		en := &work.Entries[i]
		if dx, dy, ok := ui.kmDeletePos(en); ok && dist2(dx, dy, cx, cy) <= kmDelHitR*kmDelHitR {
			return i, kmHitDelete
		}
	}
	// dpad 缩放手柄(环右下 45°)
	for i := range work.Entries {
		en := &work.Entries[i]
		if en.Type != kmTypeDpad {
			continue
		}
		if hx, hy, ok := ui.kmHandlePos(en); ok && dist2(hx, hy, cx, cy) <= (kmHandleR+6)*(kmHandleR+6) {
			return i, kmHitResize
		}
	}
	// 滑动终点手柄(可独立拖动)
	for i := range work.Entries {
		en := &work.Entries[i]
		if en.Type != kmTypeSwipe {
			continue
		}
		if ex, ey, _, ok := ui.kmPctToClient(en.X2, en.Y2); ok {
			if dist2(ex, ey, cx, cy) <= kmHitR*kmHitR {
				return i, kmHitSwipeEnd
			}
		}
	}
	// 主标记(命中半径跟随键帽实际大小:放大后整个键帽可点/可滚轮)
	for i := range work.Entries {
		en := &work.Entries[i]
		ex, ey, scale, ok := ui.kmPctToClient(en.X, en.Y)
		if !ok {
			continue
		}
		hr := int32(kmHitR)
		if en.Type != kmTypeDpad {
			w, _ := kmLabelSize(kmEntryLabel(en))
			s := kmCapScale(scale) * kmEntryScale(en)
			rx := int32(float64(w)/2*float64(s)) + int32(9*s)
			if rx > hr {
				hr = rx
			}
		}
		if dist2(ex, ey, cx, cy) <= hr*hr {
			return i, kmHitMain
		}
	}
	return -1, kmHitNone
}

// kmDeletePos 条目 × 删除钮的客户端坐标(与 kmDrawKeycap/kmDrawDpadKeys 绘制位置一致)。
// 键帽类必须乘条目缩放:× 画在缩放后的角上,命中区不跟的话会钉死在键帽内部
// ——滚轮悬停变死区、点击会误删。
func (ui *WindowsUI) kmDeletePos(en *KeymapEntry) (int32, int32, bool) {
	x, y, scale, ok := ui.kmPctToClient(en.X, en.Y)
	if !ok {
		return 0, 0, false
	}
	s := kmCapScale(scale) * kmEntryScale(en)
	if en.Type == kmTypeDpad {
		// dpad:主标记右上,以圆心 + 0.6r 处再外移(与 kmDrawDpadKeys 一致)
		r := ui.kmDpadClientR(en)
		d := int32(float64(r) * 0.6)
		return x + d/2 + 14, y - d/2 - 14, true
	}
	w, _ := kmLabelSize(kmEntryLabel(en))
	rx := int32(float64(w)/2*float64(s)) + int32(9*s)
	return x + rx - 2, y - (int32(8*s) + int32(7*s)) + 2, true
}

// kmHandlePos dpad 缩放手柄的客户端坐标(环右下 45° 边缘)。
func (ui *WindowsUI) kmHandlePos(en *KeymapEntry) (int32, int32, bool) {
	x, y, _, ok := ui.kmPctToClient(en.X, en.Y)
	if !ok {
		return 0, 0, false
	}
	r := ui.kmDpadClientR(en)
	return x + int32(float64(r)*0.7071), y + int32(float64(r)*0.7071), true
}

// kmDpadClientR dpad 环在客户端的显示半径(与 kmPaintEntry 同公式)。
func (ui *WindowsUI) kmDpadClientR(en *KeymapEntry) int32 {
	x, y, scale, ok := ui.kmPctToClient(en.X, en.Y)
	if !ok {
		return kmMarkerR
	}
	_ = x
	_ = y
	var sw, sh int32
	if scaler := ui.app.activeScaler(); scaler != nil {
		sw, sh = scaler.SourceSize()
	}
	if sw <= 0 || sh <= 0 {
		return kmMarkerR
	}
	r := int32(en.R / kmRuneScale * float64(min32(sw, sh)) * scale)
	if r < 24 {
		r = 24
	}
	if r > 600 {
		r = 600
	}
	return r
}

// kmDpadSlotPos dpad 四向键帽的客户端坐标(与 kmDrawDpadKeys 同公式;idx 0上/1左/2下/3右)。
func (ui *WindowsUI) kmDpadSlotPos(en *KeymapEntry, idx int) (int32, int32, bool) {
	x, y, _, ok := ui.kmPctToClient(en.X, en.Y)
	if !ok {
		return 0, 0, false
	}
	r := ui.kmDpadClientR(en)
	d := int32(float64(r) * 0.6)
	dirs := [4][2]int32{{0, -1}, {-1, 0}, {0, 1}, {1, 0}}
	return x + dirs[idx][0]*d, y + dirs[idx][1]*d, true
}

func (ui *WindowsUI) kmOnLButtonDown(cx, cy int32) {
	ui.mu.Lock()
	// 四向键帽优先命中:点击 W/A/S/D 小键帽 → 捕获对应槽位(点击就能改)
	if si, slot := ui.kmTryDpadSlotHitLocked(cx, cy); si >= 0 {
		ui.kmSelID = ui.kmWork.Entries[si].ID
		ui.kmCaptureID = ui.kmSelID
		ui.kmCaptureSlot = slot
		ui.mu.Unlock()
		ui.repaintKeymapOverlay()
		ui.EvalKeymapUI()
		return
	}
	idx, kind := ui.kmHitTestLocked(cx, cy)
	switch {
	case idx >= 0 && kind == kmHitDelete:
		// × 删除(雷电:就地可见删除)
		delID := ui.kmWork.Entries[idx].ID
		ui.kmWork.Entries = append(ui.kmWork.Entries[:idx], ui.kmWork.Entries[idx+1:]...)
		if ui.kmSelID == delID {
			ui.kmSelID = ""
		}
		ui.kmCaptureID = ""
		ui.mu.Unlock()
		ui.repaintKeymapOverlay()
		ui.EvalKeymapUI()
		return
	case idx >= 0 && kind == kmHitResize:
		ui.kmDragIdx = idx
		ui.kmDragB = false
		ui.kmDragResize = true
		ui.kmSelID = ui.kmWork.Entries[idx].ID
		procSetCapture.Call(uintptr(ui.hKMOverlay))
	case idx >= 0 && kind == kmHitMain:
		ui.kmDragIdx = idx
		ui.kmDragB = false
		ui.kmDragResize = false
		ui.kmSelID = ui.kmWork.Entries[idx].ID
		procSetCapture.Call(uintptr(ui.hKMOverlay))
		// 点击标记即进入改键捕获:选中的条目直接按任意键完成绑定(雷电同款)
		ui.kmCaptureID = ui.kmWork.Entries[idx].ID
		ui.kmCaptureSlot = -1
	default:
		// 空白处点击:清除改键捕获
		ui.kmCaptureID = ""
		ui.kmCaptureSlot = -1
	}
	ui.mu.Unlock()
	if idx >= 0 && kind != kmHitDelete {
		ui.repaintKeymapOverlay()
		ui.EvalKeymapUI()
	}
}

// kmTryDpadSlotHit 四向键帽命中(dpad 专属):返回 (条目下标, 槽位)。
func (ui *WindowsUI) kmTryDpadSlotHitLocked(cx, cy int32) (int, int) {
	work := ui.kmWork
	if work == nil {
		return -1, -1
	}
	for i := range work.Entries {
		en := &work.Entries[i]
		if en.Type != kmTypeDpad {
			continue
		}
		_, _, scale, ok := ui.kmPctToClient(en.X, en.Y)
		if !ok {
			continue
		}
		s := kmCapScale(scale) * kmEntryScale(en)
		hr := int32(12 * s) // 命中半径跟随键帽缩放
		for slot := 0; slot < 4; slot++ {
			if sx, sy, ok := ui.kmDpadSlotPos(en, slot); ok {
				if dist2(sx, sy, cx, cy) <= hr*hr {
					return i, slot
				}
			}
		}
	}
	return -1, -1
}

func (ui *WindowsUI) kmOnMouseMove(cx, cy int32) {
	ui.mu.Lock()
	if ui.kmDragIdx < 0 || ui.kmWork == nil || ui.kmDragIdx >= len(ui.kmWork.Entries) {
		ui.mu.Unlock()
		return
	}
	en := &ui.kmWork.Entries[ui.kmDragIdx]
	if ui.kmDragResize {
		// 缩放手柄拖动:新半径 = 指针到圆心的距离,反算回万分比(与手势端同基准)。
		mx, my, scale, ok := ui.kmPctToClient(en.X, en.Y)
		if ok {
			dx, dy := float64(cx-mx), float64(cy-my)
			d := sqrtF(dx*dx + dy*dy)
			var sw, sh int32
			if scaler := ui.app.activeScaler(); scaler != nil {
				sw, sh = scaler.SourceSize()
			}
			if sw > 0 && sh > 0 && scale > 0 {
				rSrc := d / scale                                  // 客户端 px → 源 px
				en.R = rSrc / float64(min32(sw, sh)) * kmRuneScale // 源 px → 万分比
				if en.R < 200 {
					en.R = 200
				}
				if en.R > 2500 {
					en.R = 2500
				}
			}
		}
	} else {
		pctX, pctY, ok := ui.kmClientToPct(cx, cy)
		if !ok {
			ui.mu.Unlock()
			return
		}
		if ui.kmDragB {
			en.X2, en.Y2 = pctX, pctY
		} else {
			en.X, en.Y = pctX, pctY
		}
	}
	ui.mu.Unlock()
	ui.repaintKeymapOverlay()
}

// kmOnWheel 滚轮缩放单个标记(编辑态):悬停命中的标记 ±0.1(0.6~3.0)。
// × 删除钮上滚动同样缩放所属条目——滚轮是无破坏性的尺寸手势,删除只留给点击。
func (ui *WindowsUI) kmOnWheel(cx, cy, delta int32) {
	ui.mu.Lock()
	if !ui.kmEditing || ui.kmWork == nil {
		ui.mu.Unlock()
		return
	}
	idx, _ := ui.kmHitTestLocked(cx, cy)
	if idx < 0 {
		// 四向键帽悬停也缩放所属 dpad
		if si, _ := ui.kmTryDpadSlotHitLocked(cx, cy); si >= 0 {
			idx = si
		}
	}
	if idx < 0 {
		ui.mu.Unlock()
		return
	}
	en := &ui.kmWork.Entries[idx]
	if en.Scale == 0 {
		en.Scale = 1
	}
	en.Scale += float64(delta) / 1200.0 // 一格滚轮(WHEEL_DELTA=120)=0.1
	if en.Scale < 0.6 {
		en.Scale = 0.6
	}
	if en.Scale > 3.0 {
		en.Scale = 3.0
	}
	ui.kmSelID = en.ID
	ui.mu.Unlock()
	ui.repaintKeymapOverlay() // InvalidateRect:即时视觉反馈(轻,LL 回调内安全)
	// EvalKeymapUI 走 SetTimer 防抖:它做 ReadDir+全预设解析+Marshal+WebView2 IPC,
	// 直接在 LL 钩子回调内同步跑,反复超时(300ms 预算)会被系统静默摘钩。
	// 重复 SetTimer 同 id = 重置倒计时,天然合并高频滚轮。
	procSetTimer.Call(uintptr(ui.hParent), TIMER_KM_EVAL, 120, 0)
}

func (ui *WindowsUI) kmOnRButton(cx, cy int32) {
	ui.mu.Lock()
	idx, _ := ui.kmHitTestLocked(cx, cy)
	var delID string
	if idx >= 0 {
		delID = ui.kmWork.Entries[idx].ID
		ui.kmWork.Entries = append(ui.kmWork.Entries[:idx], ui.kmWork.Entries[idx+1:]...)
		if ui.kmSelID == delID {
			ui.kmSelID = ""
		}
	}
	ui.mu.Unlock()
	if delID != "" {
		ui.repaintKeymapOverlay()
		ui.EvalKeymapUI()
	}
}

func dist2(ax, ay, bx, by int32) int32 {
	dx, dy := ax-bx, ay-by
	return dx*dx + dy*dy
}

// ─── 绘制 ────────────────────────────────────────────────────────────────────

func (ui *WindowsUI) paintKeymapOverlay(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))

	// 整屏填色键 → 全透明,视频可见
	bgBrush, _, _ := procCreateSolidBrush.Call(kmTransColor)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bgBrush)
	procDeleteObject.Call(bgBrush)

	ui.mu.Lock()
	work := ui.kmWork
	selID := ui.kmSelID
	editing := ui.kmEditing
	hintsOn := ui.app.Keymap.uiHintsLocked()
	ui.mu.Unlock()

	// 运行态(非编辑):画激活预设的轻量按键提醒(雷电「在游戏中显示按键提醒」同款)。
	// 数据源为激活预设(engine.profile)而非编辑工作副本;无 ×/缩放手柄/提示条。
	if !editing {
		if !hintsOn {
			return
		}
		prof := ui.app.Keymap.activeProfileCopy()
		if prof == nil {
			return
		}
		for i := range prof.Entries {
			ui.kmPaintEntry(hdc, &prof.Entries[i], false)
		}
		return
	}

	if work == nil {
		return
	}

	// 顶部提示条(仅编辑态)
	ui.mu.Lock()
	capID, capSlot := ui.kmCaptureID, ui.kmCaptureSlot
	ui.mu.Unlock()
	hintText := "编辑模式 · 点击标记后按任意键改绑 · 滚轮缩放大小 · 右键删除 · 完成到侧边栏点「完成」"
	if capID != "" {
		slotName := ""
		switch capSlot {
		case 0:
			slotName = "(上)"
		case 1:
			slotName = "(左)"
		case 2:
			slotName = "(下)"
		case 3:
			slotName = "(右)"
		}
		hintText = "改键中" + slotName + " · 按任意键完成绑定 · Esc 取消"
	}
	hintBrush, _, _ := procCreateSolidBrush.Call(kmColHintBg)
	hintRc := struct{ Left, Top, Right, Bottom int32 }{0, 0, rc.Right, kmHintH}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&hintRc)), hintBrush)
	procDeleteObject.Call(hintBrush)
	ui.kmDrawText(hdc, hintText, 0, 0, rc.Right, kmHintH, kmColHintTx, 12, 400)

	// 条目标记
	for i := range work.Entries {
		ui.kmPaintEntry(hdc, &work.Entries[i], work.Entries[i].ID == selID)
	}
}

// kmPaintEntry 绘制单个映射标记(雷电样式:键帽椭圆/摇杆环+四向键帽/滑动轨迹)。
// 编辑态附加:右上角 × 删除钮、dpad 环右下缩放手柄。
func (ui *WindowsUI) kmPaintEntry(hdc uintptr, en *KeymapEntry, sel bool) {
	x, y, scale, ok := ui.kmPctToClient(en.X, en.Y)
	if !ok {
		return
	}
	ui.mu.Lock()
	editing := ui.kmEditing
	ui.mu.Unlock()

	switch en.Type {
	case kmTypeDpad:
		// 环 = 实际作用范围(en.R 万分比 × 源短边 × 显示缩放,与手势端同基准)。
		// 注意不乘条目缩放:环必须严格等于手势实际偏移半径(所见即所得),
		// 滚轮只缩放四向键帽与中心点;环大小用环上手柄拖动调整。
		r := ui.kmDpadClientR(en)
		ui.kmCircle(hdc, x, y, r, kmColBorder, false)
		ui.kmCircle(hdc, x, y, kmMarkerR, kmColFill, true)
		// 四向键帽:W上/A左/S下/D右,位于环内 0.6r 处(雷电同款,所见即所得)
		ui.kmDrawDpadKeys(hdc, en, x, y, r, sel, editing, scale)
		if editing {
			// 缩放手柄:环右下 45° 边缘的亮色圆点,拖动直接改半径
			hx, hy := x+int32(float64(r)*0.7071), y+int32(float64(r)*0.7071)
			ui.kmCircle(hdc, hx, hy, kmHandleR, kmColAccent, true)
		}
	case kmTypeSwipe:
		// 起点键帽 + 轨迹线 + 终点键帽
		ui.kmDrawKeycap(hdc, en, x, y, sel, editing, scale)
		if ex, ey, _, ok := ui.kmPctToClient(en.X2, en.Y2); ok {
			pen, _, _ := procCreatePen.Call(0, 1, kmColBorder)
			oldPen, _, _ := procSelectObject.Call(hdc, pen)
			procMoveToEx.Call(hdc, uintptr(x), uintptr(y), 0)
			procLineTo.Call(hdc, uintptr(ex), uintptr(ey))
			procSelectObject.Call(hdc, oldPen)
			procDeleteObject.Call(pen)
			ui.kmDrawKeycap(hdc, en, ex, ey, sel, editing, scale)
		}
	case kmTypeAndroid:
		ui.kmDrawKeycap(hdc, en, x, y, sel, editing, scale)
	default: // tap
		ui.kmDrawKeycap(hdc, en, x, y, sel, editing, scale)
	}
}

// kmDrawKeycap 键帽(雷电样式:accent 描边 + 深底 + 键名居中)。
// 尺寸 = 显示缩放 × 条目缩放(滚轮);编辑态右上角 ×。
func (ui *WindowsUI) kmDrawKeycap(hdc uintptr, en *KeymapEntry, x, y int32, sel, editing bool, scale float64) {
	label := kmEntryLabel(en)
	s := kmCapScale(scale) * kmEntryScale(en)
	w, h := kmLabelSize(label)
	w, h = int32(float64(w)*s), int32(float64(h)*s)
	rx, ry := w/2+int32(9*s), h/2+int32(7*s)
	// 双层描边:外 accent 环 + 深底主体(选中再叠白环,高亮更强)
	outer := kmColAccent
	if sel {
		outer = kmColText // 选中=白外环
	}
	pen, _, _ := procCreatePen.Call(0, 2, uintptr(outer))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	ui.kmEllipse(hdc, x, y, rx, ry, kmColFill)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
	if sel { // 选中:内层再描 accent 环
		pen2, _, _ := procCreatePen.Call(0, 1, kmColAccent)
		op2, _, _ := procSelectObject.Call(hdc, pen2)
		ui.kmEllipse(hdc, x, y, rx-2, ry-2, kmColFill)
		procSelectObject.Call(hdc, op2)
		procDeleteObject.Call(pen2)
	}
	ui.kmDrawText(hdc, label, x-w/2, y-h/2, x+w/2, y+h/2, kmColText, int32(14*s), 700)
	if editing {
		ui.kmDrawDelete(hdc, x+rx-2, y-ry+2)
	}
}

// kmEntryScale 条目缩放(0=缺省 1.0)。
func kmEntryScale(en *KeymapEntry) float64 {
	if en.Scale <= 0 {
		return 1.0
	}
	return en.Scale
}

// kmCapScale 标记尺寸随视频显示缩放(所见即所得):视频放大/缩小多少,
// 键帽标记同比例变化——标记贴着游戏画面上的按钮,画面缩标记缩。
// 旧版钳在 0.75~1.3(标记几乎不随窗口变),与 dpad 环(纯跟随)不一致,
// 用户预期是跟随;小窗嫌标记小用「按键大小」滑杆补偿(60~300%)。
func kmCapScale(scale float64) float64 {
	return scale
}

// kmDrawDpadKeys dpad 四向键帽(W上/A左/S下/D右,环内 0.6r 处;尺寸含条目缩放)。
func (ui *WindowsUI) kmDrawDpadKeys(hdc uintptr, en *KeymapEntry, x, y, r int32, sel, editing bool, scale float64) {
	dirs := [4][2]int32{{0, -1}, {-1, 0}, {0, 1}, {1, 0}} // 上左下右(Keys 固定顺序)
	d := int32(float64(r) * 0.6)
	s := kmCapScale(scale) * kmEntryScale(en)
	for i, dir := range dirs {
		kx, ky := x+dir[0]*d, y+dir[1]*d
		label := "·"
		if i < len(en.Keys) && en.Keys[i] != "" {
			label = keyDisplayName(en.Keys[i])
		}
		w, h := kmLabelSize(label)
		w, h = int32(float64(w)*s), int32(float64(h)*s)
		rx, ry := w/2+int32(8*s), h/2+int32(6*s)
		outer := kmColAccent
		if sel {
			outer = kmColText
		}
		pen, _, _ := procCreatePen.Call(0, 2, uintptr(outer))
		oldPen, _, _ := procSelectObject.Call(hdc, pen)
		ui.kmEllipse(hdc, kx, ky, rx, ry, kmColFill)
		procSelectObject.Call(hdc, oldPen)
		procDeleteObject.Call(pen)
		ui.kmDrawText(hdc, label, kx-w/2, ky-h/2, kx+w/2, ky+h/2, kmColText, int32(13*s), 700)
	}
	// 编辑态 × 在主标记右上(以圆心为基准)
	if editing {
		ui.kmDrawDelete(hdc, x+d/2+14, y-d/2-14)
	}
}

// kmDrawDelete × 删除钮:14×14 深底方块 + 红 ×(雷电:就地可见删除,不用右键)。
func (ui *WindowsUI) kmDrawDelete(hdc uintptr, x, y int32) {
	s := int32(7)
	ui.kmRect(hdc, x-s, y-s, x+s, y+s, kmColHintBg)
	pen, _, _ := procCreatePen.Call(0, 2, 0x4040D9) // 红 #D94040(COLORREF BBGGRR)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procMoveToEx.Call(hdc, uintptr(x-3), uintptr(y-3), 0)
	procLineTo.Call(hdc, uintptr(x+4), uintptr(y+4))
	procMoveToEx.Call(hdc, uintptr(x+3), uintptr(y-3), 0)
	procLineTo.Call(hdc, uintptr(x-4), uintptr(y+4))
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
}

// kmEntryLabel 条目主标签(键名/系统键名)。
func kmEntryLabel(en *KeymapEntry) string {
	switch en.Type {
	case kmTypeAndroid:
		return androidKeyLabel(en.Android)
	default:
		if len(en.Keys) > 0 && en.Keys[0] != "" {
			return keyDisplayName(en.Keys[0])
		}
		return "未绑定"
	}
}

// kmLabelSize 按字符估算文本像素宽高(ASCII ~9px、其余 ~14px,14px 字号)。
func kmLabelSize(label string) (int32, int32) {
	w := 0
	for _, c := range label {
		if c < 128 {
			w += 9
		} else {
			w += 14
		}
	}
	if w < 14 {
		w = 14
	}
	return int32(w), 16
}

// kmEllipse 椭圆填充(中心 x,y 半径 rx,ry)。
func (ui *WindowsUI) kmEllipse(hdc uintptr, x, y, rx, ry int32, fill int32) {
	fb, _, _ := procCreateSolidBrush.Call(uintptr(fill))
	defer procDeleteObject.Call(fb)
	ob, _, _ := procSelectObject.Call(hdc, fb)
	procEllipse.Call(hdc, uintptr(x-rx), uintptr(y-ry), uintptr(x+rx), uintptr(y+ry))
	procSelectObject.Call(hdc, ob)
}

func (ui *WindowsUI) kmCircle(hdc uintptr, x, y, r int32, fill int32, solid bool) {
	var brush uintptr
	if solid {
		b, _, _ := procCreateSolidBrush.Call(uintptr(fill))
		defer procDeleteObject.Call(b)
		brush = b
	} else {
		b, _, _ := procGetStockObject.Call(NULL_BRUSH)
		brush = b
	}
	old, _, _ := procSelectObject.Call(hdc, brush)
	defer procSelectObject.Call(hdc, old)
	procEllipse.Call(hdc, uintptr(x-r), uintptr(y-r), uintptr(x+r), uintptr(y+r))
}

func (ui *WindowsUI) kmRect(hdc uintptr, l, t, r, b int32, fill int32) {
	fb, _, _ := procCreateSolidBrush.Call(uintptr(fill))
	defer procDeleteObject.Call(fb)
	ob, _, _ := procSelectObject.Call(hdc, fb)
	procRoundRect.Call(hdc, uintptr(l), uintptr(t), uintptr(r), uintptr(b), 10, 10)
	procSelectObject.Call(hdc, ob)
}

func (ui *WindowsUI) kmDrawText(hdc uintptr, s string, l, t, r, b int32, color int32, hgt, weight int32) {
	rc := struct{ Left, Top, Right, Bottom int32 }{l, t, r, b}
	fontName, _ := syscall.UTF16PtrFromString("Microsoft YaHei")
	// Quality=5(CLEARTYPE_QUALITY):抗锯齿文字。DEFAULT_QUALITY 的锯齿毛边
	// 是键帽文字"丑"的元凶——GDI 默认无平滑,必须显式开 ClearType。
	font, _, _ := procCreateFontW.Call(uintptr(hgt), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1, 0, 0, 5, 0, uintptr(unsafe.Pointer(fontName)))
	if font == 0 {
		return
	}
	// 恢复旧字体后再删新字体：被选入 DC 的对象 DeleteObject 会失败——
	// 不恢复则每次 WM_PAINT 泄漏一个 HFONT，拖动标记高频重绘几分钟
	// 就打爆 GDI 句柄配额（进程 10000），之后全窗口绘制错乱。
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	defer procSelectObject.Call(hdc, oldFont)
	defer procDeleteObject.Call(font)
	procSetBkMode.Call(hdc, 1)
	procSetTextColor.Call(hdc, uintptr(color))
	ptr, _ := syscall.UTF16PtrFromString(s)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(ptr)), ^uintptr(0),
		uintptr(unsafe.Pointer(&rc)), 0x00000025) // DT_CENTER|DT_VCENTER|DT_SINGLELINE
}

// ─── 状态推送 ────────────────────────────────────────────────────────────────

// keymapEvalPayload Eval 到侧边栏的完整负载。
type keymapEvalPayload struct {
	Enabled bool           `json:"enabled"`
	Active  string         `json:"activeId"`
	Presets []presetMeta   `json:"presets"`
	Profile *KeymapProfile `json:"profile"` // 已保存的激活预设
	Editing bool           `json:"editing"`
	SelID   string         `json:"selId"`
	Work    *KeymapProfile `json:"work"` // 编辑中的工作副本
}

// EvalKeymapUI 推送键位面板状态到侧边栏。
func (ui *WindowsUI) EvalKeymapUI() {
	if ui.chromium == nil {
		return
	}
	ui.mu.Lock()
	editing, work, selID := ui.kmEditing, ui.kmWork, ui.kmSelID
	ui.mu.Unlock()
	st := ui.app.Keymap.uiState()
	payload := keymapEvalPayload{
		Enabled: st.Enabled,
		Active:  st.Active,
		Presets: st.Presets,
		Profile: st.Profile,
		Editing: editing,
		SelID:   selID,
		Work:    work,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ui.chromium.Eval(fmt.Sprintf(`if(window.updateKeymapUI)updateKeymapUI(%s)`, b))
}

// onKeymapToggled F9 切换 toast(wndProc UI 线程)。运行态按键提醒随引擎开关显隐。
// 启用时若仍开着编辑会话:先退出编辑(保存工作副本)——编辑覆盖层必须接收鼠标
// (拖动标记),此时覆盖层不穿透,用户按 F9 会得到「映射已启用」toast 却点不动鼠标
// (2026-09-08 用户报障「开了 F9 鼠标不能控制」)。启用动作的语义是进入可玩状态,
// 必须把覆盖层收敛回点击穿透。
func (ui *WindowsUI) onKeymapToggled(enabled bool) {
	ui.mu.Lock()
	editing := ui.kmEditing
	ui.mu.Unlock()
	if enabled && editing {
		ui.KeymapSetEditing(false) // 内部已 syncKeymapOverlayVis + EvalKeymapUI + syncStateToPanel
		ui.showToast("键盘映射已启用", "已退出编辑模式", "", nil)
		return
	}
	if enabled {
		ui.showToast("键盘映射已启用", "按 F9 可随时关闭", "", nil)
	} else {
		ui.showToast("键盘映射已关闭", "按 F9 重新启用", "", nil)
	}
	ui.syncStateToPanel()
	ui.syncKeymapOverlayVis()
}
