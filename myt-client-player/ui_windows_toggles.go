//go:build windows

package main

import (
	"os"
	"time"
	"unsafe"
)

// --- 窗口状态切换（置顶/全屏/旋转/侧栏/同步/填屏/视频源尺寸）---

func (ui *WindowsUI) ToggleTopmost() {
	newVal := !ui.app.Topmost.Load()
	ui.app.Topmost.Store(newVal)
	f := uintptr(HWND_NOTOPMOST)
	if newVal {
		f = uintptr(HWND_TOPMOST)
	}
	procSetWindowPos.Call(uintptr(ui.hParent), f, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE)
	ui.syncStateToPanel()
}

func (ui *WindowsUI) ToggleFullscreen() {
	ui.mu.Lock()
	ui.fullscreen = !ui.fullscreen
	fs := ui.fullscreen
	ui.mu.Unlock()

	hwnd := uintptr(ui.hParent)
	if fs {
		var rect [4]int32
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		ui.fsRestoreRect = rect
		ui.fsRestoreRotated = ui.app.Rotated.Load()
		dwmDisable := int32(1)
		procDwmSetWindowAttribute.Call(hwnd, 3, uintptr(unsafe.Pointer(&dwmDisable)), 4)
		style, _, _ := procGetWindowLongW.Call(hwnd, uintptr(GWL_STYLE))
		procSetWindowLongW.Call(hwnd, uintptr(GWL_STYLE), style & ^uintptr(WS_OVERLAPPEDWINDOW))
		hMonitor, _, _ := procMonitorFromWindow.Call(hwnd, uintptr(MONITOR_DEFAULTTOPRIMARY))
		var monitorInfo struct {
			cbSize    uint32
			rcMonitor [4]int32
			rcWork    [4]int32
			dwFlags   uint32
		}
		monitorInfo.cbSize = uint32(unsafe.Sizeof(monitorInfo))
		procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&monitorInfo)))
		// 用 rcWork（工作区，排除任务栏）而非 rcMonitor（整屏）。投屏全屏保留任务栏可见，
		// 避免窗口底部 navBar/视频被任务栏遮挡。若任务栏已自动隐藏，rcWork≈rcMonitor 无影响。
		m := monitorInfo.rcWork
		procSetWindowPos.Call(hwnd, uintptr(HWND_TOPMOST),
			uintptr(m[0]), uintptr(m[1]), uintptr(m[2]-m[0]), uintptr(m[3]-m[1]),
			SWP_NOCOPYBITS|SWP_NOACTIVATE|SWP_FRAMECHANGED)
		ui.layoutChildren()
	} else {
		dwmDisable := int32(0)
		procDwmSetWindowAttribute.Call(hwnd, 3, uintptr(unsafe.Pointer(&dwmDisable)), 4)
		style, _, _ := procGetWindowLongW.Call(hwnd, uintptr(GWL_STYLE))
		procSetWindowLongW.Call(hwnd, uintptr(GWL_STYLE), style|uintptr(WS_OVERLAPPEDWINDOW))
		zOrder := uintptr(HWND_NOTOPMOST)
		if ui.app.Topmost.Load() {
			zOrder = uintptr(HWND_TOPMOST)
		}
		r := ui.fsRestoreRect
		rectW := r[2] - r[0]
		rectH := r[3] - r[1]
		procSetWindowPos.Call(hwnd, zOrder,
			uintptr(r[0]), uintptr(r[1]), uintptr(rectW), uintptr(rectH),
			SWP_NOCOPYBITS|SWP_NOACTIVATE|SWP_FRAMECHANGED)
		// 还原尺寸校正：全屏中旋转过（fsRestoreRotated ≠ 当前）时还原矩形形状与
		// 当前视频方向失配 → letterbox 大黑边。互换/收窄逻辑见 correctWindowShapeToVideo。
		ui.correctWindowShapeToVideo(ui.fsRestoreRotated != ui.app.Rotated.Load(), "fsRestore")
		ui.layoutChildren()
	}
	ui.syncStateToPanel()
}

