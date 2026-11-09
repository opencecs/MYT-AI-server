//go:build windows

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

// D3D11Renderer renders video via Direct3D 11 GPU path.
type D3D11Renderer struct {
	mu      sync.Mutex
	hWnd    uintptr
	handle  uintptr // opaque D3D11Renderer* from d3d11render.dll
	width   int32
	height  int32
	rotated atomic.Bool
	// fill 用户显式填满（deprecated -fill，恒 false）。受限填满由
	// thresholdFill 按 decideThresholdFill 纯几何判定，每次几何重算时同步
	// 到 C 侧 D3D11R_SetFill（预编译 DLL 已有的运行时开关，不依赖重编）。
	fill          atomic.Bool
	thresholdFill atomic.Bool
	srcW          atomic.Int32
	srcH          atomic.Int32
	input         InputHandler
	lbGeo         atomic.Pointer[letterboxGeo]
	lbStale       atomic.Bool

	// Screenshot: cache BGRA frame on demand (set by CaptureFrame)
	screenshotReq atomic.Bool
	lastFrame     []byte
	lastFrameW    int32
	lastFrameH    int32
	lastFrameRot  bool

	// 纹理路径最近一帧的槽位与尺寸（SendFrame CopyTexture 成功后记录）。
	// CaptureFrame 纹理分支经 D3D11R_ReadSlot staging 回读（控制 server HTTP
	// goroutine 调用，非 Present/解码线程，cs 互斥）。lastTexSlot -1=尚无纹理帧。
	lastTexSlot atomic.Int32
	lastTexW    atomic.Int32
	lastTexH    atomic.Int32

	// hasEverRendered: 是否已成功上传过至少一帧。无新帧且已渲染过时，16ms 定时器跳过
	// D3D11R_Present 系统调用以减 CPU（首帧前不能跳过，需 Present 黑缓冲区防绿屏）。
	hasEverRendered atomic.Bool

	// Pending frame: latest decoded frame waiting for UI thread to render
	pending atomic.Pointer[pendingFrame]

	// Deferred release: keep the last rendered frame alive for one Present cycle
	// so the GPU has time to finish reading the texture before we release it.
	lastRendered atomic.Pointer[pendingFrame]

	// onFrame: 新帧就绪时回调（由 UI 注入，PostMessage 通知 UI 线程立即 Present）。
	// 帧到即渲染，消除 WM_TIMER 16ms tick 等待，直接治"卡"。nil=回退到 WM_TIMER 轮询。
	onFrame atomic.Pointer[func()]

	// onDeviceLost: Present 检测到 DEVICE_REMOVED/RESET 时回调（UI 注入，重建渲染+解码管线）
	onDeviceLost atomic.Pointer[func()]

	// texBroken: CopyTexture 失败（设备不一致/SRV 创建失败）置位，解码层据此降级 BGRA 输出
	texBroken atomic.Bool

	// UV=0 坏帧检测（SendFrame 纹理路径探针）：
	//   uvProbeOn 探针开关（DLL 无 D3D11R_UvProbeSlot 导出时自动关闭，兼容旧 dll）
	//   badStreakCb 连续坏帧计数（单帧丢弃保上一帧，≥2 帧灰度降级）
	//   lastBadRelease 丢弃坏帧的 hwframe 延迟释放句柄列表（下一个 Present 周期后
	//   全量释放）。用列表而非单值：单值被 Store 覆盖会丢失未执行的 release →
	//   hwframe 泄漏 → 帧池耗尽卡死（连续丢帧场景）。
	uvProbeOn      atomic.Bool
	badStreakCb    atomic.Int32
	lastBadMu      sync.Mutex
	lastBadRelease []func()

	// 黑帧检测（SendFrame 纹理路径 LumaProbe 探针，与 C 侧 BGRA 路径同策略）：
	//   lumaProbeOn 亮度探针开关（DLL 无 D3D11R_LumaProbeSlot 导出时自动关闭）
	//   blackWin 滑动窗口占比判定（300ms 内黑覆盖 ≥25% 丢弃，低频黑放行）
	lumaProbeOn  atomic.Bool
	blackWin     BlackFrameWindow // 黑帧滑动窗口（SendFrame 渲染线程串行调用，无锁）
	blackDropped atomic.Int32     // 累计丢弃的黑帧数（诊断日志用）
}

