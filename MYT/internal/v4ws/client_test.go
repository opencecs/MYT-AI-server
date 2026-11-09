package v4ws

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// recorder 记录 handler 回调（消费 goroutine 串行调用 → 加锁保护供测试断言）
type recorder struct {
	mu         sync.Mutex
	hellos     int
	snapshots  []*SnapshotFrame
	instances  []InstanceItem
	clipboards int
	systems    int
	states     []int
}

func (r *recorder) OnHello(host string, h *HelloFrame) {
	r.mu.Lock()
	r.hellos++
	r.mu.Unlock()
}

func (r *recorder) OnSnapshot(host string, snap *SnapshotFrame) {
	r.mu.Lock()
	r.snapshots = append(r.snapshots, snap)
	r.mu.Unlock()
}

func (r *recorder) OnInstanceState(host string, ev *EventFrame, item InstanceItem) {
	r.mu.Lock()
	r.instances = append(r.instances, item)
	r.mu.Unlock()
}

func (r *recorder) OnClipboard(host string, ev *EventFrame, data ClipboardEventData) {
	r.mu.Lock()
	r.clipboards++
	r.mu.Unlock()
}

func (r *recorder) OnSystemResync(host string, ev *EventFrame) {
	r.mu.Lock()
	r.systems++
	r.mu.Unlock()
}

func (r *recorder) snapshotCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.snapshots)
}

func (r *recorder) instanceCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.instances)
}

func (r *recorder) lastInstanceState() (InstanceItem, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.instances) == 0 {
		return InstanceItem{}, false
	}
	return r.instances[len(r.instances)-1], true
}

// fastConn 构造一个测试用连接（压缩超时/退避，加速重连）
func fastConn(t *testing.T, host string, h Handlers) *conn {
	t.Helper()
	c := newConn(host, "admin", "myt", h)
	c.dialTimeout = 2 * time.Second
	c.watchdogTimeout = 300 * time.Millisecond
	c.backoffInitial = 20 * time.Millisecond
	c.backoffMax = 60 * time.Millisecond
	c.resyncMinInterval = 100 * time.Millisecond
	return c
}

// waitFor 轮询等待条件成立（避免脆弱的固定 sleep）
func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", msg)
}

// 正常序列：hello → snapshot → 连续事件（seq 连续，不触发 resync）
func TestClientNormalSequence(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	// seq 从 10 起（snapshot 基准），11/12 连续
	mc.send(t, `{"type":"event","seq":11,"event":"instance","action":"state","data":{"backend":"default","id":"i1","name":"n1","state":"stopping","running":false}}`)
	mc.send(t, `{"type":"event","seq":12,"event":"instance","action":"state","data":{"backend":"default","id":"i1","name":"n1","state":"stopped","running":false}}`)
	waitFor(t, 2*time.Second, func() bool { return rec.instanceCount() == 2 }, "事件未全部应用")

	last, _ := rec.lastInstanceState()
	if last.State != "stopped" {
		t.Errorf("末态 = %q, want stopped", last.State)
	}
	// 连续事件不应发 resync
	if frames := mc.sentFrames(); len(frames) != 0 {
		t.Errorf("连续 seq 不应发 resync, got %v", frames)
	}
}

// 重放/重复 seq（<= snapshot 基准）必须丢弃
func TestClientDropsReplayedEvents(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	// seq=10 等于快照基准（窗口重放）→ 丢弃；seq=11 正常应用
	mc.send(t, `{"type":"event","seq":10,"event":"instance","action":"state","data":{"id":"replay"}}`)
	mc.send(t, `{"type":"event","seq":11,"event":"instance","action":"state","data":{"id":"applied"}}`)
	waitFor(t, 2*time.Second, func() bool { return rec.instanceCount() >= 1 }, "事件未到达")
	time.Sleep(50 * time.Millisecond)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, it := range rec.instances {
		if it.ID == "replay" {
			t.Error("重放事件（seq<=snapshot.seq）不应被应用")
		}
	}
	if len(rec.instances) != 1 || rec.instances[0].ID != "applied" {
		t.Errorf("应用事件集不符: %+v", rec.instances)
	}
}

// seq 断档：应用当前帧 + 发 resync（限频内后续断档不再发）
func TestClientGapTriggersResyncWithRateLimit(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	// 从 10 跳到 15（断档）→ 应用 + resync
	mc.send(t, `{"type":"event","seq":15,"event":"instance","action":"state","data":{"id":"gap1"}}`)
	waitFor(t, 2*time.Second, func() bool { return len(mc.sentFrames()) == 1 }, "断档未发 resync")
	if got := mc.sentFrames()[0]; got != `{"action":"resync"}` {
		t.Errorf("resync 帧内容不符: %q", got)
	}

	// 限频窗口内再断档（15→20）→ 不发第二个 resync
	mc.send(t, `{"type":"event","seq":20,"event":"instance","action":"state","data":{"id":"gap2"}}`)
	waitFor(t, 2*time.Second, func() bool { return rec.instanceCount() >= 2 }, "第二帧未应用")
	time.Sleep(60 * time.Millisecond)
	if n := len(mc.sentFrames()); n != 1 {
		t.Errorf("resync 限频失效：发了 %d 次", n)
	}
}

