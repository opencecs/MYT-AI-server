//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

func (ui *WindowsUI) fitWindowToVideo() (bool, bool) {
	debugLog(fmt.Sprintf("[fitWindow] called baseWinW=%d baseWinH=%d fullscreen=%v controlled=%v", ui.baseWinW, ui.baseWinH, ui.fullscreen, ui.app.Controlled))
	if ui.fillScreen() {
		return false, false
	}
	// 定尺寸投屏（主程序传 -winw/-winh）：窗口绝对尺寸由主程序 ComputeGridLayout 网格排版定夺。
	// Controlled 批量投屏（含 NoSidebar）直接跳过 fit：排列已按视频比例(VideoAspect)算尺寸，
	// fit 再改会与排版冲突（窗口比格子小→白边/变大变不回）。用户诉求：player 不该有
	// 最大最小值与排版冲突。fit 只在单台（非 controlled，用户手动调）时用。
	// 公网 WebURL 模式也跳过：视频在 WebView2 页面内 object-fit:contain 自适应去黑边。
	// tebox 单台窗口：主程序不传 -winw，且持续 fit 会把用户拖出的窗口比例拽回源比例
	//（「拖动弹回」）。改为一次性语义：初始 fit 尚未做过时放行（按流比例设定一次
	// 初始窗口尺寸，消除默认窗口比例的黑边），fit 真正执行后置标记、此后恒跳过
	//（用户自管尺寸，拖动不回弹）；画面适配由 letterbox 受限填满兜底（偏差 ≤10%
	// 轻微拉伸填满、超出则居中黑边，任何窗口比例下渲染层都合理）。
	if ui.app.CastMode == CastModeTebox {
		if ui.teboxInitialFitDone {
			return true, false
		}
	}
	if *flagWinW > 0 && (ui.app.Controlled || !ui.app.NoSidebar || ui.app.WebURL != "") {
		return true, false
	}
	// 矩阵投屏时每个窗口也应各自 fit 到自己内容的比例（主程序只读比例定尺寸），
	// 因此不在此跳过；窗口绝对尺寸最终由主程序 ArrangePlayerWindows 网格排版控制。
	srcW, srcH := ui.videoSourceSize()
	debugLog(fmt.Sprintf("[fitWindow] videoSrc=%dx%d scaler=%v", srcW, srcH, ui.activeScaler() != nil))
	if srcW <= 0 || srcH <= 0 {
		return false, false
	}
	// 源尺寸有效、fit 确定执行：tebox 在此收口一次性语义（后续调用恒跳过）。
	// 注意必须放在源校验之后：首帧消息先于 SetSourceSize 到达时 fit 会空跑，
	// 此时不能置标记（否则初始 fit 永不执行）。
	if ui.app.CastMode == CastModeTebox {
		ui.teboxInitialFitDone = true
	}
	ui.ensureFrameChrome()

	sideW := ui.sideCollapsedW
	if ui.panelExpanded {
		sideW = ui.sideExpandedW
	}

	// 用真实客户区为基准（用户拖拽不会改写 baseWin）
	var cc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&cc)))
	clientW := cc.Right - cc.Left
	clientH := cc.Bottom - cc.Top

	sdlW := clientW - sideW
	sdlH := clientH - ui.navBarHeight

	effW, effH := srcW, srcH
	if ui.app.Rotated.Load() {
		effW, effH = srcH, srcW
	}

	minW, minH := minSdlPortraitW, minSdlPortraitH
	if effW >= effH {
		minW, minH = minSdlLandscapeW, minSdlLandscapeH
	}

	newClientW, newClientH := clientW, clientH
	if sdlW*effH > sdlH*effW {
		// 视频区过宽 → 左右黑边：保持高度、按源比例收窄宽度
		newSdlW := (sdlH*effW + effH/2) / effH
		// NoSidebar 时不 clamp minW：收窄是按视频比例，宽度由高度决定，合理大小。
		// minW=270 是 scale 1.0 的限制，批量投屏 scale<1 窗口更小，clamp 会阻止收窄导致黑边。
		if !ui.app.NoSidebar && newSdlW < minW {
			newSdlW = minW
		}
		newClientW = newSdlW + sideW
		newClientH = clientH
	} else {
		// 视频区偏窄或匹配 → 保持宽度、按源比例收窄高度
		newSdlH := (sdlW*effH + effW/2) / effW
		if !ui.app.NoSidebar && newSdlH < minH {
			newSdlH = minH
		}
		newClientW = clientW
		newClientH = newSdlH + ui.navBarHeight
	}

	// 上界 clamp 到工作区，避免撑出屏幕
	if workW, workH, ok := ui.workArea(); ok {
		maxClientW := workW - ui.frameChromeW
		maxClientH := workH - ui.frameChromeH
		if newClientW > maxClientW {
			newClientW = maxClientW
		}
		if newClientH > maxClientH {
			newClientH = maxClientH
		}
	}

	// 回写基准尺寸（面板态折算），供 REBUILD / Rotate / MinMaxInfo 使用
	if ui.panelExpanded {
		ui.baseWinW = newClientW - (ui.sideExpandedW - ui.sideCollapsedW)
	} else {
		ui.baseWinW = newClientW - sideW
	}
	ui.baseWinH = newClientH

	frameW := newClientW + ui.frameChromeW
	frameH := newClientH + ui.frameChromeH

	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetWindowRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&rc)))
	oldW := rc.Right - rc.Left
	oldH := rc.Bottom - rc.Top
	logf("[winsz] fitWindowToVideo: %dx%d → %dx%d (video %dx%d eff %dx%d client %dx%d sideK=%d)\n",
		oldW, oldH, frameW, frameH, srcW, srcH, effW, effH, newClientW, newClientH, sideW)

	// 防振荡：尺寸近乎不变则不再 SetWindowPos（避免 WM_SIZE→fit 死循环）
	if abs32(oldW-frameW) < 2 && abs32(oldH-frameH) < 2 {
		return true, false
	}

	procSetWindowPos.Call(uintptr(ui.hParent), 0,
		uintptr(rc.Left), uintptr(rc.Top), uintptr(frameW), uintptr(frameH), SWP_NOZORDER)
	return true, true
}

