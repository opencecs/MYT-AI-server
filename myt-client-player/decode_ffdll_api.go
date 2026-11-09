//go:build windows

package main

import (
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

// decode_ffdll_api.go — H264Decoder 的 GPU/软解配置与帧管理 API。
// 从 decode_ffdll.go 拆出，保持解码核心逻辑与 API 接口分离。

// GetD3D11Device 返回 FFmpeg 内部创建的 D3D11 device（若有）。
func (d *H264Decoder) GetD3D11Device() uintptr {
	if ffProcGetD3D11Device == 0 || d.handle == 0 {
		return 0
	}
	var dev uintptr
	ret, _, _ := purego.SyscallN(ffProcGetD3D11Device, d.handle, uintptr(unsafe.Pointer(&dev)))
	if ret == 0 {
		return dev
	}
	return 0
}

// SetD3D11Device 设外部 D3D11 device（渲染器 device），须在 Initialize 前调用。
// FFmpeg 解码将复用此 device，实现解码与渲染同设备（根治跨设备拷贝问题）。
// 实际传入 C 的时机在 Initialize 创建 handle 后（handle 创建前无法调用）。
func (d *H264Decoder) SetD3D11Device(device uintptr) {
	d.extDev = device
}

// SetTextureOutput 开关纹理输出路径（默认开）。渲染器无 NV12 SRV 能力（无 D3D11.3）
// 或纹理拷贝失败时关闭：硬解帧也走 FF_ReceiveBGRA（transfer_data+sws_scale）输出 BGRA。
func (d *H264Decoder) SetTextureOutput(enable bool) {
	if ffProcSetTextureOutput == 0 || d.handle == 0 {
		return
	}
	mode := uintptr(0)
	if enable {
		mode = 1
	}
	logf("[decode] SetTextureOutput(%v)\n", enable)
	purego.SyscallN(ffProcSetTextureOutput, d.handle, mode)
}

// ReopenSoftware 硬解卡死回退软解（mpv force_fallback 等价物）。
// 在解码线程调（FF_ReopenSoftware 同线程销毁 D3D11VA 资源）。返回 true=成功切软解。
func (d *H264Decoder) ReopenSoftware() bool {
	if ffProcReopenSoftware == 0 || d.handle == 0 {
		return false
	}
	ret, _, _ := purego.SyscallN(ffProcReopenSoftware, d.handle)
	if ret != 0 {
		logf("[fallback] FF_ReopenSoftware failed: ret=%d\n", ret)
		return false
	}
	logf("[fallback] reopened as software decoder (GPU decode stalled)\n")
	return true
}

// IsSoftware 返回当前是否已切软解（codec 无 hwDevCtx 或已关）。
func (d *H264Decoder) IsSoftware() bool {
	if ffProcIsSoftware == 0 || d.handle == 0 {
		return true
	}
	ret, _, _ := purego.SyscallN(ffProcIsSoftware, d.handle)
	return ret != 0
}

// ReceiveBGRA 对 FF_DecodeTexture 返回 4（软解帧已 receive）的帧转 BGRA，避免重复 send_packet。
func (d *H264Decoder) ReceiveBGRA() (bgra uintptr, w, h int32, ok bool) {
	if ffProcReceiveBGRA == 0 || d.handle == 0 {
		return 0, 0, 0, false
	}
	var outBGRA uintptr
	var ow, oh int32
	ret, _, _ := purego.SyscallN(ffProcReceiveBGRA, d.handle,
		uintptr(unsafe.Pointer(&outBGRA)),
		uintptr(unsafe.Pointer(&ow)),
		uintptr(unsafe.Pointer(&oh)))
	if ret == 1 && outBGRA != 0 && ow > 0 && oh > 0 {
		return outBGRA, ow, oh, true
	}
	return 0, 0, 0, false
}

// UnrefFrame 释放独立 frame 句柄（零拷贝路径：渲染器同设备拷贝后调用，让 hwframe 回池复用）
func (d *H264Decoder) UnrefFrame(frame uintptr) {
	if ffProcUnrefFrame != 0 && frame != 0 {
		purego.SyscallN(ffProcUnrefFrame, frame)
	}
}

// frameErrorFlags 返回解码器最近 receive 帧的 decode_error_flags
// （FF_DECODE_ERROR_*：1=INVALID_BITSTREAM 2=MISSING_REFERENCE 4=CONCEALMENT）。
// dll 无该导出（旧版）时返回 0 = 不做过滤，保持兼容。
// 智能丢帧丢 P/B 后解码器参考链断裂，FFmpeg 错误掩盖输出错帧（旧画面+撕裂混合），
// 内容随机 → UV/亮度探针难判，解码器自己的错误标志是最可靠信号：非 0 即丢弃保上一帧。
func (d *H264Decoder) frameErrorFlags() int {
	if ffProcFrameErrFlags == 0 || d.handle == 0 {
		return 0
	}
	ret, _, _ := purego.SyscallN(ffProcFrameErrFlags, d.handle)
	return int(int32(ret))
}

var frameErrDropCount atomic.Int32

// 错帧过滤连续计数兜底：丢包/PLI 重建后解码器参考链断裂，连续错帧若被全丢，
// 等不到 IDR 会永久冻结（GOP 内大部分帧 flags!=0）。同一解码会话连续 ≥N 帧
// 错帧被丢后停止过滤（放行显示错帧），直到下一个 flags==0 好帧重置计数。
// 最坏情况从「永久冻结」降级为「显示错帧（花屏/旧画面）」，由下一个 IDR 自愈。
const errFrameFilterMaxConsecutive = 6

var (
	frameErrConsec    atomic.Int32 // 连续被丢的错帧计数（flags==0 好帧重置）
	frameErrFilterOff atomic.Bool  // 兜底触发后本轮过滤已停（好帧到达重置）
)

// shouldDropErrorFrame 错帧过滤决策：返回 true=丢弃本帧。
// 连续 ≥errFrameFilterMaxConsecutive 帧被丢后兜底放行（日志记录降级事件）。
func shouldDropErrorFrame(flags int) bool {
	if flags == 0 {
		// 好帧：重置计数，恢复过滤
		if frameErrFilterOff.Swap(false) {
			logf("[decode] good frame received, error-frame filter re-enabled\n")
		}
		frameErrConsec.Store(0)
		return false
	}
	if frameErrFilterOff.Load() {
		return false // 兜底已触发：放行显示
	}
	n := frameErrConsec.Add(1)
	if n >= errFrameFilterMaxConsecutive {
		frameErrFilterOff.Store(true)
		c := frameErrDropCount.Load()
		logf("[decode] error-frame filter disabled after %d consecutive dropped (total dropped=%d), showing error frames\n",
			n, c)
		return false
	}
	return true
}

// errFrameFiltered 错帧丢弃诊断日志（首 5 条 + 每 100 条一条）。
func errFrameFiltered(flags int) {
	c := frameErrDropCount.Add(1)
	if c <= 5 || c%100 == 0 {
		logf("[decode] decode_error frame #%d dropped (hold last frame, flags=%d missing_ref=%v conceal=%v)\n",
			c, flags, flags&2 != 0, flags&4 != 0)
	}
}
