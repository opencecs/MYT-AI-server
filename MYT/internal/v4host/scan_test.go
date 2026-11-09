package v4host

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
)

// fakeDiscoverer 假发现器：返回固定 UDP 回复，便于验证扫描流程
type fakeDiscoverer struct {
	devices []discover.Device
	err     error
	called  bool
}

func (f *fakeDiscoverer) Discover(_ context.Context) ([]discover.Device, error) {
	f.called = true
	return f.devices, f.err
}

// newV4Manager 建一个 v4 模式的临时 config.Manager
func newV4Manager(t *testing.T) *config.Manager {
	t.Helper()
	cm := config.NewManager(t.TempDir())
	if err := cm.InitDirs(); err != nil {
		t.Fatalf("InitDirs 失败: %v", err)
	}
	if err := cm.SetDeployMode("v4"); err != nil {
		t.Fatalf("SetDeployMode(v4) 失败: %v", err)
	}
	return cm
}

// fakeHealth 按 IP 返回假 /health 结果（不存在的 IP 视为不可达）
func fakeHealth(infos map[string]*Info) func(context.Context, string) (*Info, error) {
	return func(_ context.Context, ip string) (*Info, error) {
		if info, ok := infos[ip]; ok {
			return info, nil
		}
		return nil, errors.New("/health 不可达")
	}
}

// 扫描：UDP 回复 3 台（1 台广播名带 _v3 被版本过滤跳过），其中 2 台 /health 通过
// → 全部进 v4 待添加桶，事件推送两次
func TestScanWritesPendingDevices(t *testing.T) {
	cm := newV4Manager(t)
	fd := &fakeDiscoverer{devices: []discover.Device{
		{IP: "192.168.1.11", DeviceID: "udp-11", DeviceType: "R1", DeviceName: "r1_v3", Version: "v3"},
		{IP: "192.168.1.11", DeviceID: "udp-11", DeviceType: "R1", DeviceName: "r1_v3", Version: "v3"}, // 同 IP 重复回复
		{IP: "192.168.1.12", DeviceID: "udp-12", DeviceType: "Q1", DeviceName: "q1_v3", Version: "v3"},
		{IP: "192.168.1.13", DeviceID: "udp-13", DeviceType: "C1", DeviceName: "c1", Version: "v0"},
		{IP: "192.168.1.14", DeviceID: "udp-14", DeviceType: "C1", DeviceName: "c1b", Version: "v0"},
	}}
	sc := NewScanner(fd, cm)
	// V4 契约：DeviceID 恒为 IP（/health 无设备标识字段），显示名/型号容错可选
	sc.health = fakeHealth(map[string]*Info{
		"192.168.1.13": {IP: "192.168.1.13", DeviceID: "192.168.1.13", DeviceName: "R1_V4", Model: "r1", DeviceIDFromIP: true},
		"192.168.1.14": {IP: "192.168.1.14", DeviceID: "192.168.1.14", DeviceIDFromIP: true},
	})
	events := map[string]int{}
	sc.SetEmitter(func(event string, _ any) { events[event]++ })

	results, err := sc.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if !fd.called {
		t.Error("未调用发现器")
	}
	if len(results) != 2 {
		t.Fatalf("验证通过数 = %d, want 2", len(results))
	}

	pending := cm.GetPendingDevices()
	if len(pending) != 2 {
		t.Fatalf("待添加数 = %d, want 2", len(pending))
	}
	byID := map[string]config.DeviceCache{}
	for _, d := range pending {
		byID[d.DeviceID] = d
	}
	got, ok := byID["192.168.1.13"]
	if !ok {
		t.Fatalf("待添加缺少 192.168.1.13: %+v", pending)
	}
	if got.IP != "192.168.1.13" || got.DeviceName != "R1_V4" || got.DeviceType != "C1" || got.State != 0 {
		t.Errorf("192.168.1.13 条目不符: %+v", got)
	}
	if _, ok := byID["192.168.1.14"]; !ok {
		t.Error("设备应以 IP 为 key 写入")
	}
	if events["pendinglist:updated"] != 1 {
		t.Errorf("pendinglist:updated 次数 = %d, want 1", events["pendinglist:updated"])
	}
	if events["devicelist:updated"] != 0 {
		t.Errorf("正式列表无变化时不应推 devicelist:updated, got %d", events["devicelist:updated"])
	}
}