func (ui *WindowsUI) ToggleRotate() {
	// 全屏/最大化:窗口尺寸固定,只翻内容方向不互换窗口(互换会把全屏窗口拽成小窗)。
	if ui.fillScreen() {
		// WebURL 模式仍走退出重开(主程序翻转 orientation)
		if ui.app.WebURL != "" {
			logf("[rotate] WebURL fullscreen, exiting with code %d to request reopen\n", ExitCodeRotate)
			os.Exit(ExitCodeRotate)
			return
		}
		ui.app.ToggleRotate()
		ui.layoutChildren()      // 无窗口 resize,手动触发 LBGeo 重算(否则映射用旧旋转几何)
		ui.repaintKeymapOverlay() // 键位标记立即按新几何重绘(不等 pump,否则标记留在旧位置)
		ui.syncStateToPanel()
		ui.showToast("画面已旋转", "全屏窗口尺寸保持不变", "", nil)
		return
	}
	// 防抖：500ms 内只处理一次旋转（WebView2 连续 resize 会崩溃）
	if ui.rotating {
		return
	}
	ui.rotating = true
	defer func() {
		time.AfterFunc(500*time.Millisecond, func() { ui.rotating = false })
	}()

	ui.app.ToggleRotate()

	// WebURL 模式：用特殊退出码通知主程序以翻转 orientation 重新打开
	if ui.app.WebURL != "" {
		logf("[rotate] WebURL mode, exiting with code %d to request reopen\n", ExitCodeRotate)
		os.Exit(ExitCodeRotate)
		return
	}

	ui.ensureFrameChrome()

	// 旋转互换的是【视频区】宽高，铬框（侧边栏/navBar/窗口边框）各归各位。
	// raw 互换整窗（frameW=旧高）会把 navBar+边框的高度错算成宽度富余：视频区比例
	// 从 16:9 失配到 ~2.0 → 画面按高铺满、宽度剩出 → 左右各 ~25px 黑边
	// （实测 2026-09-01 日志 [lb] win=481x242 draw=430x242 off=25,0）。
	// 尺寸基于旋转前的实际窗口矩形（baseWin 可能因用户拖拽过期）。
	var rcOld struct{ Left, Top, Right, Bottom int32 }
	procGetWindowRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&rcOld)))
	oldW := rcOld.Right - rcOld.Left
	oldH := rcOld.Bottom - rcOld.Top
	// 侧栏按实际宽度算：编辑键位等场景侧栏展开时若按收起宽算，视频区被高估
	// ~248px → 互换后窗口过窄变形 → 泵 aspect-refit 再拽一次 → 跳变 +
	// WebView2 双重快速 resize（连续 resize 会崩，正是防抖注释警告的场景）。
	sideW := ui.sideCollapsedW
	if ui.panelExpanded {
		sideW = ui.sideExpandedW
	}
	videoW := oldW - ui.frameChromeW - sideW // 当前视频区宽
	videoH := oldH - ui.frameChromeH - ui.navBarHeight // 当前视频区高
	// 互换视频区
	newVideoW, newVideoH := videoH, videoW
	// 转横舒适下限：互换保持面积，原竖窗偏小时转出来的横窗也偏小——
	// 视频区宽不足 comfortLandscapeVideoW 时等比放大到该宽度（默认大小放大点）。
	// 矩阵小窗(Controlled)不放大:同面积互换留在格子附近——舒适放大曾把
	// 299 宽小窗膨胀到 767(压掉两个邻居格,邻居的键位标记浮上来像"重复")。
	if !ui.app.Controlled && newVideoW > newVideoH && newVideoW > 0 && newVideoW < comfortLandscapeVideoW {
		scale := float64(comfortLandscapeVideoW) / float64(newVideoW)
		newVideoW = comfortLandscapeVideoW
		newVideoH = int32(float64(newVideoH)*scale + 0.5)
	}
	// clamp 到工作区（放大后可能超出屏幕）
	if workW, workH, ok := ui.workArea(); ok {
		maxVideoW := workW - sideW - ui.frameChromeW
		maxVideoH := workH - ui.navBarHeight - ui.frameChromeH
		if newVideoW > maxVideoW {
			newVideoW = maxVideoW
		}
		if newVideoH > maxVideoH {
			newVideoH = maxVideoH
		}
	}
	ui.baseWinW = newVideoW + ui.sideCollapsedW
	ui.baseWinH = newVideoH + ui.navBarHeight
	frameW := newVideoW + sideW + ui.frameChromeW
	frameH := newVideoH + ui.navBarHeight + ui.frameChromeH

	logf("[winsz] handleSizing: %dx%d → %dx%d (rotate swap, video %dx%d → %dx%d)\n",
		oldW, oldH, frameW, frameH, videoW, videoH, newVideoW, newVideoH)
	procSetWindowPos.Call(uintptr(ui.hParent), 0,
		uintptr(rcOld.Left), uintptr(rcOld.Top), uintptr(frameW), uintptr(frameH), SWP_NOZORDER)
	ui.syncStateToPanel()
}

