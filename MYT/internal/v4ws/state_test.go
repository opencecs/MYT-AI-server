package v4ws

import (
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// --- 在线性判据用例（2026-10 用户口径）---
//
// 在线 = 收到 snapshot；断开 = 离线。hello 只代表握手，不算在线（快照没到仍可能
// 不可用，例如服务端后端全挂时 1011）。

// stateLog 记录 onState 上报序列
type stateLog struct {
	mu     sync.Mutex
	states []int
}

func (s *stateLog) append(v int) {
	s.mu.Lock()
	s.states = append(s.states, v)
	s.mu.Unlock()
}

func (s *stateLog) snapshot() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int, len(s.states))
	copy(out, s.states)
	return out
}

func (s *stateLog) contains(want int) bool {
	for _, v := range s.snapshot() {
		if v == want {
			return true
		}
	}
	return false
}

// 收到 snapshot 才上报在线
func TestClientOnlineOnSnapshot(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	log := &stateLog{}
	c.onState = log.append
	go c.run()
	defer c.Stop()

	m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")
	waitFor(t, 2*time.Second, func() bool { return log.contains(StateOnline) }, "收到 snapshot 后未上报在线")
}

// 只发 hello 不发 snapshot：不上报在线（在线判据是快照，不是握手）
func TestClientNoSnapshotNoOnline(t *testing.T) {
	m := newMockTobox(t)
	m.noSnapshot = true
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	log := &stateLog{}
	c.onState = log.append
	go c.run()
	defer c.Stop()

	m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.hellos == 1 }, "hello 未到达")
	// 给消费循环足够时间处理（若错误地在 hello 上报在线，这里能抓到）
	time.Sleep(100 * time.Millisecond)
	if log.contains(StateOnline) {
		t.Errorf("未收到 snapshot 不应上报在线, states=%v", log.snapshot())
	}
}

// 断开连接上报离线（在线 → 断开 → 离线）
func TestClientOfflineOnDisconnect(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	log := &stateLog{}
	c.onState = log.append
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return log.contains(StateOnline) }, "未上报在线")

	mc.closeWith(websocket.StatusGoingAway, "server shutdown")
	waitFor(t, 3*time.Second, func() bool { return log.contains(StateOffline) }, "断连后未上报离线")
}

// 重连成功翻回在线（离线不是终态）
func TestClientReconnectBackOnline(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	log := &stateLog{}
	c.onState = log.append
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "首个 snapshot 未到达")

	mc.closeWith(websocket.StatusGoingAway, "server restart")
	waitFor(t, 3*time.Second, func() bool { return rec.snapshotCount() >= 2 }, "重连后未重建快照")

	// 上报序列里必须出现「离线之后再在线」：取最后一次为在线，且之前出现过离线
	states := log.snapshot()
	if len(states) == 0 || states[len(states)-1] != StateOnline {
		t.Fatalf("重连后最后一次上报应为在线, states=%v", states)
	}
	seenOffline := false
	for _, v := range states {
		if v == StateOffline {
			seenOffline = true
		}
	}
	if !seenOffline {
		t.Errorf("断连应上报过离线, states=%v", states)
	}
}
