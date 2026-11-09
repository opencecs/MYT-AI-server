//go:build darwin

package screencast

import (
	"log"
	"math"
	"sync/atomic"
)

// currentSideW 当前侧边栏宽度（0=NoSidebar），由批量启动时 SetSideW 设置。
// 与 window_windows_layout.go 同名变量互斥（build tag 隔离），darwin/Windows 各自维护。
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

// GridLayout 是一次网格布局计算的结果（与 window_windows_layout.go 同名类型互斥）。
type GridLayout struct {
	Cols     int
	Rows     int
	CellW    float64
	CellH    float64
	BaseW    float64
	BaseH    float64
	ExtraW   int
	ExtraH   int
	CX       int
	CY       int
	WorkLeft int
	WorkTop  int
}

// ComputeSingleLandscapeLayout 单台横屏投屏窗口尺寸（与 Windows 版语义一致）：
// 视频原生 1280x720 + macOS 窗口 chrome，clamp 到屏幕。竖屏单台走网格基准不经过此函数。
func ComputeSingleLandscapeLayout(sideW int) WindowPos {
	cx, cy := darwinScreenSize()
	if cx <= 0 || cy <= 0 {
		cx, cy = 1440, 900
	}

	extraW := 0
	extraH := 28

	winW := 1280.0 + float64(sideW) + float64(extraW)
	winH := 720.0 + float64(extraH)

	if float64(cx) > 0 && float64(cy) > 0 {
		maxW, maxH := float64(cx), float64(cy)
		if winW > maxW || winH > maxH {
			scale := maxW / winW
			if h := maxH / winH; h < scale {
				scale = h
			}
			winW *= scale
			winH *= scale
		}
	}

	return WindowPos{X: WinPosUnset, Y: WinPosUnset, W: int(winW), H: int(winH)}
}

// WindowPos 是单个窗口的目标位置（与 window_windows_layout.go 同名类型互斥）。
type WindowPos struct {
	X, Y     int
	W, H     int
	Col, Row int
}

// LayoutPositions 把网格布局展开成每个窗口的绝对位置。
// 算法与 window_windows_layout.go 一致：横向按 CellW+margin step，纵向按 CellH+margin step。
func (g GridLayout) LayoutPositions() []WindowPos {
	positions := make([]WindowPos, g.Cols*g.Rows)
	const margin = 2.0
	for i := 0; i < len(positions); i++ {
		col := i % g.Cols
		row := i / g.Cols
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

// ComputeGridLayout macOS 网格布局：用 darwinScreenSize() 获取屏幕尺寸，
// 简化版算法（cols=ceil(√n)，按视频比例 720:1280 双向推算取窄）。
// 不复刻 Windows 版的 navBar/sideW matrixUIScale 缩放（darwin player 走 Metal/Cocoa
// 路径，UI 缩放机制不同），但保证 positions 有合理值让批量投屏窗口排成网格。
// landscape: 横屏投屏时格子比例换成 1280x720（与 Windows 版语义一致）。
func ComputeGridLayout(count int, scale float64, sampleHwnd uintptr, sideW int, landscape bool) GridLayout {
	_ = scale // darwin 不使用 scale（与 Windows 一致，旧客户端无 scale 概念）
	if count < 1 {
		count = 1
	}

	cx, cy := darwinScreenSize()
	if cx <= 0 || cy <= 0 {
		// darwinScreenSize 失败（未授权辅助功能权限等），用兜底尺寸避免 0×0 网格
		cx, cy = 1440, 900
	}

	// macOS 窗口边框：Cocoa titled 窗口标题栏 ~28px，无侧边框
	extraW := 0
	extraH := 28

	repAspect := VideoAspect // 720/1280 = 0.5625
	if landscape {
		repAspect = LandscapeAspect // 1280/720，横屏视频不旋转
	}
	const margin = 2.0

	cols := int(math.Ceil(math.Sqrt(float64(count))))
	if cols < 1 {
		cols = 1
	}
	rows := (count + cols - 1) / cols

	availW := (float64(cx) - float64(cols-1)*margin) / float64(cols)
	availH := (float64(cy) - float64(rows-1)*margin) / float64(rows)
	if availW <= 0 || availH <= 0 {
		return GridLayout{Cols: 1, Rows: count, CX: cx, CY: cy}
	}

	// 双向推算取窄（保证不超屏）
	clientW := availW - float64(extraW) - float64(sideW)
	clientH := availH - float64(extraH)
	if clientW <= 0 {
		clientW = availW
	}
	if clientH <= 0 {
		clientH = availH
	}

	// 从宽推：sdlW = clientW → sdlH = sdlW/repAspect → winH = sdlH + extraH
	winWfromW := availW
	winHfromW := clientW/repAspect + float64(extraH)

	// 从高推：sdlH = clientH → sdlW = sdlH×repAspect → winW = sdlW + sideW + extraW
	winHfromH := availH
	winWfromH := clientH*repAspect + float64(sideW) + float64(extraW)

	var cellW, cellH float64
	if winHfromW > 0 && winHfromW <= availH {
		cellW, cellH = winWfromW, winHfromW
	} else if winWfromH > 0 && winWfromH <= availW {
		cellW, cellH = winWfromH, winHfromH
	} else {
		// 都超了，取面积较小者
		if winWfromW*winHfromW <= winWfromH*winHfromH {
			cellW, cellH = winWfromW, winHfromW
		} else {
			cellW, cellH = winWfromH, winHfromH
		}
	}

	baseW := cellW - float64(extraW) - float64(sideW)
	baseH := cellH - float64(extraH)
	if baseW < MinBaseW {
		baseW = MinBaseW
		baseH = baseW / repAspect
		cellW = baseW + float64(extraW) + float64(sideW)
		cellH = baseH + float64(extraH)
	}

	log.Printf("[layout:darwin] count=%d extra=%dx%d base=%dx%d cell=%dx%d cols=%d rows=%d screen=%dx%d",
		count, extraW, extraH, int(baseW), int(baseH), int(cellW), int(cellH), cols, rows, cx, cy)

	return GridLayout{
		Cols: cols, Rows: rows,
		CellW: cellW, CellH: cellH,
		BaseW: baseW, BaseH: baseH,
		ExtraW: extraW, ExtraH: extraH,
		CX: cx, CY: cy,
	}
}
