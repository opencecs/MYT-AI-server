//go:build windows

package main

import (
	"fmt"
	"unsafe"
)

func debugLog(msg string) {
	logf("[layout] %s\n", msg)
}

// baseClientSize 是单个投屏窗口的基准客户区大小（竖屏），用于矩阵投屏时按比例缩放 UI 元素。
const baseClientW int32 = 464
const baseClientH int32 = 780

// 原始 UI 尺寸常量（NewWindowsUI 中赋给字段的默认值）
const rawSideCollapsedW int32 = 52
const rawSideExpandedW int32 = 300
const rawNavBarHeight int32 = 48

// updateMatrixUIScale 根据当前窗口客户区大小，在矩阵投屏模式下缩放 UI 尺寸。
// 直接修改 ui 字段，确保 layoutChildren / TogglePanel / fitWindowToVideo 等所有地方一致。
func (ui *WindowsUI) updateMatrixUIScale() {
	if !ui.app.Controlled {
		ui.sideCollapsedW = rawSideCollapsedW
		ui.sideExpandedW = rawSideExpandedW
		ui.navBarHeight = rawNavBarHeight
		return
	}
	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&rc)))
	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top
	if w <= 0 || h <= 0 {
		return
	}
	// 按窗口与基准的比例缩放
	scaleW := float64(w) / float64(baseClientW)
	scaleH := float64(h) / float64(baseClientH)
	s := scaleW
	if scaleH < s {
		s = scaleH
	}
	if s > 1.0 {
		s = 1.0
	}
	if s < 0.3 {
		s = 0.3
	}
	if !ui.app.NoSidebar {
		ui.sideCollapsedW = int32(float64(rawSideCollapsedW) * s)
		if ui.sideCollapsedW < sideCollapsedMin {
			ui.sideCollapsedW = sideCollapsedMin
		}
		// 展开宽度不随 matrixUIScale 缩小：WebView2 内容按 CSS 固定像素渲染，
		// 不随窗口缩放——侧栏窗口缩到 300 设计宽以下只会把 UI 挤成一团
		// （批量小窗口曾压到 200 下限，输入框/四宫格全挤），不会省任何空间。
		// 展开是临时态，窗口按需长宽（TogglePanel +sideW 差值），保持设计宽最舒适。
		ui.sideExpandedW = rawSideExpandedW
	}
	ui.navBarHeight = int32(float64(rawNavBarHeight) * s)
	if ui.navBarHeight < navBarMinHeight {
		ui.navBarHeight = navBarMinHeight
	}
}

func (ui *WindowsUI) layoutChildren() {
	ui.layoutChildrenResize(true)
}

