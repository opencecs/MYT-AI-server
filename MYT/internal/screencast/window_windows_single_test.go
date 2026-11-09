//go:build windows

package screencast

import "testing"

// TestComputeSingleLandscapeLayout 验证单台横屏窗口尺寸：
// 视频原生 1280x720 1:1（~1348x799 含 chrome），屏内、比例匹配 16:9。
func TestComputeSingleLandscapeLayout(t *testing.T) {
	pos := ComputeSingleLandscapeLayout(SideCollapsedW)
	if pos.W <= 0 || pos.H <= 0 {
		t.Fatalf("invalid size: %dx%d", pos.W, pos.H)
	}
	if pos.X != WinPosUnset || pos.Y != WinPosUnset {
		t.Errorf("single window should be centered (sentinel), got x=%d y=%d", pos.X, pos.Y)
	}
	// 原生窗口应在 1348x799 附近（1280+52+16, 720+48+31；边框实测有 ±几 px）
	if pos.W < 1300 || pos.W > 1400 {
		t.Errorf("single landscape width = %d, want ~1348", pos.W)
	}
	if pos.H < 770 || pos.H > 830 {
		t.Errorf("single landscape height = %d, want ~799", pos.H)
	}
	// 屏内（含展开侧栏 300 的余量：+248 不超工作区）
	if pos.W+(300-SideCollapsedW) > 1920 {
		t.Errorf("expanded window exceeds screen: %d", pos.W+(300-SideCollapsedW))
	}
	t.Logf("single landscape window: %dx%d", pos.W, pos.H)
}
