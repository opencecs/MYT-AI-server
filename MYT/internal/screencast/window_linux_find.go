//go:build linux

package screencast

import (
	"log"
	"os"
	"path/filepath"
)

// findPlayer 查找可用的投屏播放器 go_legacy（结果缓存，只搜索一次；Linux 只用
// go_legacy，无 C++ player 形态）。候选顺序与 darwin 版一致：ExtractGoLegacy
// 提取目录（主程序 exe 同目录）→ 工作目录 bin/。
func findPlayer() {
	playerInitOnce.Do(func() {
		var candidates []string

		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			log.Printf("[screencast] 搜索主程序同目录: exe=%s dir=%s", exe, dir)
			candidates = append(candidates, filepath.Join(dir, "go_legacy"))
		}
		if wd, err := os.Getwd(); err == nil {
			candidates = append(candidates, filepath.Join(wd, "bin", "go_legacy"))
		}

		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				log.Printf("[screencast] 找到播放器: %s", c)
				playerPath = c
				playerFound = true
				return
			}
		}
		log.Printf("[screencast] 所有候选路径均未找到投屏播放器 (go_legacy)")
	})
}
