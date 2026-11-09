//go:build windows

package main

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// pendingFrame holds the latest decoded frame for the UI thread to consume.
type pendingFrame struct {
	bgra       uintptr
	bgraBuf    []byte // BGRA 路径：拷贝的独立 buffer，避免与解码线程 sws_scale 竞态导致绿色闪屏
	slot       int    // 纹理路径：CopyTexture 写入的槽位号（-1=无）
	width      int32
	height     int32
	stride     int32
	isTex      bool
	pts        time.Duration
	releaseRef func() // 纹理路径：延迟释放 hwframe（mpv unpooled mapping，GPU 用完才回池）
}

// d3d11BgraPool 复用 BGRA buffer，避免每帧 malloc
var d3d11BgraPool sync.Pool

func (r *D3D11Renderer) CaptureFrame() ([]byte, int32, int32, bool) {
	r.screenshotReq.Store(true)
	deadline := time.Now().Add(200 * time.Millisecond)
	texReadDone := false // 纹理回读每次截图只试一次（Map 阻塞等 GPU，避免 10ms 轮询反复 Map）
	for time.Now().Before(deadline) {
		r.mu.Lock()
		data := r.lastFrame
		w, h, rot := r.lastFrameW, r.lastFrameH, r.lastFrameRot
		r.lastFrame = nil
		r.mu.Unlock()
		if data != nil {
			return data, w, h, rot
		}
		// 纹理路径（硬解）：无 BGRA 帧，对最近拷入的槽位做 staging 回读。
		// 本函数由控制 server HTTP goroutine 调用（非 Present/解码线程），Map 安全。
		if !texReadDone {
			texReadDone = true
			if slot := r.lastTexSlot.Load(); slot >= 0 {
				if bgra, w, h := r.readTextureFrame(slot, r.lastTexW.Load(), r.lastTexH.Load()); bgra != nil {
					r.screenshotReq.Store(false)
					return bgra, w, h, r.rotated.Load()
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, 0, 0, false
}

// readTextureFrame staging 回读 + NV12→BGRA（buffer 走 d3d11BgraPool 池化）。
// 返回 nil 表示该帧不可用（回读失败 / UV 全 0 未初始化帧）。
func (r *D3D11Renderer) readTextureFrame(slot, frameW, frameH int32) ([]byte, int32, int32) {
	nv12, w, h, _, err := r.ReadSlot(slot, frameW, frameH)
	if err != nil || w <= 0 || h <= 0 {
		logf("[render] ReadSlot failed: %v\n", err)
		return nil, 0, 0
	}
	// UV plane 全 0 = D3D11VA 硬解纹理未初始化（ffdecode frameToBGRA 同款检测：
	// limited-range NV12 非法值零误判），丢弃避免绿帧上屏。采样前 64 字节即可。
	uvOff := int(w) * int(h)
	allZero := true
	for i := 0; i < 64 && uvOff+i < len(nv12); i++ {
		if nv12[uvOff+i] != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		logf("[render] ReadSlot UV plane all zero (texture not initialized), drop\n")
		return nil, 0, 0
	}
	// 不走池化：本函数可被 伪源帧泵 + control server /screenshot 两个 goroutine
	// 并发进入，池 buffer 归还/复用会竞态（读到对方写一半的 buffer）。截图/扫码
	// 频率低，每次独立分配（1280x720 约 3.5MB）换取简单可靠。
	bgra, w2, h2 := nv12ToBGRA(nv12, w, h)
	return bgra, w2, h2
}

// nv12ToBGRA NV12（Y plane + 交错 UV plane，packed stride=W）→ BGRA。
// BT.601 limited-range 整数近似（与 ffdecode sws_scale 输出一致）。
func nv12ToBGRA(nv12 []byte, w, h int32) ([]byte, int32, int32) {
	size := int(w) * int(h) * 4
	buf, _ := d3d11BgraPool.Get().([]byte)
	if cap(buf) < size {
		buf = make([]byte, size)
	}
	buf = buf[:size]
	yPlane := nv12[:int(w)*int(h)]
	uvPlane := nv12[int(w)*int(h):]
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			luma := int(yPlane[int(y)*int(w)+int(x)]) - 16
			// UV 交错：偶字节 U，奇字节 V；每 2x2 像素共享一组
			ui := (int(y)/2)*int(w) + int(x)
			u := int(uvPlane[ui&^1]) - 128
			v := int(uvPlane[ui|1]) - 128
			r := clamp8((298*luma + 409*v + 128) >> 8)
			g := clamp8((298*luma - 100*u - 208*v + 128) >> 8)
			b := clamp8((298*luma + 516*u + 128) >> 8)
			o := yi(int(w), int(y), int(x)) * 4
			buf[o] = byte(b)
			buf[o+1] = byte(g)
			buf[o+2] = byte(r)
			buf[o+3] = 0xFF
		}
	}
	return buf, w, h
}

func yi(w, y, x int) int { return y*w + x }

func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// CopyTexture 解码线程调用：把 FFmpeg hwframe 纹理拷入渲染器 copyTex 槽位（mpv copy_tex 模式）。
// 拷贝命令提交后即可释放源帧——同一 immediate context 内 GPU 命令按提交序串行执行，
// 后续解码写入必然在本次拷贝完成之后，无 use-after-free 竞态。
func (r *D3D11Renderer) CopyTexture(texture uintptr, subIdx int, frameW, frameH int32) (int, error) {
	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()
	if handle == 0 || texture == 0 || renderProcCopyTexture == 0 {
		return 0, fmt.Errorf("renderer not ready (handle=%d tex=%d proc=%d)", handle, texture, renderProcCopyTexture)
	}
	var slot int32
	ret, _, _ := purego.SyscallN(renderProcCopyTexture, handle, texture, uintptr(subIdx),
		uintptr(frameW), uintptr(frameH), uintptr(unsafe.Pointer(&slot)))
	if ret != 0 {
		return 0, fmt.Errorf("D3D11R_CopyTexture ret=%d", ret)
	}
	return int(slot), nil
}

// ReadSlot 回读指定槽位的 NV12 帧（C 侧 staging：CopyResource→Map(READ)→逐行拷，
// mpv ra_d3d11.c:736-767 同款）。Map 阻塞等 GPU——仅在控制 server HTTP goroutine
// 等非 Present/解码线程调用（cs 与渲染互斥）。返回紧凑 packing NV12
// （Y=W*H + UV=W*H/2，rowPitch=W）。
func (r *D3D11Renderer) ReadSlot(slot, frameW, frameH int32) ([]byte, int32, int32, int32, error) {
	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()
	if handle == 0 || renderProcReadSlot == 0 {
		return nil, 0, 0, 0, fmt.Errorf("renderer not ready (handle=%d proc=%d)", handle, renderProcReadSlot)
	}
	buf := make([]byte, int(frameW)*int(frameH)*3/2)
	var outW, outH, outRow int32
	ret, _, _ := purego.SyscallN(renderProcReadSlot, handle, uintptr(slot),
		uintptr(frameW), uintptr(frameH), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)), uintptr(unsafe.Pointer(&outW)), uintptr(unsafe.Pointer(&outH)),
		uintptr(unsafe.Pointer(&outRow)))
	if ret != 0 {
		return nil, 0, 0, 0, fmt.Errorf("D3D11R_ReadSlot ret=%d slot=%d", ret, slot)
	}
	return buf, outW, outH, outRow, nil
}

