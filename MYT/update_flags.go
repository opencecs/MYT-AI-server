package main

import (
	"os"
	"strconv"
)

// parseUpdateFlags 解析 --update --zip xxx --pid N 命令行参数
func parseUpdateFlags() (zipPath string, parentPID int) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--zip" && i+1 < len(args) {
			zipPath = args[i+1]
		}
		if args[i] == "--pid" && i+1 < len(args) {
			parentPID, _ = strconv.Atoi(args[i+1])
		}
	}
	return
}

// isUpdateMode 检查是否为 --update 模式
func isUpdateMode() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--update" {
			return true
		}
	}
	return false
}

// isRestartFromUpdate 检查是否从更新后重启
func isRestartFromUpdate() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--restart-from-update" {
			return true
		}
	}
	return false
}

// cleanInternalArgs 从 os.Args 中移除内部更新参数，避免传给 Wails
func cleanInternalArgs() {
	filtered := make([]string, 0, len(os.Args))
	filtered = append(filtered, os.Args[0])
	for _, arg := range os.Args[1:] {
		if arg != "--restart-from-update" {
			filtered = append(filtered, arg)
		}
	}
	os.Args = filtered
}