// 已在 v4 正式列表的候选按 IP 跳过，不重复探测、不重复进待添加
// （新语义：扫描只负责发现新设备，已知设备的状态由心跳/状态上报维护）
func TestScanUpdatesExistingDevice(t *testing.T) {
	cm := newV4Manager(t)
	cm.UpsertDevice(config.DeviceCache{DeviceID: "192.168.1.21", IP: "192.168.1.21", State: 1})

	sc := NewScanner(&fakeDiscoverer{devices: []discover.Device{
		{IP: "192.168.1.21", DeviceID: "udp-21", DeviceName: "c1", Version: "v0"},
	}}, cm)
	var probed []string
	sc.health = func(_ context.Context, ip string) (*Info, error) {
		probed = append(probed, ip)
		return &Info{IP: ip, DeviceID: ip, DeviceName: "R1_V4", DeviceIDFromIP: true}, nil
	}
	sc.healthQuick = sc.health
	events := map[string]int{}
	sc.SetEmitter(func(event string, _ any) { events[event]++ })

	results, err := sc.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if len(probed) != 0 {
		t.Errorf("已在列表的设备不应被探测, probed = %v", probed)
	}
	if len(results) != 0 {
		t.Errorf("已在列表的设备不应出现在本轮结果里: %+v", results)
	}
	if len(cm.GetPendingDevices()) != 0 {
		t.Error("已在正式列表的设备不应进待添加")
	}
	if events["devicelist:updated"] != 0 || events["pendinglist:updated"] != 0 {
		t.Errorf("无新设备不应推事件: %v", events)
	}
}

// 提交前代际校验失败：整轮丢弃，不写任何桶
func TestScanDiscardsWhenCommitInvalid(t *testing.T) {
	cm := newV4Manager(t)
	sc := NewScanner(&fakeDiscoverer{devices: []discover.Device{
		{IP: "192.168.1.31", DeviceID: "udp-31"},
	}}, cm)
	sc.health = fakeHealth(map[string]*Info{
		"192.168.1.31": {IP: "192.168.1.31", DeviceID: "192.168.1.31", DeviceIDFromIP: true},
	})
	emitted := 0
	sc.SetEmitter(func(string, any) { emitted++ })

	results, err := sc.Scan(context.Background(), func() bool { return false })
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if results != nil {
		t.Errorf("代际失效应返回 nil, got %v", results)
	}
	if len(cm.GetPendingDevices()) != 0 || len(cm.GetDeviceList()) != 0 {
		t.Error("代际失效不应写入任何列表")
	}
	if emitted != 0 {
		t.Errorf("代际失效不应推送事件, got %d", emitted)
	}
}

// 扫描期间模式已切走（不是 v4）：整轮丢弃，不写任何桶
func TestScanDiscardsWhenModeChanged(t *testing.T) {
	cm := newV4Manager(t)
	sc := NewScanner(&fakeDiscoverer{devices: []discover.Device{
		{IP: "192.168.1.41", DeviceID: "udp-41"},
	}}, cm)
	sc.health = fakeHealth(map[string]*Info{
		"192.168.1.41": {IP: "192.168.1.41", DeviceID: "192.168.1.41", DeviceIDFromIP: true},
	})
	if err := cm.SetDeployMode("local"); err != nil {
		t.Fatalf("SetDeployMode(local) 失败: %v", err)
	}

	results, err := sc.Scan(context.Background(), nil)
	if err != nil || results != nil {
		t.Fatalf("模式已切走应返回 (nil,nil), got (%v,%v)", results, err)
	}
	if len(cm.GetPendingDevices()) != 0 || len(cm.GetDeviceList()) != 0 {
		t.Error("模式已切走不应写入任何列表")
	}
}