// layoutChildrenResize 布局子窗口。fullResize=false 时渲染器只轻量更新尺寸（拖动中用），
// true 时渲染器真正 ResizeBuffers。
func (ui *WindowsUI) layoutChildrenResize(fullResize bool) {
	ui.updateMatrixUIScale()

	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(uintptr(ui.hParent), uintptr(unsafe.Pointer(&rc)))
	w := rc.Right - rc.Left
	h := rc.Bottom - rc.Top

	// 是否处于"填满屏幕"状态：显式全屏(ToggleFullscreen) 或系统最大化(顶部栏全屏按钮)。
	// 该状态下窗口尺寸由系统固定，无法再按内容比例缩放窗口。
	fill := ui.fillScreen()
	if fill {
		if ui.app.WebURL != "" {
			// 公网投屏：窗口固定填满屏幕，不能再缩放窗口比例。改为在窗口内对 WebView
			// 视频区做等比例 letterbox（四周露黑、内容居中），避免内容被拉伸("拉没了")。
			// 本地(D3D11)由渲染器内部居中，不走此分支，保持整窗填充。
			ui.repositionWebFill(w, h)
			ui.blackBars = true
			procInvalidateRect.Call(uintptr(ui.hParent), 0, 1)
			procUpdateWindow.Call(uintptr(ui.hParent))
			return
		}
		// 本地投屏全屏/最大化：不再特判——直接落普通布局，右侧功能栏照常显示（用户要求）。
		// 横屏 16:9 视频在宽屏全屏本就有左右黑边，把功能栏放右侧比纯黑边实用；
		// 侧栏里的全屏按钮也是全屏态的退出入口（旧版藏侧栏后全屏没有任何退出按钮）。
		// 视频区 letterbox 由 D3D11 渲染器内部处理（任何窗口比例都居中无拉伸）。
	}
	ui.blackBars = false

	sideW := ui.sideCollapsedW
	if ui.panelExpanded {
		sideW = ui.sideExpandedW
	}
	leftW := w - sideW
	if leftW < 50 {
		leftW = 50
	}
	sdlH := h - ui.navBarHeight
	if sdlH < 0 {
		sdlH = 0
	}
	// sdlArea 填满客户区（主程序 winw 已含 sideW，sdlArea=客户区-sideW-navBar 比例=视频比例，
	// 无需收窄/居中，D3D11 letterbox off=0 无黑边）。navBar 贴底（y=sdlH=h-navBar，无留白）。
	videoX := int32(0)
	debugLog(fmt.Sprintf("[layout] leftW=%d sdlH=%d sideW=%d videoX=%d w=%d h=%d panelExp=%v nosidebar=%v", leftW, sdlH, sideW, videoX, w, h, ui.panelExpanded, ui.app.NoSidebar))
	procMoveWindow.Call(uintptr(ui.hSdlArea), uintptr(videoX), 0, uintptr(leftW), uintptr(sdlH), 1)
	procMoveWindow.Call(uintptr(ui.hNavBar), uintptr(videoX), uintptr(sdlH), uintptr(leftW), uintptr(ui.navBarHeight), 1)
	procMoveWindow.Call(uintptr(ui.hSidePanel), uintptr(leftW), 0, uintptr(sideW), uintptr(h), 1)
	procInvalidateRect.Call(uintptr(ui.hNavBar), 0, 0)
	if d3d := ui.app.D3D11Ren.Load(); d3d != nil {
		if fullResize {
			d3d.UpdateSize(leftW, sdlH)
		} else {
			d3d.UpdateSizeLight(leftW, sdlH)
		}
	} else if ren := ui.app.Renderer.Load(); ren != nil {
		ren.UpdateSize(leftW, sdlH)
	}
}
func (ui *WindowsUI) showWindowAndLayout() {
	if ui.app.Hidden {
		return
	}
	procShowWindow.Call(uintptr(ui.hParent), SW_SHOW)
}

// repositionWebFill 公网投屏全屏/最大化时：窗口固定填满屏幕，无法再缩放窗口比例，
// 改为把 WebView 视频区按内容比例等比例 letterbox 居中放在窗口内，四周露黑。
// 视频尺寸未知时整窗填充（避免空白），待 fitprobe 上报后由 WM_APP_PUBFIT 重排。
func (ui *WindowsUI) repositionWebFill(w, h int32) {
	procMoveWindow.Call(uintptr(ui.hNavBar), 0, 0, 0, 0, 0)
	procMoveWindow.Call(uintptr(ui.hSidePanel), 0, 0, 0, 0, 0)
	sw := ui.webVideoW.Load()
	sh := ui.webVideoH.Load()
	logf("[webfill] fill w=%d h=%d web=%dx%d rotated=%v\n", w, h, sw, sh, ui.app.Rotated.Load())
	if sw <= 0 || sh <= 0 {
		procMoveWindow.Call(uintptr(ui.hSdlArea), 0, 0, uintptr(w), uintptr(h), 1)
		if ui.webChromium != nil {
			ui.webChromium.Resize()
		}
		return
	}
	effW, effH := sw, sh
	if ui.app.Rotated.Load() {
		effW, effH = sh, sw
	}
	var vw, vh int32
	if w*effH > h*effW {
		vh = h
		vw = (h*effW + effH/2) / effH
	} else {
		vw = w
		vh = (w*effH + effW/2) / effW
	}
	vx := (w - vw) / 2
	vy := (h - vh) / 2
	logf("[webfill] video=%dx%d off=%d,%d\n", vw, vh, vx, vy)
	procMoveWindow.Call(uintptr(ui.hSdlArea), uintptr(vx), uintptr(vy), uintptr(vw), uintptr(vh), 1)
	if ui.webChromium != nil {
		ui.webChromium.Resize()
	}
}