// UvProbeSlot 对指定槽位的 UV plane 顶部区域（64x4 微小区域）做 staging 回读采样，
// 判断 UV 是否全 0（D3D11VA 半帧/损坏帧，limited-range NV12 非法值，零误判）。
// 解码线程在 CopyTexture 之后调用（C 侧 cs 与 Present/渲染互斥，无竞态）。
// 返回值：1=UV 全 0（坏帧），0=正常，-1=探针失败（调用方按正常帧放行，不误伤）。
func (r *D3D11Renderer) UvProbeSlot(slot, frameW, frameH int32) int {
	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()
	if handle == 0 || renderProcUvProbe == 0 {
		return -1
	}
	ret, _, _ := purego.SyscallN(renderProcUvProbe, handle, uintptr(slot),
		uintptr(frameW), uintptr(frameH))
	return int(int32(ret))
}

// LumaProbeSlot 亮度+色度联合探针（D3D11R_LumaProbeSlot，新版 dll 才有）：
// Y 上/中/下 3 条带各 64x4 全 <16 占比 ≥95% → bit0 亮度黑；UV 全 0 → bit1（未初始化帧）；
// UV 全 128 → bit2（标准黑色度）。黑帧检测与坏帧分类用。
// 返回 -1 表示探针失败（调用方按正常帧放行）。
func (r *D3D11Renderer) LumaProbeSlot(slot, frameW, frameH int32) int {
	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()
	if handle == 0 || renderProcLumaProbe == 0 {
		return -1
	}
	ret, _, _ := purego.SyscallN(renderProcLumaProbe, handle, uintptr(slot),
		uintptr(frameW), uintptr(frameH))
	return int(int32(ret))
}

