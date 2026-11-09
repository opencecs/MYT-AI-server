//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// MetalRenderer renders video via Metal GPU path.
type MetalRenderer struct {
	mu      sync.Mutex
	nsView  uintptr
	handle  uintptr // opaque MetalRenderer* from metalrender.dylib
	width   int32
	height  int32
	rotated atomic.Bool
	fill    atomic.Bool
	srcW    atomic.Int32
	srcH    atomic.Int32
	input   InputHandler
	lbGeo   atomic.Pointer[letterboxGeo]
	lbStale atomic.Bool

	pendingW   atomic.Int32
	pendingH   atomic.Int32
	hasPending atomic.Bool

	screenshotReq atomic.Bool
	lastFrame     []byte
	lastFrameW    int32
	lastFrameH    int32
	lastFrameRot  bool
}

var (
	metalDLLOnce sync.Once
	metalDLLLoad error

	metalProcCreate   uintptr
	metalProcDestroy  uintptr
	metalProcRender   uintptr
	metalProcPresent  uintptr
	metalProcIdle     uintptr
	metalProcResize   uintptr
	metalProcRotate   uintptr
	metalProcLB       uintptr
)

func loadMetalRender() error {
	metalDLLOnce.Do(func() {
		dylibName := "metalrender.dylib"

		exePath, _ := os.Executable()
		candidates := []string{filepath.Dir(exePath), os.TempDir()}

		var lastErr error
		for _, dir := range candidates {
			dylibPath := filepath.Join(dir, dylibName)

			extract := true
			if info, err := os.Stat(dylibPath); err == nil && info.Size() == int64(len(metalrenderDLL)) {
				extract = false
			}
			if extract {
				if err := os.WriteFile(dylibPath, metalrenderDLL, 0o644); err != nil {
					lastErr = fmt.Errorf("metalrender: write %s: %w", dylibPath, err)
					continue
				}
			}

			handle, err := purego.Dlopen(dylibPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				lastErr = fmt.Errorf("metalrender: load %s: %w", dylibPath, err)
				continue
			}
			lastErr = nil

			procs := []struct {
				name string
				dst  *uintptr
			}{
				{"MetalR_Create", &metalProcCreate},
				{"MetalR_Destroy", &metalProcDestroy},
				{"MetalR_RenderBGRA", &metalProcRender},
				{"MetalR_Present", &metalProcPresent},
				{"MetalR_RenderIdle", &metalProcIdle},
				{"MetalR_Resize", &metalProcResize},
				{"MetalR_SetRotation", &metalProcRotate},
				{"MetalR_GetLetterbox", &metalProcLB},
			}
			procErr := false
			for _, p := range procs {
				addr, symErr := purego.Dlsym(handle, p.name)
				if symErr != nil {
					lastErr = fmt.Errorf("metalrender: %s not found: %w", p.name, symErr)
					procErr = true
					break
				}
				*p.dst = addr
			}
			if !procErr {
				metalDLLLoad = nil
				return
			}
		}
		metalDLLLoad = lastErr
	})
	return metalDLLLoad
}

// NewMetalRenderer creates a Metal GPU renderer for the given NSView.
func NewMetalRenderer(nsView uintptr, width, height int, rotated, fill bool) (*MetalRenderer, error) {
	if err := loadMetalRender(); err != nil {
		return nil, err
	}

	handle, _, _ := purego.SyscallN(metalProcCreate, nsView)
	if handle == 0 {
		return nil, fmt.Errorf("MetalR_Create failed")
	}

	r := &MetalRenderer{
		nsView: nsView,
		handle: handle,
		width:  int32(width),
		height: int32(height),
	}
	r.SetRotation(rotated)
	r.fill.Store(fill)
	return r, nil
}

func (r *MetalRenderer) SetInputHandler(h InputHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.input = h
}

func (r *MetalRenderer) SetSourceSize(w, h int32) {
	if r.srcW.Load() == w && r.srcH.Load() == h {
		return
	}
	r.srcW.Store(w)
	r.srcH.Store(h)
	r.lbStale.Store(true)
	r.recomputeLetterbox()
}

func (r *MetalRenderer) SourceSize() (int32, int32) {
	return r.srcW.Load(), r.srcH.Load()
}

func (r *MetalRenderer) recomputeLetterbox() {
	if !r.lbStale.Load() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	winW, winH := r.width, r.height
	if r.fill.Load() {
		r.lbGeo.Store(&letterboxGeo{drawW: winW, drawH: winH, winW: winW, winH: winH})
		r.lbStale.Store(false)
		return
	}
	srcW, srcH := r.srcW.Load(), r.srcH.Load()
	effW, effH := srcW, srcH
	if r.rotated.Load() && srcW > 0 && srcH > 0 && srcW >= srcH {
		effW, effH = srcH, srcW
	}

	var drawW, drawH, offX, offY int32
	if effW > 0 && effH > 0 {
		if winW*effH > winH*effW {
			drawH = winH
			drawW = (winH*effW + effH/2) / effH
			offX = (winW - drawW) / 2
		} else {
			drawW = winW
			drawH = (winW*effH + effW/2) / effW
			offY = (winH - drawH) / 2
		}
		// 黑边 ≤2 像素时直接填满，消除整数截断残余黑边（1-2px 宽高比偏差肉眼不可见）
		if offX > 0 && offX <= 2 {
			drawW = winW
			offX = 0
		}
		if offY > 0 && offY <= 2 {
			drawH = winH
			offY = 0
		}
	}

	r.lbGeo.Store(&letterboxGeo{drawW: drawW, drawH: drawH, offX: offX, offY: offY, winW: winW, winH: winH})
	r.lbStale.Store(false)

	// 同步到 dylib
	if r.handle != 0 && metalProcLB != 0 {
		var dW, dH, oX, oY int32
		purego.SyscallN(metalProcLB, r.handle,
			uintptr(unsafe.Pointer(&dW)), uintptr(unsafe.Pointer(&dH)),
			uintptr(unsafe.Pointer(&oX)), uintptr(unsafe.Pointer(&oY)))
	}
}

