//go:build linux

// render_linux.go — X11 渲染器（Linux）：对照 render.go 的 GDI Renderer 接口集实现，
// BGRA→ZPixmap 32bpp 同字节序直 blit，缩放走 XRender 双线性（cgo X11，绘制路径见
// render_linux_paint.go）。
package main

/*
#cgo pkg-config: x11 xrender

#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/extensions/Xrender.h>

// myXDestroyImage XDestroyImage 是 Xlib 宏（f.destroy_image 间接调用），cgo 无法
// 引用宏，须用 static 包装在 C 侧展开。
// 释放语义：本渲染器的 XImage.data 指向 Go 堆缓冲（r.frameBuf），默认
// destroy_image 会 free(data)——对 Go 指针调 C free 非法。销毁前把 data 置 nil
// （Xlib 约定 data==NULL 时 destroy_image 不释放），Go 内存交还 GC 管理。
static int myXDestroyImage(XImage* img) {
    if (img) img->data = NULL;
    return XDestroyImage(img);
}
*/
import "C"

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Renderer X11 渲染器（Linux）：对照 render.go 的 GDI Renderer 接口集实现，
// BGRA→ZPixmap 32bpp 同字节序直 blit，缩放走 XRender 双线性。
type Renderer struct {
	mu      sync.Mutex
	win     uintptr // X11 window（由 ui_linux.go 创建后注入）
	dpy     uintptr // Display*
	width   int32
	height  int32
	rotated atomic.Bool
	fill    atomic.Bool
	frameCh chan *VideoFrame
	input   InputHandler
	overlay string

	srcW atomic.Int32
	srcH atomic.Int32

	lbGeo   atomic.Pointer[letterboxGeo]
	lbStale atomic.Bool

	captureReq   atomic.Bool
	lastFrame    []byte
	lastFrameW   int32
	lastFrameH   int32
	lastFrameRot bool

	// X11 资源（Attach 时创建，Close/Detach 释放）
	xgc      uintptr // GC
	ximg     uintptr // XImage*
	ximgW    int32
	ximgH    int32
	ximgPix  uintptr // XImage 挂的 Pixmap
	ximgPicH uintptr // XImage 的 Picture

	frameBuf []byte // 当前帧 BGRA 副本（repaintX 用）
	frameW   int32
	frameH   int32
	rotBuf   []byte // 旋转后缓冲
	rotW     int32
	rotH     int32
}

func NewRenderer(win, dpy uintptr, width, height int, rotated, fill bool) (*Renderer, error) {
	r := &Renderer{
		win:     win,
		dpy:     dpy,
		width:   int32(width),
		height:  int32(height),
		frameCh: make(chan *VideoFrame, 2),
	}
	r.rotated.Store(rotated)
	r.fill.Store(fill)
	if win != 0 && dpy != 0 {
		r.attach()
	}
	return r, nil
}

// attach 在 X11 资源就绪后创建 GC（每次帧渲染时按需创建 Picture）。
func (r *Renderer) attach() {
	gc := C.XCreateGC((*C.Display)(unsafe.Pointer(r.dpy)), C.Drawable(r.win), 0, nil)
	if gc != nil {
		r.xgc = uintptr(unsafe.Pointer(gc))
	}
}

// gcH 把 uintptr 存储的 GC 恢复为 cgo 指针（GC 是 *_XGC 指针型：存储时经
// unsafe.Pointer 中转为 uintptr，恢复必须同样经 unsafe.Pointer 回指针，
// 不能像 XID 类型那样直接整数转换）。
func (r *Renderer) gcH() C.GC {
	return C.GC(unsafe.Pointer(r.xgc))
}

func (r *Renderer) SetInputHandler(h InputHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.input = h
}

func (r *Renderer) SetSourceSize(w, h int32) {
	if r.srcW.Load() == w && r.srcH.Load() == h {
		return
	}
	r.mu.Lock()
	r.srcW.Store(w)
	r.srcH.Store(h)
	r.invalidateLetterbox()
	r.mu.Unlock()
}

func (r *Renderer) letterbox() (drawW, drawH, offX, offY int32) {
	if !r.lbStale.Load() {
		if g := r.lbGeo.Load(); g != nil {
			return g.drawW, g.drawH, g.offX, g.offY
		}
	}
	// 受限填满：比例偏差 ≤ 阈值时拉伸填满（与 Windows GDI/D3D11 同一判定）。
	geo := computeLetterbox(r.width, r.height, r.srcW.Load(), r.srcH.Load(), r.rotated.Load(), r.fill.Load())
	r.lbGeo.Store(&geo)
	r.lbStale.Store(false)
	return geo.drawW, geo.drawH, geo.offX, geo.offY
}

func (r *Renderer) invalidateLetterbox() {
	r.lbStale.Store(true)
}

func (r *Renderer) SourceSize() (int32, int32) {
	return r.srcW.Load(), r.srcH.Load()
}