// RenderSlot UI 线程调用：绘制 CopyTexture 写入的槽位（不接触解码器纹理）。
func (r *D3D11Renderer) RenderSlot(slot, frameW, frameH int32) {
	r.SetSourceSize(frameW, frameH)
	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()
	if handle == 0 || renderProcRenderSlot == 0 {
		return
	}
	ret, _, _ := purego.SyscallN(renderProcRenderSlot, handle, uintptr(slot), uintptr(frameW), uintptr(frameH))
	if c := renderTexLogCount.Add(1); c <= 5 {
		logf("[render] RenderSlot #%d ret=%d slot=%d %dx%d\n", c, ret, slot, frameW, frameH)
	}
	if ret != 0 {
		logf("[render] RenderSlot failed: ret=%d slot=%d\n", ret, slot)
	}
}

func (r *D3D11Renderer) SendFrame(frame *VideoFrame) {
	if frame == nil || frame.BGRA.BGRA == 0 {
		return
	}
	// Screenshot capture (BGRA CPU path only)
	if r.screenshotReq.Load() && !frame.BGRA.IsTexture {
		size := int(frame.Width) * int(frame.Height) * 4
		if size > 0 {
			buf := make([]byte, size)
			src := unsafe.Slice((*byte)(unsafe.Pointer(frame.BGRA.BGRA)), size)
			copy(buf, src)
			r.mu.Lock()
			r.lastFrame = buf
			r.lastFrameW = frame.Width
			r.lastFrameH = frame.Height
			r.lastFrameRot = r.rotated.Load()
			r.mu.Unlock()
			r.screenshotReq.Store(false)
		}
	}

	// Store frame for UI thread to consume — replaces any previous pending frame
	pf := &pendingFrame{
		slot:   -1,
		width:  frame.Width,
		height: frame.Height,
		stride: int32(frame.Width) * 4,
		isTex:  frame.BGRA.IsTexture,
		pts:    frame.PTS,
	}

	if frame.BGRA.IsTexture {
		// 槽位路径（mpv copy_tex + unpooled mapping）：解码线程 CopyTexture 提交 GPU 拷贝命令，
		// 但不立即 unref 源 hwframe——CopyTexture 是异步 GPU 命令，GPU 还在读源纹理。
		// 立即 unref 会让 hwframe 回 FFmpeg 池被下一帧复用，与未完成的 GPU 拷贝竞争 → 池乱/卡死。
		// 改为延迟 unref：存到 pendingFrame.releaseRef，由 renderPending 在下一个 Present 周期后
		// 调用（此时 GPU 已用完源纹理），mpv “hold hwimage as long as necessary” 同款。
		slot, err := r.CopyTexture(frame.BGRA.BGRA, frame.BGRA.SubIdx, frame.Width, frame.Height)
		if err != nil {
			// 拷贝提交失败：立即释放源帧（不让它泄漏），通知降级 BGRA
			if frame.releaseRef != nil {
				frame.releaseRef()
			}
			logf("[render] CopyTexture failed: %v, 通知解码层降级 BGRA 输出\n", err)
			r.texBroken.Store(true)
			return
		}
		// UV=0 坏帧检测（64x4 微区域探针，D3D11VA 半帧/损坏帧，limited-range
		// NV12 非法值零误判）。分级处置（与 C 侧 BGRA 路径 badStreak 同策略）：
		//   单帧坏帧：不上 pending（UI 无新帧 → 保持上一帧，用户无感）；
		//   连续 ≥2 帧：放行渲染（shader 逐像素 UV<0.01→128 灰度修复，不闪绿）。
		// 探针失败（-1）按正常帧放行，不误伤。
		probe := -1
		if r.uvProbeOn.Load() {
			probe = r.UvProbeSlot(int32(slot), frame.Width, frame.Height)
		}
		if probe == 1 {
			streak := r.badStreakCb.Add(1)
			if streak < 2 {
				// 单帧坏帧：不送渲染。源 hwframe 延迟一个 Present 周期释放
				// （CopyTexture 已提交拷贝命令，立即 unref 会与 GPU 读竞争 → 池乱），
				// 下一帧 SendFrame 时被 swap 释放，等价于正常帧的 lastRendered 时序。
				if frame.releaseRef != nil {
					r.deferBadRelease(frame.releaseRef)
				} else {
					// 无延迟释放句柄（防御）：只能立即释放，风险同注释
					logf("[render] UV=0 frame without releaseRef, released immediately\n")
				}
				if c := badFrameLogCount.Add(1); c <= 5 || c%100 == 0 {
					logf("[render] UV=0 bad frame #%d dropped (hold last frame)\n", c)
				}
				return
			}
			if c := badFrameLogCount.Add(1); c <= 5 || c%100 == 0 {
				logf("[render] UV=0 bad frame #%d streak=%d, render as gray (degraded)\n", c, streak)
			}
		} else {
			r.badStreakCb.Store(0)
		}
		// 黑帧检测（LumaProbe 亮度+色度联合探针，与 C 侧 BGRA 路径同款滑动窗口占比策略）：
		//   bit0=Y 暗点占比 ≥95% 亮度黑，bit1=UV 全 0（未初始化帧），bit2=UV 全 128（标准黑）。
		//   300ms 窗口内黑覆盖 ≥25%：丢弃保上一帧（成对交替每段 streak=1 也能拦住）；
		//   <25%：放行渲染（可能是真黑屏内容，如设备熄屏）。
		//   bit1 场景已被上面 UV=0 分支先行处理，这里只拦 bit0。
		// 探针失败（-1）按正常帧放行，不误伤。
		if r.lumaProbeOn.Load() && probe != 1 {
			luma := r.LumaProbeSlot(int32(slot), frame.Width, frame.Height)
			// 探针失败（-1）按正常帧放行（与注释/C 侧契约一致）：-1 为全 1 位掩码，
			// 直接 luma&1 会误判「Y 全黑」→ 每帧被丢 → 永久冻结。
			black := luma >= 0 && luma&1 != 0
			drop, show := r.blackWin.Observe(time.Now(), black)
			if black {
				if drop {
					// 黑占比超阈：不送渲染，源 hwframe 延迟一个 Present 周期释放
					// （同 UV=0 坏帧的 lastBadRelease 时序）。
					if frame.releaseRef != nil {
						r.deferBadRelease(frame.releaseRef)
					}
					c := r.blackDropped.Add(1)
					if c <= 5 || c%100 == 0 {
						logf("[render] black frame #%d dropped (reason=window: black coverage >= 25%%, hold last, luma=%d)\n", c, luma)
					}
					return
				}
				if show {
					if c := r.blackDropped.Load(); c > 0 {
						// 漏检疑点：探针判黑但窗口占比低 → 上屏。若设备端正在闪，
						// 这里是「检测不稳定」的漏网帧，日志密度与 dropped 对比可定位。
						logf("[render] black frame shown (reason=window: black coverage < 25%%, possible real black, luma=%d, dropped so far=%d)\n", luma, c)
					} else {
						logf("[render] black frame shown (reason=lone: no recent black, possible real black, luma=%d)\n", luma)
					}
				}
			}
		}
		pf.slot = slot
		pf.releaseRef = frame.releaseRef // 延迟到 Present 后释放（见 releasePendingFrame）
		// 记录最近纹理帧槽位/尺寸：CaptureFrame 纹理分支 staging 回读用
		r.lastTexSlot.Store(int32(slot))
		r.lastTexW.Store(frame.Width)
		r.lastTexH.Store(frame.Height)
	} else {
		// BGRA CPU 路径：frame.BGRA.BGRA 指向解码器内部复用的 buffer，
		// UI 线程 RenderBGRA 的 memcpy 与解码线程 sws_scale 会竞争同一块内存，
		// 读到半 YUV 半 BGRA 的混合行 → 绿色闪屏。拷贝到独立 buffer 彻底解除竞态。
		size := int(frame.BGRA.Size)
		if size <= 0 {
			size = int(frame.Width) * int(frame.Height) * 4
		}
		if size > 0 {
			buf, _ := d3d11BgraPool.Get().([]byte)
			if cap(buf) < size {
				buf = make([]byte, size)
			} else {
				buf = buf[:size]
			}
			CopyBGRA(buf, frame.BGRA)
			/* 绿帧/黑帧分类已由 C 侧统一处理（frameToBGRA 检测修复），
			   Go 侧不再重复检测——旧版 BGRA 路径“收到什么渲染什么”的稳定性
			   与新版的帧分类防护在 C 侧合一。 */
			pf.bgraBuf = buf
			pf.bgra = uintptr(unsafe.Pointer(&buf[0]))
		}
	}

	// Release the previous pending frame's texture if needed
	old := r.pending.Swap(pf)
	if old != nil {
		releasePendingFrame(old)
	}

	// 新帧就绪，通知 UI 线程立即 Present（不等 WM_TIMER 16ms tick）。
	// 这是"帧到即渲染"的核心：消除 15-31ms 定时器抖动，直接治"卡"。
	if fn := r.onFrame.Load(); fn != nil && *fn != nil {
		(*fn)()
	}
}

