//go:build windows

package util

import (
	"syscall"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
	procSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	procGetConsoleWindow   = kernel32.NewProc("GetConsoleWindow")
)

func attachConsoleWindows() {
	// 检查是否已存在控制台窗口
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd != 0 {
		// 已有控制台（dev 模式），设置 UTF-8 编码
		procSetConsoleOutputCP.Call(65001)
		procSetConsoleCP.Call(65001)
		return
	}

	// 正式版（-H windowsgui）无控制台，不主动创建
}
