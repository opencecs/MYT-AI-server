//go:build linux

package main

// render_linux_paint.go — X11 渲染器绘制路径（对照 render_paint.go GDI 实现）：
// RenderFrame（UI 线程事件循环驱动）→ renderFrame / repaintX / renderIdle。
// BGRA → XImage ZPixmap 32bpp（小端字节序字节布局同为 B,G,R,A，零转换），
// 缩放经 XRender 双线性（等价 StretchBlt），letterbox 黑边先填再 blit。

/*
#cgo pkg-config: x11 xrender

#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/extensions/Xrender.h>
#include <string.h>

// createFrameImage 用给定 BGRA 缓冲创建 XImage（不拷贝，数据就地读取）。
static XImage* createFrameImage(Display* dpy, unsigned char* data, int w, int h) {
    Visual* vis = DefaultVisual(dpy, DefaultScreen(dpy));
    XImage* img = XCreateImage(dpy, vis, 24, ZPixmap, 0, (char*)data,
                               (unsigned int)w, (unsigned int)h, 32, w * 4);
    return img;
}

// myXDestroyImage XDestroyImage 是 Xlib 宏（f.destroy_image 间接调用），cgo 无法
// 引用宏，须用 static 包装在 C 侧展开。
// 释放语义：本渲染器的 XImage.data 指向 Go 堆缓冲（r.frameBuf），默认
// destroy_image 会 free(data)——对 Go 指针调 C free 非法。销毁前把 data 置 nil
// （Xlib 约定 data==NULL 时 destroy_image 不释放），Go 内存交还 GC 管理。
static int myXDestroyImage(XImage* img) {
    if (img) img->data = NULL;
    return XDestroyImage(img);
}

// myDefaultScreen/myDefaultVisual 同为 Xlib 宏，cgo 无法直接引用，包装展开。
static int myDefaultScreen(Display* dpy) { return DefaultScreen(dpy); }
static Visual* myDefaultVisual(Display* dpy, int scr) { return DefaultVisual(dpy, scr); }

// scaleXRender 把 srcPic（w×h）经 XRender 双线性缩放绘制到 dst drawable
// 的 (dx,dy,dw,dh) 区域（等价 Windows StretchBlt 双线性语义）。
static void scaleXRender(Display* dpy, Drawable dst, GC gc,
                         Picture srcPic, int w, int h,
                         int dx, int dy, int dw, int dh) {
    XRenderPictFormat* fmt = XRenderFindVisualFormat(dpy, DefaultVisual(dpy, DefaultScreen(dpy)));
    if (!fmt) return;
    Picture dstPic = XRenderCreatePicture(dpy, dst, fmt, 0, NULL);
    if (!dstPic) return;
    XRenderSetPictureFilter(dpy, srcPic, FilterBilinear, NULL, 0);
    XTransform xform;
    memset(&xform, 0, sizeof(xform));
    xform.matrix[0][0] = (w << 16) / dw;
    xform.matrix[1][1] = (h << 16) / dh;
    xform.matrix[2][2] = 1 << 16;
    XRenderSetPictureTransform(dpy, srcPic, &xform);
    XRenderComposite(dpy, PictOpSrc, srcPic, None, dstPic,
                     0, 0, 0, 0, dx, dy, dw, dh);
    XRenderFreePicture(dpy, dstPic);
}
*/
import "C"

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// renderTick 渲染节拍观测：每 120 次 RenderFrame 打一条低频日志（约 2s 一条），
// 用于确认渲染循环活着（停摆时该日志停=渲染侧问题，还在打=渲染侧正常）。
var renderTick atomic.Int32

// paintDiagOnce 首次成功绘制参数一次性日志：win/pixmap/源帧/dst 矩形，
// 供真机核对绘制目标与几何（黑屏排查：参数异常即断点所在）。
var paintDiagOnce atomic.Bool

// logPaintDiag 首次绘制时打一条参数日志（持锁调用）。
func (r *Renderer) logPaintDiag(srcW, srcH, offX, offY, drawW, drawH int32) {
	if !paintDiagOnce.CompareAndSwap(false, true) {
		return
	}
	logf("[render] first paint: win=0x%x pixm=0x%x pic=0x%x img=%dx%d src=%dx%d draw=(%d,%d %dx%d) winSize=%dx%d rot=%v\n",
		r.win, r.ximgPix, r.ximgPicH, r.ximgW, r.ximgH, srcW, srcH,
		offX, offY, drawW, drawH, r.width, r.height, r.rotated.Load())
}

// RenderFrame UI 线程调用：取最新帧渲染（对照 render_paint.go RenderFrame）。
func (r *Renderer) RenderFrame() {
	var newest *VideoFrame
	drained := 0
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
			drained++
		default:
			goto done
		}
	}
