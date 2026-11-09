package main

import (
	"testing"

	"github.com/moyunteng/myt/internal/cloudapi"
	"github.com/moyunteng/myt/internal/config"
)

// TestBuildPublicDeviceListPreservesMetrics 阶段1 全量替换必须保留旧条目的指标字段。
// 30s 轮询 SetDeviceList 裸替换若抹掉磁盘/内存（WS system/stats 20s 写回），
// 存储条会在「替换清零 ↔ stats 回填」间循环闪烁（公网模式实测现象）。
func TestBuildPublicDeviceListPreservesMetrics(t *testing.T) {
	old := []config.DeviceCache{
		{
			DeviceID: "pub1", DeviceType: "r1q", State: 1, Order: 3,
			DiskUsed: 100, DiskTotal: 400, MemUsed: 5, MemTotal: 16, BindStatus: 1,
		},
	}
	candidates := []cloudapi.GatewayDevice{{DeviceID: "pub1", Online: true}, {DeviceID: "pub2", Online: true}}

	got := buildPublicDeviceList(candidates, old)

	if len(got) != 2 {
		t.Fatalf("应返回 2 台，实际 %d", len(got))
	}
	byID := map[string]config.DeviceCache{}
	for _, d := range got {
		byID[d.DeviceID] = d
	}

	// pub1：指标/型号/勾选/状态全部保留，身份字段按公网约定覆盖
	p1 := byID["pub1"]
	if p1.DiskUsed != 100 || p1.DiskTotal != 400 || p1.MemUsed != 5 || p1.MemTotal != 16 || p1.BindStatus != 1 {
		t.Errorf("pub1 指标字段被抹掉：disk=%d/%d mem=%d/%d bind=%d", p1.DiskUsed, p1.DiskTotal, p1.MemUsed, p1.MemTotal, p1.BindStatus)
	}
	if p1.DeviceType != "r1q" || p1.Order != 3 || p1.State != 1 {
		t.Errorf("pub1 型号/勾选/状态未保留：type=%s order=%d state=%d", p1.DeviceType, p1.Order, p1.State)
	}
	if p1.IP != "" || p1.DeviceName != "pub1" || p1.Version != "" {
		t.Errorf("pub1 身份字段未按公网约定覆盖：ip=%q name=%q ver=%q", p1.IP, p1.DeviceName, p1.Version)
	}

	// pub2：新设备零值起步（型号留空由阶段2补全），不得带出旧值
	p2 := byID["pub2"]
	if p2.DeviceType != "" || p2.DiskUsed != 0 || p2.MemUsed != 0 || p2.Order != 0 {
		t.Errorf("pub2 新设备应零值起步：type=%q disk=%d mem=%d order=%d", p2.DeviceType, p2.DiskUsed, p2.MemUsed, p2.Order)
	}
}

// TestBuildPublicDeviceListDropsRemoved 网关列表中已不存在的设备不保留（下架设备不出现在新列表）。
func TestBuildPublicDeviceListDropsRemoved(t *testing.T) {
	old := []config.DeviceCache{
		{DeviceID: "pub1", DiskUsed: 100},
		{DeviceID: "gone", DiskUsed: 50},
	}
	got := buildPublicDeviceList([]cloudapi.GatewayDevice{{DeviceID: "pub1", Online: true}}, old)
	if len(got) != 1 || got[0].DeviceID != "pub1" {
		t.Fatalf("已下架设备应被移除，实际 %v", got)
	}
}

// TestBuildPublicDeviceListKeepsOfflineKnownDevice 网关 online=false 只表示设备侧此刻断连，
// 不等于下架：已在列表中的设备必须保留（State 置离线），否则一次网关抖动就把设备从列表抹掉，
// 5s 后落档写盘，用户看到设备消失。
func TestBuildPublicDeviceListKeepsOfflineKnownDevice(t *testing.T) {
	old := []config.DeviceCache{
		{DeviceID: "pub1", State: 0, DeviceType: "r1q", DiskUsed: 100, Order: 2},
	}
	got := buildPublicDeviceList([]cloudapi.GatewayDevice{{DeviceID: "pub1", Online: false}}, old)

	if len(got) != 1 || got[0].DeviceID != "pub1" {
		t.Fatalf("网关标记离线的已知设备应保留，实际 %v", got)
	}
	if got[0].State != 1 {
		t.Errorf("离线设备 State 应置 1，实际 %d", got[0].State)
	}
	// 型号/勾选/指标不因离线丢失
	if got[0].DeviceType != "r1q" || got[0].Order != 2 || got[0].DiskUsed != 100 {
		t.Errorf("离线设备的型号/勾选/指标不应丢失：type=%q order=%d disk=%d",
			got[0].DeviceType, got[0].Order, got[0].DiskUsed)
	}
}

// TestBuildPublicDeviceListSkipsOfflineUnknownDevice 从未进过列表的设备即使网关返回
// online=false 也不新增（避免把账号下长期离线设备灌进列表）。
func TestBuildPublicDeviceListSkipsOfflineUnknownDevice(t *testing.T) {
	got := buildPublicDeviceList([]cloudapi.GatewayDevice{
		{DeviceID: "online-new", Online: true},
		{DeviceID: "offline-new", Online: false},
	}, nil)

	if len(got) != 1 || got[0].DeviceID != "online-new" {
		t.Fatalf("离线新设备不应入列，实际 %v", got)
	}
}