// releasePendingFrame 释放 pendingFrame 持有的资源。
// 纹理帧：调 releaseRef（UnrefFrame）释放源 hwframe 回 FFmpeg 池——由 renderPending
// 在下一个 Present 周期后调用，此时 GPU 已用完源纹理，回池复用安全（mpv unpooled mapping）。
// BGRA 帧：buffer 归池。
func releasePendingFrame(pf *pendingFrame) {
	if pf == nil {
		return
	}
	if pf.releaseRef != nil {
		pf.releaseRef()
	}
	if pf.bgraBuf != nil {
		d3d11BgraPool.Put(pf.bgraBuf)
	}
}

// deferBadRelease 记录被丢弃坏帧的延迟释放句柄（SendFrame 解码线程调用）。
// 累积到列表，renderPending 下一个 Present 周期后全量执行，保证不漏。
func (r *D3D11Renderer) deferBadRelease(release func()) {
	r.lastBadMu.Lock()
	r.lastBadRelease = append(r.lastBadRelease, release)
	r.lastBadMu.Unlock()
}

// releaseDeferredBad 释放所有累积的坏帧句柄（renderPending UI 线程调用）。
func (r *D3D11Renderer) releaseDeferredBad() {
	r.lastBadMu.Lock()
	list := r.lastBadRelease
	r.lastBadRelease = nil
	r.lastBadMu.Unlock()
	for _, rel := range list {
		rel()
	}
}

