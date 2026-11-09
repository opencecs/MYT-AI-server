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

// mockGateway 模拟公网网关透传：接受 /v1/devices/{id}/passthrough/ws/events
// 路径的 WS 升级（记录鉴权头），行为与 mockSDK 相同（hello+snapshot+事件广播）。
type mockGateway struct {
	server *httptest.Server
	mu     sync.Mutex
	conns  []*websocket.Conn

	authSeen []string // 每次握手收到的 Authorization 头
	pathSeen []string // 每次握手收到的路径
}

func newMockGateway(t *testing.T) *mockGateway {
	t.Helper()
	g := &mockGateway{}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 记录握手请求头与路径（断言 Bearer/透传路径）
		g.mu.Lock()
		g.authSeen = append(g.authSeen, r.Header.Get("Authorization"))
		g.pathSeen = append(g.pathSeen, r.URL.Path)
		g.mu.Unlock()

		if !strings.HasPrefix(r.URL.Path, "/v1/devices/") ||
			!strings.HasSuffix(r.URL.Path, "/passthrough/ws/events") {
			http.NotFound(w, r) // 非透传路径一律 404（对齐真实网关 unknown endpoint）
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		g.mu.Lock()
		g.conns = append(g.conns, c)
		g.mu.Unlock()

		ctx := r.Context()
		c.Write(ctx, websocket.MessageText,
			[]byte(`{"type":"hello","proto":1,"sdkVersion":"v208","deviceId":"pubdev1"}`))
		c.Write(ctx, websocket.MessageText,
			[]byte(`{"type":"snapshot","seq":10,"data":{"count":1,"list":[{"id":"c1","name":"a","status":"running"}]}}`))
		for {
			if _, _, err := c.Reader(ctx); err != nil {
				return
			}
		}
	}))
	t.Cleanup(g.server.Close)
	return g
}

// host 返回网关地址（host:port 形态，与 SetPublicDialer 的 gatewayHost 一致）。
func (g *mockGateway) host() string {
	u := g.server.URL
	return u[strings.LastIndex(u, "/")+1:]
}

func (g *mockGateway) broadcast(frame string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.conns {
		c.Write(context.Background(), websocket.MessageText, []byte(frame))
	}
}

func (g *mockGateway) handshakes() (auths, paths []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string{}, g.authSeen...), append([]string{}, g.pathSeen...)
}

// TestPublicConnDialAndFrames 公网形态端到端：网关透传路径拨号 + Bearer 头 +
// hello/snapshot 帧消费 + host 回调语义为 device_id（公网容器缓存的 IP 字段
// 约定存 device_id——截图 key 与网关透传标识依赖它，绝不能是网关地址）。
func TestPublicConnDialAndFrames(t *testing.T) {
	g := newMockGateway(t)
	h := &recordingHandlers{}
	var gotHost string
	var hostMu sync.Mutex
	wrapped := &capturingHandlers{inner: h, onSnapshot: func(host string) {
		hostMu.Lock()
		gotHost = host
		hostMu.Unlock()
	}}

	c := newPublicConn("pubdev1", g.host(), func() (string, error) { return "jwt-token-xyz", nil }, wrapped)
	if c.gatewayPath == "" {
		t.Fatalf("public conn 应携带网关透传路径")
	}
	go c.run()
	defer c.Stop()

	waitFor(t, 5*time.Second, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.hellos >= 1 && h.snapshots >= 1
	})

	// 事件帧照常消费（帧协议与本地同构）
	g.broadcast(`{"type":"event","seq":11,"event":"container","action":"stats","data":[{"containerId":"c1","cpuUsage":5.5}]}`)
	waitFor(t, 5*time.Second, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.statsSeen >= 1
	})

	// 拨号头：Bearer（每次拨号现取的 token）
	auths, paths := g.handshakes()
	if len(auths) == 0 {
		t.Fatalf("mock 网关未收到握手")
	}
	if auths[0] != "Bearer jwt-token-xyz" {
		t.Fatalf("Authorization 头应为 Bearer JWT，实际 %q", auths[0])
	}
	if !strings.HasPrefix(paths[0], "/v1/devices/pubdev1/passthrough/ws/events") {
		t.Fatalf("拨号路径应为网关透传形态，实际 %q", paths[0])
	}

	// host 回调语义：device_id（非网关地址）
	hostMu.Lock()
	defer hostMu.Unlock()
	if gotHost != "pubdev1" {
		t.Fatalf("OnSnapshot 的 host 回调应为 device_id（公网缓存约定），实际 %q", gotHost)
	}
}

