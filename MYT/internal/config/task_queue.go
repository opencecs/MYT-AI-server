package config

import (
	"path/filepath"
)

// TaskQueuePath 任务列表文件路径（供 taskqueue 包持久化使用）
func (m *Manager) TaskQueuePath() string {
	return filepath.Join(m.ConfigDir(), "task_queue.json")
}
