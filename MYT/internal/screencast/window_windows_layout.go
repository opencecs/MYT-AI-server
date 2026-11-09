//go:build windows

package screencast

import (
	"log"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// currentSideW 存当前批量投屏的侧边栏宽度（有侧边栏=SideCollapsedW，NoSidebar=0）。
// 批量启动时设置，排列按钮读取 → winw 算含 sideW，sdlArea 比例=视频比例无黑边。
var currentSideW atomic.Int32

// SetSideW 更新当前侧边栏宽度（批量启动时调用）。
func SetSideW(w int) {
	if w < 0 {
		w = 0
	}
	currentSideW.Store(int32(w))
}

// GetSideW 读取当前侧边栏宽度，0=NoSidebar。
func GetSideW() int {
	return int(currentSideW.Load())
}

// ─── 网格布局计算 ───────────────────────────────────────────────────────────
// 纯计算：给定窗口数 + 缩放，算出网格参数（列数、行数、单元尺寸）。
// 批量预计算（启动时定位）和手动重排（滑块）共用，保证布局一致。
// 从 window_windows_arrange.go 提取，避免排列逻辑膨胀 + 消除重复。

// GridLayout 是一次网格布局计算的结果。
type GridLayout struct {
	Cols     int     // 列数
	Rows     int     // 行数
	CellW    float64 // 单元宽度（含边框，= 客户区宽 + extraW）
	CellH    float64 // 单元高度（含边框，= 客户区高 + extraH）
	BaseW    float64 // 客户区净宽（不含边框）
	BaseH    float64 // 客户区净高（不含边框）
	ExtraW   int     // 窗口边框宽（左右各一份）
	ExtraH   int     // 窗口标题栏+边框高
	CX       int     // 屏幕宽（工作区）
	CY       int     // 屏幕高（工作区）
	WorkLeft int     // 工作区左偏移（任务栏在左时>0）
	WorkTop  int     // 工作区顶偏移（任务栏在顶时>0）
}

// windowMetricsOnce 计算窗口边框尺寸（extraW/extraH），缓存避免重复测量。
// extraW/extraH 取决于窗口样式（WS_THICKFRAME 等），样式固定则只需算一次。
var windowMetricsOnce struct {
	done   bool
	extraW int
	extraH int
}

// computeWindowMetrics 测量窗口边框（标题栏+边框，不含 navBar）尺寸并缓存。
// 优先用已有窗口实测（最准）；无窗口时用 AdjustWindowRect 按批量投屏窗口样式估算。
// navBar 不含在内（批量小窗口 navBar 被 matrixUIScale 缩放，需在 ComputeGridLayout 动态算）。
func computeWindowMetrics(sampleHwnd uintptr) (frameW, frameH int) {
	if windowMetricsOnce.done {
		return windowMetricsOnce.extraW, windowMetricsOnce.extraH
	}
	u32 := windows.NewLazyDLL("user32.dll")
	// 优先实测已有窗口
	if sampleHwnd != 0 {
		pGetClientRect := u32.NewProc("GetClientRect")
		pGetWindowRect := u32.NewProc("GetWindowRect")
		var rcClient, rcWindow struct{ Left, Top, Right, Bottom int32 }
		pGetClientRect.Call(sampleHwnd, uintptr(unsafe.Pointer(&rcClient)))
		ret, _, _ := pGetWindowRect.Call(sampleHwnd, uintptr(unsafe.Pointer(&rcWindow)))
		if ret != 0 {
			frameW = int(rcWindow.Right-rcWindow.Left) - int(rcClient.Right-rcClient.Left)
			frameH = int(rcWindow.Bottom-rcWindow.Top) - int(rcClient.Bottom-rcClient.Top)
			if frameW < 0 {
				frameW = 0
			}
			if frameH < 0 {
				frameH = 0
			}
			windowMetricsOnce.done = true
			windowMetricsOnce.extraW = frameW
			windowMetricsOnce.extraH = frameH
			return frameW, frameH
		}
	}
	// 无窗口：用 AdjustWindowRect 按批量投屏窗口样式估算。
	// 样式与 ui_windows_create.go 中 Controlled 窗口一致：
	// WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX | WS_THICKFRAME | WS_MAXIMIZEBOX
	pAdjustWindowRect := u32.NewProc("AdjustWindowRect")
	const (
		WS_OVERLAPPED  = 0x00000000
		WS_CAPTION     = 0x00C00000
		WS_SYSMENU     = 0x00080000
		WS_MINIMIZEBOX = 0x00020000
		WS_THICKFRAME  = 0x00040000
		WS_MAXIMIZEBOX = 0x00010000
	)
	style := uintptr(WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX | WS_THICKFRAME | WS_MAXIMIZEBOX)
	var rc struct{ Left, Top, Right, Bottom int32 }
	rc.Right = 100 // 任意客户区尺寸，只需差值
	rc.Bottom = 100
	pAdjustWindowRect.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	frameW = int(rc.Right-rc.Left) - 100
	frameH = int(rc.Bottom-rc.Top) - 100
	if frameW < 0 {
		frameW = 0
	}
	if frameH < 0 {
		frameH = 0
	}
	windowMetricsOnce.done = true
	windowMetricsOnce.extraW = frameW
	windowMetricsOnce.extraH = frameH
	return frameW, frameH
}

// ComputeGridLayout 计算网格布局（完全照旧客户端 ArrangeProjectionWindows 算法）。
// count: 要排列的窗口数；scale: 保留参数（旧客户端无 scale，不再使用，传任意值）；
// sampleHwnd: 边框测量窗口句柄（0=用估算）；sideW: 侧边栏宽（有侧边栏=SideCollapsedW，NoSidebar=0）；
// landscape: 横屏投屏（-landscape 视频不旋转）时视频区按 1280x720 比例算格子，
// 否则按横屏源转竖后的 720x1280 算——窗口形状必须与视频实际显示比例匹配，错配即大黑边。
// winw 必须含 sideW，否则 sdlArea(=客户区-sideW-navBar) 比例偏移 → 黑边。
func ComputeGridLayout(count int, scale float64, sampleHwnd uintptr, sideW int, landscape bool) GridLayout {
	_ = scale // 旧客户端无 scale 概念，排列铺满屏幕，此参数不再使用
	if count < 1 {
		count = 1
	}
	u32 := windows.NewLazyDLL("user32.dll")
	pGetSystemMetrics := u32.NewProc("GetSystemMetrics")
	pSystemParametersInfoW := u32.NewProc("SystemParametersInfoW")
	const (
		SM_CXSCREEN     = 0
		SM_CYSCREEN     = 1
		SPI_GETWORKAREA = 0x0030
	)
	// 优先用工作区（排除任务栏），让窗口不被任务栏遵住。
	// 任务栏在底部/侧边都能正确处理。失败时 fallback 到全屏。
	var workArea struct{ Left, Top, Right, Bottom int32 }
	cx, cy := 0, 0
	if ret, _, _ := pSystemParametersInfoW.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&workArea)), 0); ret != 0 {
		cx = int(workArea.Right - workArea.Left)
		cy = int(workArea.Bottom - workArea.Top)
	}
	if cx == 0 || cy == 0 {
		cxScreen, _, _ := pGetSystemMetrics.Call(SM_CXSCREEN)
		cyScreen, _, _ := pGetSystemMetrics.Call(SM_CYSCREEN)
		cx = int(cxScreen)
		cy = int(cyScreen)
	}

	// 窗口边框（标题栏+边框，不含 navBar）。批量小窗口 navBar 被 player
	// matrixUIScale 缩放（48→18），固定 NavBarHeight 会导致 sdlArea 比例偏移黑边。
	frameW, frameH := computeWindowMetrics(sampleHwnd)

	// 统一网格基准：横屏源 1280x720 转竖后 eff=720x1280，比例 720:1280（VideoAspect）。
	// 用视频源实际比例（而非旧 464:780）排列，窗口与视频匹配，无需 fit 收窄 →
	// 无左右黑边、无网格空隙。竖屏源(480:800)比例 0.6 略宽，黑边≤6px 由 letterbox
	// 填满阈值处理（拉伸无感，不 fit 制造空隙）。
	// 横屏投屏（-landscape）视频不旋转，格子比例换成 1280x720。
	repAspect := VideoAspect
	if landscape {
		repAspect = LandscapeAspect
	}

	// 旧客户端无 scale，不再使用 scale 校验
	// 完全照旧客户端 ArrangeProjectionWindows 算法：
	//   cols = ceil(√n)，rows = ceil(n/cols)
	//   rowHeight = 屏高/rows - 2×margin（旧客户端这是窗口整体高）
	//   windowHeight = rowHeight，windowWidth = rowHeight × 9/16
	//   若 windowWidth > 屏宽/cols - 2×margin：以宽算高
	// 差异：我们有 navBar(48)+边框，windowHeight 是含这些的窗口整体高，
	// 视频区 baseH = windowHeight - extraH。cellH = windowHeight（不再加 extraH）。
	const margin = 2.0

	// 枚举列数 c=1..n，选窗口面积最大的布局（比固定 ceil√n 铺满率更高）。
	// 每个列数算 rows=ceil(n/c)，双向推算窗口尺寸取窄（保证不超屏），选面积最大者。
	bestCols, bestRows := 1, count
	var bestW, bestH float64
	for c := 1; c <= count; c++ {
		r := (count + c - 1) / c
		// 每格可用空间：availW = (屏宽 - (c-1)×margin) / c，availH 同理
		availW := (float64(cx) - float64(c-1)*margin) / float64(c)
		availH := (float64(cy) - float64(r-1)*margin) / float64(r)
		if availW <= 0 || availH <= 0 {
			continue
		}

		// navBar + sideW 都按 matrixUIScale 缩放（复刻 player updateMatrixUIScale）：
		//   scale = min(客户区宽/baseClientW, 客户区高/baseClientH)，clamp[0.3,1.0]
		//   navBar = clamp(NavBarHeight×scale, NavBarMinHeight, NavBarHeight)
		//   sideW = clamp(SideCollapsedW×scale, SideCollapsedMin, SideCollapsedW)
		// 客户区宽高用 avail-frame 估算（缩放比例足够准）。
		clientW := availW - float64(frameW)
		clientH := availH - float64(frameH)
		scaleW := clientW / BaseClientW
		scaleH := clientH / BaseClientH
		uiScale := scaleW
		if scaleH < uiScale {
			uiScale = scaleH
		}
		if uiScale > 1.0 {
			uiScale = 1.0
		}
		if uiScale < 0.3 {
			uiScale = 0.3
		}
		navBarH := float64(NavBarHeight) * uiScale
		if navBarH < NavBarMinHeight {
			navBarH = NavBarMinHeight
		}
		// sideW 按 matrixUIScale 缩放（NoSidebar 时传入 sideW=0，缩放后仍 0）
		scaledSideW := float64(sideW) * uiScale
		if sideW > 0 && scaledSideW < SideCollapsedMin {
			scaledSideW = SideCollapsedMin
		}
		extraHf := float64(frameH) + navBarH // 边框高 + 缩放后 navBar

		// 双向推算窗口尺寸，取窄者（保证不超屏）：
		// 从宽推：sdlW = availW - sideW - extraW → sdlH = sdlW/repAspect → winH = sdlH + navBar + frameH
		// 从高推：sdlH = availH - navBar - frameH → sdlW = sdlH×repAspect → winW = sdlW + sideW + extraW
		// sdlArea = (winW - sideW - extraW) × (winH - navBar - frameH)，比例 = repAspect 无黑边。
		// sideW 用缩放后的 scaledSideW（与 player 实际一致），避免主程序多算 sideW → sdlArea 偏胖黑边。
		// 从宽推
		sdlW := availW - scaledSideW - float64(frameW)
		winHfromW := 0.0
		winWfromW := 0.0
		if sdlW > 0 {
			sdlH := sdlW / repAspect
			winHfromW = sdlH + extraHf
			winWfromW = availW // 宽用满
		}
		// 从高推
		sdlH := availH - extraHf
		winWfromH := 0.0
		winHfromH := 0.0
		if sdlH > 0 {
			sdlWfromH := sdlH * repAspect
			winWfromH = sdlWfromH + scaledSideW + float64(frameW)
			winHfromH = availH // 高用满
		}

		// 取窄方向（两边都不超可用空间）
		var w, h float64
		if winHfromW > 0 && winHfromW <= availH && winWfromW > 0 {
			// 从宽推的高不超 availH → 用从宽推
			w, h = winWfromW, winHfromW
		} else if winWfromH > 0 && winWfromH <= availW && winHfromH > 0 {
			// 从高推的宽不超 availW → 用从高推
			w, h = winWfromH, winHfromH
		} else if winHfromW > 0 && winWfromH > 0 {
			// 都超了，取面积较小者（至少能放下）
			if winWfromW*winHfromW <= winWfromH*winHfromH {
				w, h = winWfromW, winHfromW
			} else {
				w, h = winWfromH, winHfromH
			}
		} else {
			continue
		}

		// 选窗口面积最大的列数
		if w*h > bestW*bestH {
			bestW, bestH = w, h
			bestCols, bestRows = c, r
		}
	}

	cols, rows := bestCols, bestRows
	windowWidth, windowHeight := bestW, bestH

	// 重新算 extraH/navBar（用选中列数的 avail，保证与枚举时一致）
	availH := (float64(cy) - float64(rows-1)*margin) / float64(rows)
	availW := (float64(cx) - float64(cols-1)*margin) / float64(cols)
	clientW := availW - float64(frameW)
	clientH := availH - float64(frameH)
	uiScale := clientW / BaseClientW
	if h := clientH / BaseClientH; h < uiScale {
		uiScale = h
	}
	if uiScale > 1.0 {
		uiScale = 1.0
	}
	if uiScale < 0.3 {
		uiScale = 0.3
	}
	navBarH := float64(NavBarHeight) * uiScale
	if navBarH < NavBarMinHeight {
		navBarH = NavBarMinHeight
	}
	extraW := frameW
	extraH := int(float64(frameH) + navBarH) // 边框高 + 缩放后 navBar

	// cell = 窗口整体尺寸。base = 客户区净尺寸（不含边框）。
	cellW := windowWidth
	cellH := windowHeight
	baseW := windowWidth - float64(extraW)
	baseH := windowHeight - float64(extraH)
	if baseW < MinBaseW {
		baseW = MinBaseW
		baseH = baseW / repAspect
		cellW = baseW + float64(extraW)
		cellH = baseH + float64(extraH)
	}

	log.Printf("[layout] count=%d extra=%dx%d base=%dx%d cell=%dx%d cols=%d rows=%d screen=%dx%d",
		count, extraW, extraH, int(baseW), int(baseH), int(cellW), int(cellH), cols, rows, cx, cy)

	return GridLayout{
		Cols: cols, Rows: rows,
		CellW: cellW, CellH: cellH,
		BaseW: baseW, BaseH: baseH,
		ExtraW: extraW, ExtraH: extraH,
		CX: cx, CY: cy,
		WorkLeft: int(workArea.Left), WorkTop: int(workArea.Top),
	}
}

