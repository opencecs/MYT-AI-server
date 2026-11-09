//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	ffInitOnce  sync.Once
	ffLoadError error

	ffProcInit              uintptr
	ffProcCreateDecoder     uintptr
	ffProcDestroyDecoder    uintptr
	ffProcDecode            uintptr
	ffProcFlush             uintptr
	ffProcGetDecoderInfo    uintptr
	ffProcCreateAACDecoder  uintptr
	ffProcDecodeAAC         uintptr
	ffProcDestroyAACDecoder uintptr
)

// loadFFDecode 提取并加载 libffdecode.so（Linux 纯软解：ffdecode.c 的平台 guard
// 在非 _WIN32/__APPLE__ 上自动落纯软解路径，无 -hwaccel 逻辑）。
func loadFFDecode() error {
	ffInitOnce.Do(func() {
		soName := "libffdecode.so"

		exePath, _ := os.Executable()
		candidates := []string{
			filepath.Dir(exePath),
			os.TempDir(),
		}
		if dir, ok := os.LookupEnv("XDG_DATA_HOME"); ok && dir != "" {
			candidates = append(candidates, dir)
		}

		var lastErr error
		for _, dir := range candidates {
			soPath := filepath.Join(dir, soName)

			extract := true
			// 不能只比较大小：新版 so 大小可能碰巧与旧版相同，导致跳过更新跑旧 so。
			if data, err := os.ReadFile(soPath); err == nil && sha256.Sum256(data) == sha256.Sum256(ffdecodeSO) {
				extract = false
			}
			if extract {
				if err := os.WriteFile(soPath, ffdecodeSO, 0o644); err != nil {
					lastErr = fmt.Errorf("ffdecode: write %s: %w", soPath, err)
					continue
				}
			}

			handle, err := purego.Dlopen(soPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				lastErr = fmt.Errorf("ffdecode: load %s: %w", soPath, err)
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
	}
	for _, p := range optionalProcs {
		addr, err := purego.Dlsym(handle, p.name)
		if err == nil {
			*p.dst = addr
		}
	}
	return nil
}

// H264Decoder wraps FFmpeg for H.264 decoding via libffdecode.so (software only).
type H264Decoder struct {
	handle     uintptr
	lastW      int32
	lastH      int32
	failStreak atomic.Int32 // FF_Decode 连续失败计数（成功清零，观测点）

	reqCh    chan []byte
	resultCh chan DecodeResult
	doneCh   chan struct{}
	closed   chan struct{}
}

// NewH264Decoder 创建解码器。hwAccel 参数与 Windows 版签名对齐（app.go 平台无关
// 调用 NewH264Decoder(a.HardwareDecode)）；Linux 恒为软解，参数仅保持签名兼容。
func NewH264Decoder(hwAccel bool) *H264Decoder {
	_ = hwAccel
	return &H264Decoder{
		reqCh:    make(chan []byte, 2),
		resultCh: make(chan DecodeResult, 2),
		doneCh:   make(chan struct{}),
		closed:   make(chan struct{}),
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
	logf("[decode] H264 decoder created (FFmpeg/software, %s)\n", infoStr)
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
		// 解码失败观测：连续失败计数（成功即清零），每 100 次打一条低频日志。
		// 防静默停产（如曾发生的 FF_Flush drain 态 EAGAIN 连锁失败）无从排查。
		d.failStreak.Add(1)
		if c := d.failStreak.Load(); c%100 == 1 {
			logf("[decode] ff decode failed x%d (ret=%d)\n", c, ret)
		}
		return DecodeResult{}
	}
	d.failStreak.Store(0)

	if outWidth <= 0 || outHeight <= 0 || outBGRA == 0 {
		return DecodeResult{}
	}

	if !decodeLogOnce.Swap(true) {
		infoStr := queryDecoderInfo(d.handle)
		logf("[decode] first frame decoded (%dx%d), %s\n", outWidth, outHeight, infoStr)
	}

	// 分辨率变化时不调 FF_Flush：avcodec_send_packet(NULL) 会把解码器置入 drain
	// （EOF）模式，此后所有 send_packet 返回 EAGAIN、解码静默停产（黑屏根因——
	// 且本实现 FF_Flush 未调 avcodec_flush_buffers 收尾，drain 态不可恢复）。
	// 每帧 UV 检测已能处理分辨率变化的过渡帧，与 windows 版（decode_ffdll.go
	// 「分辨率变化时不调 FF_Flush」注释）保持同一策略。
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

// ─── 平台不适用能力的 no-op 兜底 ─────────────────────────────────────────────
// app.go / app_connect_cb.go / app_control.go 在无渲染器（D3D11Ren==nil）路径下
// 会调用这些方法；Linux 无 D3D11 硬解，全部走安全默认值。

// GetD3D11Device Linux 无 D3D11，恒返 0。
func (d *H264Decoder) GetD3D11Device() uintptr { return 0 }

// SetD3D11Device Linux 无 D3D11，no-op。
func (d *H264Decoder) SetD3D11Device(device uintptr) {}

// SetTextureOutput Linux 恒为 BGRA 输出（软解），no-op。
func (d *H264Decoder) SetTextureOutput(enable bool) {}

// IsSoftware Linux 恒为软解，恒 true。
func (d *H264Decoder) IsSoftware() bool { return true }

// ReopenSoftware Linux 恒为软解，无需回退，恒 false（未发生切换）。
func (d *H264Decoder) ReopenSoftware() bool { return false }

