//go:build !windows

package main

import "os/exec"

// configureHiddenCmd 非 Windows 平台空实现
func configureHiddenCmd(cmd *exec.Cmd) {}
