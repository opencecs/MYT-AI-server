//go:build darwin

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	ffInitOnce  sync.Once
	ffLoadError error

	ffProcInit           uintptr
	ffProcCreateDecoder  uintptr
	ffProcDestroyDecoder uintptr
	ffProcDecode         uintptr
	ffProcFlush          uintptr
	ffProcGetDecoderInfo uintptr
	ffProcCreateAACDecoder  uintptr
	ffProcDecodeAAC         uintptr
	ffProcDestroyAACDecoder uintptr
	ffProcGetD3D11Device    uintptr
	ffProcDecodeTexture     uintptr
)

func loadFFDecode() error {
	ffInitOnce.Do(func() {
		dylibName := "ffdecode.dylib"

		exePath, _ := os.Executable()
		candidates := []string{
			filepath.Dir(exePath),
			os.TempDir(),
		}

		var lastErr error
		for _, dir := range candidates {
			dylibPath := filepath.Join(dir, dylibName)

			extract := true
			if info, err := os.Stat(dylibPath); err == nil && info.Size() == int64(len(ffdecodeDLL)) {
				extract = false
			}
			if extract {
				if err := os.WriteFile(dylibPath, ffdecodeDLL, 0o644); err != nil {
					lastErr = fmt.Errorf("ffdecode: write %s: %w", dylibPath, err)
					continue
				}
			}

			handle, err := purego.Dlopen(dylibPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				lastErr = fmt.Errorf("ffdecode: load %s: %w", dylibPath, err)
				continue
			}
			lastErr = nil
			if err := resolveFFProcs(handle); err != nil {
				lastErr = err
				continue
			}
			return
		}

		ffLoadError = lastErr
	})
	return ffLoadError
}

func resolveFFProcs(handle uintptr) error {
	requiredProcs := []struct {
		name string
		dst  *uintptr
	}{
		{"FF_Init", &ffProcInit},
		{"FF_CreateDecoder", &ffProcCreateDecoder},
		{"FF_DestroyDecoder", &ffProcDestroyDecoder},
		{"FF_Decode", &ffProcDecode},
		{"FF_Flush", &ffProcFlush},
	}
	for _, p := range requiredProcs {
		addr, err := purego.Dlsym(handle, p.name)
		if err != nil {
			return fmt.Errorf("ffdecode: %s not found: %w", p.name, err)
		}
		*p.dst = addr
	}

	optionalProcs := []struct {
		name string
		dst  *uintptr
	}{
		{"FF_CreateAACDecoder", &ffProcCreateAACDecoder},
		{"FF_DecodeAAC", &ffProcDecodeAAC},
		{"FF_DestroyAACDecoder", &ffProcDestroyAACDecoder},
		{"FF_GetDecoderInfo", &ffProcGetDecoderInfo},
		{"FF_GetD3D11Device", &ffProcGetD3D11Device},
		{"FF_DecodeTexture", &ffProcDecodeTexture},
	}
	for _, p := range optionalProcs {
		addr, err := purego.Dlsym(handle, p.name)
		if err == nil {
			*p.dst = addr
		}
	}
	return nil
}

// H264Decoder wraps FFmpeg for H.264 decoding via ffdecode.dylib.
type H264Decoder struct {
	handle  uintptr
	lastW   int32
	lastH   int32

	reqCh    chan []byte
	resultCh chan DecodeResult
	doneCh   chan struct{}
	closed   chan struct{}
}

func NewH264Decoder() *H264Decoder {
	return &H264Decoder{
		reqCh:   make(chan []byte, 2),
		resultCh: make(chan DecodeResult, 2),
		doneCh:  make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func queryDecoderInfo(handle uintptr) string {
	if ffProcGetDecoderInfo == 0 {
		return "info=N/A"
	}
	infoPtr, _, _ := purego.SyscallN(ffProcGetDecoderInfo, handle)
	if infoPtr == 0 {
		return "info=null"
	}
	n := 0
	for n < 255 {
		b := *(*byte)(unsafe.Pointer(uintptr(infoPtr) + uintptr(n)))
		if b == 0 {
			break
		}
		n++
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(infoPtr)), n))
}