// tryFitPublicWindow 公网投屏（WebURL 无本地 scaler）专用窗口自适应：
// 单台公网窗口按视频比例放大到接近工作区满屏（内容完整、无黑边、不贴死屏边）；
// 批量（多窗口矩阵）由主程序网格排版定绝对尺寸，这里仅按比例去黑边。公网窗口
// 无论单台/批量都以 Controlled=true 启动，故用 countMatrixWindows()>1 区分批量。
func (ui *WindowsUI) tryFitPublicWindow() (bool, bool) {
	logf("[pubfit] delegating to fitWindowToVideo (keep small, no fullscreen enlarge)\n")
	// 单台/批量公网统一走 fitWindowToVideo：在各自初始小尺寸上按内容比例去黑边，
	// 不再放大到接近满屏（用户要求保持小尺寸，加载成功窗口不"突然放大"）。
	return ui.fitWindowToVideo()
}

// workArea returns the work-area size of the monitor hosting the window.
func (ui *WindowsUI) workArea() (int32, int32, bool) {
	hMonitor, _, _ := procMonitorFromWindow.Call(uintptr(ui.hParent), uintptr(MONITOR_DEFAULTTOPRIMARY))
	if hMonitor == 0 {
		return 0, 0, false
	}
	var mi struct {
		cbSize    uint32
		rcMonitor [4]int32
		rcWork    [4]int32
		dwFlags   uint32
	}
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	if r, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return 0, 0, false
	}
	return mi.rcWork[2] - mi.rcWork[0], mi.rcWork[3] - mi.rcWork[1], true
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// countMatrixWindows returns the number of top-level matrix player windows
// across all processes (本进程 + 其他矩阵实例)。>1 表示多台矩阵网格。
func (ui *WindowsUI) countMatrixWindows() int {
	count := 0
	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var cls [256]uint16
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 255)
		if syscall.UTF16ToString(cls[:]) == matrixClassName {
			count++
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return count
}

func (ui *WindowsUI) minFrameW() int32 {
	ui.ensureFrameChrome()
	clientW := ui.baseWinW
	if ui.panelExpanded {
		clientW = ui.baseWinW + (ui.sideExpandedW - ui.sideCollapsedW)
	}
	return clientW + ui.frameChromeW
}

func (ui *WindowsUI) minFrameH() int32 {
	return ui.baseWinH + ui.frameChromeH
}

// ToggleTopmost/ToggleFullscreen/ToggleRotate/TogglePanel/SyncState/fillScreen/videoSourceSize
