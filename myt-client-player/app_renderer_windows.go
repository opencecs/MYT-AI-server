//go:build windows

package main

import "sync/atomic"

// rendererFields is embedded in App on Windows.
type rendererFields struct {
	Renderer atomic.Pointer[Renderer]
	D3D11Ren atomic.Pointer[D3D11Renderer]
}

// closeRenderer closes the Windows renderers.
func (a *App) closeRenderer() {
	if d3d := a.D3D11Ren.Swap(nil); d3d != nil {
		d3d.Close()
	}
	if ren := a.Renderer.Swap(nil); ren != nil {
		ren.Close()
	}
}

// setRendererRotation updates rotation on the active renderer.
func (a *App) setRendererRotation(rotated bool) {
	if d3d := a.D3D11Ren.Load(); d3d != nil {
		d3d.SetRotation(rotated)
	}
	if ren := a.Renderer.Load(); ren != nil {
		ren.SetRotation(rotated)
	}
}

// captureRendererFrame captures a frame from the active renderer.
func (a *App) captureRendererFrame() (data []byte, w, h int32, rotated bool) {
	if d3d := a.D3D11Ren.Load(); d3d != nil {
		return d3d.CaptureFrame()
	}
	if ren := a.Renderer.Load(); ren != nil {
		return ren.CaptureFrame()
	}
	return nil, 0, 0, false
}


func (a *App) activeRenderer() frameRenderer {
	if d3d := a.D3D11Ren.Load(); d3d != nil {
		return d3d
	}
	if ren := a.Renderer.Load(); ren != nil {
		return ren
	}
	return nil
}

func (a *App) activeScaler() inputScaler {
	if d3d := a.D3D11Ren.Load(); d3d != nil {
		return d3d
	}
	if ren := a.Renderer.Load(); ren != nil {
		return ren
	}
	return nil
}
