//go:build linux

package updater

import (
	"fmt"
	"log"
	"os"
	"syscall"
)

// NeedsElevation Linux 下一般不需要提权
func NeedsElevation() bool {
	return false
}

// StartElevatedUpdate Linux 下不需要 UAC 提权
func StartElevatedUpdate(zipPath string, parentPID int) error {
	_ = zipPath
	_ = parentPID
	return nil
}

// isProcessRunning Linux 使用 os.FindProcess + Signal(0) 检测
func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// showUpdateError Linux 下写 stderr
func showUpdateError(msg string) {
	fmt.Fprintf(os.Stderr, "[updater] %s\n", msg)
}

// moveFileReplace 使用 os.Rename 替换目标文件（POSIX 原子操作）
func moveFileReplace(src, dst string) error {
	log.Printf("[updater] moveFileReplace: %s -> %s", src, dst)
	return os.Rename(src, dst)
}

// PrepareElevatedUpdate Linux 不需要提权准备，直接返回 zipPath
func PrepareElevatedUpdate(zipPath string) (string, error) {
	return zipPath, nil
}

// CleanupElevatedUpdate Linux 无需清理
func CleanupElevatedUpdate() {}
