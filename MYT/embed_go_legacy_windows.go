//go:build windows

package main

import (
	"crypto/sha256"
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed embedded/go_legacy.exe
var goLegacyExe []byte

var (
	goLegacyOnce  sync.Once
	goLegacyPath  string
	goLegacyReady bool
)

// killProcessesByPath 结束所有可执行文件路径等于 target 的进程。
// 用于解除旧 go_legacy.exe 孤儿进程对目标文件的占用（占用会导致提取静默失败、投屏跑旧版本）。
func killProcessesByPath(target string) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return
	}
	target = strings.ToLower(filepath.Clean(target))
	for {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "go_legacy.exe") {
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, entry.ProcessID)
			if err == nil {
				var buf [windows.MAX_PATH]uint16
				size := uint32(len(buf))
				if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err == nil {
					procPath := strings.ToLower(filepath.Clean(windows.UTF16ToString(buf[:size])))
					if procPath == target {
						if err := windows.TerminateProcess(h, 0); err == nil {
							log.Printf("[go_legacy] 已结束占用文件的旧进程 PID=%d", entry.ProcessID)
						}
					}
				}
				windows.CloseHandle(h)
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
}

// ExtractGoLegacy 从主程序中提取 go_legacy.exe 到数据目录，返回完整路径
func ExtractGoLegacy() string {
	goLegacyOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		dir := filepath.Dir(exe)

		target := filepath.Join(dir, "go_legacy.exe")

		// 已存在且内容一致则跳过。不能只比较大小，避免新版 player 大小相同但内容不同。
		if data, err := os.ReadFile(target); err == nil && sha256.Sum256(data) == sha256.Sum256(goLegacyExe) {
			goLegacyPath = target
			goLegacyReady = true
			return
		}

		// 覆盖前先结束仍在运行的旧 go_legacy 进程（旧进程会锁住目标文件导致写入失败）
		killProcessesByPath(target)

		var writeErr error
		for attempt := 1; attempt <= 3; attempt++ {
			if writeErr = os.WriteFile(target, goLegacyExe, 0755); writeErr == nil {
				break
			}
			log.Printf("[go_legacy] 提取写入失败(第%d/3次): %v", attempt, writeErr)
			time.Sleep(300 * time.Millisecond)
		}
		if writeErr != nil {
			// 不再静默失败：记录错误，调用方拿到空路径后应报错而非回退到旧 exe
			log.Printf("[go_legacy] 提取最终失败: %v", writeErr)
			return
		}

		log.Printf("[go_legacy] 提取成功: %s (%d bytes)", target, len(goLegacyExe))
		goLegacyPath = target
		goLegacyReady = true
	})
	return goLegacyPath
}