var (
	renderDLLOnce sync.Once
	renderDLLLoad error

	renderProcCreate           uintptr
	renderProcCreateWithDevice uintptr
	renderProcDestroy          uintptr
	renderProcGetDevice        uintptr
	renderProcIsWarp           uintptr
	renderProcCanNV12          uintptr
	renderProcRenderBGRA       uintptr
	renderProcCopyTexture      uintptr
	renderProcReadSlot         uintptr
	renderProcUvProbe          uintptr
	renderProcLumaProbe        uintptr
	renderProcRenderSlot       uintptr
	renderProcRepresent        uintptr
	renderProcResize           uintptr
	renderProcRotate           uintptr
	renderProcFill             uintptr
	renderProcPresent          uintptr
)

func loadD3D11Render() error {
	renderDLLOnce.Do(func() {
		dllName := "d3d11render.dll"

		exePath, _ := os.Executable()
		candidates := []string{filepath.Dir(exePath), os.TempDir()}

		var lastErr error
		for _, dir := range candidates {
			dllPath := filepath.Join(dir, dllName)

			extract := true
			// 不能只比较大小：新版 dll 大小可能碰巧与旧版相同，导致跳过更新跑旧 dll。
			if data, err := os.ReadFile(dllPath); err == nil && sha256.Sum256(data) == sha256.Sum256(d3d11renderDLL) {
				extract = false
			}
			if extract {
				if err := os.WriteFile(dllPath, d3d11renderDLL, 0o644); err != nil {
					lastErr = fmt.Errorf("d3d11render: write %s: %w", dllPath, err)
					continue
				}
			}

			handle, err := windows.LoadLibrary(dllPath)
			if err != nil {
				lastErr = fmt.Errorf("d3d11render: load %s: %w", dllPath, err)
				continue
			}
			lastErr = nil

			procs := []struct {
				name string
				dst  *uintptr
			}{
				{"D3D11R_Create", &renderProcCreate},
				{"D3D11R_CreateWithDevice", &renderProcCreateWithDevice},
				{"D3D11R_Destroy", &renderProcDestroy},
				{"D3D11R_GetDevice", &renderProcGetDevice},
				{"D3D11R_IsWarp", &renderProcIsWarp},
				{"D3D11R_CanRenderNV12", &renderProcCanNV12},
				{"D3D11R_RenderBGRA", &renderProcRenderBGRA},
				{"D3D11R_CopyTexture", &renderProcCopyTexture},
				{"D3D11R_ReadSlot", &renderProcReadSlot},
				{"D3D11R_UvProbeSlot", &renderProcUvProbe},
				{"D3D11R_LumaProbeSlot", &renderProcLumaProbe},
				{"D3D11R_RenderSlot", &renderProcRenderSlot},
				{"D3D11R_Represent", &renderProcRepresent},
				{"D3D11R_Resize", &renderProcResize},
				{"D3D11R_SetRotation", &renderProcRotate},
				{"D3D11R_SetFill", &renderProcFill},
				{"D3D11R_Present", &renderProcPresent},
			}
			procErr := false
			for _, p := range procs {
				addr, err := windows.GetProcAddress(handle, p.name)
				if err != nil {
					lastErr = fmt.Errorf("d3d11render: %s not found: %w", p.name, err)
					procErr = true
					break
				}
				*p.dst = uintptr(addr)
			}
			if !procErr {
				renderDLLLoad = nil
				return
			}
		}
		renderDLLLoad = lastErr
	})
	return renderDLLLoad
}