// consumePending takes the latest pending frame for rendering on the UI thread.
// Returns nil if no new frame is available.
func (r *D3D11Renderer) consumePending() *pendingFrame {
	pf := r.pending.Swap(nil)
	return pf
}

// renderPending uploads the latest pending frame to GPU. Called from Present (UI thread).
// 返回是否有新帧上传（无帧时调用方可跳过 D3D11R_Present 系统调用，减 CPU）。
func (r *D3D11Renderer) renderPending() bool {
	// Release the previous frame now — the GPU has had at least
	// one full Present cycle (~16 ms) to finish reading it.
	if last := r.lastRendered.Swap(nil); last != nil {
		releasePendingFrame(last)
	}
	// 之前被丢弃的坏帧（UV=0 / 黑帧）的 hwframe：延迟一个 Present 周期后全量释放
	// （CopyTexture 已提交拷贝命令，GPU 还在读源纹理，立即 unref 会与 GPU 竞争 → 池乱）。
	// 全量释放（非单值覆盖）：连续丢帧时每个被丢帧的 release 都必须执行，
	// 否则 hwframe 泄漏 → 帧池耗尽卡死。
	r.releaseDeferredBad()

	pf := r.consumePending()
	if pf == nil {
		return false
	}
	if pf.isTex {
		r.RenderSlot(int32(pf.slot), pf.width, pf.height)
	} else {
		r.RenderBGRA(pf.bgra, pf.width, pf.height, pf.stride)
	}

	// Defer release: keep the frame alive for one more Present cycle so the
	// GPU can finish consuming the texture/buffer. We'll release it on the next call.
	r.lastRendered.Store(pf)
	r.hasEverRendered.Store(true)
	return true
}