func (d *H264Decoder) Initialize() error {
	if err := loadFFDecode(); err != nil {
		return err
	}

	purego.SyscallN(ffProcInit)

	handle, _, _ := purego.SyscallN(ffProcCreateDecoder)
	if handle == 0 {
		return fmt.Errorf("FF_CreateDecoder failed")
	}

	d.handle = handle

	go d.runLoop()

	infoStr := queryDecoderInfo(d.handle)
	logf("[decode] H264 decoder created (FFmpeg/VideoToolbox, %s)\n", infoStr)
	return nil
}

func (d *H264Decoder) runLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(d.closed)

	for {
		select {
		case <-d.doneCh:
			d.destroyOnThread()
			return
		default:
		}

		select {
		case <-d.doneCh:
			d.destroyOnThread()
			return
		case data := <-d.reqCh:
			res := d.decodeOnThread(data)
			select {
			case d.resultCh <- res:
			case <-d.doneCh:
				return
			}
		}
	}
}

func (d *H264Decoder) decodeOnThread(data []byte) DecodeResult {
	if len(data) == 0 {
		return DecodeResult{}
	}

	_ = ffProcDecodeTexture

	var outBGRA uintptr
	var outWidth, outHeight int32

	ret, _, _ := purego.SyscallN(ffProcDecode,
		d.handle,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&outBGRA)),
		uintptr(unsafe.Pointer(&outWidth)),
		uintptr(unsafe.Pointer(&outHeight)),
	)

	if ret != 1 {
		return DecodeResult{}
	}

	if outWidth <= 0 || outHeight <= 0 || outBGRA == 0 {
		return DecodeResult{}
	}

	if !decodeLogOnce.Swap(true) {
		infoStr := queryDecoderInfo(d.handle)
		logf("[decode] first frame decoded (%dx%d), %s\n", outWidth, outHeight, infoStr)
	}

	needFlush := d.lastW != 0 && d.lastH != 0 && (outWidth != d.lastW || outHeight != d.lastH)
	if needFlush {
		purego.SyscallN(ffProcFlush, d.handle, 0, 0, 0)
	}
	d.lastW = outWidth
	d.lastH = outHeight

	size := int(outWidth) * int(outHeight) * 4
	return DecodeResult{Width: outWidth, Height: outHeight, BGRA: outBGRA, Size: size}
}

// Decode 将 NALU 发给解码线程，非阻塞。
func (d *H264Decoder) Decode(nalus [][]byte) (DecodeResult, bool) {
	totalSize := 0
	for _, nalu := range nalus {
		totalSize += 4 + len(nalu)
	}
	if totalSize == 0 || totalSize > 8*1024*1024 {
		return DecodeResult{}, false
	}

	buf := make([]byte, totalSize)
	off := 0
	for _, nalu := range nalus {
		binary.BigEndian.PutUint32(buf[off:], 0x00000001)
		off += 4
		copy(buf[off:], nalu)
		off += len(nalu)
	}

	select {
	case d.reqCh <- buf:
	default:
		return DecodeResult{}, false
	}

	select {
	case res := <-d.resultCh:
		return res, true
	case <-time.After(2 * time.Second):
		logf("[decode] timeout waiting for result\n")
		DrainLoop:
		for {
			select {
			case <-d.resultCh:
				continue DrainLoop
			default:
				break DrainLoop
			}
		}
		return DecodeResult{}, false
	}
}

func (d *H264Decoder) destroyOnThread() {
	if d.handle != 0 {
		purego.SyscallN(ffProcDestroyDecoder, d.handle)
		d.handle = 0
	}
}

func (d *H264Decoder) GetD3D11Device() uintptr { return 0 }

func (d *H264Decoder) Close() {
	select {
	case <-d.doneCh:
		return
	default:
	}
	close(d.doneCh)
	select {
	case <-d.closed:
	case <-time.After(2 * time.Second):
		logf("[decode] Close: runLoop did not exit in 2s\n")
	}
}
