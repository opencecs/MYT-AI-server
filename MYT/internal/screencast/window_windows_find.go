//go:build windows

package screencast

import (
	"log"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// findPlayer 查找 go_legacy.exe（结果缓存，只搜索一次）
func findPlayer() {
	playerInitOnce.Do(func() {
		var candidates []string

		// 主程序所在目录（go_legacy.exe 通过 embed 提取到同目录）
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			candidates = append(candidates, filepath.Join(dir, "go_legacy.exe"))
		}
		if wd, err := os.Getwd(); err == nil {
			candidates = append(candidates, filepath.Join(wd, "bin", "go_legacy.exe"))
		}

		for _, path := range candidates {
			if _, err := os.Stat(path); err == nil {
				log.Printf("[screencast] 找到播放器: %s", path)
				playerPath = path
				playerFound = true
				return
			}
		}
		log.Printf("[screencast] 未找到 go_legacy.exe")
	})
}

// findWindowByPID 通过进程 PID 查找该进程拥有的顶级可见窗口句柄
func findWindowByPID(pid int) uintptr {
	var found uintptr

	cb := windows.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var winPID uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&winPID)))
		if int(winPID) == pid {
			vis, _, _ := procIsWindowVisible.Call(hwnd)
			if vis != 0 {
				found = hwnd
				return 0
			}
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}

func setWindowText(hwnd uintptr, text *uint16) error {
	r, _, err := procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(text)))
	if r == 0 {
		return err
	}
	return nil
}