func (r *D3D11Renderer) ScaleMouseCoords(mx, my int32) (sx, sy uint16, srcW, srcH int32, ok bool) {
	srcW, srcH = r.srcW.Load(), r.srcH.Load()
	rotated := r.rotated.Load()
	g := r.lbGeo.Load()
	if g == nil {
		return 0, 0, 0, 0, false
	}
	sx, sy, ok = scaleMouseCoords(mx, my, g, srcW, srcH, rotated)
	return
}

func (r *D3D11Renderer) LBGeo() *letterboxGeo { return r.lbGeo.Load() }
func (r *D3D11Renderer) IsRotated() bool      { return r.rotated.Load() }

// RenderBGRA uploads BGRA CPU pixels to GPU and draws.
func (r *D3D11Renderer) RenderBGRA(bgraData uintptr, frameW, frameH int32, stride int32) {
	r.SetSourceSize(frameW, frameH)

	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()

	if handle == 0 {
		return
	}

	purego.SyscallN(renderProcRenderBGRA, handle, bgraData, uintptr(frameW), uintptr(frameH), uintptr(stride))
}

// Present 上传最新 pending 帧到 GPU 并翻页上屏（UI 线程调用）。
// 无新帧且已渲染过时跳过 D3D11R_Present 系统调用以减 CPU；
// 首帧前必须 Present 黑缓冲区防绿屏。
// Present 返回 1 表示设备丢失（DEVICE_REMOVED/RESET），通知 UI 层重建管线。
func (r *D3D11Renderer) Present() {
	hasFrame := r.renderPending()

	r.mu.Lock()
	handle := r.handle
	skip := !hasFrame && r.hasEverRendered.Load()
	r.mu.Unlock()
	if skip || handle == 0 || renderProcPresent == 0 {
		return
	}
	ret, _, _ := purego.SyscallN(renderProcPresent, handle)
	if ret == 1 {
		// DEVICE_REMOVED / DEVICE_RESET：驱动重置或 VRAM 压力，渲染不可恢复。
		// 通知 UI 层重建渲染器+解码器管线。
		logf("[render] D3D11 device removed/reset detected, requesting pipeline rebuild\n")
		if fn := r.onDeviceLost.Load(); fn != nil && *fn != nil {
			(*fn)()
		}
	}
}

// Represent 兜底重绘最近一帧并 Present（DWM surface 失效/遮挡恢复场景）。
// C 侧自带 300ms 节流：正常帧流时每帧都在绘制，此调用直接返回。
func (r *D3D11Renderer) Represent() {
	r.mu.Lock()
	handle := r.handle
	r.mu.Unlock()
	if handle == 0 || renderProcRepresent == 0 {
		return
	}
	ret, _, _ := purego.SyscallN(renderProcRepresent, handle)
	if ret == 1 {
		logf("[render] Represent: device removed/reset detected\n")
		if fn := r.onDeviceLost.Load(); fn != nil && *fn != nil {
			(*fn)()
		}
	}
}
