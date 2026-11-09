package eventws

import (
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// TestStopAllKeepsServiceUsable StopAll 全停后不置 stopped：
// 后续 Sync 仍可正常建连（模式切到 v2 全停 local/public 连接后，
// 再切回 local 时 Sync 必须能重建），且去重状态被清空。
func TestStopAllKeepsServiceUsable(t *testing.T) {
	s := NewService()
	s.conns["dev1"] = newConn("dev1", "10.10.0.1", nil, nil)
	s.conns["dev2"] = newConn("dev2", "10.10.0.2", nil, nil)
	s.lastForward["dev1"] = time.Now()
	s.lastState["dev1"] = 1

	s.StopAll()

	if len(s.conns) != 0 {
		t.Fatalf("StopAll 后连接应清空，剩 %d 条", len(s.conns))
	}
	if s.stopped {
		t.Fatal("StopAll 不得置 stopped（否则后续 Sync 无法重建连接）")
	}
	if len(s.lastForward) != 0 || len(s.lastState) != 0 {
		t.Fatal("StopAll 应清空 lastForward/lastState（防跨模式残留去重）")
	}

	// 与 Stop 的行为区分：Sync 在 stopped 时会直接返回，StopAll 后应继续工作。
	devices := []config.DeviceCache{{DeviceID: "dev1", IP: "10.10.0.1"}}
	s.Sync(devices)
	if _, ok := s.conns["dev1"]; !ok {
		t.Fatal("StopAll 后 Sync 应能重建 dev1 连接")
	}
	s.Stop() // 清理 goroutine
}
