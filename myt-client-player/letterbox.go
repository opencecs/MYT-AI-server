package main

// letterbox.go — letterbox 几何计算与鼠标坐标映射的共享实现。
//
// GDI Renderer 和 D3D11Renderer 各自维护缓存（lbGeo/lbStale），
// 但核心计算逻辑完全相同，提取到此处消除冗余。

// letterboxFillTolerance 受限填满的比例相对偏差阈值（10%）。
//
// 窗口比例与视频有效比例的偏差 ≤ 该阈值时，轻微拉伸填满（不出黑边，变形
// 幅度与旧 fill 行为一致，肉眼难察觉）；偏差更大时走标准 letterbox 居中黑边。
// 覆盖典型场景：单台竖屏壳子 480:798（≈0.6015）装 720:1280（0.5625）流，
// 偏差 ~6.9%，原本贴得很好的画面不再出现 ~15px 左右黑边。
const letterboxFillTolerance = 0.10

// decideThresholdFill 受限填满判定（纯几何、无状态）。
// rotated=true 且源横屏（srcW>=srcH）时，有效尺寸宽高互换。
// 源尺寸未知（≤0）不填满，保持 letterbox 待源元数据就绪后重算。
// 返回 winW==drawW && winH==drawH 的填满判定（供调用方决定是否走 fill 语义）。
func decideThresholdFill(winW, winH, srcW, srcH int32, rotated bool) bool {
	if winW <= 0 || winH <= 0 || srcW <= 0 || srcH <= 0 {
		return false
	}
	effW, effH := srcW, srcH
	if rotated && srcW >= srcH {
		effW, effH = srcH, srcW
	}
	// 相对偏差 = |winW/winH - effW/effH| / (effW/effH)，
	// 整数实现：winW*effH 与 winH*effW 的差占 winH*effW 的比例。
	winRatio := winW * effH
	srcRatio := winH * effW
	if srcRatio == 0 {
		return false
	}
	diff := winRatio - srcRatio
	if diff < 0 {
		diff = -diff
	}
	// diff/srcRatio ≤ letterboxFillTolerance  ⟺  diff*100 ≤ srcRatio*10
	return diff*100 <= srcRatio*int32(letterboxFillTolerance*100)
}

// computeLetterbox 根据窗口尺寸和视频源尺寸计算 letterbox 几何。
// rotated=true 且源横屏（srcW>=srcH）时，有效尺寸宽高互换。
//
// 受限填满语义（2026-10-28 用户拍板，替代「彻底放弃 fill」方案）：
//   - 窗口与视频比例偏差 ≤ letterboxFillTolerance（10%）→ 拉伸填满（轻微变形，
//     与旧 fill 的 ~7% 变形一致，肉眼难察觉，不出黑边）；
//   - 偏差 > 阈值 → 标准 letterbox 居中黑边（拖宽/最大化成 16:9 等大偏差场景）。
//
// fill=true 仍表示无条件填满（deprecated -fill 参数不再传入，语义保留）。
// ⚠️ 与 C 侧 d3d11render.cpp computeLetterbox 的 fill 短路对应：C 侧 fill 由
// Go 侧 recomputeLetterbox 按 decideThresholdFill 驱动 D3D11R_SetFill 同步，
// 两侧渲染几何由此保持一致；阈值调整只改本文件一处。
func computeLetterbox(winW, winH, srcW, srcH int32, rotated, fill bool) letterboxGeo {
	if fill || decideThresholdFill(winW, winH, srcW, srcH, rotated) {
		return letterboxGeo{drawW: winW, drawH: winH, winW: winW, winH: winH}
	}

	effW, effH := srcW, srcH
	if rotated && srcW > 0 && srcH > 0 && srcW >= srcH {
		effW, effH = srcH, srcW
	}

	var drawW, drawH, offX, offY int32
	if effW > 0 && effH > 0 {
		if winW*effH > winH*effW {
			drawH = winH
			drawW = (winH*effW + effH/2) / effH
			offX = (winW - drawW) / 2
		} else {
			drawW = winW
			drawH = (winW*effH + effW/2) / effW
			offY = (winH - drawH) / 2
		}
		// 黑边 ≤2 像素时直接填满，消除整数截断残余黑边（与 C 侧同阈值）。
		if offX > 0 && offX <= 2 {
			drawW = winW
			offX = 0
		}
		if offY > 0 && offY <= 2 {
			drawH = winH
			offY = 0
		}
	}
	return letterboxGeo{drawW: drawW, drawH: drawH, offX: offX, offY: offY, winW: winW, winH: winH}
}

// scaleMouseCoords 将窗口坐标 (mx,my) 映射到视频源坐标。
// rotated=true 时坐标轴互换（窗口 Y→设备 X，窗口 X→设备 Y 翻转）。
// relInBounds=false 表示点击落在 letterbox 黑边区域外。
func scaleMouseCoords(mx, my int32, g *letterboxGeo, srcW, srcH int32, rotated bool) (sx, sy uint16, ok bool) {
	if g == nil || g.drawW <= 0 || g.drawH <= 0 || srcW <= 0 || srcH <= 0 {
		return 0, 0, false
	}
	relX := int32(mx) - g.offX
	relY := int32(my) - g.offY
	if relX < 0 {
		relX = 0
	} else if relX > g.drawW {
		relX = g.drawW
	}
	if relY < 0 {
		relY = 0
	} else if relY > g.drawH {
		relY = g.drawH
	}
	var x, y int32
	if rotated {
		x = relY * srcW / g.drawH
		y = (g.drawW - relX) * srcH / g.drawW
	} else {
		x = relX * srcW / g.drawW
		y = relY * srcH / g.drawH
	}
	if x < 0 {
		x = 0
	} else if x > srcW {
		x = srcW
	}
	if y < 0 {
		y = 0
	} else if y > srcH {
		y = srcH
	}
	return uint16(x), uint16(y), true
}
