//go:build windows

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type Renderer struct {
	mu      sync.Mutex
	hwnd    uintptr
	width   int32
	height  int32
	rotated atomic.Bool
	fill    atomic.Bool // stretch 填满（跳过 letterbox 比例，轻微变形）
	frameCh chan *VideoFrame
	input   InputHandler
	overlay string

	srcW atomic.Int32
	srcH atomic.Int32

	lbGeo   atomic.Pointer[letterboxGeo]
	lbStale atomic.Bool

	hBlackBrush uintptr

	captureReq   atomic.Bool
	lastFrame    []byte
	lastFrameW   int32
	lastFrameH   int32
	lastFrameRot bool

	recycleBuf []byte
	recycleW   int32
	recycleH   int32

	hBitmap uintptr
	pBits   unsafe.Pointer
	bmpW    int32
	bmpH    int32
	hMemDC  uintptr
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func NewRenderer(hwnd uintptr, width, height int, rotated, fill bool) (*Renderer, error) {
	r := &Renderer{
		hwnd:    hwnd,
		width:   int32(width),
		height:  int32(height),
		frameCh: make(chan *VideoFrame, 2),
	}
	r.rotated.Store(rotated)
	r.fill.Store(fill)
	r.hBlackBrush, _, _ = procCreateSolidBrush.Call(0x00000000)
	return r, nil
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

	// 受限填满：比例偏差 ≤ 阈值时拉伸填满（不出黑边），偏差大走 letterbox。
	// 判定纯几何，随尺寸/源变化重算，无状态；与 D3D11 路径共用
	// computeLetterbox 内同一判定，鼠标映射与 GDI 绘制天然一致。
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

func (r *Renderer) SendFrame(frame *VideoFrame) {
	// GDI 渲染器无法处理 GPU 纹理帧（解码器应已降级 BGRA 输出；此处兑底防御，
	// 避免把纹理指针当 BGRA 内存拷贝出黑帧/垃圾画面）。
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
	if r.recycleW == f.Width && r.recycleH == f.Height && cap(r.recycleBuf) >= cap(f.Data) {
		return
	}
	r.recycleBuf = f.Data
	r.recycleW = f.Width
	r.recycleH = f.Height
}

func (r *Renderer) getRecycleBuf(w, h int32, needSize int) []byte {
	if r.recycleW == w && r.recycleH == h && cap(r.recycleBuf) >= needSize {
		buf := r.recycleBuf[:needSize]
		r.recycleBuf = nil
		r.recycleW = 0
		r.recycleH = 0
		return buf
	}
	r.recycleBuf = nil
	r.recycleW = 0
	r.recycleH = 0
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

func (r *Renderer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for {
		select {
		case f := <-r.frameCh:
			framePool.Put(f)
		default:
			goto drained
		}
	}
drained:

	if r.hBitmap != 0 {
		procDeleteObject.Call(r.hBitmap)
		r.hBitmap = 0
		r.pBits = nil
	}
	if r.hMemDC != 0 {
		procDeleteDC.Call(r.hMemDC)
		r.hMemDC = 0
	}
	r.recycleBuf = nil
	r.recycleW = 0
	r.recycleH = 0
	r.lastFrame = nil
	if r.hBlackBrush != 0 {
		procDeleteObject.Call(r.hBlackBrush)
		r.hBlackBrush = 0
	}
}

func (r *Renderer) Size() (int32, int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.width, r.height
}

func (r *Renderer) CaptureFrame() (data []byte, w, h int32, rotated bool) {
	// 与 D3D11 路径（d3d11_frame.go CaptureFrame）同款 200ms 等待循环：
	// 置 captureReq → UI 线程 renderFrame 消费并拷贝 lastFrame → 轮询取走。
	// 软解渲染帧空窗（首帧未到/瞬时无帧）时短暂等待即可取到，不再立即返 nil。
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
