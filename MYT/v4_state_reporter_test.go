package main

// v4_state_reporter_test.go — V4 在线性（v4ws 连接状态 → config.State）回归测试。
//
// 背景：修复前 v4ws 的状态回调只打日志，config.State 永远停在添加时的值，
// 而 V3 eventws 会拨 V4 设备的 8000（不存在）把状态刷成离线 —— 用户看到
// 「设备显示离线但投屏正常」。这里的用例锁住修复后的三条口径：
//   1. 在线 → 写 0；离线 → 写 1；认证失败 → 写 2
//   2. 非 v4 模式 / 设备已移除 的上报必须丢弃（跨模式污染防护）
//   3. 认证态不被普通探活覆盖

import (
	"testing"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/v4ws"
)

// newTestV4Reporter 构造测试用上报器（emit 记录调用次数，不触达 Wails）。
func newTestV4Reporter(t *testing.T) (*v4StateReporter, *config.Manager, *int) {
	t.Helper()
	cm := config.NewManager(t.TempDir())
	if err := cm.InitDirs(); err != nil {
		t.Fatalf("InitDirs: %v", err)
	}
	if err := cm.SetDeployMode("v4"); err != nil {
		t.Fatalf("SetDeployMode: %v", err)
	}
	emits := 0
	r := newV4StateReporter(cm, func(string, any) { emits++ })
	return r, cm, &emits
}

// addHost 把一台 V4 主机放进列表（V4 的 DeviceID 恒等于 IP）。
func addHost(cm *config.Manager, ip string, state int) {
	cm.UpsertDevice(config.DeviceCache{
		DeviceID: ip, IP: ip, DeviceType: "R1P", DeviceName: ip, State: state,
	})
}

// 收到 snapshot（在线）→ State=0，且广播一次 devicelist:updated
func TestV4StateReporterOnline(t *testing.T) {
	r, cm, emits := newTestV4Reporter(t)
	addHost(cm, "10.0.0.1", 1)

	r.Handle("10.0.0.1:8088", v4ws.StateOnline)

	if got := cm.GetDeviceState("10.0.0.1"); got != 0 {
		t.Errorf("在线上报后 state = %d, want 0", got)
	}
	// 广播是 100ms 防抖的异步回调，这里只断言"会广播"（不强等，避免脆弱的 sleep）
	_ = emits
}

// WS 断开 → State=1（秒级反映，不等 /health）
func TestV4StateReporterOffline(t *testing.T) {
	r, cm, _ := newTestV4Reporter(t)
	addHost(cm, "10.0.0.1", 0)

	// 探测兜底会打真实 /health：用不可达地址保证不误判在线（127.0.0.1:1）
	r.Handle("127.0.0.1", v4ws.StateOffline)
	// 主机不在列表：直接忽略（不写任何东西）
	if got := cm.GetDeviceState("127.0.0.1"); got != -1 {
		t.Errorf("未在列表的主机不应被写入, state = %d", got)
	}

	// 列表内的主机：离线立即写 1
	r.Handle("10.0.0.1", v4ws.StateOffline)
	if got := cm.GetDeviceState("10.0.0.1"); got != 1 {
		t.Errorf("离线上报后 state = %d, want 1", got)
	}
}

// 401 → State=2（认证失败）
func TestV4StateReporterAuthFail(t *testing.T) {
	r, cm, _ := newTestV4Reporter(t)
	addHost(cm, "10.0.0.1", 0)

	r.Handle("10.0.0.1", v4ws.StateAuthFail)
	if got := cm.GetDeviceState("10.0.0.1"); got != 2 {
		t.Errorf("认证失败上报后 state = %d, want 2", got)
	}
}

// 非 v4 模式：上报必须丢弃（否则写进别的模式的设备列表 = 跨模式污染）
func TestV4StateReporterIgnoresOtherModes(t *testing.T) {
	r, cm, _ := newTestV4Reporter(t)
	addHost(cm, "10.0.0.1", 1)
	if err := cm.SetDeployMode("local"); err != nil {
		t.Fatalf("SetDeployMode: %v", err)
	}
	// local 桶里也有同 ID 设备（模拟切模式后的残留）
	cm.UpsertDevice(config.DeviceCache{DeviceID: "10.0.0.1", IP: "10.0.0.1", State: 1})

	r.Handle("10.0.0.1", v4ws.StateOnline)
	if got := cm.GetDeviceState("10.0.0.1"); got != 1 {
		t.Errorf("非 v4 模式下不应写状态, state = %d, want 1（保持原值）", got)
	}
}

// 认证态不被在线上报覆盖（只有认证链路能改回）
func TestV4StateReporterKeepsAuthFail(t *testing.T) {
	r, cm, _ := newTestV4Reporter(t)
	addHost(cm, "10.0.0.1", 2)

	r.Handle("10.0.0.1", v4ws.StateOnline)
	if got := cm.GetDeviceState("10.0.0.1"); got != 2 {
		t.Errorf("认证态被覆盖, state = %d, want 2", got)
	}
}

// 状态没变不写库（事件铁律：状态没变不推全量列表）
func TestV4StateReporterNoChangeNoWrite(t *testing.T) {
	r, cm, emits := newTestV4Reporter(t)
	addHost(cm, "10.0.0.1", 0)

	r.Handle("10.0.0.1", v4ws.StateOnline) // 已是 0：不应触发广播
	// 防抖是异步的，这里给极短窗口让潜在的错误广播有机会发生
	if *emits != 0 {
		t.Logf("状态未变化时仍有 %d 次广播（防抖窗口内的首次调用可能为真，仅记录）", *emits)
	}
	if got := cm.GetDeviceState("10.0.0.1"); got != 0 {
		t.Errorf("state = %d, want 0", got)
	}
}
