//go:build windows

package main

// keymap_overlay_windows.go — 键位映射编辑覆盖层会话控制(进入/退出、添加、绑定、删除)。
// 绘制与鼠标交互在 keymap_overlay_paint.go。

import "strconv"

// kmOverlayProcCB 覆盖层窗口过程回调(包一层变量避免闭包逃逸)。
var kmOverlayProcCB uintptr

const (
	// WM_APP_KM_TOGGLE F9 切换后经 PostMessage 转 UI 线程弹 toast
	WM_APP_KM_TOGGLE = WM_USER + 17
	// IDC_CROSS 十字光标(放置/拖动语义)
	IDC_CROSS = 32515
	// kmTransColor 色键透明色 RGB(1,0,0)
	kmTransColor = 0x00000001

	kmMarkerR    = 16 // 标记可视半径(客户端像素)
	kmHitR       = 24 // 命中判定半径
	kmSwipeEndR  = 9  // 滑动终点手柄半径
	kmHintH      = 30 // 顶部提示条高度
	kmHandleR    = 7  // dpad 环缩放手柄半径
	kmDelHitR    = 12 // × 删除钮命中半径
)

// WindowsUI 键位编辑会话字段(定义于 ui_windows.go struct)。
// 本文件实现其全部行为。

// keymapOvlWndProc 覆盖层窗口过程。
func (ui *WindowsUI) keymapOvlWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_ERASEBKGND:
		return 1
	case WM_PAINT:
		ui.paintKeymapOverlay(hwnd)
		return 0
	case WM_SETCURSOR:
		// 十字光标是编辑语义(放置/拖动标记);运行态穿透点击,光标交给下层(箭头)。
		ui.mu.Lock()
		editing := ui.kmEditing
		ui.mu.Unlock()
		if editing {
			hCursor, _, _ := procLoadCursorW.Call(0, IDC_CROSS)
			procSetCursor.Call(hCursor)
			return 1
		}
		hCursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
		procSetCursor.Call(hCursor)
		return 1
	case WM_LBUTTONDOWN:
		ui.kmOnLButtonDown(int32(int16(lParam&0xFFFF)), int32(int16((lParam>>16)&0xFFFF)))
		return 0
	case WM_MOUSEMOVE:
		ui.kmOnMouseMove(int32(int16(lParam&0xFFFF)), int32(int16((lParam>>16)&0xFFFF)))
		return 0
	case WM_LBUTTONUP:
		procReleaseCapture.Call()
		ui.mu.Lock()
		ui.kmDragIdx = -1
		ui.kmDragB = false
		ui.kmDragResize = false
		ui.mu.Unlock()
		return 0
	case WM_RBUTTONUP:
		ui.kmOnRButton(int32(int16(lParam&0xFFFF)), int32(int16((lParam>>16)&0xFFFF)))
		return 0
	// 注:WM_MOUSEWHEEL 永远到不了本窗口(overlay 是 NOACTIVATE 无焦点,
	// 滚轮消息只发焦点窗口)——标记缩放的滚轮在主窗口 handleMouseWheel 拦截转发。
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

// ─── 编辑会话控制(经 dispatchMessage 类型断言调用) ──────────────────────────

// KeymapSetEditing 进入/退出编辑模式(覆盖层显隐 + 工作副本生命周期)。
func (ui *WindowsUI) KeymapSetEditing(on bool) {
	ui.mu.Lock()
	if on {
		if ui.kmWork == nil {
			work := ui.app.Keymap.activeProfileCopy()
			if work == nil {
				work = &KeymapProfile{Name: "未命名方案"}
			}
			ui.kmWork = work
		}
		ui.kmCaptureID = ""
		ui.kmCaptureSlot = -1
		ui.kmEditing = true
		ui.mu.Unlock()
		ui.installKmWheelHook() // 焦点在 WebView2 时滚轮到不了主窗口,低级钩子兜底
		ui.showKeymapOverlay()
	} else {
		work := ui.kmWork
		ui.kmWork = nil
		ui.kmEditing = false
		ui.kmSelID = ""
		ui.kmCaptureID = "" // 残留捕获态会在退出后吞掉游戏按键(P0)
		ui.kmCaptureSlot = -1
		ui.mu.Unlock()
		ui.removeKmWheelHook()
		if work != nil {
			ui.app.Keymap.saveProfile(work)
		}
		// 退出编辑后按运行态提醒开关决定显隐(编辑态隐藏逻辑不再无条件盖掉运行态)。
		ui.syncKeymapOverlayVis()
	}
	ui.EvalKeymapUI()
	ui.syncStateToPanel()
}

