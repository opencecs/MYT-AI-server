//go:build darwin

package main

// D3D11Renderer is a stub on macOS; the type assertion in app_connect_cb.go
// will simply not match, falling through to the CPU BGRA path.
type D3D11Renderer struct{}

func (r *D3D11Renderer) SendFrame(frame *VideoFrame) {}
func (r *D3D11Renderer) SetOverlay(text string)      {}