// capturingHandlers 包装一层以捕获 host 参数（recordingHandlers 不记录 host）。
type capturingHandlers struct {
	inner      *recordingHandlers
	onSnapshot func(host string)
}

func (c *capturingHandlers) OnHello(deviceID, host string, f *HelloFrame) {
	c.inner.OnHello(deviceID, host, f)
}
func (c *capturingHandlers) OnSnapshot(deviceID, host string, s *SnapshotFrame) {
	c.onSnapshot(host)
	c.inner.OnSnapshot(deviceID, host, s)
}
func (c *capturingHandlers) OnContainerEvent(deviceID, host string, ev *EventFrame, d ContainerEventData) {
	c.inner.OnContainerEvent(deviceID, host, ev, d)
}
func (c *capturingHandlers) OnBootEvent(deviceID string, ev *EventFrame, d BootEventData, uiStatus string) {
	c.inner.OnBootEvent(deviceID, ev, d, uiStatus)
}
func (c *capturingHandlers) OnStatsEvent(deviceID string, ev *EventFrame, stats []ContainerStatsItem) {
	c.inner.OnStatsEvent(deviceID, ev, stats)
}
func (c *capturingHandlers) OnSystemStats(deviceID string, ev *EventFrame, raw json.RawMessage) {
	c.inner.OnSystemStats(deviceID, ev, raw)
}
func (c *capturingHandlers) OnTagsChangedEvent(deviceID string, ev *EventFrame, tags []string) {
	c.inner.OnTagsChangedEvent(deviceID, ev, tags)
}

// TestSetPublicDialerStripsScheme 网关地址带 scheme 防护：cloudapi.GatewayURL
// 是 http:// 开头的完整地址，dialURL 会自行拼 ws://——双重 scheme 是非法 URL
// 必然拨号失败（曾因此全部设备静默离线）。SetPublicDialer 必须剥掉前缀。
func TestSetPublicDialerStripsScheme(t *testing.T) {
	svc := NewService()
	svc.SetPublicDialer(func() (string, error) { return "jwt", nil }, "http://gw.example.com:8081/")
	svc.mu.Lock()
	gw := svc.publicGateway
	svc.mu.Unlock()
	if gw != "gw.example.com:8081" {
		t.Fatalf("网关地址应剥掉 scheme 与尾部斜杠，实际 %q", gw)
	}
}

// TestSyncConnectsPublicDevices Sync 对空 IP 设备：配置了公网拨号 → 建连；
// 未配置 → 维持不建连现状（回归保护）。
func TestSyncConnectsPublicDevices(t *testing.T) {
	g := newMockGateway(t)
	svc := NewService()
	svc.SetHandlers(&recordingHandlers{})
	defer svc.Stop()

	// 未配置 publicDialer：空 IP 设备不连（现状）
	svc.Sync([]config.DeviceCache{{DeviceID: "pub1", IP: ""}})
	time.Sleep(300 * time.Millisecond) // 给潜在拨号留出时间窗口
	if svc.Connected("pub1") {
		t.Fatalf("未配置公网拨号时空 IP 设备不应建连（维持现状）")
	}

	// 配置后：空 IP 设备经网关建连
	svc.SetPublicDialer(func() (string, error) { return "jwt", nil }, g.host())
	svc.Sync([]config.DeviceCache{{DeviceID: "pub1", IP: ""}})
	waitFor(t, 5*time.Second, func() bool { return svc.Connected("pub1") })
}

// TestSyncStopsPublicConnsOnLocalList 模式切换语义：本地列表对账时公网连接全停。
func TestSyncStopsPublicConnsOnLocalList(t *testing.T) {
	g := newMockGateway(t)
	svc := NewService()
	svc.SetHandlers(&recordingHandlers{})
	svc.SetPublicDialer(func() (string, error) { return "jwt", nil }, g.host())
	defer svc.Stop()

	svc.Sync([]config.DeviceCache{{DeviceID: "pub1", IP: ""}})
	waitFor(t, 5*time.Second, func() bool { return svc.Connected("pub1") })

	// 切到 local 列表（带 IP 的另一台设备）：pub1 不在列表 → 连接被停
	svc.Sync([]config.DeviceCache{{DeviceID: "local1", IP: "10.10.0.1"}})
	waitFor(t, 5*time.Second, func() bool { return !svc.Connected("pub1") })
}