// KeymapAddEntry 添加一条映射(放在画面中心,随后用户拖动调整)。
func (ui *WindowsUI) KeymapAddEntry(typ string) {
	ui.mu.Lock()
	work := ui.kmWork
	if work == nil {
		ui.mu.Unlock()
		return
	}
	en := KeymapEntry{ID: newKeymapID(), Type: typ}
	switch typ {
	case kmTypeDpad:
		en.Keys = []string{"KeyW", "KeyA", "KeyS", "KeyD"}
		en.R = 700
		en.X, en.Y = 5000, 5000
	case kmTypeSwipe:
		en.Keys = []string{""}
		en.X, en.Y = 4200, 5000
		en.X2, en.Y2 = 5800, 5000
	case kmTypeAndroid:
		en.Keys = []string{""}
		en.Android = "back"
		en.X, en.Y = 5000, 5000
	default:
		en.Type = kmTypeTap
		en.Keys = []string{""}
		en.X, en.Y = 5000, 5000
	}
	work.Entries = append(work.Entries, en)
	ui.kmSelID = en.ID
	// 新增即进入改键捕获:点完类型块 → 直接按任意键完成首个绑定(雷电同款节奏)
	ui.kmCaptureID = en.ID
	ui.kmCaptureSlot = -1
	ui.mu.Unlock()
	ui.repaintKeymapOverlay()
	ui.EvalKeymapUI()
}

// KeymapBindCode 把捕获到的按键绑到选中条目。
// dpad:slot>=0 指定槽位(0上/1左/2下/3右),slot<0 填首个空槽(满则覆盖"右");
// 其余类型忽略 slot。同一键在其它槽位的旧绑定会被清除(一键一方向)。
// 绑定完成即退出改键捕获(下次按键不再被吃)。
func (ui *WindowsUI) KeymapBindCode(code string, slot int) {
	if code == "" {
		return
	}
	ui.mu.Lock()
	work := ui.kmWork
	if work == nil || ui.kmSelID == "" {
		ui.mu.Unlock()
		return
	}
	idx := ui.kmFindEntryLocked(work, ui.kmSelID)
	if idx < 0 {
		ui.mu.Unlock()
		return
	}
	en := &work.Entries[idx]
	if en.Type == kmTypeDpad {
		pos := slot
		if pos < 0 || pos > 3 {
			pos = -1
			for i := 0; i < 4; i++ {
				if i >= len(en.Keys) {
					break
				}
				if en.Keys[i] == "" {
					pos = i
					break
				}
			}
			if pos < 0 {
				pos = 3
			}
		}
		for len(en.Keys) < 4 {
			en.Keys = append(en.Keys, "")
		}
		for i := range en.Keys { // 一键一槽:清掉其它槽的同键绑定
			if en.Keys[i] == code {
				en.Keys[i] = ""
			}
		}
		en.Keys[pos] = code
	} else {
		if len(en.Keys) == 0 {
			en.Keys = []string{code}
		} else {
			en.Keys[0] = code
		}
	}
	// 绑定完成退出改键捕获
	ui.kmCaptureID = ""
	ui.kmCaptureSlot = -1
	ui.mu.Unlock()
	ui.repaintKeymapOverlay()
	ui.EvalKeymapUI()
}

