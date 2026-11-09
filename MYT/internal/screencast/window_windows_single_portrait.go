//go:build windows

package screencast

import (
	"log"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 单台竖屏画面基准（精确对齐壳子画面 480x798，比例 0.6015）：
//   壳子画面 480x798（用户实测确认），基准 = 480/0.7 x 798/0.7 ≈ 686x1140，
//   乘用户倍率（默认 0.7）还原壳子画面。
//   ⚠️ 该比例 ≠ 视频流比例（720:1280=0.5625），不传 -fill 会 letterbox 出左右
//   各 ~15px 黑边（480x798 窗内按 0.5625 等比画 448x798）。曾用 -fill 拉伸填满，
//   2026-10-28 用户拍板「画面别变形」优先于「贴合壳子」，player 侧已忽略 -fill。
const (
	singlePortraitPicW = 480.0 / 0.7 // 画面宽基准 ≈ 685.7
	singlePortraitPicH = 798.0 / 0.7 // 画面高基准 = 1140
)

// ComputeSinglePortraitLayout 单台竖屏投屏窗口尺寸（非网格）：
// 画面基准 686x1140 × 用户倍率 + 侧栏/导航栏/边框得整窗外框。
// 默认 scale=0.7、sideW=52 时窗口 548x885 → SDL 区画面恰为 480x798（壳子同款）。
// navBar 按 matrixUIScale 语义缩放（复刻 player updateMatrixUIScale）；
// 小屏放不下时 clamp 到工作区等比缩小。
func ComputeSinglePortraitLayout(sideW int, scale float64) WindowPos {
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
	picW, picH := singlePortraitPicW*scale, singlePortraitPicH*scale

	// navBar 按 matrixUIScale 缩放（与 player updateMatrixUIScale 同式）：
	// uiScale = min(clientW/BaseClientW, clientH/BaseClientH)，clamp[0.3,1.0]
	clientW := picW + float64(sideW)
	clientH := picH + float64(NavBarHeight)
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

	winW := picW + float64(sideW) + float64(frameW)
	winH := picH + navBarH + float64(frameH)

	// clamp 到工作区（小屏放不下时等比缩）
	if cx > 0 && cy > 0 {
		maxW, maxH := float64(cx), float64(cy)
		if winW > maxW || winH > maxH {
			s := minFloat(maxW/winW, maxH/winH)
			winW *= s
			winH *= s
		}
	}

	log.Printf("[layout] single portrait: win=%dx%d pic=%.0fx%.0f sideW=%d frame=%dx%d scale=%.2f work=%dx%d",
		int(winW), int(winH), picW, picH, sideW, frameW, frameH, scale, cx, cy)
	return WindowPos{X: WinPosUnset, Y: WinPosUnset, W: int(winW), H: int(winH)}
}
