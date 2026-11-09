package config

import (
	"testing"
	"time"
)

// TestMergeAndroidListEmptyResultKeepsOld 修复 1：单设备空结果不清旧数据。
//
// mytsdk 忙时间歇返回 code=200 + data:[]，按「成功空列表」合并会把该主机容器
// 整体清空（截图全丢、列表闪断）。保护语义：缓存中该设备原有条目非空时，
// 空结果不覆盖；真实删光场景由 ReplaceAndroidListForDevice / RemoveAndroidsByDevice
// 等显式路径负责，不受本保护影响。
func TestMergeAndroidListEmptyResultKeepsOld(t *testing.T) {
	m := newTestManager(t)
	seedAndroids(m, "d1", "c1", "c2")
	seedAndroids(m, "d2", "x1")

	// 旧数据非空 + 新结果空 → 保留旧数据
	m.MergeAndroidList([]DeviceAndroidResult{
		{DeviceID: "d1", Androids: nil},
	}, time.Now(), map[string]string{"d1": "192.0.2.2"})
	if got := m.GetAndroidsByDevice("d1"); len(got) != 2 {
		t.Fatalf("空结果不应清掉旧数据，剩 %d 个", len(got))
	}
	if got := m.GetAndroidsByDevice("d2"); len(got) != 1 {
		t.Fatalf("其他设备不受影响，剩 %d 个", len(got))
	}

	// 新结果非空 → 正常覆盖（旧条目被替换）
	m.MergeAndroidList([]DeviceAndroidResult{
		{DeviceID: "d1", Androids: []AndroidInfo{{ID: "c9", DeviceID: "d1", Status: "running"}}},
	}, time.Now(), nil)
	if got := m.GetAndroidsByDevice("d1"); len(got) != 1 || got[0].ID != "c9" {
		t.Fatalf("非空结果应正常覆盖: %+v", got)
	}
}

// TestMergeAndroidListEmptyResultNoCacheWrites 修复 1 边界：
// 缓存中该设备本无条目时，空结果是真实事实（新设备/确实没有实例），照常处理
// （写入空结果、标记 updated，避免幽灵残留在 updated 集合语义里）。
func TestMergeAndroidListEmptyResultNoCacheWrites(t *testing.T) {
	m := newTestManager(t)

	// 缓存为空 + 空结果：不报保护日志、列表保持为空
	m.MergeAndroidList([]DeviceAndroidResult{
		{DeviceID: "d_new", Androids: nil},
	}, time.Now(), nil)
	if got := m.GetAndroidsByDevice("d_new"); len(got) != 0 {
		t.Fatalf("新设备空结果应照常处理，得到 %d 个", len(got))
	}

	// 新设备先拿到非空结果，再拿到空结果 → 触发保护保留
	seedAndroids(m, "d_new", "c1")
	m.MergeAndroidList([]DeviceAndroidResult{
		{DeviceID: "d_new", Androids: nil},
	}, time.Now(), nil)
	if got := m.GetAndroidsByDevice("d_new"); len(got) != 1 {
		t.Fatalf("有缓存后空结果应保留旧数据，剩 %d 个", len(got))
	}
}

// TestGetRunningAndroidsRunnableSet 修复 2：截图目标口径放宽为可运行集合。
//
// 对齐前端 RUNNABLE_STATUSES（running/starting/restarting/resetting，paused 归一
// starting）；created/exited/dead/removing 排除；无 IP 且无端口映射的纯名称条目
// 不入选（抓不到图）。
func TestGetRunningAndroidsRunnableSet(t *testing.T) {
	m := newTestManager(t)
	list := []AndroidInfo{
		{ID: "a1", DeviceID: "d1", IP: "10.0.0.1", Status: "running"},    // 入选
		{ID: "a2", DeviceID: "d1", IP: "10.0.0.1", Status: "starting"},   // 入选（抖动不再剔除）
		{ID: "a3", DeviceID: "d1", IP: "10.0.0.1", Status: "restarting"}, // 入选
		{ID: "a4", DeviceID: "d1", IP: "10.0.0.1", Status: "resetting"},  // 入选
		{ID: "a5", DeviceID: "d1", IP: "10.0.0.1", Status: "paused"},     // 入选（归一 starting）
		{ID: "a6", DeviceID: "d1", IP: "10.0.0.1", Status: "created"},    // 排除（未开机）
		{ID: "a7", DeviceID: "d1", IP: "10.0.0.1", Status: "exited"},     // 排除（关机）
		{ID: "a8", DeviceID: "d1", IP: "10.0.0.1", Status: "dead"},       // 排除
		{ID: "a9", DeviceID: "d1", IP: "10.0.0.1", Status: "removing"},   // 排除
		{ID: "a10", DeviceID: "d1", Status: "running"},                   // 排除（无 IP/端口，抓不到）
		{ID: "a11", DeviceID: "d1", Status: "running", PortBindings: map[string][]PortBinding{"9082/tcp": {{HostPort: "30001"}}}}, // 入选（有端口映射）
	}
	m.MergeAndroidList([]DeviceAndroidResult{{DeviceID: "d1", Androids: list}}, time.Time{}, nil)

	got := m.GetRunningAndroids()
	ids := make(map[string]bool, len(got))
	for _, a := range got {
		ids[a.ID] = true
	}
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5", "a11"} {
		if !ids[id] {
			t.Errorf("%s 应入选（可运行口径）", id)
		}
	}
	for _, id := range []string{"a6", "a7", "a8", "a9", "a10"} {
		if ids[id] {
			t.Errorf("%s 不应入选", id)
		}
	}
}

// TestMergeAndroidListMixedResults 混合批次：一台空结果受保护、另一台正常覆盖。
func TestMergeAndroidListMixedResults(t *testing.T) {
	m := newTestManager(t)
	seedAndroids(m, "d1", "c1", "c2")
	seedAndroids(m, "d2", "x1")

	m.MergeAndroidList([]DeviceAndroidResult{
		{DeviceID: "d1", Androids: nil}, // 空结果 → 保留
		{DeviceID: "d2", Androids: []AndroidInfo{{ID: "x9", DeviceID: "d2", Status: "running"}}},
	}, time.Now(), nil)

	if got := m.GetAndroidsByDevice("d1"); len(got) != 2 {
		t.Fatalf("d1 空结果应保留旧数据，剩 %d 个", len(got))
	}
	if got := m.GetAndroidsByDevice("d2"); len(got) != 1 || got[0].ID != "x9" {
		t.Fatalf("d2 非空结果应正常覆盖: %+v", got)
	}
}
