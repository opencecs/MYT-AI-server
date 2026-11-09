package util

import (
	"fmt"
	"os"
	"sync"
)

// RotatingWriter 按大小轮转的日志写入器：写满 maxBytes 就把当前文件滚成 .1、
// 原 .1 滚成 .2 ……最多保留 backups 份，总占用上限 ≈ maxBytes × (backups+1)。
//
// 不引 lumberjack 的原因：它不在 vendor 里，为「按大小轮转」这一件事引入外部依赖
// 还要同步维护 vendor 补丁（见 AGENTS.md 的 vendor 纪律）不划算。
//
// 安全用于 log.SetOutput：Write 内部加锁，log 包自身也有锁，不会交错写坏。
type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	backups  int
	file     *os.File
	size     int64
}

// NewRotatingWriter 创建轮转写入器。maxBytes<=0 表示不轮转（仅追加）；
// backups<=0 表示轮转时直接截断重开，不保留历史。
func NewRotatingWriter(path string, maxBytes int64, backups int) *RotatingWriter {
	return &RotatingWriter{path: path, maxBytes: maxBytes, backups: backups}
}

// Open 预打开底层文件，供调用方在切换 log 输出前探测可写性。
func (w *RotatingWriter) Open() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return nil
	}
	return w.open()
}

// Close 关闭底层文件（进程退出或停用日志时调用）。
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	return nil
}

// rotate 关闭当前文件并滚动备份：path → path.1 → path.2 …
//
// 每步都先 Remove 目标再 Rename——Windows 的 os.Rename 目标已存在时会失败
// （Unix 才静默覆盖），不先删会导致轮转在 Windows 上整体失败。
func (w *RotatingWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if w.backups > 0 {
		_ = os.Remove(backupPath(w.path, w.backups))
		for i := w.backups - 1; i >= 1; i-- {
			from := backupPath(w.path, i)
			if _, err := os.Stat(from); err != nil {
				continue
			}
			to := backupPath(w.path, i+1)
			_ = os.Remove(to)
			if err := os.Rename(from, to); err != nil {
				return err
			}
		}
		if _, err := os.Stat(w.path); err == nil {
			_ = os.Remove(backupPath(w.path, 1))
			if err := os.Rename(w.path, backupPath(w.path, 1)); err != nil {
				return err
			}
		}
	}
	return w.open()
}

func backupPath(path string, i int) string {
	return fmt.Sprintf("%s.%d", path, i)
}
