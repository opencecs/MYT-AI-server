//go:build linux

package screencast

import (
	"log"
	"math"
	"sync/atomic"
)

// Linux 布局实现（对齐 window_darwin_layout.go / window_darwin_single_portrait.go
// 的纯计算逻辑）：Linux 上 go_legacy 播放器不可用（NewProjectionWindow 返回 error），
// 这些函数仅为满足编译与调用方参数传递，使用固定兜底屏幕尺寸（1440x900）。

// linuxScreenFallbackSize Linux 无平台屏幕探测，用兜底尺寸（与 darwin 探测失败
// 时的兜底一致）
func linuxScreenFallbackSize() (int, int) { return 1440, 900 }

// currentSideW 当前侧边栏宽度（0=NoSidebar），由批量启动时 SetSideW 设置。
// 与 window_windows_layout.go / window_darwin_layout.go 同名变量互斥（build tag 隔离）。
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

// WindowPos 是单个窗口的目标位置（与 window_windows_layout.go 同名类型互斥）。
type WindowPos struct {
	X, Y     int
	W, H     int
	Col, Row int
}

// LayoutPositions 把网格布局展开成每个窗口的绝对位置（与 darwin 版算法一致）。
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

// ComputeSingleLandscapeLayout 单台横屏投屏窗口尺寸（对齐 darwin 版语义，
// 窗口 chrome extraH=28）。
func ComputeSingleLandscapeLayout(sideW int) WindowPos {
	_, _ = linuxScreenFallbackSize()

	extraH := 28
	winW := 1280.0 + float64(sideW)
	winH := 720.0 + float64(extraH)

	return WindowPos{X: WinPosUnset, Y: WinPosUnset, W: int(winW), H: int(winH)}
}

// ComputeSinglePortraitLayout 单台竖屏投屏窗口尺寸（对齐 window_darwin_single_portrait.go：
// 画面基准 480/0.7 x 798/0.7 ≈ 686x1140 × 用户倍率 + 侧栏/导航栏/chrome）。
func ComputeSinglePortraitLayout(sideW int, scale float64) WindowPos {
	cx, cy := linuxScreenFallbackSize()

	const (
		singlePortraitPicW = 480.0 / 0.7
		singlePortraitPicH = 798.0 / 0.7
	)

	extraH := 28
	picW, picH := singlePortraitPicW*scale, singlePortraitPicH*scale
	winW := picW + float64(sideW)
	winH := picH + float64(NavBarHeight) + float64(extraH)

	if float64(cx) > 0 && float64(cy) > 0 {
		maxW, maxH := float64(cx), float64(cy)
		if winW > maxW || winH > maxH {
			s := maxW / winW
			if h := maxH / winH; h < s {
				s = h
			}
			winW *= s
			winH *= s
		}
	}

	log.Printf("[layout] single portrait (linux): win=%dx%d pic=%.0fx%.0f sideW=%d scale=%.2f",
		int(winW), int(winH), picW, picH, sideW, scale)
	return WindowPos{X: WinPosUnset, Y: WinPosUnset, W: int(winW), H: int(winH)}
}

// ComputeGridLayout 网格布局（对齐 window_darwin_layout.go 的简化算法：
// cols=ceil(√n)，按视频比例双向推算取窄；linux 固定兜底屏幕尺寸）。
func ComputeGridLayout(count int, scale float64, sampleHwnd uintptr, sideW int, landscape bool) GridLayout {
	_ = scale // 与 darwin 一致，不使用 scale
	if count < 1 {
		count = 1
	}

	cx, cy := linuxScreenFallbackSize()

	extraW := 0
	extraH := 28

	repAspect := VideoAspect // 720/1280 = 0.5625
	if landscape {
		repAspect = LandscapeAspect // 1280/720
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

	clientW := availW - float64(extraW) - float64(sideW)
	clientH := availH - float64(extraH)
	if clientW <= 0 {
		clientW = availW
	}
	if clientH <= 0 {
		clientH = availH
	}

	winWfromW := availW
	winHfromW := clientW/repAspect + float64(extraH)

	winHfromH := availH
	winWfromH := clientH*repAspect + float64(sideW) + float64(extraW)

	var cellW, cellH float64
	if winHfromW > 0 && winHfromW <= availH {
		cellW, cellH = winWfromW, winHfromW
	} else if winWfromH > 0 && winWfromH <= availW {
		cellW, cellH = winWfromH, winHfromH
	} else {
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

	log.Printf("[layout:linux] count=%d base=%dx%d cell=%dx%d cols=%d rows=%d screen=%dx%d",
		count, int(baseW), int(baseH), int(cellW), int(cellH), cols, rows, cx, cy)

	return GridLayout{
		Cols: cols, Rows: rows,
		CellW: cellW, CellH: cellH,
		BaseW: baseW, BaseH: baseH,
		ExtraW: extraW, ExtraH: extraH,
		CX: cx, CY: cy,
	}
}
