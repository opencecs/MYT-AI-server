//go:build windows

package main

import (
	"unsafe"
)

func (r *Renderer) RenderFrame() {
	var newest *VideoFrame
	for {
		select {
		case frame := <-r.frameCh:
			if newest != nil {
				r.mu.Lock()
				r.recycleFrameBuf(newest)
				r.mu.Unlock()
				framePool.Put(newest)
			}
			newest = frame
		default:
			goto done
		}
	}
done:
	if newest != nil {
		r.renderFrame(newest)
		r.mu.Lock()
		r.recycleFrameBuf(newest)
		r.mu.Unlock()
		framePool.Put(newest)
	} else if r.srcW.Load() > 0 && r.srcH.Load() > 0 && r.hBitmap != 0 {
		r.repaintDIB()
	} else if r.srcW.Load() == 0 && r.srcH.Load() == 0 {
		r.renderIdle()
	}
}

func (r *Renderer) renderIdle() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.hwnd == 0 || r.hBlackBrush == 0 {
		return
	}

	hdc, _, _ := procGetDC.Call(r.hwnd)
	if hdc == 0 {
		return
	}
	defer procReleaseDC.Call(r.hwnd, hdc)

	var rc struct{ Left, Top, Right, Bottom int32 }
	procGetClientRect.Call(r.hwnd, uintptr(unsafe.Pointer(&rc)))
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), r.hBlackBrush)
}

// repaintDIB re-blits the existing DIB content at the current window size.
// Called when no new frame is available but the window has been resized.
func (r *Renderer) repaintDIB() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.hwnd == 0 || r.hBitmap == 0 || r.hMemDC == 0 {
		return
	}

	hdc, _, _ := procGetDC.Call(r.hwnd)
	if hdc == 0 {
		return
	}
	defer procReleaseDC.Call(r.hwnd, hdc)

	winW, winH := r.width, r.height
	srcW, srcH := r.bmpW, r.bmpH
	if srcW <= 0 || srcH <= 0 {
		return
	}

	drawW, drawH, offX, offY := r.letterbox()

	r.paintLetterboxBars(hdc, drawW, drawH, offX, offY, winW, winH)

	procSetStretchBltMode.Call(hdc, 3)
	procSetBrushOrgEx.Call(hdc, uintptr(offX)&1, uintptr(offY)&1, 0)

	procStretchBlt.Call(
		hdc, uintptr(offX), uintptr(offY), uintptr(drawW), uintptr(drawH),
		r.hMemDC, 0, 0, uintptr(srcW), uintptr(srcH),
		0x00CC0020,
	)
}

// paintLetterboxBars paints the black bars around the video area.
// Called BEFORE StretchBlt to clear ghost pixels from previous resize frames,
// without the flicker that a full-window FillRect would cause.
func (r *Renderer) paintLetterboxBars(hdc uintptr, drawW, drawH, offX, offY, winW, winH int32) {
	blackBr := r.hBlackBrush
	if blackBr == 0 {
		return
	}
	if offX == 0 && offY == 0 && offX+drawW == winW && offY+drawH == winH {
		return
	}
	if offY > 0 {
		rc := [4]int32{0, 0, winW, offY}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), blackBr)
	}
	if offY+drawH < winH {
		rc := [4]int32{0, offY + drawH, winW, winH}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), blackBr)
	}
	if offX > 0 {
		rc := [4]int32{0, offY, offX, offY + drawH}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), blackBr)
	}
	if offX+drawW < winW {
		rc := [4]int32{offX + drawW, offY, winW, offY + drawH}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), blackBr)
	}
}

