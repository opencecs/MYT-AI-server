package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"time"
	"unsafe"
)

// saveScreenshotJPEG 保存 JPEG 字节数据为文件（设备端 task=snap 返回 JPEG，直接保存）。
func saveScreenshotJPEG(jpegBytes []byte) string {
	dir := screenshotDir()
	name := fmt.Sprintf("showpic_%d_%d.jpg", time.Now().UnixMilli(), 0)
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		logf("[screenshot] create failed: %v\n", err)
		return ""
	}
	defer f.Close()
	if _, err := f.Write(jpegBytes); err != nil {
		logf("[screenshot] write failed: %v\n", err)
		return ""
	}
	return path
}

// rotateJPEG90CW 把 JPEG 图片旋转 90°顺时针（player 旋转状态时，设备截图需转后再保存）。
func rotateJPEG90CW(jpegBytes []byte) ([]byte, error) {
	img, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	// 90°CW: 新图宽=原高，高=原宽
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, x, img.At(x, y))
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// saveScreenshot saves BGRA pixel data as a PNG file.
// If rotated is true, the frame is rotated 90°CW before saving (w/h are swapped).
// Returns the saved file path, or empty string on error.
func saveScreenshot(bgra []byte, w, h int32, rotated bool) string {
	pngBytes, err := encodeFramePNG(bgra, w, h, rotated)
	if err != nil {
		logf("[screenshot] encode failed: %v\n", err)
		return ""
	}

	dir := screenshotDir()
	name := fmt.Sprintf("showpic_%d_%d.png", time.Now().UnixMilli(), 0)
	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
		logf("[screenshot] create failed: %v\n", err)
		return ""
	}

	logf("[screenshot] saved: %s\n", path)
	return path
}

// encodeFramePNG BGRA 帧转 RGBA（含 90°CW 旋转）并编码为 PNG 内存字节。
// saveScreenshot（落盘）与 control server /screenshot（内存直出）共用。
func encodeFramePNG(bgra []byte, w, h int32, rotated bool) ([]byte, error) {
	if len(bgra) < int(w)*int(h)*4 {
		return nil, fmt.Errorf("frame buffer too small: %d < %d", len(bgra), int(w)*int(h)*4)
	}

	var img *image.RGBA
	if rotated {
		// Rotate 90°CW: swap dimensions, rotate pixels + BGRA→RGBA
		rotW, rotH := h, w
		img = image.NewRGBA(image.Rect(0, 0, int(rotW), int(rotH)))
		src32 := unsafe.Slice((*uint32)(unsafe.Pointer(&bgra[0])), int(w)*int(h))
		dst32 := unsafe.Slice((*uint32)(unsafe.Pointer(&img.Pix[0])), int(rotW)*int(rotH))
		for y := int32(0); y < h; y++ {
			for x := int32(0); x < w; x++ {
				// 90°CW: src(x,y) → dst[x*rotW + (h-1-y)]
				v := src32[y*w+x]
				// BGRA -> RGBA: swap R and B
				dst32[x*rotW+(h-1-y)] = (v & 0xFF000000) | ((v & 0x00FF0000) >> 16) | (v & 0x0000FF00) | ((v & 0x000000FF) << 16)
			}
		}
	} else {
		img = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
		pix := img.Pix

		// BGRA -> RGBA conversion using uint32 byte-swap
		pixCount := int(w) * int(h)
		src32 := unsafe.Slice((*uint32)(unsafe.Pointer(&bgra[0])), pixCount)
		dst32 := unsafe.Slice((*uint32)(unsafe.Pointer(&pix[0])), pixCount)
		for i := range pixCount {
			v := src32[i]
			dst32[i] = (v & 0xFF000000) | ((v & 0x00FF0000) >> 16) | (v & 0x0000FF00) | ((v & 0x000000FF) << 16)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
