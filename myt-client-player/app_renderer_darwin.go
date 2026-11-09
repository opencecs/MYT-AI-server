//go:build darwin

package main

import "sync/atomic"

// rendererFields is embedded in App on macOS.
type rendererFields struct {
	MetalRen atomic.Pointer[MetalRenderer]
}

// closeRenderer closes the Metal renderer.
func (a *App) closeRenderer() {
	if m := a.MetalRen.Swap(nil); m != nil {
		m.Close()
	}
}

// setRendererRotation updates rotation on the Metal renderer.
func (a *App) setRendererRotation(rotated bool) {
	if m := a.MetalRen.Load(); m != nil {
		m.SetRotation(rotated)
	}
}

// captureRendererFrame captures a frame from the Metal renderer.
func (a *App) captureRendererFrame() (data []byte, w, h int32, rotated bool) {
	if m := a.MetalRen.Load(); m != nil {
		return m.CaptureFrame()
	}
	return nil, 0, 0, false
}
func (a *App) activeRenderer() frameRenderer {
	if m := a.MetalRen.Load(); m != nil {
		return m
	}
	return nil
}

func (a *App) activeScaler() inputScaler {
	if m := a.MetalRen.Load(); m != nil {
		return m
	}
	return nil
}