func (r *Renderer) renderFrame(frame *VideoFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.hwnd == 0 {
		return
	}

	rotated := r.rotated.Load() && frame.Width >= frame.Height

	if r.captureReq.Load() {
		frameCopy := make([]byte, len(frame.Data))
		copy(frameCopy, frame.Data)
		r.lastFrame = frameCopy
		r.lastFrameW = frame.Width
		r.lastFrameH = frame.Height
		r.lastFrameRot = rotated
		r.captureReq.Store(false)
	}

	hdc, _, _ := procGetDC.Call(r.hwnd)
	if hdc == 0 {
		return
	}
	defer procReleaseDC.Call(r.hwnd, hdc)

	if !rotated {
		if r.hBitmap == 0 || r.bmpW != frame.Width || r.bmpH != frame.Height {
			r.recreateDIB(frame.Width, frame.Height, hdc)
		}
	}

	if !rotated {
		if r.pBits == nil {
			return
		}
		dibSize := int(frame.Width) * int(frame.Height) * 4
		if frame.Format == PixelFormatBGRA {
			n := min(len(frame.Data), dibSize)
			copy(unsafe.Slice((*byte)(r.pBits), dibSize), frame.Data[:n])
		} else {
			pixCount := min(len(frame.Data), dibSize) / 4
			src32 := unsafe.Slice((*uint32)(unsafe.Pointer(&frame.Data[0])), pixCount)
			dst32 := unsafe.Slice((*uint32)(r.pBits), pixCount)
			i := 0
			for i+3 < pixCount {
				s0, s1, s2, s3 := src32[i], src32[i+1], src32[i+2], src32[i+3]
				dst32[i] = (s0 & 0xFF000000) | ((s0 & 0x00FF0000) >> 16) | (s0 & 0x0000FF00) | ((s0 & 0x000000FF) << 16)
				dst32[i+1] = (s1 & 0xFF000000) | ((s1 & 0x00FF0000) >> 16) | (s1 & 0x0000FF00) | ((s1 & 0x000000FF) << 16)
				dst32[i+2] = (s2 & 0xFF000000) | ((s2 & 0x00FF0000) >> 16) | (s2 & 0x0000FF00) | ((s2 & 0x000000FF) << 16)
				dst32[i+3] = (s3 & 0xFF000000) | ((s3 & 0x00FF0000) >> 16) | (s3 & 0x0000FF00) | ((s3 & 0x000000FF) << 16)
				i += 4
			}
			for i < pixCount {
				rgba := src32[i]
				dst32[i] = (rgba & 0xFF000000) | ((rgba & 0x00FF0000) >> 16) | (rgba & 0x0000FF00) | ((rgba & 0x000000FF) << 16)
				i++
			}
		}
	}

	if r.hMemDC == 0 {
		memDC, _, _ := procCreateCompatibleDC.Call(hdc)
		if memDC == 0 {
			return
		}
		r.hMemDC = memDC
		if r.hBitmap != 0 {
			procSelectObject.Call(memDC, r.hBitmap)
		}
	}
	memDC := r.hMemDC

	winW, winH := r.width, r.height
	srcW, srcH := frame.Width, frame.Height

	if rotated {
		rotW, rotH := srcH, srcW
		if r.bmpW != rotW || r.bmpH != rotH {
			r.recreateDIB(rotW, rotH, hdc)
		}
		if r.pBits != nil {
			rotateBGRA90CW(frame.Data, unsafe.Slice((*byte)(r.pBits), int(rotW)*int(rotH)*4), srcW, srcH)
			r.blitRotated(hdc, memDC, rotW, rotH, winW, winH)
		}
	} else {
		drawW, drawH, offX, offY := r.letterbox()

		// Paint letterbox bars BEFORE StretchBlt to clear ghost pixels
		r.paintLetterboxBars(hdc, drawW, drawH, offX, offY, winW, winH)

		procSetStretchBltMode.Call(hdc, 3)
		procSetBrushOrgEx.Call(hdc, uintptr(offX)&1, uintptr(offY)&1, 0)

		procStretchBlt.Call(
			hdc, uintptr(offX), uintptr(offY), uintptr(drawW), uintptr(drawH),
			memDC, 0, 0, uintptr(srcW), uintptr(srcH),
			0x00CC0020,
		)
	}
}

// SetFill 设置画面拉伸填满模式（跳过 letterbox 比例，SDL 区满幅渲染）。
func (r *Renderer) SetFill(fill bool) {
	r.fill.Store(fill)
	r.lbStale.Store(true)
}

func (r *Renderer) recreateDIB(w, h int32, hdc uintptr) {
	oldBitmap := r.hBitmap

	bmi := bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       w,
		Height:      -h,
		Planes:      1,
		BitCount:    32,
		Compression: 0,
		SizeImage:   uint32(w * h * 4),
	}

	if hdc == 0 {
		hdc, _, _ = procGetDC.Call(r.hwnd)
		if hdc == 0 {
			return
		}
		defer procReleaseDC.Call(r.hwnd, hdc)
	}

	var pBits unsafe.Pointer
	hBmp, _, _ := procCreateDIBSection.Call(
		hdc,
		uintptr(unsafe.Pointer(&bmi)),
		0,
		uintptr(unsafe.Pointer(&pBits)),
		0, 0,
	)
	if hBmp == 0 || pBits == nil {
		return
	}

	r.hBitmap = hBmp
	r.pBits = pBits
	r.bmpW = w
	r.bmpH = h

	// Zero DIB pixels to prevent flashing uninitialized memory before first frame
	if pBits != nil {
		dibSize := int(w) * int(h) * 4
		copy(unsafe.Slice((*byte)(pBits), dibSize), make([]byte, dibSize))
	}

	if r.hMemDC != 0 {
		procSelectObject.Call(r.hMemDC, hBmp)
	}
	if oldBitmap != 0 {
		procDeleteObject.Call(oldBitmap)
	}
}

func (r *Renderer) blitRotated(hdc, memDC uintptr, rotW, rotH, winW, winH int32) {
	if r.pBits == nil {
		return
	}

	drawW, drawH, offX, offY := r.letterbox()

	// Paint letterbox bars BEFORE StretchBlt to clear ghost pixels
	r.paintLetterboxBars(hdc, drawW, drawH, offX, offY, winW, winH)

	procSetStretchBltMode.Call(hdc, 3)
	procSetBrushOrgEx.Call(hdc, uintptr(offX)&1, uintptr(offY)&1, 0)

	procStretchBlt.Call(
		hdc, uintptr(offX), uintptr(offY), uintptr(drawW), uintptr(drawH),
		memDC, 0, 0, uintptr(rotW), uintptr(rotH),
		0x00CC0020,
	)
}

func rotateBGRA90CW(src, dst []byte, w, h int32) {
	src32 := unsafe.Slice((*uint32)(unsafe.Pointer(&src[0])), int(w)*int(h))
	dst32 := unsafe.Slice((*uint32)(unsafe.Pointer(&dst[0])), int(w)*int(h))
	const tileSize int32 = 64
	for yStart := int32(0); yStart < h; yStart += tileSize {
		yEnd := yStart + tileSize
		if yEnd > h {
			yEnd = h
		}
		for y := yStart; y < yEnd; y++ {
			srcRow := src32[y*w : y*w+w]
			dstIdx := h - 1 - y
			for x := int32(0); x < w; x++ {
				dst32[dstIdx] = srcRow[x]
				dstIdx += h
			}
		}
	}
}