// 空结果安全：UDP 无回复 → (nil, nil)，不写不推
func TestScanEmptyDiscovery(t *testing.T) {
	cm := newV4Manager(t)
	sc := NewScanner(&fakeDiscoverer{}, cm)
	emitted := 0
	sc.SetEmitter(func(string, any) { emitted++ })

	results, err := sc.Scan(context.Background(), nil)
	if err != nil || results != nil {
		t.Fatalf("空发现应返回 (nil,nil), got (%v,%v)", results, err)
	}
	if emitted != 0 || len(cm.GetPendingDevices()) != 0 {
		t.Error("空发现不应写库或推事件")
	}
}

// 发现失败透传错误
func TestScanDiscoveryError(t *testing.T) {
	cm := newV4Manager(t)
	sc := NewScanner(&fakeDiscoverer{err: errors.New("无广播地址")}, cm)
	if _, err := sc.Scan(context.Background(), nil); err == nil {
		t.Fatal("发现失败应返回错误")
	}
}

// V2/V3 名候选降级探测：不再按版本硬过滤丢弃，而是用短超时 /health 探测；
// 通过的与 v0 候选同路径落库（tebox 装在 V3 盒子上 /health 是通的），失败的静默丢弃
func TestScanProbesV2V3Fallback(t *testing.T) {
	cm := newV4Manager(t)
	sc := NewScanner(&fakeDiscoverer{devices: []discover.Device{
		{IP: "192.168.2.11", DeviceID: "udp-11", DeviceType: "Q1", DeviceName: "q1_v2", Version: "v2"},
		{IP: "192.168.2.12", DeviceID: "udp-12", DeviceType: "R1", DeviceName: "r1_v3", Version: "v3"},
		{IP: "192.168.2.13", DeviceID: "udp-13", DeviceType: "C1", DeviceName: "c1", Version: "v0"},
	}}, cm)
	quickProbed := make(map[string]bool)
	var quickMu sync.Mutex
	sc.health = fakeHealth(map[string]*Info{
		"192.168.2.13": {IP: "192.168.2.13", DeviceID: "192.168.2.13", DeviceIDFromIP: true},
	})
	sc.healthQuick = func(_ context.Context, ip string) (*Info, error) {
		quickMu.Lock()
		quickProbed[ip] = true
		quickMu.Unlock()
		if ip == "192.168.2.12" {
			return &Info{IP: ip, DeviceID: ip, DeviceIDFromIP: true}, nil
		}
		return nil, errors.New("/health 不可达")
	}

	results, err := sc.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	// v2/v3 名候选走短超时探测（.11 失败静默丢弃，.12 通过落库）
	quickMu.Lock()
	defer quickMu.Unlock()
	if !quickProbed["192.168.2.11"] || !quickProbed["192.168.2.12"] {
		t.Errorf("v2/v3 候选应走短超时探测, quickProbed = %v", quickProbed)
	}
	if len(results) != 2 {
		t.Fatalf("验证通过数 = %d, want 2（v2 失败丢弃 + v3 tebox 通过 + v0 通过）", len(results))
	}
	byID := map[string]Result{}
	for _, r := range results {
		byID[r.DeviceID] = r
	}
	// 通过降级探测的 V3 盒子正常进待添加，版本原样保留 v3
	got, ok := byID["192.168.2.12"]
	if !ok || !got.IsNew || got.Version != "v3" {
		t.Errorf("装了 tebox 的 V3 盒子应进待添加且保留 v3 版本: %+v", got)
	}
	pending := cm.GetPendingDevices()
	if len(pending) != 2 {
		t.Fatalf("待添加数 = %d, want 2（v3 tebox + v0）", len(pending))
	}
}

