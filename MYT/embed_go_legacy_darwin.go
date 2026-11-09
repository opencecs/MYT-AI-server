//go:build darwin

package main

import (
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

// ExtractGoLegacy 从主程序中提取 go_legacy 到数据目录，返回完整路径
func ExtractGoLegacy() string {
	goLegacyOnce.Do(func() {
		// 优先写到数据目录（~/Library/Application Support/），避免只读位置写入失败
		dir := getDarwinDataDir()
		if dir == "" {
			exe, err := os.Executable()
			if err != nil {
				log.Printf("[go_legacy] 获取可执行文件路径失败: %v", err)
				return
			}
			dir = filepath.Dir(exe)
		}
		target := filepath.Join(dir, "go_legacy")

		log.Printf("[go_legacy] target=%s embeddedSize=%d", target, len(goLegacyBin))

		// 已存在且大小一致则跳过
		if info, err := os.Stat(target); err == nil && int(info.Size()) == len(goLegacyBin) {
			log.Printf("[go_legacy] 已存在且大小一致，跳过提取: %s", target)
			goLegacyPath = target
			goLegacyReady = true
			return
		}

		if err := os.WriteFile(target, goLegacyBin, 0755); err != nil {
			log.Printf("[go_legacy] 写入失败: %v", err)
			return
		}

		log.Printf("[go_legacy] 提取成功: %s (%d bytes)", target, len(goLegacyBin))
		goLegacyPath = target
		goLegacyReady = true
	})
	return goLegacyPath
}
