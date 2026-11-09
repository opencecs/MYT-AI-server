package v4ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// 测试基建：假 tobox 服务端 + manager 层用例（Connect 幂等 / Sync 对账 / StopAll / Stop）。
//
// mockTobox 按 tobox_player /ws/events 协议实现一个最小服务端：
// 建连后发 hello → snapshot，之后由测试驱动发事件 / 关闭连接。
type mockTobox struct {
	server *httptest.Server
	// dialed 每次建连通知（容量 16，测试里够用）
	dialed chan *mockConn
	// requireAuth 非空时校验 Basic Auth（user:pass），不匹配直接 401
	requireAuth string
	// noSnapshot true 时只发 hello（模拟快照构建失败前的中断）
	noSnapshot bool
	// snapshotSeq 首帧 snapshot 的 seq
	snapshotSeq int64
	// snapshotJSON 自定义 snapshot 帧（空则用默认 1 台实例）
	snapshotJSON string
	// helloProto 覆盖 hello.proto（协议兼容性测试）
	helloProto int

	mu    sync.Mutex
	conns []*mockConn
}

type mockConn struct {
	ws *websocket.Conn
	// frames 客户端发来的文本帧（resync 请求）
	mu     sync.Mutex
	frames []string
}

func (c *mockConn) sentFrames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.frames))
	copy(out, c.frames)
	return out
}

func newMockTobox(t *testing.T) *mockTobox {
	t.Helper()
	m := &mockTobox{
		dialed:      make(chan *mockConn, 16),
		snapshotSeq: 10,
		helloProto:  ProtoVersion,
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/events" {
			http.NotFound(w, r)
			return
		}
		if m.requireAuth != "" {
			user, pass, ok := r.BasicAuth()
			if !ok || user+":"+pass != m.requireAuth {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":401,"message":"unauthorized","data":null}`))
				return
			}
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		mc := &mockConn{ws: ws}
		m.mu.Lock()
		m.conns = append(m.conns, mc)
		m.mu.Unlock()
		m.dialed <- mc

		ctx := context.Background()
		_ = ws.Write(ctx, websocket.MessageText, []byte(helloFrameJSON(m.helloProto)))
		if !m.noSnapshot {
			_ = ws.Write(ctx, websocket.MessageText, []byte(m.snapshotFrameJSON()))
		}
		// 持续读客户端帧（resync），记录后忽略
		for {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				mc.mu.Lock()
				mc.frames = append(mc.frames, string(data))
				mc.mu.Unlock()
			}
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

func helloFrameJSON(proto int) string {
	return `{"type":"hello","proto":` + itoa(proto) + `,"sdkVersion":"0.7.0","serverTime":1792406110}`
}

func (m *mockTobox) snapshotFrameJSON() string {
	if m.snapshotJSON != "" {
		return m.snapshotJSON
	}
	return `{"type":"snapshot","seq":` + itoa64(m.snapshotSeq) +
		`,"data":{"instances":[{"backend":"default","id":"i1","name":"n1","state":"running","running":true}],"total":1}}`
}

// host 返回拨号用 host（127.0.0.1:port）
func (m *mockTobox) host() string {
	u := m.server.URL
	return u[strings.LastIndex(u, "/")+1:]
}

// waitConn 等一次建连（超时失败）
func (m *mockTobox) waitConn(t *testing.T, d time.Duration) *mockConn {
	t.Helper()
	select {
	case c := <-m.dialed:
		return c
	case <-time.After(d):
		t.Fatal("等待建连超时")
		return nil
	}
}

func (c *mockConn) send(t *testing.T, frame string) {
	t.Helper()
	if err := c.ws.Write(context.Background(), websocket.MessageText, []byte(frame)); err != nil {
		t.Errorf("发送帧失败: %v", err)
	}
}

func (c *mockConn) closeWith(code websocket.StatusCode, reason string) {
	_ = c.ws.Close(code, reason)
}

func itoa(n int) string { return itoa64(int64(n)) }
func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// newStatusServer 起一个固定返回指定 HTTP 状态的假服务端（401/503 等升级前拒绝）
func newStatusServer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	u := srv.URL
	return u[strings.LastIndex(u, "/")+1:]
}

// --- manager 层 ---

// manager 层：Connect 幂等 / Sync 对账（停旧建新）/ StopAll / Disconnect

func newTestService(m *mockTobox) *Service {
	s := NewService("admin", "myt")
	// 测试里 host 已含端口，不需要端口覆盖
	_ = m
	return s
}

func TestServiceConnectIdempotent(t *testing.T) {
	m := newMockTobox(t)
	s := newTestService(m)
	rec := &recorder{}
	s.SetHandlers(rec)
	defer s.Stop()

	host := m.host()
	s.Connect(host)
	s.Connect(host) // 幂等：复用同一条连接

	m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return rec.snapshotCount() == 1 }, "snapshot 未到达")

	// 重复 Connect 不应产生第二条连接
	select {
	case <-m.dialed:
		t.Error("重复 Connect 不应重复建连")
	case <-time.After(300 * time.Millisecond):
	}
	if n := s.ConnCount(); n != 1 {
		t.Errorf("连接数 = %d, want 1", n)
	}
}

func TestServiceSyncStopsRemovedHosts(t *testing.T) {
	m := newMockTobox(t)
	s := newTestService(m)
	s.SetHandlers(&recorder{})
	defer s.Stop()

	host := m.host()
	s.Sync([]string{host})
	m.waitConn(t, 2*time.Second)
	waitFor(t, 2*time.Second, func() bool { return s.ConnCount() == 1 }, "连接未建立")

	// 主机从列表移除 → Sync 应断开
	s.Sync([]string{})
	waitFor(t, 2*time.Second, func() bool { return s.ConnCount() == 0 }, "移除后连接未断开")
}

func TestServiceStopAllKeepsUsable(t *testing.T) {
	m := newMockTobox(t)
	s := newTestService(m)
	s.SetHandlers(&recorder{})

	host := m.host()
	s.Sync([]string{host})
	m.waitConn(t, 2*time.Second)

	s.StopAll() // 不置位 stopped，后续仍可建连
	waitFor(t, 2*time.Second, func() bool { return s.ConnCount() == 0 }, "StopAll 后连接未断开")

	s.Sync([]string{host})
	m.waitConn(t, 3*time.Second)
	waitFor(t, 3*time.Second, func() bool { return s.ConnCount() == 1 }, "StopAll 后应可重新建连")
	s.Stop()
}

func TestServiceDisconnect(t *testing.T) {
	m := newMockTobox(t)
	s := newTestService(m)
	s.SetHandlers(&recorder{})
	defer s.Stop()

	host := m.host()
	s.Connect(host)
	m.waitConn(t, 2*time.Second)

	s.Disconnect(host)
	waitFor(t, 2*time.Second, func() bool { return !s.Connected(host) }, "Disconnect 后仍标记已连接")
}

// Stop 后不再建连（程序退出语义）
func TestServiceStopPreventsConnect(t *testing.T) {
	m := newMockTobox(t)
	s := newTestService(m)
	s.SetHandlers(&recorder{})
	s.Stop()

	s.Connect(m.host())
	select {
	case <-m.dialed:
		t.Error("Stop 后不应建连")
	case <-time.After(300 * time.Millisecond):
	}
}
