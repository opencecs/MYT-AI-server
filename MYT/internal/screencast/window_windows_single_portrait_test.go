//go:build windows

package screencast

import (
	"testing"
)

// TestComputeSinglePortraitLayout 验证单台竖屏窗口尺寸：
// 目标是「窗口内画面（SDL 区）精确等于壳子画面 480x798」（±10px 容差）。
// 画面比例 0.6015 ≠ 流比例 0.5625——不传 -fill 会 letterbox 左右各 ~15px 黑边
//（2026-10-28 用户拍板「不变形」优先，-fill 已废弃，player 侧恒忽略）。
// player 语义：画面宽 = 客户区 - sideW，画面高 = 客户区 - navBar（matrixUIScale 缩放）；
// 客户区 = 窗口外框 - 边框（computeWindowMetrics 实测）。
func TestComputeSinglePortraitLayout(t *testing.T) {
	pos := ComputeSinglePortraitLayout(SideCollapsedW, 0.7)
	frameW, frameH := computeWindowMetrics(0)
	// navBar：uiScale = min((480+52)/464, (798+48)/780) = min(1.15, 1.09) → 1.0（大窗不缩）
	navBarH := float64(NavBarHeight)
	picW := float64(pos.W) - float64(SideCollapsedW) - float64(frameW)
	picH := float64(pos.H) - navBarH - float64(frameH)
	if diff := int(picW) - 480; diff < -10 || diff > 10 {
		t.Errorf("picW = %.0f, want ~480 (±10)", picW)
	}
	if diff := int(picH) - 798; diff < -10 || diff > 10 {
		t.Errorf("picH = %.0f, want ~798 (±10)", picH)
	}
	// 居中哨兵：不传 -winx/-winy
	if pos.X != WinPosUnset || pos.Y != WinPosUnset {
		t.Errorf("X/Y = %d/%d, want WinPosUnset", pos.X, pos.Y)
	}
}
