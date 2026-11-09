//go:build linux

package main

import (
	"os"
	"path/filepath"
)

// getDarwinDataDir Linux 版（函数名保持与 main_windows.go / main_darwin.go 一致，
// 不改调用方）：返回 XDG 数据目录 ~/.local/share/myt（对应 macOS 的
// ~/Library/Application Support/com.moyunteng.v3ai）。调用方（getRootDir /
// rtmp_server.go）已对空串容错（回退 exe 同目录）。
func getDarwinDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".local", "share", "myt")
	os.MkdirAll(dir, 0755)
	return dir
}