// SendFrame 接收解码帧：BGRA CPU 缓冲拷贝入队（对照 render.go SendFrame）。
func (r *Renderer) SendFrame(frame *VideoFrame) {
	if frame.BGRA.IsTexture {
		return
	}
	r.SetSourceSize(frame.Width, frame.Height)

	for {
		select {
		case old := <-r.frameCh:
			r.mu.Lock()
			r.recycleFrameBuf(old)
			r.mu.Unlock()
			framePool.Put(old)
		default:
			goto enq
		}
	}
enq:

	vf := framePool.Get().(*VideoFrame)
	vf.Width = frame.Width
	vf.Height = frame.Height
	vf.Format = frame.Format
	vf.PTS = frame.PTS

	r.mu.Lock()
	needSize := frame.BGRA.Size
	if buf := r.getRecycleBuf(frame.Width, frame.Height, needSize); buf != nil {
		CopyBGRA(buf, frame.BGRA)
		vf.Data = buf
	} else {
		buf := make([]byte, needSize)
		CopyBGRA(buf, frame.BGRA)
		vf.Data = buf
	}
	r.mu.Unlock()

	select {
	case r.frameCh <- vf:
	default:
		r.mu.Lock()
		r.recycleFrameBuf(vf)
		r.mu.Unlock()
		framePool.Put(vf)
	}
}

func (r *Renderer) recycleFrameBuf(f *VideoFrame) {
	if f.Data == nil {
		return
	}
	if r.frameW == f.Width && r.frameH == f.Height && cap(r.frameBuf) >= cap(f.Data) {
		r.rotBuf = f.Data
		r.rotW = f.Width
		r.rotH = f.Height
		return
	}
	r.rotBuf = f.Data
	r.rotW = f.Width
	r.rotH = f.Height
}

func (r *Renderer) getRecycleBuf(w, h int32, needSize int) []byte {
	if r.rotW == w && r.rotH == h && cap(r.rotBuf) >= needSize {
		buf := r.rotBuf[:needSize]
		r.rotBuf = nil
		r.rotW = 0
		r.rotH = 0
		return buf
	}
	r.rotBuf = nil
	r.rotW = 0
	r.rotH = 0
	return nil
}

func (r *Renderer) SetRotation(rotated bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rotated.Load() != rotated {
		r.rotated.Store(rotated)
		r.invalidateLetterbox()
	}
}

func (r *Renderer) SetOverlay(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overlay = text
}

func (r *Renderer) UpdateSize(w, h int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.width != w || r.height != h {
		r.width = w
		r.height = h
		r.invalidateLetterbox()
	}
}

// Detach 窗口销毁前解绑 X11 资源（与 Close 区分：Close 是进程退出路径）。
func (r *Renderer) Detach() {
	r.mu.Lock()
	r.win = 0
	r.mu.Unlock()
	r.releaseX11()
}

func (r *Renderer) releaseX11() {
	r.mu.Lock()
	defer r.mu.Unlock()
	dpy := (*C.Display)(unsafe.Pointer(r.dpy))
	if r.ximgPicH != 0 && dpy != nil {
		C.XRenderFreePicture(dpy, C.Picture(r.ximgPicH))
		r.ximgPicH = 0
	}
	if r.ximgPix != 0 && dpy != nil {
		C.XFreePixmap(dpy, C.Pixmap(r.ximgPix))
		r.ximgPix = 0
	}
	if r.ximg != 0 && dpy != nil {
		C.myXDestroyImage((*C.XImage)(unsafe.Pointer(r.ximg)))
		r.ximg = 0
	}
	if r.xgc != 0 && dpy != nil {
		C.XFreeGC(dpy, r.gcH())
		r.xgc = 0
	}
	r.frameBuf = nil
	r.frameW = 0
	r.frameH = 0
}

func (r *Renderer) Close() {
	for {
		select {
		case f := <-r.frameCh:
			framePool.Put(f)
		default:
			goto drained
		}
	}
drained:
	r.releaseX11()
	r.lastFrame = nil
}

func (r *Renderer) Size() (int32, int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.width, r.height
}

func (r *Renderer) CaptureFrame() (data []byte, w, h int32, rotated bool) {
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		r.mu.Lock()
		if r.lastFrame != nil {
			data = r.lastFrame
			r.lastFrame = nil
			w, h = r.lastFrameW, r.lastFrameH
			rotated = r.lastFrameRot
			r.mu.Unlock()
			return data, w, h, rotated
		}
		r.mu.Unlock()
		if time.Now().After(deadline) {
			break
		}
		r.captureReq.Store(true)
		time.Sleep(10 * time.Millisecond)
	}
	return nil, 0, 0, false
}

func (r *Renderer) ScaleMouseCoords(mx, my int32) (sx, sy uint16, srcW, srcH int32, ok bool) {
	srcW, srcH = r.srcW.Load(), r.srcH.Load()
	rotated := r.rotated.Load()

	g := r.lbGeo.Load()
	if g == nil {
		return 0, 0, 0, 0, false
	}
	sx, sy, ok = scaleMouseCoords(mx, my, g, srcW, srcH, rotated)
	return
}

func (r *Renderer) LBGeo() *letterboxGeo { return r.lbGeo.Load() }
func (r *Renderer) IsRotated() bool      { return r.rotated.Load() }