// ComputeSingleLandscapeLayout 单台横屏投屏窗口尺寸（非网格）：视频源原生分辨率
// 1:1（1280x720 无缩放最清晰，窗口 ~1348x799），比 4 格基准大 60%，吃满剩余屏宽；
// 侧栏展开后高度 ~760，功能列表不再滚动（批量小窗滚动是 8 台分屏的硬约束）。
// 尺寸 clamp 到工作区（小屏自适应等比缩小，保 16:9）。竖屏单台走网格基准
// （原生 720x1280 高 1359 超屏放不下），不经过此函数。
func ComputeSingleLandscapeLayout(sideW int) WindowPos {
	u32 := windows.NewLazyDLL("user32.dll")
	pSystemParametersInfoW := u32.NewProc("SystemParametersInfoW")
	const SPI_GETWORKAREA = 0x0030
	var workArea struct{ Left, Top, Right, Bottom int32 }
	cx, cy := 0, 0
	if ret, _, _ := pSystemParametersInfoW.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&workArea)), 0); ret != 0 {
		cx = int(workArea.Right - workArea.Left)
		cy = int(workArea.Bottom - workArea.Top)
	}

	frameW, frameH := computeWindowMetrics(0)

	// 横屏单台：视频原生 1280x720 + 全尺寸 chrome（大窗 uiScale≈1，navBar/side 不缩）
	sdlW, sdlH := 1280.0, 720.0
	navBarH := float64(NavBarHeight)
	winW := sdlW + float64(sideW) + float64(frameW)
	winH := sdlH + navBarH + float64(frameH)

	// clamp 到工作区（小屏放不下原生时等比缩，保 16:9）
	if cx > 0 && cy > 0 {
		maxW, maxH := float64(cx), float64(cy)
		if winW > maxW || winH > maxH {
			scale := minFloat(maxW/winW, maxH/winH)
			winW *= scale
			winH *= scale
		}
	}

	log.Printf("[layout] single landscape: win=%dx%d sideW=%d frame=%dx%d work=%dx%d",
		int(winW), int(winH), sideW, frameW, frameH, cx, cy)
	return WindowPos{X: WinPosUnset, Y: WinPosUnset, W: int(winW), H: int(winH)}
}

