package main

import "testing"

// tebox_orientation_test.go — tebox 方向判定用例。
// 语义：宽>高=横屏流（QEMU GPU 预旋转，V3 形态侧躺存储）→ Rotated=true 走
// 旋转管线；宽<高=竖屏流 → false；宽==高 → 保持当前（正方形无方向语义，
// 翻转会在重复启停横竖流时抖动）。

func TestTeboxOrientationFor(t *testing.T) {
	cases := []struct {
		name          string
		w, h          int32
		current       bool
		wantRotated   bool
		wantChanged   bool
	}{
		// 竖屏流（tebox 常规 720x1600）：保持 false。当前已是 false → 无变化。
		{"portrait-stays-false", 720, 1600, false, false, false},
		// 当前异常为 true 时纠正到 false。
		{"portrait-corrects-true", 720, 1600, true, false, true},
		// 横屏流（预旋转 1600x720）→ true。
		{"landscape-to-true", 1600, 720, false, true, true},
		// 横屏流且已旋转 → 保持 true 无变化（分辨率小变化不重翻）。
		{"landscape-stays-true", 1280, 720, true, true, false},
		// 正方形：保持当前状态（false）。
		{"square-keeps-false", 1000, 1000, false, false, false},
		// 正方形：保持当前状态（true）。
		{"square-keeps-true", 1000, 1000, true, true, false},
		// 其他常见分辨率：1080x1920 竖 / 1920x1080 横。
		{"fhd-portrait", 1080, 1920, false, false, false},
		{"fhd-landscape", 1920, 1080, false, true, true},
	}
	for _, tc := range cases {
		gotRotated, gotChanged := teboxOrientationFor(tc.w, tc.h, tc.current)
		if gotRotated != tc.wantRotated || gotChanged != tc.wantChanged {
			t.Errorf("%s: teboxOrientationFor(%d,%d,cur=%v) = (%v,%v), want (%v,%v)",
				tc.name, tc.w, tc.h, tc.current, gotRotated, gotChanged,
				tc.wantRotated, tc.wantChanged)
		}
	}
}

// App.ApplyTeboxOrientation 仅 tebox 模式生效；RTSP 模式（方向由 -landscape
// 参数决定）调用无副作用。nonGUI App 构造方式参照 tebox_source_test.go。
func TestApplyTeboxOrientationModeGating(t *testing.T) {
	a := NewApp()
	// RTSP 模式：不翻转，Rotated 保持初始 false。
	a.Rotated.Store(true)
	if a.ApplyTeboxOrientation(1600, 720) {
		t.Error("non-tebox mode should not apply orientation")
	}
	if !a.Rotated.Load() {
		t.Error("non-tebox mode Rotated should be untouched")
	}

	// tebox 模式：横屏流翻转 true，竖屏流回 false，同分辨率幂等。
	a.CastMode = CastModeTebox
	a.Rotated.Store(false)
	if !a.ApplyTeboxOrientation(1600, 720) {
		t.Error("tebox landscape should flip to rotated=true")
	}
	if !a.Rotated.Load() {
		t.Error("Rotated should be true after landscape frame")
	}
	if a.ApplyTeboxOrientation(1600, 720) {
		t.Error("same resolution should be idempotent (no re-flip)")
	}
	if !a.ApplyTeboxOrientation(720, 1600) {
		t.Error("tebox portrait frame should flip back to rotated=false")
	}
	if a.Rotated.Load() {
		t.Error("Rotated should be false after portrait frame")
	}
	// 非法尺寸（解码过渡帧 0x0）忽略。
	if a.ApplyTeboxOrientation(0, 0) {
		t.Error("zero-size frame should be ignored")
	}
}