// NewD3D11Renderer creates a D3D11 GPU renderer for the given HWND.
func NewD3D11Renderer(hwnd uintptr, width, height int, rotated, fill bool) (*D3D11Renderer, error) {
	return NewD3D11RendererWithDevice(hwnd, width, height, rotated, fill, 0)
}

// DeviceHandle 返回渲染器的 D3D11 device（已 AddRef），供 FFmpeg 解码器复用以实现同设备。
// 调用方负责 Release（FFmpeg hwdevice 会处理）。
func (r *D3D11Renderer) DeviceHandle() uintptr {
	if r.handle == 0 || renderProcGetDevice == 0 {
		return 0
	}
	dev, _, _ := purego.SyscallN(renderProcGetDevice, r.handle)
	return dev
}

// IsWarp 返回 true 表示渲染器 device 是 WARP 软光栅（无 GPU 回退）。
// 此时不应把 device 传给解码器做 D3D11VA 硬解（WARP 无视频解码器，会失败/崩溃）。
func (r *D3D11Renderer) IsWarp() bool {
	if r.handle == 0 || renderProcIsWarp == 0 {
		return false
	}
	ret, _, _ := purego.SyscallN(renderProcIsWarp, r.handle)
	return ret != 0
}

// CanRenderNV12 返回 true 表示渲染器具备 NV12 平面 SRV 能力（D3D11.3 PlaneSlice）。
// false 时解码器应关闭纹理输出（FF_SetTextureOutput(0)）走 BGRA 回读，否则 GPU 模式黑屏。
func (r *D3D11Renderer) CanRenderNV12() bool {
	if r.handle == 0 || renderProcCanNV12 == 0 {
		return false
	}
	ret, _, _ := purego.SyscallN(renderProcCanNV12, r.handle)
	return ret != 0
}

// NewD3D11RendererWithDevice creates a D3D11 renderer sharing the decoder's D3D11 device.
// d3d11Device=0 means create its own device (BGRA CPU fallback path).
func NewD3D11RendererWithDevice(hwnd uintptr, width, height int, rotated, fill bool, d3d11Device uintptr) (*D3D11Renderer, error) {
	if err := loadD3D11Render(); err != nil {
		return nil, err
	}

	var handle uintptr
	if d3d11Device != 0 && renderProcCreateWithDevice != 0 {
		handle, _, _ = purego.SyscallN(renderProcCreateWithDevice, hwnd, d3d11Device)
	} else {
		handle, _, _ = purego.SyscallN(renderProcCreate, hwnd)
	}
	if handle == 0 {
		return nil, fmt.Errorf("D3D11R_Create failed")
	}

	r := &D3D11Renderer{
		hWnd:   hwnd,
		handle: handle,
		width:  int32(width),
		height: int32(height),
	}
	r.lastTexSlot.Store(-1) // 尚无纹理帧（0 是合法槽位，用 -1 作哨兵）
	// D3D11R_UvProbeSlot 是新版 dll 才有的导出：存在即开启 UV=0 坏帧探针
	// （旧 dll 时 auto-disable，兼容嵌入的旧产物，行为退回旧版）。
	r.uvProbeOn.Store(renderProcUvProbe != 0)
	if r.uvProbeOn.Load() {
		logf("[render] UV=0 bad-frame probe enabled (d3d11render.dll supports UvProbeSlot)\n")
	}
	// D3D11R_LumaProbeSlot 是新版 dll 才有的导出：存在即开启黑帧亮度探针
	// （旧 dll 时 auto-disable，兼容嵌入的旧产物，行为退回旧版）。
	r.lumaProbeOn.Store(renderProcLumaProbe != 0)
	if r.lumaProbeOn.Load() {
		logf("[render] black-frame luma probe enabled (d3d11render.dll supports LumaProbeSlot)\n")
	}
	r.SetRotation(rotated)
	r.SetFill(fill)
	return r, nil
}