// 已在 v4 正式列表的候选按 IP 跳过，不重复探测（含 DeviceID 为空的旧条目兜底）
func TestScanSkipsDevicesAlreadyInList(t *testing.T) {
	cm := newV4Manager(t)
	// DeviceID 为空、以 IP 为主的旧条目：GetDeviceByIP 兜底命中
	cm.UpsertDevice(config.DeviceCache{DeviceID: "", IP: "192.168.2.21", State: 1})
	cm.UpsertDevice(config.DeviceCache{DeviceID: "192.168.2.22", IP: "192.168.2.22", State: 1})

	sc := NewScanner(&fakeDiscoverer{devices: []discover.Device{
		{IP: "192.168.2.21", DeviceID: "udp-21", DeviceName: "old", Version: "v3"},
		{IP: "192.168.2.22", DeviceID: "192.168.2.22", DeviceName: "known", Version: "v0"},
		{IP: "192.168.2.23", DeviceID: "udp-23", DeviceName: "new", Version: "v0"},
	}}, cm)
	var probed []string
	sc.health = func(_ context.Context, ip string) (*Info, error) {
		probed = append(probed, ip)
		return &Info{IP: ip, DeviceID: ip, DeviceIDFromIP: true}, nil
	}
	sc.healthQuick = func(_ context.Context, ip string) (*Info, error) {
		probed = append(probed, ip)
		return &Info{IP: ip, DeviceID: ip, DeviceIDFromIP: true}, nil
	}

	results, err := sc.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if len(probed) != 1 || probed[0] != "192.168.2.23" {
		t.Errorf("应只探测不在列表的 192.168.2.23, probed = %v", probed)
	}
	if len(results) != 1 || results[0].IP != "192.168.2.23" || !results[0].IsNew {
		t.Errorf("结果应只含新设备: %+v", results)
	}
	if len(cm.GetPendingDevices()) != 1 {
		t.Errorf("待添加数 = %d, want 1", len(cm.GetPendingDevices()))
	}
}

// 不能漏掉 V4：v0/v1/空 Version 的候选必须全部走 /health 验证
// （V4 设备的广播名后缀格式未确认，可能被 ParseVersion 判为 v0 裸名）
func TestScanValidatesV0V1AndEmptyVersion(t *testing.T) {
	cm := newV4Manager(t)
	sc := NewScanner(&fakeDiscoverer{devices: []discover.Device{
		{IP: "192.168.3.11", DeviceID: "udp-11", DeviceName: "c1", Version: "v0"},
		{IP: "192.168.3.12", DeviceID: "udp-12", DeviceName: "p1_10", Version: "v1"},
		{IP: "192.168.3.13", DeviceID: "udp-13", DeviceName: "", Version: ""},
	}}, cm)
	var probed []string
	sc.health = func(_ context.Context, ip string) (*Info, error) {
		probed = append(probed, ip)
		return &Info{IP: ip, DeviceID: ip, DeviceIDFromIP: true}, nil
	}
	sc.healthQuick = func(_ context.Context, ip string) (*Info, error) {
		probed = append(probed, ip)
		return &Info{IP: ip, DeviceID: ip, DeviceIDFromIP: true}, nil
	}

	results, err := sc.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan 失败: %v", err)
	}
	if len(probed) != 3 {
		t.Errorf("v0/v1/空版本候选都应探测, probed = %v", probed)
	}
	if len(results) != 3 {
		t.Errorf("验证通过数 = %d, want 3", len(results))
	}
}

// ProbeIPs：手动添加的定向探测，返回成功设备与失败 IP（顺序稳定）
func TestProbeIPs(t *testing.T) {
	cm := newV4Manager(t)
	sc := NewScanner(&fakeDiscoverer{}, cm)
	sc.health = fakeHealth(map[string]*Info{
		"10.0.0.2": {IP: "10.0.0.2", DeviceID: "10.0.0.2", DeviceIDFromIP: true},
	})

	found, failed := sc.ProbeIPs(context.Background(), []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"})
	if len(found) != 1 || found[0].DeviceID != "10.0.0.2" {
		t.Fatalf("found 不符: %+v", found)
	}
	if len(failed) != 2 || failed[0] != "10.0.0.1" || failed[1] != "10.0.0.3" {
		t.Errorf("failed 不符: %v", failed)
	}
}
