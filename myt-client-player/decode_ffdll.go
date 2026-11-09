//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
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
	ffProcGetD3D11Device    uintptr
	ffProcSetD3D11Device    uintptr
	ffProcReceiveBGRA       uintptr
	ffProcDecodeTexture     uintptr
	ffProcReceiveTexture    uintptr
	ffProcUnrefFrame        uintptr
	ffProcSetHWAccel        uintptr
	ffProcSetTextureOutput  uintptr
	ffProcReopenSoftware    uintptr
	ffProcIsSoftware        uintptr
	ffProcFrameErrFlags     uintptr
)

// H264Decoder wraps FFmpeg for H.264 decoding via ffdecode.dll.
type H264Decoder struct {
	handle uintptr
	extDev uintptr // 外部 D3D11 device（渲染器），Initialize 时传给 FFmpeg
	lastW  int32
	lastH  int32

	// hwAccel 是否启用硬件解码。true=允许 D3D11VA/DXVA2 硬解，false=纯 CPU 软解。
	// 由启动参数 -hwaccel 决定，MYT_NO_HWACCEL 环境变量可强制覆盖关闭（调试用）。
	hwAccel bool

	reqCh    chan []byte
	resultCh chan DecodeResult
	doneCh   chan struct{}
	closed   chan struct{}
}