// handleSizing constrains window resize to maintain video aspect ratio.
func (ui *WindowsUI) handleSizing(wParam, lParam uintptr) {
	scaler := ui.activeScaler()
	if scaler == nil {
		return
	}
	srcW, srcH := scaler.SourceSize()
	if srcW <= 0 || srcH <= 0 {
		return
	}

	type rect struct{ Left, Top, Right, Bottom int32 }
	pRect := (*rect)(unsafe.Pointer(lParam))
	r := *pRect

	ui.ensureFrameChrome()
	frameW := ui.frameChromeW
	frameH := ui.frameChromeH

	effW, effH := srcW, srcH
	if ui.app.Rotated.Load() {
		effW, effH = srcH, srcW
	}
	// minSdl 最小尺寸已统一到 WM_GETMINMAXINFO 的 100×140（不再在 WM_SIZING clamp）

	sideW := ui.sideCollapsedW
	if ui.panelExpanded {
		sideW = ui.sideExpandedW
	}

	propTotalW := r.Right - r.Left
	propTotalH := r.Bottom - r.Top
	propW := propTotalW - frameW - sideW
	propH := propTotalH - frameW - ui.navBarHeight
	// min/max 仅手动拖拽生效。NoSidebar 批量投屏（controlled 禁拖）跳过，
	// 允许缩到排版尺寸，避免与排版冲突。有侧边栏单台仍保留最小尺寸保护。
	if !ui.app.NoSidebar {
		propW = max(propW, 100)
		propH = max(propH, 140)
	}

	edge := uint32(wParam)
	switch edge {
	case WMSZ_LEFT, WMSZ_RIGHT:
		lockedH := (propW*effH + effW/2) / effW
		if !ui.app.NoSidebar {
			lockedH = max(lockedH, 140)
		}
		totalH := lockedH + ui.navBarHeight + frameH
		if edge == WMSZ_LEFT {
			r.Left = r.Right - (propW + sideW + frameW)
		} else {
			r.Right = r.Left + (propW + sideW + frameW)
		}
		r.Bottom = r.Top + totalH

	case WMSZ_TOP, WMSZ_BOTTOM:
		lockedW := (propH*effW + effH/2) / effH
		if !ui.app.NoSidebar {
			lockedW = max(lockedW, 100)
		}
		totalW := lockedW + sideW + frameW
		if edge == WMSZ_TOP {
			r.Top = r.Bottom - (propH + ui.navBarHeight + frameH)
		} else {
			r.Bottom = r.Top + (propH + ui.navBarHeight + frameH)
		}
		r.Right = r.Left + totalW

	default: // corners
		var lockedW, lockedH int32
		if propW*effH > propH*effW {
			lockedH = propH
			lockedW = (propH*effW + effH/2) / effH
		} else {
			lockedW = propW
			lockedH = (propW*effH + effW/2) / effW
		}
		if !ui.app.NoSidebar {
			lockedW = max(lockedW, 100)
			lockedH = max(lockedH, 140)
		}
		totalW := lockedW + sideW + frameW
		totalH := lockedH + ui.navBarHeight + frameH
		switch edge {
		case WMSZ_TOPLEFT:
			r.Left = r.Right - totalW
			r.Top = r.Bottom - totalH
		case WMSZ_TOPRIGHT:
			r.Right = r.Left + totalW
			r.Top = r.Bottom - totalH
		case WMSZ_BOTTOMLEFT:
			r.Left = r.Right - totalW
			r.Bottom = r.Top + totalH
		case WMSZ_BOTTOMRIGHT:
			r.Right = r.Left + totalW
			r.Bottom = r.Top + totalH
		}
	}
	*pRect = r
}

func (ui *WindowsUI) handleDpiChanged(lParam uintptr) {
	ui.frameChromeW = 0
	ui.frameChromeH = 0
	ui.layoutChildren()
}

// fitWindowToVideo adapts the window so the video area aspect ratio matches the
// source, eliminating letterbox black bars. Two-way: if the video area is wider
// than the source (left/right bars) it narrows to fit height; if narrower
// (top/bottom bars) it shrinks to fit width. Returns (done, changed): done=false
// means the fit was skipped (caller should not mark the source as fitted);
// changed=true means the window size was actually modified this call (caller
// should skip presenting this tick to avoid showing an uninitialized buffer).
// fitWindowToVideo/tryFitPublicWindow/workArea/abs32/countMatrixWindows/minFrameW/minFrameH 已拆分到 ui_windows_fit.go
// Toggle* 已拆分到 ui_windows_toggles.go