func (r *MetalRenderer) SendFrame(frame *VideoFrame) {
	if frame == nil || frame.BGRA.BGRA == 0 {
		return
	}
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

	stride := int32(frame.Width) * 4
	r.SetSourceSize(frame.Width, frame.Height)

	r.mu.Lock()
	handle := r.handle
	if handle != 0 {
		purego.SyscallN(metalProcRender, handle, frame.BGRA.BGRA,
			uintptr(frame.Width), uintptr(frame.Height), uintptr(stride))
	}
	r.mu.Unlock()
}

func (r *MetalRenderer) Present() {
	if r.hasPending.Swap(false) {
		w := r.pendingW.Load()
		h := r.pendingH.Load()
		r.mu.Lock()
		handle := r.handle
		if handle != 0 && metalProcResize != 0 {
			purego.SyscallN(metalProcResize, handle, uintptr(w), uintptr(h))
		}
		r.mu.Unlock()
	}

	r.mu.Lock()
	handle := r.handle
	if handle != 0 && metalProcPresent != 0 {
		purego.SyscallN(metalProcPresent, handle)
	}
	r.mu.Unlock()
}

func (r *MetalRenderer) RenderIdle() {
	r.mu.Lock()
	handle := r.handle
	if handle != 0 {
		purego.SyscallN(metalProcIdle, handle)
	}
	r.mu.Unlock()
}

func (r *MetalRenderer) UpdateSize(w, h int32) {
	r.mu.Lock()
	r.width = w
	r.height = h
	r.mu.Unlock()
	r.pendingW.Store(w)
	r.pendingH.Store(h)
	r.hasPending.Store(true)
	r.lbStale.Store(true)
	r.recomputeLetterbox()
}

func (r *MetalRenderer) SetRotation(rotated bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rotated.Load() != rotated {
		r.rotated.Store(rotated)
		r.lbStale.Store(true)
		if r.handle != 0 && metalProcRotate != 0 {
			rot := uintptr(0)
			if rotated {
				rot = 1
			}
			purego.SyscallN(metalProcRotate, r.handle, rot)
		}
	}
}

func (r *MetalRenderer) SetOverlay(text string) {}

func (r *MetalRenderer) Close() {
	r.mu.Lock()
	handle := r.handle
	r.handle = 0
	if handle != 0 {
		purego.SyscallN(metalProcDestroy, handle)
	}
	r.mu.Unlock()
}

func (r *MetalRenderer) Size() (int32, int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.width, r.height
}

func (r *MetalRenderer) CaptureFrame() ([]byte, int32, int32, bool) {
	r.screenshotReq.Store(true)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		data := r.lastFrame
		w, h, rot := r.lastFrameW, r.lastFrameH, r.lastFrameRot
		r.lastFrame = nil
		r.mu.Unlock()
		if data != nil {
			return data, w, h, rot
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, 0, 0, false
}

func (r *MetalRenderer) ScaleMouseCoords(mx, my int32) (sx, sy uint16, srcW, srcH int32, ok bool) {
	srcW, srcH = r.srcW.Load(), r.srcH.Load()
	rotated := r.rotated.Load()
	g := r.lbGeo.Load()
	if g == nil {
		return 0, 0, 0, 0, false
	}
	drawW, drawH, offX, offY := g.drawW, g.drawH, g.offX, g.offY
	if drawW <= 0 || drawH <= 0 || srcW <= 0 || srcH <= 0 {
		return 0, 0, 0, 0, false
	}
	relX := int32(mx) - offX
	relY := int32(my) - offY
	if relX < 0 {
		relX = 0
	} else if relX > drawW {
		relX = drawW
	}
	if relY < 0 {
		relY = 0
	} else if relY > drawH {
		relY = drawH
	}
	var x, y int32
	if rotated {
		x = relY * srcW / drawH
		y = (drawW - relX) * srcH / drawW
	} else {
		x = relX * srcW / drawW
		y = relY * srcH / drawH
	}
	if x < 0 {
		x = 0
	} else if x > srcW {
		x = srcW
	}
	if y < 0 {
		y = 0
	} else if y > srcH {
		y = srcH
	}
	return uint16(x), uint16(y), srcW, srcH, true
}

func (r *MetalRenderer) LBGeo() *letterboxGeo { return r.lbGeo.Load() }
func (r *MetalRenderer) IsRotated() bool       { return r.rotated.Load() }
