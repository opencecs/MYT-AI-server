//go:build linux

package main

import "sync/atomic"

// rendererFields is embedded in App on Linux.
// 字段构成依据：
//   - X11Ren：Linux 渲染器唯一实现（render_linux.go 的 X11 Renderer，UI 层创建后 Store）。
//   - D3D11Ren：恒 nil 的桩字段。app.go initDecoder（平台无关）按
//     `a.D3D11Ren.Load() == nil → 解码器输出 BGRA` 分支走软解路径，与 darwin 版
//     d3d11_renderer_darwin.go 恒 nil 桩模式一致（control_server.go 亦引用该字段判
//     截图错误文案，Linux 纯软解恒走"渲染帧空窗"分支，语义正确）。
//     类型断言桩 D3D11Renderer 见下方，app_connect_cb.go 的断言不会命中，自动落
//     CPU BGRA 路径；方法集需覆盖 app.go / app_connect_cb.go 平台无关分支引用的
//     IsWarp/DeviceHandle/CanRenderNV12/TexBroken（编译期方法集检查要求）。
type rendererFields struct {
	X11Ren   atomic.Pointer[Renderer]
	D3D11Ren atomic.Pointer[D3D11Renderer]
}

// closeRenderer closes the X11 renderer.
func (a *App) closeRenderer() {
	if r := a.X11Ren.Swap(nil); r != nil {
		r.Close()
	}
}

// setRendererRotation updates rotation on the X11 renderer.
func (a *App) setRendererRotation(rotated bool) {
	if r := a.X11Ren.Load(); r != nil {
		r.SetRotation(rotated)
	}
}

// captureRendererFrame captures a frame from the X11 renderer.
func (a *App) captureRendererFrame() (data []byte, w, h int32, rotated bool) {
	if r := a.X11Ren.Load(); r != nil {
		return r.CaptureFrame()
	}
	return nil, 0, 0, false
}

func (a *App) activeRenderer() frameRenderer {
	if r := a.X11Ren.Load(); r != nil {
		return r
	}
	return nil
}

func (a *App) activeScaler() inputScaler {
	if r := a.X11Ren.Load(); r != nil {
		return r
	}
	return nil
}

// D3D11Renderer is a stub on Linux (对照 d3d11_renderer_darwin.go)：无 D3D11，
// app_connect_cb.go 的类型断言不会命中，落 CPU BGRA 路径。
type D3D11Renderer struct{}

func (r *D3D11Renderer) SendFrame(frame *VideoFrame) {}
func (r *D3D11Renderer) SetOverlay(text string)      {}

// app.go / app_connect_cb.go 平台无关分支引用的方法（恒 nil 桩不会被调用，
// 为满足编译期方法集检查而存在）。
func (r *D3D11Renderer) IsWarp() bool          { return false }
func (r *D3D11Renderer) DeviceHandle() uintptr { return 0 }
func (r *D3D11Renderer) CanRenderNV12() bool   { return false }
func (r *D3D11Renderer) TexBroken() bool       { return false }

// UnrefFrame Linux 无纹理帧路径，no-op（app_connect_cb.go 平台无关调用）。
func (d *H264Decoder) UnrefFrame(frame uintptr) {}
