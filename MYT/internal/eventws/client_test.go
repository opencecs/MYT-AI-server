package eventws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/moyunteng/myt/internal/config"
)

// mockSDK 按协议实现一个最小 SDK WS 端点，验证 client 行为。
type mockSDK struct {
	server  *httptest.Server
	mu      sync.Mutex
	conns   []*websocket.Conn
	helloCh chan struct{} // 每次建连通知
}

func newMockSDK(t *testing.T) *mockSDK {
	t.Helper()
	m := &mockSDK{helloCh: make(chan struct{}, 8)}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/events" {
			http.NotFound(w, r) // 非 /ws/events 一律 404：模拟老 SDK
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conns = append(m.conns, c)
		m.mu.Unlock()
		m.helloCh <- struct{}{}

		ctx := context.Background()
		// hello + snapshot（协议文档 §3 建连序列）
		c.Write(ctx, websocket.MessageText,
			[]byte(`{"type":"hello","proto":1,"sdkVersion":"v206","serverTime":100}`))
		c.Write(ctx, websocket.MessageText,
			[]byte(`{"type":"snapshot","seq":10,"data":{"count":1,"list":[{"id":"c1","name":"a","status":"running"}]}}`))
		// 消费客户端可能发来的 resync（忽略内容）
		for {
			_, _, err := c.Reader(ctx)
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

// host 返回拨号用 host（127.0.0.1:port）。
func (m *mockSDK) host() string {
	u := m.server.URL // http://127.0.0.1:port
	return u[strings.LastIndex(u, "/")+1:]
}

// hostWith404 返回一个必 404 的拨号 host（老 SDK 模拟）。
func hostWith404(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(srv.Close)
	u := srv.URL
	return u[strings.LastIndex(u, "/")+1:]
}

func (m *mockSDK) broadcast(frame string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.conns {
		c.Write(context.Background(), websocket.MessageText, []byte(frame))
	}
}

func (m *mockSDK) dropAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.conns {
		c.Close(websocket.StatusAbnormalClosure, "drop")
	}
	m.conns = nil
}

// recordingHandlers 记录收到的回调
type recordingHandlers struct {
	mu          sync.Mutex
	hellos      int
	snapshots   int
	containers  []ContainerEventData
	boots       []string
	statsSeen   int
	tagsChanged [][]string
}

func (h *recordingHandlers) OnHello(deviceID, host string, f *HelloFrame) {
	h.mu.Lock(); defer h.mu.Unlock(); h.hellos++
}
func (h *recordingHandlers) OnSnapshot(deviceID, host string, s *SnapshotFrame) {
	h.mu.Lock(); defer h.mu.Unlock(); h.snapshots++
}
func (h *recordingHandlers) OnContainerEvent(deviceID, host string, ev *EventFrame, d ContainerEventData) {
	h.mu.Lock(); defer h.mu.Unlock(); h.containers = append(h.containers, d)
}
func (h *recordingHandlers) OnBootEvent(deviceID string, ev *EventFrame, d BootEventData, uiStatus string) {
	h.mu.Lock(); defer h.mu.Unlock(); h.boots = append(h.boots, uiStatus)
}
func (h *recordingHandlers) OnStatsEvent(deviceID string, ev *EventFrame, stats []ContainerStatsItem) {
	h.mu.Lock(); defer h.mu.Unlock(); h.statsSeen++
}
func (h *recordingHandlers) OnSystemStats(deviceID string, ev *EventFrame, raw json.RawMessage) {
}
func (h *recordingHandlers) OnTagsChangedEvent(deviceID string, ev *EventFrame, tags []string) {
	h.mu.Lock(); defer h.mu.Unlock()
	h.tagsChanged = append(h.tagsChanged, tags)
}

func (h *recordingHandlers) count(f func() int) int {
	h.mu.Lock(); defer h.mu.Unlock()
	return f()
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待条件超时 %v", timeout)
}

func TestClientReceivesFrames(t *testing.T) {
	m := newMockSDK(t)
	h := &recordingHandlers{}

	c := newConn("dev1", m.host(), nil, h)
	go c.run()
	defer c.Stop()

	// 拨号 → hello+snapshot
	waitFor(t, 5*time.Second, func() bool {
		h.mu.Lock(); defer h.mu.Unlock()
		return h.hellos >= 1 && h.snapshots >= 1
	})

	// 推 lifecycle 事件
	m.broadcast(`{"type":"event","seq":11,"event":"container","action":"stop","data":{"containerId":"c1","name":"a","status":"stopped"}}`)
	waitFor(t, 5*time.Second, func() bool {
		h.mu.Lock(); defer h.mu.Unlock()
		return len(h.containers) >= 1 && h.containers[0].Status == "stopped"
	})

	// 推 boot 事件（seq 连续）
	m.broadcast(`{"type":"event","seq":12,"event":"boot","action":"booting","data":{"containerId":"c1"}}`)
	waitFor(t, 5*time.Second, func() bool {
		h.mu.Lock(); defer h.mu.Unlock()
		return len(h.boots) >= 1 && h.boots[0] == "starting"
	})

	// 推 stats 事件（数组形态）
	m.broadcast(`{"type":"event","seq":13,"event":"container","action":"stats","data":[{"containerId":"c1","cpuUsage":5.5}]}`)
	waitFor(t, 5*time.Second, func() bool {
		h.mu.Lock(); defer h.mu.Unlock()
		return h.statsSeen >= 1
	})
}

func TestClientReconnectAfterDrop(t *testing.T) {
	m := newMockSDK(t)
	h := &recordingHandlers{}

	c := newConn("dev1", m.host(), nil, h)
	go c.run()
	defer c.Stop()

	waitFor(t, 5*time.Second, func() bool {
		h.mu.Lock(); defer h.mu.Unlock()
		return h.hellos >= 1
	})

	// 断连 → 退避（首退避 1s）→ 重连 → 新 hello
	m.dropAll()
	waitFor(t, 10*time.Second, func() bool {
		h.mu.Lock(); defer h.mu.Unlock()
		return h.hellos >= 2
	})

	// 重连后收到新事件（新序列：snapshot seq=10 基准，事件 seq=11）
	m.broadcast(`{"type":"event","seq":11,"event":"container","action":"start","data":{"containerId":"c2","status":"running"}}`)
	waitFor(t, 5*time.Second, func() bool {
		h.mu.Lock(); defer h.mu.Unlock()
		return len(h.containers) >= 1
	})
}

// TestClient404LogThrottle 404 日志节流契约：每 degradedLogInterval 最多一条。
// 回归：旧实现每轮 404 都刷新 last404Log（滑动窗口）——持续 404 时首条之后
// 永不再记录，违背「每 10 分钟一条」。
func TestClient404LogThrottle(t *testing.T) {
	var mu sync.Mutex
	lines := 0
	c := newConn("dev1", hostWith404(t), nil, nil)
	c.logHook = func(string) { mu.Lock(); lines++; mu.Unlock() }
	defer c.Stop()

	count := func() int { mu.Lock(); defer mu.Unlock(); return lines }
	dial := func() {
		if _, err := c.connectOnce(); err != nil {
			t.Fatalf("connectOnce: %v", err)
		}
	}

	dial()
	if n := count(); n != 1 {
		t.Fatalf("首次 404 应记录一条 console 日志，实际 %d", n)
	}
	// 节流间隔内：静默，且**不得刷新窗口**——旧实现每轮 404 都把 last404Log
	// 滑到 now（滑动窗口），持续 404 的设备首条之后永不再记录，违背节流契约。
	c.mu404.Lock()
	c.last404Log = time.Now().Add(-5 * time.Minute)
	c.mu404.Unlock()
	dial()
	if n := count(); n != 1 {
		t.Fatalf("间隔内重复 404 应静默，实际 %d 条", n)
	}
	c.mu404.Lock()
	since := time.Since(c.last404Log)
	c.mu404.Unlock()
	if since < 4*time.Minute {
		t.Fatalf("静默路径刷新了节流窗口（滑动窗口 bug）：last404Log 距今仅 %v", since)
	}
	// 窗口到期：再记录
	c.mu404.Lock()
	c.last404Log = time.Now().Add(-degradedLogInterval - time.Minute)
	c.mu404.Unlock()
	dial()
	if n := count(); n != 2 {
		t.Fatalf("到期后 404 应再记录一条，实际 %d 条", n)
	}
}

// TestClient404BackoffRetry 404（老 SDK）与普通断线同节奏：退避重连不停止。
// run 循环在 404 后继续（无 ErrDegraded），状态上报走 offline，收到多轮上报证明在重试。
func TestClient404BackoffRetry(t *testing.T) {
	h := &recordingHandlers{}
	states := make(chan int, 32)
	c := newConn("dev1", hostWith404(t), nil, h)
	c.onState = func(state int) { states <- state }
	go func() { _ = c.run() }()
	defer c.Stop()

	// 404 → 连续多轮 offline 上报（退避 1s 起，5s 内至少 2 轮拨号）
	deadline := time.Now().Add(5 * time.Second)
	count := 0
	for time.Now().Before(deadline) {
		select {
		case s := <-states:
			if s != 1 {
				t.Fatalf("404 应上报离线(1)，得到 %d", s)
			}
			count++
			if count >= 2 {
				return // 两轮上报 = 确认在退避重连（老行为是停止+降级）
			}
		case <-time.After(500 * time.Millisecond):
		}
	}
	t.Fatalf("5s 内仅 %d 轮 404 上报，期望 >=2（退避重连未生效？）", count)
}

func TestManagerLifecycle(t *testing.T) {
	m := newMockSDK(t)
	h := &recordingHandlers{}
	helloCount := make(chan struct{}, 16) // hello 经 manager 路由到 helloHandler（devicedetail 路径）

	svc := NewService()
	svc.dialPortOverride = m.host()[strings.LastIndex(m.host(), ":")+1:] // 测试缝：mock 端口
	svc.SetHandlers(h)
	svc.SetHelloHandler(func(deviceID string, hf *HelloFrame) { helloCount <- struct{}{} })
	defer svc.Stop()

	svc.Connect("dev1", "127.0.0.1")
	if !svc.Connected("dev1") {
		t.Fatalf("Connect 后应处于连接中状态")
	}
	waitFor(t, 5*time.Second, func() bool { return len(helloCount) >= 1 })

	// 重复 Connect 幂等
	svc.Connect("dev1", "127.0.0.1")

	// Sync 空列表：全部断开
	svc.Sync(nil)
	if svc.Connected("dev1") {
		t.Fatalf("Sync(nil) 后应断开连接")
	}

	// Sync 加回：重连
	svc.Sync([]config.DeviceCache{{DeviceID: "dev1", IP: "127.0.0.1"}})
	waitFor(t, 5*time.Second, func() bool { return len(helloCount) >= 2 })
}

// TestManager404RetriesAtBackoff 404（老 SDK）不再降级：连接保持重试
// （Connected 持续为 true——run 循环退避重连，conn 不退出）。
func TestManager404RetriesAtBackoff(t *testing.T) {
	h := &recordingHandlers{}

	svc := NewService()
	host404 := hostWith404(t)
	svc.dialPortOverride = host404[strings.LastIndex(host404, ":")+1:]
	svc.SetHandlers(h)
	defer svc.Stop()

	svc.Connect("dev1", "127.0.0.1")
	waitFor(t, 5*time.Second, func() bool { return svc.Connected("dev1") })
	// 404 后连接仍在（退避重连中）——老行为是降级退出（Connected=false）
	time.Sleep(1500 * time.Millisecond)
	if !svc.Connected("dev1") {
		t.Fatalf("404 后连接应保持退避重连（Connected=true），不应降级退出")
	}
}

func TestManagerPublicDeviceSkipped(t *testing.T) {
	svc := NewService()
	defer svc.Stop()
	svc.Connect("dev1", "") // public 模式设备无 IP
	if svc.Connected("dev1") {
		t.Fatalf("空 IP 设备不应建连")
	}
}