done:
	if newest != nil {
		if drained > 1 {
			logf("[render] drained %d frames from queue\n", drained)
		}
		if c := renderTick.Add(1); c%120 == 1 {
			// pts：帧时间戳（持续变化=解码在产出新帧）；smp：frameBuf 首 8 字节
			// 十六进制（内容抽样，静止画面恒定、动态画面变化）。
			r.mu.Lock()
			smp := ""
			if len(r.frameBuf) >= 8 {
				smp = fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x",
					r.frameBuf[0], r.frameBuf[1], r.frameBuf[2], r.frameBuf[3],
					r.frameBuf[4], r.frameBuf[5], r.frameBuf[6], r.frameBuf[7])
			}
			r.mu.Unlock()
			logf("[render] tick #%d %dx%d rot=%v q=%d pts=%v smp=%s\n",
				c, newest.Width, newest.Height, r.rotated.Load(), len(r.frameCh), newest.PTS, smp)
		}
		r.renderFrame(newest)
		r.mu.Lock()
		r.recycleFrameBuf(newest)
		r.mu.Unlock()
		framePool.Put(newest)
	} else if r.srcW.Load() > 0 && r.srcH.Load() > 0 {
		r.repaintX()
	} else if r.srcW.Load() == 0 && r.srcH.Load() == 0 {
		r.renderIdle()
	}
}

// Present 事件循环节拍兜底（与 MetalRenderer.Present 同名同语义）：无新帧时重绘。
func (r *Renderer) Present() {
	if r.srcW.Load() > 0 {
		r.repaintX()
	}
}

// renderIdle 无源尺寸时整窗涂黑（对照 GDI renderIdle）。
func (r *Renderer) renderIdle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fillBlack()
}

// fillBlack 持锁状态下整窗涂黑（GC foreground 需为黑）。
func (r *Renderer) fillBlack() {
	if r.win == 0 || r.dpy == 0 || r.xgc == 0 {
		return
	}
	dpy := (*C.Display)(unsafe.Pointer(r.dpy))
	C.XSetForeground(dpy, r.gcH(), 0)
	g := r.lbGeo.Load()
	if g != nil && g.winW > 0 && g.winH > 0 {
		C.XFillRectangle(dpy, C.Drawable(r.win), r.gcH(), 0, 0,
			C.uint(g.winW), C.uint(g.winH))
	} else {
		C.XFillRectangle(dpy, C.Drawable(r.win), r.gcH(), 0, 0,
			C.uint(r.width), C.uint(r.height))
	}
	C.XFlush(dpy)
}

// repaintX 无新帧但窗口尺寸变化时，重画已持有帧（对照 repaintDIB）。
func (r *Renderer) repaintX() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.win == 0 || r.dpy == 0 || r.frameBuf == nil || r.frameW <= 0 || r.frameH <= 0 {
		return
	}
	dpy := (*C.Display)(unsafe.Pointer(r.dpy))

	winW, winH := r.width, r.height
	drawW, drawH, offX, offY := r.letterbox()

	r.paintLetterboxBarsLocked(dpy, drawW, drawH, offX, offY, winW, winH)

	srcPic := r.ximgPicH
	if srcPic == 0 {
		return
	}
	C.scaleXRender(dpy, C.Drawable(r.win), r.gcH(), C.Picture(srcPic), C.int(r.frameW), C.int(r.frameH), C.int(offX), C.int(offY), C.int(drawW), C.int(drawH))
	C.XFlush(dpy)
}

// renderFrame 渲染一帧（对照 GDI renderFrame）。
func (r *Renderer) renderFrame(frame *VideoFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.win == 0 || r.dpy == 0 {
		return
	}
	dpy := (*C.Display)(unsafe.Pointer(r.dpy))

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

	srcW, srcH := frame.Width, frame.Height
	var pix []byte
	var pixW, pixH int32
	if rotated {
		rotW, rotH := srcH, srcW
		need := int(rotW) * int(rotH) * 4
		if cap(r.rotBuf) < need {
			r.rotBuf = make([]byte, need)
		}
		rotateBGRA90CW(frame.Data, r.rotBuf[:need], srcW, srcH)
		pix, pixW, pixH = r.rotBuf[:need], rotW, rotH
	} else {
		pix, pixW, pixH = frame.Data, srcW, srcH
	}

	// 持久化一份当前帧（repaintX 用；对照 GDI 的 DIB 常驻）
	r.frameBuf = append(r.frameBuf[:0], pix...)
	r.frameW = pixW
	r.frameH = pixH

	winW, winH := r.width, r.height
	drawW, drawH, offX, offY := r.letterbox()

	r.paintLetterboxBarsLocked(dpy, drawW, drawH, offX, offY, winW, winH)

	srcPic := r.ensurePictureLocked(dpy, r.frameBuf, pixW, pixH)
	if srcPic == 0 {
		return
	}
	r.logPaintDiag(pixW, pixH, offX, offY, drawW, drawH)
	C.scaleXRender(dpy, C.Drawable(r.win), r.gcH(), C.Picture(srcPic), C.int(pixW), C.int(pixH), C.int(offX), C.int(offY), C.int(drawW), C.int(drawH))
	C.XFlush(dpy)
}

