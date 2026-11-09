//go:build windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// StartWindowDrag 发起窗口拖动（绕过 Wails 内置的 ReleaseCapture 检查）
func (s *AppService) StartWindowDrag() {
	if s.app == nil {
		return
	}
	win := s.app.Window.Current()
	if win == nil {
		return
	}
	hwnd := w32.HWND(uintptr(win.NativeWindow()))
	if hwnd == 0 {
		return
	}
	w32.ReleaseCapture()
	w32.PostMessage(hwnd, w32.WM_NCLBUTTONDOWN, w32.HTCAPTION, 0)
}