// resync-required：不在当前连接发 resync，直接断开重连（并重建快照）
func TestClientResyncRequiredReconnects(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "首个 snapshot 未到达")

	mc.send(t, `{"type":"resync-required"}`)
	// 重连后应收到第二个 snapshot
	m.waitConn(t, 3*time.Second)
	waitFor(t, 3*time.Second, func() bool { return rec.snapshotCount() >= 2 }, "重连后未重建快照")
	if frames := mc.sentFrames(); len(frames) != 0 {
		t.Errorf("resync-required 不应在当前连接发 resync, got %v", frames)
	}
}

// 1013（Try Again Later）：关闭后立即重连
func TestClient1013Reconnects(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	mc.closeWith(websocket.StatusTryAgainLater, "event overflow, resync required")
	m.waitConn(t, 3*time.Second)
	waitFor(t, 3*time.Second, func() bool { return rec.snapshotCount() >= 2 }, "1013 后未重连重建快照")
}

// 401：升级前拒绝 → 状态上报为认证失败，且持续退避重连
func TestClient401ReportsAuthFail(t *testing.T) {
	m := newMockTobox(t)
	m.requireAuth = "admin:wrong" // 客户端用默认 admin/myt → 401
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	c.password = "definitely-not-it"
	var states []int
	var mu sync.Mutex
	c.onState = func(s int) {
		mu.Lock()
		states = append(states, s)
		mu.Unlock()
	}
	go c.run()
	defer c.Stop()

	waitFor(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range states {
			if s == StateAuthFail {
				return true
			}
		}
		return false
	}, "401 未上报认证失败")
	if rec.snapshotCount() != 0 {
		t.Error("401 时不应收到快照")
	}
}

// 503：连接数已满 → 退避重试（不上报认证失败）
func TestClient503BacksOff(t *testing.T) {
	srv := newStatusServer(t, 503, `too many event connections`)
	rec := &recorder{}
	c := fastConn(t, srv, rec)
	var mu sync.Mutex
	states := []int{}
	c.onState = func(s int) {
		mu.Lock()
		states = append(states, s)
		mu.Unlock()
	}
	go c.run()
	defer c.Stop()

	waitFor(t, 3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(states) > 0
	}, "503 未上报离线")
	mu.Lock()
	defer mu.Unlock()
	for _, s := range states {
		if s == StateAuthFail {
			t.Error("503 不应上报认证失败")
		}
	}
}

// **关键用例**：tobox 空闲时无数据帧，只有协议层 ping（每 5s）。
// 看门狗必须靠 OnPingReceived 重置，否则会误杀健康连接。
func TestClientIdleWithPingNotKilled(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	c.watchdogTimeout = 600 * time.Millisecond // 压缩：约 2 个 ping 周期
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	// 持续发 ping（不发任何数据帧），总时长 > 3 个看门狗周期
	// 若看门狗只看数据帧，连接会在 600ms 后被关闭 → snapshotCount 不会增长、会重连
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := mc.ws.Ping(context.Background()); err != nil {
			t.Fatalf("ping 失败（连接被看门狗误杀？）: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if n := rec.snapshotCount(); n != 1 {
		t.Errorf("空闲+ping 期间不应重连重建快照（snapshot 数 = %d, want 1）", n)
	}
	// 没有第二次建连 = 连接从未被看门狗误杀
	select {
	case <-m.dialed:
		t.Error("空闲+ping 期间发生了重连（看门狗误杀健康连接）")
	case <-time.After(200 * time.Millisecond):
	}
}

// 反向用例：完全没有 ping 也没有数据帧 → 看门狗兜底断开（服务端静默消失场景）
func TestClientWatchdogKillsSilentConnection(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	c.watchdogTimeout = 300 * time.Millisecond
	go c.run()
	defer c.Stop()

	m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	// 服务端不再发任何帧（含 ping）→ 看门狗应在 ~300ms 后断开并重连（第二个 snapshot）
	m.waitConn(t, 3*time.Second)
	waitFor(t, 3*time.Second, func() bool { return rec.snapshotCount() >= 2 }, "看门狗未断开静默连接")
}

// 协议不兼容（hello.proto != 1）：停止重连
func TestClientIncompatibleProtoStops(t *testing.T) {
	m := newMockTobox(t)
	m.helloProto = 2
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return c.fatal.Load() }, "协议不兼容未置 fatal")

	// 不应再重连（1 秒内只有首次建连）
	select {
	case <-m.dialed:
		t.Error("协议不兼容后不应重连")
	case <-time.After(700 * time.Millisecond):
	}
}

// instance/state deleted：作为普通事件下发（适配层负责移除缓存）
func TestClientDeletedEventDelivered(t *testing.T) {
	m := newMockTobox(t)
	rec := &recorder{}
	c := fastConn(t, m.host(), rec)
	go c.run()
	defer c.Stop()

	mc := m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	mc.send(t, `{"type":"event","seq":11,"event":"instance","action":"state","data":{"backend":"cap1","id":"coloros-1","name":"coloros-1","running":false,"state":"deleted"}}`)
	waitFor(t, 2*time.Second, func() bool { return rec.instanceCount() == 1 }, "deleted 事件未到达")
	item, _ := rec.lastInstanceState()
	if item.State != StateDeleted || item.Running {
		t.Errorf("deleted 事件字段不符: %+v", item)
	}
}
