package config

import (
	"testing"
	"time"
)

// ReplaceAndroidListForDevice：权威全量替换（允许清空），只影响目标设备。
// 与 MergeAndroidList 的「合并为空保留旧数据」保护互补：WS 快照/REST 全量是事实，
// 设备上最后一台实例被删除时必须清空，否则幽灵条目永久残留。
func TestReplaceAndroidListForDevice(t *testing.T) {
	m := tempManager(t)
	if err := m.SetDeployMode("v4"); err != nil {
		t.Fatal(err)
	}
	m.SetAndroidList([]AndroidInfo{
		{ID: "be/a", Name: "a", DeviceID: "10.0.0.1", IndexNum: 1},
		{ID: "be/b", Name: "b", DeviceID: "10.0.0.1", IndexNum: 2},
		{ID: "be/x", Name: "x", DeviceID: "10.0.0.2", IndexNum: 1},
	})

	// 1) 用一台实例替换：同设备旧条目被清掉，其他设备不受影响
	m.ReplaceAndroidListForDevice("10.0.0.1", []AndroidInfo{
		{ID: "be/c", Name: "c", DeviceID: "10.0.0.1", IndexNum: 1},
	})
	got := m.GetAndroidList()
	if len(got) != 2 {
		t.Fatalf("替换后条数 = %d, want 2: %+v", len(got), got)
	}
	ids := map[string]bool{}
	for _, a := range got {
		ids[a.ID] = true
	}
	if !ids["be/c"] || !ids["be/x"] {
		t.Errorf("替换结果不符: %+v", got)
	}

	// 2) 空列表替换 = 清空该设备（这正是 MergeAndroidList 做不到的）
	m.ReplaceAndroidListForDevice("10.0.0.1", nil)
	got = m.GetAndroidList()
	if len(got) != 1 || got[0].ID != "be/x" {
		t.Fatalf("清空后应只剩另一台设备的条目: %+v", got)
	}

	// 3) 空 deviceID 直接忽略（防误清全部）
	m.ReplaceAndroidListForDevice("", nil)
	if len(m.GetAndroidList()) != 1 {
		t.Error("空 deviceID 不应改动缓存")
	}
}

// MergeAndroidList 与 ReplaceAndroidListForDevice 的语义差异（回归防护）：
// Merge 合并为空时保留旧数据，Replace 则清空。
func TestMergeKeepsOldButReplaceClears(t *testing.T) {
	m := tempManager(t)
	if err := m.SetDeployMode("v4"); err != nil {
		t.Fatal(err)
	}
	m.SetAndroidList([]AndroidInfo{{ID: "be/a", DeviceID: "10.0.0.1"}})

	m.MergeAndroidList([]DeviceAndroidResult{{DeviceID: "10.0.0.1", Androids: nil}}, time.Time{}, nil)
	if n := len(m.GetAndroidList()); n != 1 {
		t.Errorf("Merge 合并为空应保留旧数据，实际 %d 条", n)
	}

	m.ReplaceAndroidListForDevice("10.0.0.1", nil)
	if n := len(m.GetAndroidList()); n != 0 {
		t.Errorf("Replace 空列表应清空，实际 %d 条", n)
	}
}
