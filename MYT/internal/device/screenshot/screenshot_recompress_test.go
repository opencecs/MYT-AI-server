package screenshot

// screenshot_recompress_test.go — 重压缩 helper 单测。
//
// 覆盖：PNG→JPEG q75 显著缩小；已是大 JPEG 重编码仍缩小；小 JPEG 跳过不动；
// 非图字节透传；同输入输出逐字节一致（保证 storeImage 字节 diff 有效）。

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// makeTestImage 生成指定尺寸的测试图（伪随机噪声：接近真实截图的不可压缩性，
// 纯色/平滑渐变图 JPEG 压得过分好，测不出代表性数据）。
func makeTestImage(t *testing.T, w, h int) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(12345)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223 // LCG 伪随机
			img.Set(x, y, color.RGBA{
				R: uint8(seed >> 16),
				G: uint8(seed >> 8),
				B: uint8(seed),
				A: 255,
			})
		}
	}
	return img
}

// encodeTestImage 把测试图编码为指定格式的字节。
func encodeTestImage(t *testing.T, img image.Image, format string, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	}
	if err != nil {
		t.Fatalf("编码测试 %s 失败: %v", format, err)
	}
	return buf.Bytes()
}

func TestRecompressPNGToJPEG(t *testing.T) {
	src := encodeTestImage(t, makeTestImage(t, 320, 711), "png", 0)
	out, ct := recompressImage(src)
	if ct != "image/jpeg" {
		t.Fatalf("期望 image/jpeg，得到 %q", ct)
	}
	if !bytes.HasPrefix(out, jpegMagic) {
		t.Fatal("输出不是 JPEG 魔数")
	}
	ratio := float64(len(out)) / float64(len(src))
	t.Logf("PNG %dB → JPEG %dB（%.1f%%）", len(src), len(out), ratio*100)
	if ratio > 0.5 {
		t.Fatalf("压缩收益不足：PNG %d → JPEG %d", len(src), len(out))
	}
}

func TestRecompressLargeJPEG(t *testing.T) {
	// q95 的大 JPEG（模拟 V3/公网 1080p 原图）→ q75 应显著缩小
	src := encodeTestImage(t, makeTestImage(t, 320, 711), "jpeg", 95)
	out, ct := recompressImage(src)
	if ct != "image/jpeg" || !bytes.HasPrefix(out, jpegMagic) {
		t.Fatalf("期望原样返回 JPEG，得到 ct=%q", ct)
	}
	t.Logf("JPEG q95 %dB → q75 %dB", len(src), len(out))
	if len(out) >= len(src) {
		t.Fatalf("q75 重编码未缩小: %d → %d", len(src), len(out))
	}
}

func TestRecompressSmallJPEGSkipped(t *testing.T) {
	src := encodeTestImage(t, makeTestImage(t, 64, 128), "jpeg", 75)
	if len(src) >= recompressJPEGSkipSize {
		t.Fatalf("测试前提不成立：小 JPEG 应小于阈值 %d，实际 %d", recompressJPEGSkipSize, len(src))
	}
	out, ct := recompressImage(src)
	if ct != "image/jpeg" || !bytes.Equal(out, src) {
		t.Fatal("小 JPEG 应原样返回（避免无谓的一代画质损失）")
	}
}

func TestRecompressNonImagePassthrough(t *testing.T) {
	data := []byte(`{"error":"not an image"}`)
	out, ct := recompressImage(data)
	if ct != "" || !bytes.Equal(out, data) {
		t.Fatal("非图字节应原样透传且 contentType 为空")
	}
}

func TestRecompressDeterministic(t *testing.T) {
	src := encodeTestImage(t, makeTestImage(t, 320, 711), "png", 0)
	out1, _ := recompressImage(src)
	out2, _ := recompressImage(src)
	if !bytes.Equal(out1, out2) {
		t.Fatal("同输入两次重编码输出不一致，会破坏 storeImage 字节 diff")
	}
}

func TestRecompressUnchangedForSameSource(t *testing.T) {
	// storeImage 场景：服务端返回同画面同字节 → 重压缩结果也同字节 → diff 判"未变化"
	src := encodeTestImage(t, makeTestImage(t, 320, 711), "png", 0)
	a, ctA := recompressImage(src)
	b, ctB := recompressImage(src)
	if ctA != ctB || !bytes.Equal(a, b) {
		t.Fatal("重压缩破坏了未变化画面的字节一致性")
	}
}
