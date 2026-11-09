package updater

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NeedsElevation 检测程序是否安装在需要管理员权限的目录下
func NeedsElevation() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	exeDir := filepath.Dir(exePath)

	for _, pf := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("ProgramW6432"),
	} {
		if pf != "" && strings.HasPrefix(strings.ToLower(exeDir), strings.ToLower(pf)) {
			log.Printf("[updater] 需要 UAC 提权: 程序安装在 %s", exeDir)
			return true
		}
	}
	return false
}

// PrepareElevatedUpdate 将 zip 复制到 ProgramData 公共目录，确保提权进程能访问
func PrepareElevatedUpdate(zipPath string) (string, error) {
	// 创建公共临时目录
	publicDir := filepath.Join(os.Getenv("ProgramData"), "myt-update")
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		return "", fmt.Errorf("创建公共目录失败: %w", err)
	}

	// 复制 zip 到公共目录
	dstZip := filepath.Join(publicDir, "update.zip")
	src, err := os.Open(zipPath)
	if err != nil {
		return "", fmt.Errorf("打开源文件失败: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(dstZip)
	if err != nil {
		return "", fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer dst.Close()

	if _, err := dst.ReadFrom(src); err != nil {
		os.Remove(dstZip)
		return "", fmt.Errorf("复制文件失败: %w", err)
	}

	log.Printf("[updater] zip 已复制到公共目录: %s", dstZip)
	return dstZip, nil
}

// CleanupElevatedUpdate 清理公共目录中的临时文件
func CleanupElevatedUpdate() {
	publicDir := filepath.Join(os.Getenv("ProgramData"), "myt-update")
	os.RemoveAll(publicDir)
}

// StartElevatedUpdate 通过 ShellExecuteW("runas") 启动提权更新进程
func StartElevatedUpdate(zipPath string, parentPID int) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	// 先将 zip 复制到公共目录，提权进程才能访问
	publicZip, err := PrepareElevatedUpdate(zipPath)
	if err != nil {
		return fmt.Errorf("准备提权更新失败: %w", err)
	}

	argStr := fmt.Sprintf("--update --zip \"%s\" --pid %d", publicZip, parentPID)
	log.Printf("[updater] UAC 提权命令: %s %s", exePath, argStr)

	mod := windows.NewLazyDLL("shell32.dll")
	proc := mod.NewProc("ShellExecuteW")

	result, _, err := proc.Call(
		0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("runas"))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(exePath))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(argStr))),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(exeDir))),
		0,
	)

	if result == 0 || (result > 0 && result <= 32) {
		// 用户可能拒绝了 UAC，清理公共目录
		CleanupElevatedUpdate()
		return fmt.Errorf("ShellExecute 提权失败: result=%d, err=%v", result, err)
	}

	log.Printf("[updater] UAC 提权请求已发送")
	return nil
}

// isProcessRunning 使用 Windows OpenProcess + WaitForSingleObject 检测进程是否仍在运行
// 仅用 OpenProcess 不够：进程退出后内核对象仍存在，OpenProcess 会成功返回
// 必须配合 WaitForSingleObject(h, 0) 来判断进程是否真正终止
func isProcessRunning(pid int) bool {
	kernel32 := windows.NewLazyDLL("kernel32.dll")
	openProcess := kernel32.NewProc("OpenProcess")

	handle, _, _ := openProcess.Call(
		uintptr(syscall.SYNCHRONIZE),
		uintptr(0),
		uintptr(pid),
	)
	if handle == 0 {
		return false
	}
	defer kernel32.NewProc("CloseHandle").Call(handle)

	// WaitForSingleObject(h, 0): WAIT_OBJECT_0 表示进程已终止，WAIT_TIMEOUT 表示仍在运行
	waitResult, _, _ := kernel32.NewProc("WaitForSingleObject").Call(
		handle,
		0, // 0 超时，立即返回
	)
	return waitResult != 0 // 0 = WAIT_OBJECT_0 = 已终止
}

// showUpdateError Windows 下弹出 MessageBox 提示更新失败
func showUpdateError(msg string) {
	user32 := windows.NewLazyDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	title, _ := windows.UTF16PtrFromString("魔云腾更新")
	text, _ := windows.UTF16PtrFromString(msg)
	messageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}

// moveFileReplace 使用 Windows MoveFileExW + MOVEFILE_REPLACE_EXISTING 替换文件
// 注意：此函数在目标文件被运行中进程独占占用时也会失败，
// 仅在目标文件未被独占锁定时可作为原子替换方案
func moveFileReplace(src, dst string) error {
	kernel32 := windows.NewLazyDLL("kernel32.dll")
	moveFileEx := kernel32.NewProc("MoveFileExW")

	const (
		MOVEFILE_REPLACE_EXISTING = 0x00000001
	)

	srcPtr, _ := windows.UTF16PtrFromString(src)
	dstPtr, _ := windows.UTF16PtrFromString(dst)

	ret, _, err := moveFileEx.Call(
		uintptr(unsafe.Pointer(srcPtr)),
		uintptr(unsafe.Pointer(dstPtr)),
		uintptr(MOVEFILE_REPLACE_EXISTING),
	)

	if ret == 0 {
		return fmt.Errorf("MoveFileExW 失败: %w", err)
	}

	log.Printf("[updater] MoveFileExW 替换成功: %s → %s", src, dst)
	return nil
}
