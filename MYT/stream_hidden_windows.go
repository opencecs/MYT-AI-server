//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// configureHiddenCmd 隐藏子进程窗口（Windows）
func configureHiddenCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}