// SetInputHandler sets the handler for forwarded input events.
func (r *D3D11Renderer) SetInputHandler(h InputHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.input = h
}

// SetOnFrame 注入新帧就绪回调。UI 创建 renderer 后调用，回调里 PostMessage(WM_APP_RENDER)
// 通知 UI 线程立即 Present，不再等 WM_TIMER 16ms tick。
func (r *D3D11Renderer) SetOnFrame(fn func()) {
	r.onFrame.Store(&fn)
}

// SetOnDeviceLost 注入设备丢失回调（Present 检测到 DEVICE_REMOVED/RESET 时触发）。
func (r *D3D11Renderer) SetOnDeviceLost(fn func()) {
	r.onDeviceLost.Store(&fn)
}

// TexBroken 返回 true 表示纹理拷贝管线已损坏（应降级 BGRA 输出）。
func (r *D3D11Renderer) TexBroken() bool { return r.texBroken.Load() }

// SetSourceSize updates video source dimensions (for letterbox/mouse scaling).
func (r *D3D11Renderer) SetSourceSize(w, h int32) {
	if r.srcW.Load() == w && r.srcH.Load() == h {
		return
	}
	r.srcW.Store(w)
	r.srcH.Store(h)
	r.lbStale.Store(true)
	r.recomputeLetterbox()
}

func (r *D3D11Renderer) SourceSize() (int32, int32) {
	return r.srcW.Load(), r.srcH.Load()
}

func (r *D3D11Renderer) recomputeLetterbox() {
	if !r.lbStale.Load() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// 与 C 侧 computeLetterbox 单一事实源一致：窗口尺寸一律读 HWND 当前客户区。
	// r.width/height 是 swapchain 尺寸，拖动中（轻量 resize）两者不同步，
	// 用缓存值会与 C 侧几何不一致导致画面跳动/黑边。
	winW, winH := r.width, r.height
	if r.hWnd != 0 {
		var rc struct{ Left, Top, Right, Bottom int32 }
		if ret, _, _ := procGetClientRect.Call(r.hWnd, uintptr(unsafe.Pointer(&rc))); ret != 0 {
			winW = rc.Right - rc.Left
			winH = rc.Bottom - rc.Top
		}
	}
	geo := computeLetterbox(winW, winH, r.srcW.Load(), r.srcH.Load(), r.rotated.Load(), r.fill.Load())
	debugLog(fmt.Sprintf("[lb] win=%dx%d src=%dx%d draw=%dx%d off=%d,%d", winW, winH, r.srcW.Load(), r.srcH.Load(), geo.drawW, geo.drawH, geo.offX, geo.offY))
	r.lbGeo.Store(&geo)
	r.lbStale.Store(false)
	// 受限填满同步：把阈值判定结果驱动到 C 侧 D3D11R_SetFill（DLL 已有
	// 运行时开关），让 C 渲染几何（fill 短路 vs letterbox）与 Go geo 一致。
	// 仅在 lbStale 时机（尺寸/源/旋转变化）调用，收敛不抖动；SetFill 内部
	// 与值未变时幂等。computeLetterbox 已含同一判定（fill||threshold），
	// 判定值变化时 geo 与 C 侧同步切换。
	r.syncThresholdFill(winW, winH)
}

// syncThresholdFill 将受限填满判定同步到 C 侧（阈值内 SetFill(true)，
// 超阈值 SetFill(false)）。判定是纯几何计算，每次 recomputeLetterbox
// 重算（窗口恢复接近流比例时自动切回填满），无状态。
func (r *D3D11Renderer) syncThresholdFill(winW, winH int32) {
	want := r.fill.Load() || decideThresholdFill(winW, winH, r.srcW.Load(), r.srcH.Load(), r.rotated.Load())
	if r.thresholdFill.Swap(want) == want {
		return
	}
	if r.handle != 0 && renderProcFill != 0 {
		f := uintptr(0)
		if want {
			f = 1
		}
		purego.SyscallN(renderProcFill, r.handle, f)
	}
}