func (ui *WindowsUI) TogglePanel(expand bool) {
	// 全屏/最大化:窗口尺寸固定,展开/收起只重排布局,不动窗口
	// (侧栏在全屏可见后,这里 SetWindowPos 会把全屏窗口拽成小窗、破坏全屏态)。
	if ui.fillScreen() {
		ui.panelExpanded = expand
		ui.layoutChildren()
		ui.syncStateToPanel()
		if !expand && ui.hSdlArea != 0 {
			procSetFocus.Call(uintptr(ui.hSdlArea)) // 收起还焦点,F9 恢复可用
		}
		return
	}
	ui.panelExpanded = expand
	ui.ensureFrameChrome()

	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetWindowRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&rc)))
	clientW := ui.baseWinW + (ui.sideExpandedW - ui.sideCollapsedW)
	if !expand {
		clientW = ui.baseWinW
	}
	frameW := clientW + ui.frameChromeW
	sideW := ui.sideCollapsedW
	if expand {
		sideW = ui.sideExpandedW
	}
	sdlW := clientW - sideW

	newH := rc.Bottom - rc.Top
	scaler := ui.activeScaler()
	if scaler != nil {
		srcW, srcH := scaler.SourceSize()
		if srcW > 0 && srcH > 0 {
			effW, effH := srcW, srcH
			if ui.app.Rotated.Load() {
				effW, effH = srcH, srcW
			}
			if sdlW > 0 {
				sdlH := (sdlW*effH + effW/2) / effW
				newH = sdlH + ui.navBarHeight + ui.frameChromeH
			}
		}
	}
	procSetWindowPos.Call(uintptr(ui.hParent), 0,
		uintptr(rc.Left), uintptr(rc.Top), uintptr(frameW), uintptr(newH), SWP_NOZORDER)
	if !expand && ui.hSdlArea != 0 {
		procSetFocus.Call(uintptr(ui.hSdlArea)) // 收起还焦点:WebView2 不再持有键盘,F9 恢复可用
	}
	logf("[winsz] SyncState: → %dx%d (frameW=%d newH=%d)\n", frameW, newH, frameW, newH)
}

func (ui *WindowsUI) SyncState() {
	ui.syncStateToPanel()
}

// fillScreen 返回窗口是否处于"填满屏幕"状态：显式全屏(ToggleFullscreen) 或系统最大化。
// 该状态下窗口尺寸由系统固定，播放器不得再通过 fit 去缩放窗口（否则用户点全屏后
// 窗口会被拽回/缩回，表现为"自动靠左"）。
// videoEffSize 当前视频有效显示尺寸（源尺寸按旋转态互换；无源返回 0）。
func (ui *WindowsUI) videoEffSize() (int32, int32) {
	w, h := ui.videoSourceSize()
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	if ui.app.Rotated.Load() {
		return h, w
	}
	return w, h
}

// correctWindowShapeToVideo 校正当前窗口形状到视频方向（最大化/全屏还原后调用）。
// rotatedSinceEnter 为真（还原前的旋转态 ≠ 当前）→ 视频区互换（面积保持，与
// ToggleRotate 非全屏路径同语义）；否则仅收窄（只收不放）。差异 ≤2px 不调整防抖。
func (ui *WindowsUI) correctWindowShapeToVideo(rotatedSinceEnter bool, src string) {
	effW, effH := ui.videoEffSize()
	if effW <= 0 || effH <= 0 {
		return
	}
	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetWindowRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&rc)))
	rectW, rectH := rc.Right-rc.Left, rc.Bottom-rc.Top
	sideW := ui.sideCollapsedW
	if ui.panelExpanded {
		sideW = ui.sideExpandedW
	}
	videoW := rectW - ui.frameChromeW - sideW
	videoH := rectH - ui.frameChromeH - ui.navBarHeight
	if videoW <= 0 || videoH <= 0 {
		return
	}
	newW, newH := videoW, videoH
	if rotatedSinceEnter {
		newW, newH = videoH, videoW // 全屏/最大化中旋转过：互换保持面积
	}
	// 统一按当前视频方向收窄校正（互换后通常 no-op；兜底互换后仍失配/未旋转但失配）
	if newW*effH > newH*effW {
		newW = (newH*effW + effH/2) / effH
	} else {
		newH = (newW*effH + effW/2) / effW
	}
	if abs32(newW-videoW) <= 2 && abs32(newH-videoH) <= 2 {
		return // 整数截断残余级差异，不动（防抖）
	}
	procSetWindowPos.Call(uintptr(ui.hParent), 0,
		uintptr(rc.Left), uintptr(rc.Top),
		uintptr(newW+sideW+ui.frameChromeW), uintptr(newH+ui.navBarHeight+ui.frameChromeH),
		SWP_NOZORDER)
	logf("[winsz] %s 校正: video %dx%d → %dx%d\n", src, videoW, videoH, newW, newH)
}

func (ui *WindowsUI) fillScreen() bool {
	if ui.fullscreen {
		return true
	}
	if r, _, _ := procIsZoomed.Call(uintptr(ui.hParent)); r != 0 {
		return true
	}
	return false
}

func (ui *WindowsUI) videoSourceSize() (int32, int32) {
	if s := ui.activeScaler(); s != nil {
		return s.SourceSize()
	}
	// 公网投屏（WebURL）无本地 scaler，用页面汇报的视频真实尺寸来 fit 窗口比例。
	if ui.app.WebURL != "" {
		if w, h := ui.webVideoW.Load(), ui.webVideoH.Load(); w > 0 && h > 0 {
			return w, h
		}
	}
	return 0, 0
}
