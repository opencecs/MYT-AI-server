package main

import (
	"testing"

	"github.com/moyunteng/myt/internal/config"
)

// newDiscoverSvcForTest 只注入 config 管理器——全选/全不选只依赖它。
func newDiscoverSvcForTest(t *testing.T, devices []config.DeviceCache) *DiscoverServiceWrapper {
	t.Helper()
	cm := config.NewManager(t.TempDir())
	cm.SetDeviceList(devices)
	return &DiscoverServiceWrapper{configManager: cm}
}

func orderMapOf(list []config.DeviceCache) map[string]int {
	m := make(map[string]int, len(list))
	for _, d := range list {
		m[d.DeviceID] = d.Order
	}
	return m
}

// TestSelectAllDevicesCoversNonOnlineDevices 「全选」必须覆盖离线与待认证(401)设备。
//
// 旧实现 SelectAllOnline 只给 State==0 的设备写 order，而前端「全选」把列表里
// 全部设备乐观置为勾选 —— 两边语义不一致，于是下一条按 order 回灌勾选的事件
// （devicestats 指标心跳，500ms 节流）就把离线/401 设备挤掉，
// 用户看到的现象就是"全选后 401 的设备选上又自动取消"。
func TestSelectAllDevicesCoversNonOnlineDevices(t *testing.T) {
	svc := newDiscoverSvcForTest(t, []config.DeviceCache{
		{DeviceID: "online", IP: "10.10.0.1", State: 0},
		{DeviceID: "offline", IP: "10.10.0.2", State: 1},
		{DeviceID: "authfail", IP: "10.10.0.3", State: 2}, // 设备接口 401 → 待认证
	})

	after := svc.SelectAllDevices()

	if len(after) != 3 {
		t.Fatalf("应返回全部 3 台设备的权威快照，实际 %d 台", len(after))
	}
	for _, d := range after {
		if d.Order == 0 {
			t.Errorf("设备 %s(state=%d) 未被勾选：全选仍在按在线状态过滤", d.DeviceID, d.State)
		}
	}
	// 返回值必须是写入之后的快照（前端据此收敛勾选，不能靠旧数据）
	if got := orderMapOf(svc.configManager.GetDeviceList()); len(got) != 3 {
		t.Fatalf("config 中的设备数应保持一致，实际 %d", len(got))
	}
}

// TestSelectAllDevicesKeepsExistingOrder 全选只补齐未勾选的设备，
// 不得重排已勾选设备的 order —— order 同时是勾选顺序/展示序，重排会让已选设备顺序跳掉。
func TestSelectAllDevicesKeepsExistingOrder(t *testing.T) {
	svc := newDiscoverSvcForTest(t, []config.DeviceCache{
		{DeviceID: "a", IP: "10.10.0.1", State: 0, Order: 7},
		{DeviceID: "b", IP: "10.10.0.2", State: 0, Order: 3},
		{DeviceID: "c", IP: "10.10.0.3", State: 1, Order: 0},
	})

	got := orderMapOf(svc.SelectAllDevices())

	if got["a"] != 7 || got["b"] != 3 {
		t.Errorf("已勾选设备的 order 被改动: a=%d b=%d（应保持 7/3）", got["a"], got["b"])
	}
	if got["c"] <= 7 {
		t.Errorf("新勾选设备 c 的 order=%d 应大于现有最大 order 7，否则与已有勾选顺序冲突", got["c"])
	}
}

// TestSelectAllDevicesIdempotent 重复全选不新增设备、不改变已勾选集合。
func TestSelectAllDevicesIdempotent(t *testing.T) {
	svc := newDiscoverSvcForTest(t, []config.DeviceCache{
		{DeviceID: "a", IP: "10.10.0.1", State: 0},
		{DeviceID: "b", IP: "10.10.0.2", State: 2},
	})

	first := orderMapOf(svc.SelectAllDevices())
	second := orderMapOf(svc.SelectAllDevices())

	if len(second) != len(first) {
		t.Fatalf("重复全选后设备数变了: %d → %d", len(first), len(second))
	}
	for id, order := range first {
		if second[id] != order {
			t.Errorf("重复全选改动了 %s 的 order: %d → %d（已勾选的不该再被改写）", id, order, second[id])
		}
	}
}

// TestDeselectAllClearsEveryDevice 全不选清空所有设备的 order（含离线/待认证），
// 与 SelectAllDevices 的"不看 State"语义保持对称。
func TestDeselectAllClearsEveryDevice(t *testing.T) {
	svc := newDiscoverSvcForTest(t, []config.DeviceCache{
		{DeviceID: "a", IP: "10.10.0.1", State: 0, Order: 1},
		{DeviceID: "b", IP: "10.10.0.2", State: 1, Order: 2},
		{DeviceID: "c", IP: "10.10.0.3", State: 2, Order: 3},
	})

	for _, d := range svc.DeselectAll() {
		if d.Order != 0 {
			t.Errorf("设备 %s(state=%d) 取消勾选后 order=%d，应为 0", d.DeviceID, d.State, d.Order)
		}
	}
	for id, order := range orderMapOf(svc.configManager.GetDeviceList()) {
		if order != 0 {
			t.Errorf("config 里设备 %s 的 order 未清空: %d", id, order)
		}
	}
}