// UpdateSize updates the renderer size. Since layoutChildren is always called
// from the UI thread (WM_SIZE, WM_DPICHANGED, etc.), we can execute the D3D11
// resize immediately instead of deferring — this prevents letterbox black bars
// caused by the DLL using stale dimensions during the delay.
// fullResize=false 时只更新 Go 侧尺寸+letterbox（拖动中高频调用，避免 ResizeBuffers 闪黑），
// true 时真正 ResizeBuffers（拖动结束/常规 resize 调用）。
func (r *D3D11Renderer) UpdateSize(w, h int32) {
	r.updateSize(w, h, true)
}

// UpdateSizeLight 轻量更新：只更新尺寸+letterbox，不 ResizeBuffers（拖动中用）。
func (r *D3D11Renderer) UpdateSizeLight(w, h int32) {
	r.updateSize(w, h, false)
}

func (r *D3D11Renderer) updateSize(w, h int32, fullResize bool) {
	r.mu.Lock()
	r.width = w
	r.height = h
	handle := r.handle // mutex 内读 handle，避免与 Close 的 handle=0 竞态
	r.mu.Unlock()
	r.lbStale.Store(true)
	r.recomputeLetterbox()
	if !fullResize {
		// 拖动中：只更新尺寸与 letterbox，不 ResizeBuffers，避免 DXGI 重建闪黑。
		// swapchain 仍是旧尺寸，Present 时 DWM 会缩放显示，画面跟随但可能有轻微模糊。
		return
	}
	// Execute D3D11 resize immediately (we are on the UI thread)
	if handle != 0 && renderProcResize != 0 {
		purego.SyscallN(renderProcResize, handle, uintptr(w), uintptr(h))
	}
	// resize 后立即 Present：D3D11R_Resize 内部已把上一帧重绘到新 back buffer 并置 C 侧 pendingFrame=1，
	// 但未 Present。拖动窗口时若不立即上屏，DWM 会显示被清黑的 back buffer → 黑色闪。
	// 直接调 C 侧 D3D11R_Present（绕过 Go 侧 Present 的 skip 逻辑，它会因 Go 侧无新帧而跳过）。
	if handle != 0 && renderProcPresent != 0 {
		purego.SyscallN(renderProcPresent, handle)
	}
}

// SetRotation updates the rotation state.
func (r *D3D11Renderer) SetRotation(rotated bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rotated.Load() != rotated {
		r.rotated.Store(rotated)
		r.lbStale.Store(true)
		if r.handle != 0 {
			rot := uintptr(0)
			if rotated {
				rot = 1
			}
			purego.SyscallN(renderProcRotate, r.handle, rot)
		}
	}
}

// SetFill 设置画面拉伸填满模式（跳过 letterbox 比例，SDL 区满幅渲染）。
func (r *D3D11Renderer) SetFill(fill bool) {
	r.fill.Store(fill)
	r.lbStale.Store(true)
	if r.handle != 0 && renderProcFill != 0 {
		f := uintptr(0)
		if fill {
			f = 1
		}
		purego.SyscallN(renderProcFill, r.handle, f)
	}
	r.recomputeLetterbox()
}

// SetOverlay is a no-op for D3D11 renderer.
func (r *D3D11Renderer) SetOverlay(text string) {}

// Close destroys the D3D11 renderer.
func (r *D3D11Renderer) Close() {
	// Release any held pending/deferred frames before destroying the renderer.
	releasePendingFrame(r.pending.Swap(nil))
	releasePendingFrame(r.lastRendered.Swap(nil))
	r.mu.Lock()
	handle := r.handle
	r.handle = 0
	r.mu.Unlock()
	if handle != 0 {
		purego.SyscallN(renderProcDestroy, handle)
	}
}

// Size returns current window dimensions.
func (r *D3D11Renderer) Size() (int32, int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.width, r.height
}
