package main

import "testing"

// letterbox_test.go — 受限填满（decideThresholdFill）判定用例。
// 阈值 letterboxFillTolerance=10%：偏差 ≤10% 填满（无黑边轻微拉伸），
// 偏差 >10% letterbox 居中黑边。判定纯几何无状态，窗口形状变化即重算。

func TestDecideThresholdFill(t *testing.T) {
	cases := []struct {
		name             string
		winW, winH       int32
		srcW, srcH       int32
		rotated          bool
		want             bool
	}{
		// 验收①：单台竖屏小窗（壳子 480:798≈0.6015）放 720:1280（0.5625）流，
		// 偏差 ~6.9% ≤10% → 填满（恢复旧 fill 效果，无 ~15px 黑边）。
		{"single-portrait-shell", 480, 798, 720, 1280, false, true},
		// 同比例不同尺寸同样填满（窗口放大缩小不改变判定）。
		{"single-portrait-scaled", 960, 1596, 720, 1280, false, true},
		// 验收②：最大化 16:9 窗口放竖屏流，偏差远超阈值 → letterbox 黑边。
		{"maximized-16x9-vs-portrait", 1600, 900, 720, 1280, false, false},
		// 拖成宽扁：横屏流放 16:9 之外的宽扁窗。
		{"wide-flat-vs-landscape", 2000, 600, 1280, 720, false, false},
		// 恰好同比例 → 填满（整数截断残余黑边也消除，见下方专测）。
		{"same-aspect", 640, 1138, 720, 1280, false, true},
		// 阈值边界：偏差恰为 10%（srcRatio=1000, winRatio=1100）→ 填满。
		{"boundary-exactly-10pct", 1100, 1000, 1000, 1000, false, true},
		// 阈值外一点点：偏差 >10% → letterbox。
		{"just-over-10pct", 1101, 1000, 1000, 1000, false, false},
		// 旋转：横屏源旋转进竖窗（有效比例宽高互换后同比例）→ 填满。
		{"rotated-landscape-in-portrait", 720, 1280, 1280, 720, true, true},
		// 源尺寸未知 → 不填满（等源元数据就绪重算）。
		{"unknown-source", 480, 798, 0, 0, false, false},
	}
	for _, tc := range cases {
		if got := decideThresholdFill(tc.winW, tc.winH, tc.srcW, tc.srcH, tc.rotated); got != tc.want {
			t.Errorf("%s: decideThresholdFill(%d,%d,%d,%d,rot=%v)=%v, want %v",
				tc.name, tc.winW, tc.winH, tc.srcW, tc.srcH, tc.rotated, got, tc.want)
		}
	}
}

// 同比例窗口填满：1280x720 源放 1280x720 窗。
func TestDecideThresholdFillSameAspect(t *testing.T) {
	if !decideThresholdFill(1280, 720, 1280, 720, false) {
		t.Error("same aspect should fill")
	}
}

// 验收③（无状态）：填满 ↔ letterbox 随窗口形状往返切换，geo 相应变化。
func TestComputeLetterboxRoundTrip(t *testing.T) {
	shell := computeLetterbox(480, 798, 720, 1280, false, false)
	if shell.drawW != 480 || shell.drawH != 798 || shell.offX != 0 || shell.offY != 0 {
		t.Errorf("shell window should fill: geo=%+v", shell)
	}
	maxed := computeLetterbox(1600, 900, 720, 1280, false, false)
	if maxed.drawW == 1600 || maxed.offX == 0 {
		t.Errorf("16:9 window should letterbox (horizontal bars): geo=%+v", maxed)
	}
	back := computeLetterbox(480, 798, 720, 1280, false, false)
	if back.drawW != 480 || back.drawH != 798 {
		t.Errorf("restored window should fill again: geo=%+v", back)
	}
}

// 验收④（鼠标映射）：填满时 draw 区=全窗口，角点映射到源角点；letterbox
// 时黑边区域 relInBounds clamp 到边缘（不越界）。
func TestScaleMouseCoordsBothModes(t *testing.T) {
	// 填满：480x798 窗装 720x1280 流（轻微拉伸），窗口角点→源角点附近
	// （整数映射语义：窗口最末像素 479→源 718，mx=drawW 才映射到 srcW=720）。
	g := computeLetterbox(480, 798, 720, 1280, false, false)
	sx, sy, ok := scaleMouseCoords(479, 797, &g, 720, 1280, false)
	if !ok || sx != 718 || sy != 1278 {
		t.Errorf("fill mode bottom-right: got (%d,%d,%v), want (718,1278,true)", sx, sy, ok)
	}
	sx, sy, ok = scaleMouseCoords(0, 0, &g, 720, 1280, false)
	if !ok || sx != 0 || sy != 0 {
		t.Errorf("fill mode top-left: got (%d,%d,%v), want (0,0,true)", sx, sy, ok)
	}
	// letterbox：1600x900 窗装 720x1280 流，offX 居中；黑边点击 clamp 到源边缘。
	g = computeLetterbox(1600, 900, 720, 1280, false, false)
	if g.offX <= 0 {
		t.Fatalf("expect horizontal bars, geo=%+v", g)
	}
	sx, sy, ok = scaleMouseCoords(10, 450, &g, 720, 1280, false) // 左黑边
	if !ok || sx != 0 {
		t.Errorf("letterbox left bar: got (%d,%d,%v), want (0,*,true)", sx, sy, ok)
	}
	sx, _, _ = scaleMouseCoords(1590, 450, &g, 720, 1280, false) // 右黑边
	if sx != 720 {
		t.Errorf("letterbox right bar: got sx=%d, want 720", sx)
	}
}