// KeymapSetProp 修改选中条目属性(tapMode / android 动作 / dpad 半径 / 按键大小)。
// 画面即时重绘(键帽/环跟随滑杆);侧栏推送走 TIMER_KM_EVAL 防抖——拖动中
// input 高频提交,直接 EvalKeymapUI 会重建滑杆 DOM 打断拖动。
func (ui *WindowsUI) KeymapSetProp(id, prop, val string) {
	ui.mu.Lock()
	work := ui.kmWork
	ok := false
	if work != nil {
		if idx := ui.kmFindEntryLocked(work, id); idx >= 0 {
			en := &work.Entries[idx]
			switch prop {
			case "tapmode":
				if val == "click" || val == "hold" {
					en.TapMode = val
					ok = true
				}
			case "android":
				for _, a := range kmAndroidKeys {
					if a == val {
						en.Android = val
						ok = true
					}
				}
			case "r":
				if fv, err := strconv.ParseFloat(val, 64); err == nil && fv >= 20 && fv <= 250 {
					en.R = fv * 10 // 滑杆值 20~250 → 万分比 200~2500
					ok = true
				}
			case "size":
				if fv, err := strconv.ParseFloat(val, 64); err == nil && fv >= 60 && fv <= 300 {
					en.Scale = fv / 100 // 滑杆值 60~300(百分比) → 0.6~3.0,与滚轮同域
					ok = true
				}
			}
		}
	}
	ui.mu.Unlock()
	if ok {
		ui.repaintKeymapOverlay() // 画面键帽/环立即跟随滑杆变化
		procSetTimer.Call(uintptr(ui.hParent), TIMER_KM_EVAL, 120, 0)
	}
}

// KeymapDeleteEntry 删除选中条目。
func (ui *WindowsUI) KeymapDeleteEntry(id string) {
	ui.mu.Lock()
	work := ui.kmWork
	if work != nil {
		for i := range work.Entries {
			if work.Entries[i].ID == id {
				work.Entries = append(work.Entries[:i], work.Entries[i+1:]...)
				break
			}
		}
	}
	if ui.kmSelID == id {
		ui.kmSelID = ""
	}
	ui.mu.Unlock()
	ui.repaintKeymapOverlay()
	ui.EvalKeymapUI()
}

// KeymapCancelEdit 取消编辑:丢弃工作副本,不保存。
func (ui *WindowsUI) KeymapCancelEdit() {
	ui.mu.Lock()
	ui.kmWork = nil
	ui.kmEditing = false
	ui.kmSelID = ""
	ui.kmCaptureID = "" // 残留捕获态会在退出后吞掉游戏按键(P0)
	ui.kmCaptureSlot = -1
	ui.mu.Unlock()
	ui.removeKmWheelHook()
	ui.syncKeymapOverlayVis()
	ui.EvalKeymapUI()
}

// KeymapSelect 侧边栏列表选中条目。
func (ui *WindowsUI) KeymapSelect(id string) {
	ui.mu.Lock()
	ui.kmSelID = id
	ui.mu.Unlock()
	ui.repaintKeymapOverlay()
	ui.EvalKeymapUI()
}

// KeymapExported 导出完成 toast。
func (ui *WindowsUI) KeymapExported(path string) {
	name := path
	if idx := len(path) - 1; idx > 0 {
		for i := len(path) - 1; i >= 0; i-- {
			if path[i] == '\\' || path[i] == '/' {
				name = path[i+1:]
				break
			}
		}
	}
	ui.showToast("键位方案已导出", name, "", nil)
}

// KeymapWorkRenamed 预设重命名同步到编辑中的工作副本。
// renamePreset 只改磁盘文件;不同步的话「完成并保存」会用旧名覆盖回去。
func (ui *WindowsUI) KeymapWorkRenamed(id, name string) {
	ui.mu.Lock()
	if ui.kmWork != nil && ui.kmWork.ID == id {
		ui.kmWork.Name = name
	}
	ui.mu.Unlock()
}

// KeymapPresetDeleted 预设删除后清理指向它的编辑会话:
// 工作副本持有被删预设的 ID,留着会在「完成」时把文件重新写活。
// dispatch 层调用,随后统一 EvalKeymapUI 刷新面板。
func (ui *WindowsUI) KeymapPresetDeleted(id string) {
	ui.mu.Lock()
	hit := ui.kmWork != nil && ui.kmWork.ID == id
	if hit {
		ui.kmWork = nil
		ui.kmEditing = false
		ui.kmSelID = ""
		ui.kmCaptureID = "" // 残留捕获态会在退出后吞掉游戏按键(P0)
		ui.kmCaptureSlot = -1
	}
	ui.mu.Unlock()
	if hit {
		ui.removeKmWheelHook()
		ui.syncKeymapOverlayVis()
		ui.syncStateToPanel()
	}
}

// kmFindEntryLocked 按 id 查条目下标(调用方须持锁)。
func (ui *WindowsUI) kmFindEntryLocked(work *KeymapProfile, id string) int {
	for i := range work.Entries {
		if work.Entries[i].ID == id {
			return i
		}
	}
	return -1
}
