//go:build windows

package screencast

import (
	"math"
	"testing"
)

// TestComputeGridLayoutAspect 验证网格格子的视频区比例与投屏方向匹配：
// 竖屏投屏（横屏源转竖显示）→ sdlArea ≈ 720:1280；
// 横屏投屏（-landscape 视频不旋转）→ sdlArea ≈ 1280:720。
// 比例错配 = 视频 letterbox 大黑边（横屏视频塞竖屏格子只占窗口高 ~32%）。
func TestComputeGridLayoutAspect(t *testing.T) {
	for _, tc := range []struct {
		name      string
		landscape bool
		want      float64
	}{
		{"portrait", false, VideoAspect},
		{"landscape", true, LandscapeAspect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := ComputeGridLayout(8, 1.0, 0, SideCollapsedW, tc.landscape)

			if g.Cols < 1 || g.Rows < 1 {
				t.Fatalf("invalid grid: cols=%d rows=%d", g.Cols, g.Rows)
			}
			if g.BaseW <= 0 || g.BaseH <= 0 {
				t.Fatalf("invalid base size: %v x %v", g.BaseW, g.BaseH)
			}

			// BaseW/BaseH 是客户区净尺寸（BaseH 已扣边框+navBar，即 sdl 高）；
			// sdl 宽 = BaseW - 缩放后 sideW。缩放参数复刻 ComputeGridLayout 内部
			// 的 matrixUIScale（基准 BaseClientW/H，clamp [0.3,1.0]，sideW 下限
			// SideCollapsedMin），允许与内部计算有细微取整差。
			uiScale := math.Min(g.BaseW/BaseClientW, g.BaseH/BaseClientH)
			uiScale = math.Max(0.3, math.Min(1.0, uiScale))
			scaledSideW := float64(SideCollapsedW) * uiScale
			if scaledSideW < SideCollapsedMin {
				scaledSideW = SideCollapsedMin
			}
			sdlW := g.BaseW - scaledSideW
			if sdlW <= 0 {
				t.Fatalf("non-positive sdl width: baseW=%v scaledSideW=%v", g.BaseW, scaledSideW)
			}
			got := sdlW / g.BaseH

			// 容差 5%：覆盖 uiScale 复刻取整差与 MinBaseW 兜底路径。
			if math.Abs(got/tc.want-1) > 0.05 {
				t.Errorf("sdl area aspect = %.4f, want ≈%.4f (base=%vx%v scaledSideW=%.1f cell=%vx%v)",
					got, tc.want, g.BaseW, g.BaseH, scaledSideW, g.CellW, g.CellH)
			}

			// 粗粒度方向断言（不依赖精确数值）：竖屏格子必须高>宽，横屏格子宽>高。
			if !tc.landscape && g.CellH <= g.CellW {
				t.Errorf("portrait cell should be taller than wide: %vx%v", g.CellW, g.CellH)
			}
			if tc.landscape && g.CellW <= g.CellH {
				t.Errorf("landscape cell should be wider than tall: %vx%v", g.CellW, g.CellH)
			}
		})
	}
}
