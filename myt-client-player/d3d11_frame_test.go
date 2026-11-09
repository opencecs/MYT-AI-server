//go:build windows

package main

// d3d11_frame_test.go — NV12→BGRA 转换小型样本单测（BT.601 limited-range）。

import "testing"

func TestNV12ToBGRAColors(t *testing.T) {
	// 2x2 NV12：Y 4 字节 + UV 2 字节（一行 UV，2 像素共享 1 组 → 实际 1 组交错 UV）
	// 样本：全帧 Y=235,U=128,V=128 → 白（255,255,255）
	w, h := int32(2), int32(2)
	nv12 := []byte{235, 235, 235, 235, 128, 128}
	bgra, gw, gh := nv12ToBGRA(nv12, w, h)
	if gw != w || gh != h {
		t.Fatalf("尺寸异常: %dx%d", gw, gh)
	}
	for i := 0; i < 4; i++ {
		b, g, r := bgra[i*4], bgra[i*4+1], bgra[i*4+2]
		if b != 255 || g != 255 || r != 255 || bgra[i*4+3] != 0xFF {
			t.Fatalf("白色样本像素 %d = BGRA(%d,%d,%d,%d), want (255,255,255,255)", i, b, g, r, bgra[i*4+3])
		}
	}
}

func TestNV12ToBGRABlackAndRed(t *testing.T) {
	w, h := int32(2), int32(2)
	// 像素 0：Y=16（luma=0）；像素 1：Y=81（luma=65）。2x2 共享一组 UV（U=90,V=240）
	// → 整帧红调：像素 0 = 暗红 clamp(0,0,179)，像素 1 = 红 (0,0,255)
	nv12 := []byte{16, 81, 16, 81, 90, 240}
	bgra, _, _ := nv12ToBGRA(nv12, w, h)
	// 像素 0：r=(0+409*112+128)>>8=179, g=(-100*(-38)-208*112)>>8<0→0, b=516*(-38)<0→0
	if bgra[2] != 179 || bgra[1] != 0 || bgra[0] != 0 {
		t.Fatalf("像素0 应为暗红 BGRA(0,0,179): (%d,%d,%d)", bgra[0], bgra[1], bgra[2])
	}
	// 像素 1：luma=65 → r=(298*65+409*112+128)>>8=255, g<0→0, b<0→0 → 红
	if bgra[6] != 255 || bgra[5] != 0 || bgra[4] != 0 {
		t.Fatalf("红样本应 BGRA(0,0,255): (%d,%d,%d)", bgra[4], bgra[5], bgra[6])
	}
	// alpha 恒 255
	for i := 0; i < 4; i++ {
		if bgra[i*4+3] != 0xFF {
			t.Fatalf("alpha 应为 255")
		}
	}
}

func TestNV12ToBGRAClamp(t *testing.T) {
	// Y=255,U=0,V=0（u=v=-128 超界）→ 按公式 r=74 g=416 b=20 → g clamp 到 255
	w, h := int32(1), int32(1)
	nv12 := []byte{255, 0, 0}
	bgra, _, _ := nv12ToBGRA(nv12, w, h)
	r, g, b := bgra[2], bgra[1], bgra[0]
	if r != 74 || g != 255 || b != 20 {
		t.Fatalf("clamp 异常: BGRA(%d,%d,%d), want (20,255,74)", b, g, r)
	}
}