// ensurePictureLocked 把 BGRA 缓冲变成 XRender Picture（XImage→Pixmap→Picture）。
// 分辨率变化时重建；同尺寸时 XImage data 原地换指针再重新挂 Pixmap。
func (r *Renderer) ensurePictureLocked(dpy *C.Display, pix []byte, w, h int32) uintptr {
	if r.ximg != 0 && r.ximgW == w && r.ximgH == h {
		// 同尺寸：XImage data 原地换成新 Go 缓冲。旧缓冲归 Go GC 管理
		// （销毁时 myXDestroyImage 会先把 data 置 nil，绝不 free Go 指针）。
		img := (*C.XImage)(unsafe.Pointer(r.ximg))
		img.data = (*C.char)(unsafe.Pointer(&pix[0]))
		// data 换指针后必须重新上传：XPutImage 把 XImage 数据拷贝进 Pixmap，
		// 后续 XRender composite 读的是 Pixmap 而非 XImage——缺这一步则
		// Pixmap 内容永远停在首次上传的帧。
		C.XPutImage(dpy, C.Drawable(r.ximgPix), r.gcH(), img, 0, 0, 0, 0, C.uint(w), C.uint(h))
	} else {
		r.destroyPictureLocked(dpy)
		img := C.createFrameImage(dpy, (*C.uchar)(unsafe.Pointer(&pix[0])), C.int(w), C.int(h))
		if img == nil {
			return 0
		}
		r.ximg = uintptr(unsafe.Pointer(img))
		r.ximgW = w
		r.ximgH = h
	}
	img := (*C.XImage)(unsafe.Pointer(r.ximg))
	if r.ximgPix == 0 {
		pixm := C.XCreatePixmap(dpy, C.Drawable(r.win), C.uint(w), C.uint(h), 24)
		if pixm == 0 {
			return 0
		}
		r.ximgPix = uintptr(pixm)
		C.XPutImage(dpy, C.Drawable(r.ximgPix), r.gcH(), img, 0, 0, 0, 0, C.uint(w), C.uint(h))
		fmt := C.XRenderFindVisualFormat(dpy, C.myDefaultVisual(dpy, C.myDefaultScreen(dpy)))
		if fmt == nil {
			return 0
		}
		if r.ximgPicH != 0 {
			C.XRenderFreePicture(dpy, C.Picture(r.ximgPicH))
		}
		r.ximgPicH = uintptr(C.XRenderCreatePicture(dpy, C.Drawable(r.ximgPix), fmt, 0, nil))
	}
	return r.ximgPicH
}

func (r *Renderer) destroyPictureLocked(dpy *C.Display) {
	if r.ximgPicH != 0 {
		C.XRenderFreePicture(dpy, C.Picture(r.ximgPicH))
		r.ximgPicH = 0
	}
	if r.ximgPix != 0 {
		C.XFreePixmap(dpy, C.Pixmap(r.ximgPix))
		r.ximgPix = 0
	}
	if r.ximg != 0 {
		C.myXDestroyImage((*C.XImage)(unsafe.Pointer(r.ximg)))
		r.ximg = 0
	}
}

// paintLetterboxBarsLocked 涂黑 letterbox 黑边（持锁调用，对照 GDI paintLetterboxBars）。
func (r *Renderer) paintLetterboxBarsLocked(dpy *C.Display, drawW, drawH, offX, offY, winW, winH int32) {
	if r.xgc == 0 || r.win == 0 {
		return
	}
	if offX == 0 && offY == 0 && offX+drawW == winW && offY+drawH == winH {
		return
	}
	gc := r.gcH()
	drawable := C.Drawable(r.win)
	C.XSetForeground(dpy, gc, 0)
	if offY > 0 {
		C.XFillRectangle(dpy, drawable, gc, 0, 0, C.uint(winW), C.uint(offY))
	}
	if offY+drawH < winH {
		C.XFillRectangle(dpy, drawable, gc, 0, C.int(offY+drawH), C.uint(winW), C.uint(winH-offY-drawH))
	}
	if offX > 0 {
		C.XFillRectangle(dpy, drawable, gc, 0, C.int(offY), C.uint(offX), C.uint(drawH))
	}
	if offX+drawW < winW {
		C.XFillRectangle(dpy, drawable, gc, C.int(offX+drawW), C.int(offY), C.uint(winW-offX-drawW), C.uint(drawH))
	}
}

// rotateBGRA90CW 像素旋转 90°顺时针（tile 优化与 Windows 版一致）。
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
