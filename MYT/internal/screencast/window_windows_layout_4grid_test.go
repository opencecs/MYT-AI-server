//go:build windows

package screencast

import (
	"testing"
)

// TestLandscapeSingleCell4Grid 验证横屏少量投屏（4 格基准）的格子尺寸：
// 2x2 排布 ≈881x515（与竖屏单台 299x515 同高），侧栏展开后高度舒适。
func TestLandscapeSingleCell4Grid(t *testing.T) {
	g := ComputeGridLayout(4, 1.0, 0, SideCollapsedW, true)
	t.Logf("landscape 4-grid: cell=%dx%d cols=%d rows=%d base=%dx%d",
		int(g.CellW), int(g.CellH), g.Cols, g.Rows, int(g.BaseW), int(g.BaseH))
	if g.CellH < 450 {
		t.Errorf("landscape 4-grid cell too short: %dx%d (sidebar needs height)", int(g.CellW), int(g.CellH))
	}
	// 展开侧栏（300 设计宽）后总宽不超工作区
	if g.CellW+(300-SideCollapsedW) > float64(g.CX) {
		t.Errorf("expanded window would exceed work area: cellW=%v cx=%d", g.CellW, g.CX)
	}
}
