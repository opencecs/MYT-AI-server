package eventws

// IP 漂移重建回归：设备重启换 IP 后，刷新（Sync/Connect 带新 IP）必须停旧建新。
// 历史 bug：Connect 对已有连接幂等跳过、不比对拨号目标——老连接永远拨旧 IP，
// 状态靠 60s HTTP 探测硬撑在线，事件流（snapshot/容器事件/指标）死亡，
// 直到重启客户端才恢复。

import (
	"strings"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// driftService 构造拨号指向 mock SDK 的服务（dialPortOverride 测试缝）。
func driftService(t *testing.T, m *mockSDK) *Service {
	t.Helper()
	svc := NewService()
	svc.dialPortOverride = m.host()[strings.LastIndex(m.host(), ":")+1:]
	svc.SetHandlers(&recordingHandlers{})
	return svc
}

// registered 当前注册的连接（持锁快照）。
func registered(svc *Service, deviceID string) *conn {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return svc.conns[deviceID]
}

// TestConnectSameIPReuses 同 IP 重复 Connect/Sync：复用连接不抖动
// （300 台设备每轮刷新全量断连是事故级回归——事件流全部重握手）。
func TestConnectSameIPReuses(t *testing.T) {
	m := newMockSDK(t)
	svc := driftService(t, m)
	defer svc.Stop()

	svc.Connect("dev1", "127.0.0.1")
	first := registered(svc, "dev1")
	if first == nil {
		t.Fatalf("Connect 后应有连接注册")
	}
	svc.Connect("dev1", "127.0.0.1")
	svc.Sync([]config.DeviceCache{{DeviceID: "dev1", IP: "127.0.0.1"}})
	if again := registered(svc, "dev1"); again != first {
		t.Fatalf("目标一致时应复用连接（同 IP 抖动回归）")
	}
	select {
	case <-first.done:
		t.Fatalf("复用的连接不应被停止")
	default:
	}
}

// TestConnectIPDriftRebuildEndToEnd 全链路：旧 IP 不可达 → 刷新带新 IP →
// 旧连接停止、新连接按新 IP 真正建连并收到 hello（事件流恢复）。
func TestConnectIPDriftRebuildEndToEnd(t *testing.T) {
	m := newMockSDK(t)
	svc := driftService(t, m)
	defer svc.Stop()

	hello := make(chan struct{}, 8)
	svc.SetHelloHandler(func(deviceID string, h *HelloFrame) { hello <- struct{}{} })

	// 初始：旧 IP 无人监听（mock 只绑 127.0.0.1）→ 拨号失败属预期
	svc.Connect("dev1", "127.0.0.2")
	first := registered(svc, "dev1")
	if first == nil || first.host != "127.0.0.2:"+svc.dialPortOverride {
		t.Fatalf("初始连接应拨旧 IP，实际 %v", first)
	}

	// 刷新：列表 IP 修正 → 停旧建新
	svc.Sync([]config.DeviceCache{{DeviceID: "dev1", IP: "127.0.0.1"}})
	second := registered(svc, "dev1")
	if second == nil || second == first {
		t.Fatalf("漂移后应注册新连接")
	}
	if second.host != "127.0.0.1:"+svc.dialPortOverride {
		t.Fatalf("新连接应拨新 IP，实际 %s", second.host)
	}
	select {
	case <-first.done:
	default:
		t.Fatalf("旧连接应被停止")
	}
	// 新连接到 mock：hello 到达 = 事件流真正恢复（不止状态翻绿）
	select {
	case <-hello:
	case <-time.After(5 * time.Second):
		t.Fatalf("漂移重建后 5s 内未收到 hello（事件流未恢复）")
	}
}

// TestSyncPublicDeviceDrift local→public 形态漂移：设备 IP 置空 + 配置公网
// 拨号后 Sync，旧 local 连接应重建为网关透传连接（此前同样被幂等跳过）。
func TestSyncPublicDeviceDrift(t *testing.T) {
	m := newMockSDK(t)
	svc := driftService(t, m)
	defer svc.Stop()

	svc.Connect("dev1", "127.0.0.1")
	// 网关地址用本机必拒端口，避免测试外联
	svc.SetPublicDialer(func() (string, error) { return "tok", nil }, "127.0.0.1:1")
	svc.Sync([]config.DeviceCache{{DeviceID: "dev1", IP: ""}})
	if c := registered(svc, "dev1"); c == nil || c.gatewayPath == "" {
		t.Fatalf("local→public 漂移应重建为网关透传连接")
	}
}

// TestCleanupConnKeepsNewerConn 漂移重建后旧连接 run() 迟到退出，
// 注销不得误删新连接的注册（误删会让下轮 Sync 重复建连 → 连接泄漏）。
func TestCleanupConnKeepsNewerConn(t *testing.T) {
	m := newMockSDK(t)
	svc := driftService(t, m)
	defer svc.Stop()

	svc.Connect("dev1", "127.0.0.1")
	first := registered(svc, "dev1")
	svc.Connect("dev1", "127.0.0.2") // 漂移重建
	second := registered(svc, "dev1")
	if first == nil || second == nil || second == first {
		t.Fatalf("漂移重建前置失败")
	}

	svc.cleanupConn("dev1", first) // 旧连接循环迟到退出
	if still := registered(svc, "dev1"); still != second {
		t.Fatalf("旧连接迟到的注销误删了新连接注册")
	}
	svc.cleanupConn("dev1", second) // 新连接自己退出：正常注销
	if svc.Connected("dev1") {
		t.Fatalf("新连接退出后应注销")
	}
}

// TestReportStateSuppressedAfterStop 已停止连接的迟到上报必须被抑制
// （漂移重建窗口内旧连接的拨号失败不再污染状态/触发无谓复核）。
func TestReportStateSuppressedAfterStop(t *testing.T) {
	calls := 0
	c := newConn("dev1", "127.0.0.1:8000", nil, nil)
	c.onState = func(state int) { calls++ }
	c.reportState(stateOnline)
	if calls != 1 {
		t.Fatalf("未停止的连接应正常上报")
	}
	c.Stop()
	c.reportState(stateOffline)
	if calls != 1 {
		t.Fatalf("已停止连接的迟到上报应被抑制，实际 %d 次", calls)
	}
}
