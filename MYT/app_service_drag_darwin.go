//go:build !windows

package main

func (s *AppService) StartWindowDrag() {
	// macOS: Wails handles frameless window drag natively
}
