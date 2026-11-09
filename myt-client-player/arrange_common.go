package main

// arrangeScaledLayout computes the best grid layout for batch projection windows.
// Each window keeps the same client-area aspect ratio as a single projection window,
// is scaled down to fit within the screen work area, and never exceeds the original size.
// extraW/extraH = window frame size (title bar + border), used to convert client area → window size.
// Returns (windowWidth, windowHeight, bestCols).
func arrangeScaledLayout(count, areaW, areaH, singleClientW, singleClientH, extraW, extraH int, scale float64) (int, int, int) {
	if singleClientW <= 0 || singleClientH <= 0 {
		singleClientW, singleClientH = 464, 780
	}

	// 应用缩放因子
	if scale <= 0 {
		scale = 1.0
	}
	if scale < 0.6 {
		scale = 0.6
	}
	if scale > 1.0 {
		scale = 1.0
	}
	singleClientW = int(float64(singleClientW) * scale)
	singleClientH = int(float64(singleClientH) * scale)

	aspect := float64(singleClientW) / float64(singleClientH)
	// 窗口间距：无间隔排列
	const gap = 0

	bestW, bestH, bestCols := 0, 0, 1
	bestArea := 0

	for cols := 1; cols <= count; cols++ {
		rows := (count + cols - 1) / cols

		// 每个格子可用空间（减去间距）
		cellW := (areaW - gap*(cols-1)) / cols
		cellH := (areaH - gap*(rows-1)) / rows
		if cellW <= 0 || cellH <= 0 {
			continue
		}

		// 从格子大小减去边框，得到客户区可用空间
		clientW := cellW - extraW
		clientH := cellH - extraH
		if clientW <= 0 || clientH <= 0 {
			continue
		}

		// 在客户区内保持比例缩放
		cw, ch := clientW, clientH
		if float64(cw)/float64(ch) > aspect {
			cw = int(float64(ch) * aspect)
		} else {
			ch = int(float64(cw) / aspect)
		}

		// 不超过原始单窗口客户区大小
		if cw > singleClientW {
			cw = singleClientW
			ch = int(float64(cw) / aspect)
		}
		if ch > singleClientH {
			ch = singleClientH
			cw = int(float64(ch) * aspect)
		}

		if cw <= 0 || ch <= 0 {
			continue
		}

		// 转换为窗口尺寸
		ww := cw + extraW
		hh := ch + extraH

		area := cw * ch
		if area > bestArea {
			bestArea = area
			bestW = ww
			bestH = hh
			bestCols = cols
		}
	}

	return bestW, bestH, bestCols
}