func NewH264Decoder(hwAccel bool) *H264Decoder {
	return &H264Decoder{
		hwAccel:  hwAccel,
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

	// 创建 handle 后立即把外部 device 传给 FFmpeg（openCodec 前设置）
	if d.extDev != 0 && ffProcSetD3D11Device != 0 {
		purego.SyscallN(ffProcSetD3D11Device, d.handle, d.extDev)
	}

	// 硬件解码开关：默认按启动参数 -hwaccel（d.hwAccel）。
	// MYT_NO_HWACCEL=1 环境变量可强制关闭（调试覆盖，优先级最高）。
	// WARP 软光栅（无 GPU）已在上层 Connect 判定不传 extDev，这里无需再处理。
	if ffProcSetHWAccel != 0 {
		disableHw := false
		if v, ok := os.LookupEnv("MYT_NO_HWACCEL"); ok && (v == "1" || v == "true") {
			disableHw = true
			logf("[decode] MYT_NO_HWACCEL=1, 强制关闭硬件加速（环境变量覆盖）")
		} else if !d.hwAccel {
			disableHw = true
			logf("[decode] -hwaccel=false，使用软件解码")
		} else {
			logf("[decode] GPU 加速已开启")
		}
		if disableHw {
			purego.SyscallN(ffProcSetHWAccel, handle, 0)
		}
	}

	go d.runLoop()

	infoStr := queryDecoderInfo(d.handle)
	logf("[decode] H264 decoder created (FFmpeg, %s)\n", infoStr)
	return nil
}

func (d *H264Decoder) runLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(d.closed)

	windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED)

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

	/* 零拷贝路径优先：FF_DecodeTexture → copy_tex 方案（D3D11VA 纹理拷贝到独立 staging）。
	   成功返回 IsTexture=true（GPU 纹理，由 D3D11R_RenderTexture 直接渲染，零 CPU 搬运）。
	   失败（软解/无硬解）回退 CPU BGRA 路径（transfer_data + sws_scale）。 */
	if ffProcDecodeTexture != 0 {
		var texPtr uintptr
		var subIdx int32
		var w, h int32
		var frameHandle uintptr
		ret, _, _ := purego.SyscallN(ffProcDecodeTexture,
			d.handle,
			uintptr(unsafe.Pointer(&data[0])),
			uintptr(len(data)),
			uintptr(unsafe.Pointer(&texPtr)),
			uintptr(unsafe.Pointer(&subIdx)),
			uintptr(unsafe.Pointer(&w)),
			uintptr(unsafe.Pointer(&h)),
			uintptr(unsafe.Pointer(&frameHandle)),
		)
		if ret == 1 && texPtr != 0 && w > 0 && h > 0 {
			// 错帧过滤（decode_error_flags 非零=缺参考/掩盖）：丢弃保上一帧。
			// 纹理帧此刻已 receive，直接 unref 释放（未提交任何 GPU 拷贝，无竞态）。
			if flags := d.frameErrorFlags(); flags != 0 && shouldDropErrorFrame(flags) {
				d.UnrefFrame(frameHandle)
				errFrameFiltered(flags)
				return DecodeResult{}
			}
			if !decodeLogOnce.Swap(true) {
				logf("[decode] first frame decoded via texture (%dx%d), zero-copy GPU path\n", w, h)
			}
			d.lastW = w
			d.lastH = h
			return DecodeResult{Width: w, Height: h, BGRA: texPtr, SubIdx: int(subIdx), IsTexture: true, FrameHandle: frameHandle}
		}
		// 损坏帧说明（版本漂移修正）：C 源码 ffdecode.c 无 return 3（全文件核实）。
		// 硬解 UV=0 半帧的灰度修复在 C 侧 frameToBGRA 内完成
		// （transfer_data 后检测、UV 置 128），
		// 正常经 ret==4/BGRA 路径输出，无需 Go 侧丢弃分支。
		// ret==4 软解帧已 receive（NALU 已消耗），或纹理输出被禁用（渲染器无 NV12 SRV）：
		// 调 ReceiveBGRA 取 BGRA，不能再调 FF_Decode（会重复 send_packet 同一 NALU，扰乱参考帧 → 马赛克）
		if ret == 4 {
			bgra, ow, oh, ok := d.ReceiveBGRA()
			if ok {
				if flags := d.frameErrorFlags(); flags != 0 && shouldDropErrorFrame(flags) {
					errFrameFiltered(flags)
					return DecodeResult{}
				}
				if !decodeLogOnce.Swap(true) {
					infoStr := queryDecoderInfo(d.handle)
					logf("[decode] first frame decoded (%dx%d), %s\n", ow, oh, infoStr)
				}
				d.lastW = ow
				d.lastH = oh
				size := int(ow) * int(oh) * 4
				return DecodeResult{Width: ow, Height: oh, BGRA: bgra, Size: size}
			}
			return DecodeResult{}
		}
		// 走到这里 = FF_DecodeTexture 返回但无输出：ret==2（EAGAIN，packet 已被
		// send_packet 入队消费）/ ret<0（真错误）/ ret==1 但输出参数非法（防御）。
		// 一律结束本帧处理，绝不回退 FF_Decode 重复 send 同一 NALU
		// （解码器队列 ≥2 时同一 P 帧入队两次 → 参考帧错乱/花屏）。
		// FF_Decode 回退只服务旧版 DLL（无 FF_DecodeTexture 导出），见外层 if。
		// ret==2 EAGAIN 时的补收：FF_ReceiveTexture 只 receive 不 send，队列里通常
		// 已有下一帧可立即输出；仍无帧则丢一帧输出机会（下一帧到来自然恢复）。
		// 旧版 DLL 无 FF_ReceiveTexture 符号（ffProcReceiveTexture==0）时直接结束，
		// 下帧到来时解码器自然输出（Go 规避兜底）。
		if ret == 2 && ffProcReceiveTexture != 0 && d.handle != 0 {
			var texPtr uintptr
			var subIdx int32
			var w, h int32
			var frameHandle uintptr
			rret, _, _ := purego.SyscallN(ffProcReceiveTexture,
				d.handle,
				uintptr(unsafe.Pointer(&texPtr)),
				uintptr(unsafe.Pointer(&subIdx)),
				uintptr(unsafe.Pointer(&w)),
				uintptr(unsafe.Pointer(&h)),
				uintptr(unsafe.Pointer(&frameHandle)),
			)
			if rret == 1 && texPtr != 0 && w > 0 && h > 0 {
				// 错帧过滤（同上）：补收帧同样可能是缺参考错帧
				if flags := d.frameErrorFlags(); flags != 0 && shouldDropErrorFrame(flags) {
					d.UnrefFrame(frameHandle)
					errFrameFiltered(flags)
					return DecodeResult{}
				}
				if !decodeLogOnce.Swap(true) {
					logf("[decode] first frame via ReceiveTexture (%dx%d), zero-copy GPU path\n", w, h)
				}
				d.lastW = w
				d.lastH = h
				return DecodeResult{Width: w, Height: h, BGRA: texPtr, SubIdx: int(subIdx), IsTexture: true, FrameHandle: frameHandle}
			}
			if rret == 4 {
				bgra, ow, oh, ok := d.ReceiveBGRA()
				if ok {
					if flags := d.frameErrorFlags(); flags != 0 && shouldDropErrorFrame(flags) {
						errFrameFiltered(flags)
						return DecodeResult{}
					}
					if !decodeLogOnce.Swap(true) {
						infoStr := queryDecoderInfo(d.handle)
						logf("[decode] first frame decoded (%dx%d), %s\n", ow, oh, infoStr)
					}
					d.lastW = ow
					d.lastH = oh
					size := int(ow) * int(oh) * 4
					return DecodeResult{Width: ow, Height: oh, BGRA: bgra, Size: size}
				}
			}
			// rret==2（仍无帧）或 rret<0：本次包处理结束，不重复 send
		}
		return DecodeResult{}
	}

	/* CPU BGRA 路径：FF_Decode → transfer_data + sws_scale → BGRA buffer。 */
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

	// 错帧过滤（同上）：CPU 回退路径同样可能是缺参考错帧
	if flags := d.frameErrorFlags(); flags != 0 && shouldDropErrorFrame(flags) {
		errFrameFiltered(flags)
		return DecodeResult{}
	}

	if !decodeLogOnce.Swap(true) {
		infoStr := queryDecoderInfo(d.handle)
		logf("[decode] first frame decoded (%dx%d), %s\n", outWidth, outHeight, infoStr)
	}

	// 分辨率变化时不调 FF_Flush：每帧 UV 检测已能处理分辨率变化的过渡帧，
	// 且 FF_Flush 与 BGRA 输出缓冲的生命周期有交互风险（悬垂指针），保持不调用。
	d.lastW = outWidth
	d.lastH = outHeight

	// 绿闪诊断：采样解码输出首行前 4 像素，确认 outBGRA 是否脏（绿色/垃圾）
	if c := greenDiagCount.Add(1); c <= 5 {
		buf := unsafe.Slice((*byte)(unsafe.Pointer(outBGRA)), min(16, int(outWidth)*4))
		greenDiagWrite("DECODE frame=%d w=%d h=%d px=%v", c, outWidth, outHeight, buf)
	}

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
		logf("[decode] Close: runLoop did not exit in 2s, leaking D3D11 resources\n")
	}
}