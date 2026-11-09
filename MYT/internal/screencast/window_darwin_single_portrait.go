//go:build darwin

package screencast

import (
	"log"
)

// 单台竖屏画面基准（精确对齐壳子画面 480x798，同 window_windows_single_portrait.go）：
// 基准 = 480/0.7 x 798/0.7 ≈ 686x1140，乘用户倍率（默认 0.7）还原壳子画面。
// ⚠️ 比例 0.6015 ≠ 流比例 0.5625，需配合 fill（stretch 填满，轻微变形）。
const (
	singlePortraitPicW = 480.0 / 0.7
	singlePortraitPicH = 798.0 / 0.7
)

// ComputeSinglePortraitLayout 单台竖屏投屏窗口尺寸（非网格）：
// 画面基准 686x1140 × 用户倍率 + 侧栏/导航栏/chrome 得整窗外框。
// 默认 scale=0.7、sideW=52 时画面恰为 480x798（壳子同款）。
func ComputeSinglePortraitLayout(sideW int, scale float64) WindowPos {
	cx, cy := darwinScreenSize()
	if cx <= 0 || cy <= 0 {
		cx, cy = 1440, 900
	}

	extraH := 28 // macOS 窗口 chrome（与 ComputeSingleLandscapeLayout 同款估算）
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

	log.Printf("[layout] single portrait: win=%dx%d pic=%.0fx%.0f sideW=%d scale=%.2f screen=%dx%d",
		int(winW), int(winH), picW, picH, sideW, scale, cx, cy)
	return WindowPos{X: WinPosUnset, Y: WinPosUnset, W: int(winW), H: int(winH)}
}