// WindowPos 是单个窗口的目标位置（窗口整体坐标，含边框，CreateWindow 直接用）。
type WindowPos struct {
	X, Y     int
	W, H     int
	Col, Row int
}

// LayoutPositions 把网格布局展开成每个窗口的绝对位置。
// 横向 step 用客户区净宽 baseW，窗口整宽 = baseW + extraW，
// 相邻窗口重叠量恰好 = extraW（边框宽），右邻完全覆盖左窗边框 → 视觉无缝。
// 纵向按整窗高度 step 保留标题栏可见。
func (g GridLayout) LayoutPositions() []WindowPos {
	positions := make([]WindowPos, g.Cols*g.Rows)
	const margin = 2.0
	for i := 0; i < len(positions); i++ {
		col := i % g.Cols
		row := i / g.Cols
		// x = margin + col×(winW+margin)，y = margin + row×(winH+margin)，加 workArea 偏移
		x := float64(col)*(g.CellW+margin) + margin + float64(g.WorkLeft)
		y := float64(row)*(g.CellH+margin) + margin + float64(g.WorkTop)
		positions[i] = WindowPos{
			X: int(x), Y: int(y),
			W:   int(g.CellW),
			H:   int(g.CellH),
			Col: col, Row: row,
		}
	}
	return positions
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
