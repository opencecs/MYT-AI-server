//go:build linux

package main

import (
	"crypto/sha256"
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"sync"
)

//go:embed embedded/go_legacy
var goLegacyBin []byte

var (
	goLegacyOnce  sync.Once
	goLegacyPath  string
	goLegacyReady bool
)

// ExtractGoLegacy 从主程序中提取 go_legacy 到 exe 同目录，返回完整路径。
// 提取失败时返回空串（调用方 main.go 仅告警不 panic；findPlayer 找不到播放器
// 时 NewProjectionWindow 返回错误，投屏不可用但主程序正常启动）。
func ExtractGoLegacy() string {
	goLegacyOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			log.Printf("[go_legacy] 获取可执行文件路径失败: %v", err)
			return
		}
		dir := filepath.Dir(exe)
		target := filepath.Join(dir, "go_legacy")

		log.Printf("[go_legacy] target=%s embeddedSize=%d", target, len(goLegacyBin))

		// 已存在且内容一致则跳过。不能只比较大小，避免新版 player 大小相同但内容不同。
		if data, err := os.ReadFile(target); err == nil && sha256.Sum256(data) == sha256.Sum256(goLegacyBin) {
			goLegacyPath = target
			goLegacyReady = true
			return
		}

		if err := os.WriteFile(target, goLegacyBin, 0o755); err != nil {
			log.Printf("[go_legacy] 提取写入失败: %v", err)
			return
		}

		log.Printf("[go_legacy] 提取成功: %s (%d bytes)", target, len(goLegacyBin))
		goLegacyPath = target
		goLegacyReady = true
	})
	return goLegacyPath
}
