package config

import (
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m := NewManager(dir)
	if err := m.InitDirs(); err != nil {
		t.Fatal(err)
	}
	return m
}

func seedAndroids(m *Manager, deviceID string, ids ...string) {
	list := make([]AndroidInfo, 0, len(ids))
	for _, id := range ids {
		list = append(list, AndroidInfo{ID: id, DeviceID: deviceID, Status: "running", CPUUsage: 1})
	}
	m.MergeAndroidList([]DeviceAndroidResult{{DeviceID: deviceID, Androids: list}}, time.Time{}, nil)
}

func TestPatchAndroidStatus(t *testing.T) {
	m := newTestManager(t)
	seedAndroids(m, "d1", "c1", "c2")

	if !m.PatchAndroidStatus("d1", "c1", "stopped") {
		t.Fatal("c1 应命中")
	}
	got := m.GetAndroidsByDevice("d1")
	if len(got) != 2 || got[0].Status != "stopped" || got[1].Status != "running" {
		t.Fatalf("补丁后状态错误: %+v", got)
	}

	// 未命中：容器 ID 不存在
	if m.PatchAndroidStatus("d1", "c999", "stopped") {
		t.Fatal("c999 不应命中")
	}
	// 未命中：设备 ID 不匹配
	if m.PatchAndroidStatus("d2", "c1", "stopped") {
		t.Fatal("其他设备的 c1 不应命中")
	}

	// 其他设备的数据不受影响
	seedAndroids(m, "d2", "x1")
	m.PatchAndroidStatus("d1", "c1", "starting")
	if m.GetAndroidsByDevice("d2")[0].Status != "running" {
		t.Fatal("d2 数据不应被 d1 的补丁影响")
	}
}

func TestPatchAndroidStats(t *testing.T) {
	m := newTestManager(t)
	seedAndroids(m, "d1", "c1", "c2")

	patches := []AndroidStatsPatch{
		{ContainerID: "c1", CPUUsage: 12.5, MemUsage: 100, DiskUsage: 200},
		{ContainerID: "c999", CPUUsage: 9}, // 不存在：忽略
	}
	if hit := m.PatchAndroidStats("d1", patches); hit != 1 {
		t.Fatalf("应命中 1 个，得到 %d", hit)
	}
	got := m.GetAndroidsByDevice("d1")
	if got[0].CPUUsage != 12.5 || got[0].MemUsage != 100 || got[0].DiskUsage != 200 {
		t.Fatalf("c1 stats 未更新: %+v", got[0])
	}
	if got[1].CPUUsage != 1 {
		t.Fatalf("c2 不应被更新: %+v", got[1])
	}

	// 空补丁
	if hit := m.PatchAndroidStats("d1", nil); hit != 0 {
		t.Fatal("空补丁应返回 0")
	}
}

func TestRemoveAndroidByContainer(t *testing.T) {
	m := newTestManager(t)
	seedAndroids(m, "d1", "c1", "c2", "c3")

	if !m.RemoveAndroidByContainer("d1", "c2") {
		t.Fatal("c2 应命中移除")
	}
	got := m.GetAndroidsByDevice("d1")
	if len(got) != 2 {
		t.Fatalf("移除后应剩 2 个，得到 %d", len(got))
	}
	for _, a := range got {
		if a.ID == "c2" {
			t.Fatal("c2 不应存在")
		}
	}

	// 二次移除：不命中
	if m.RemoveAndroidByContainer("d1", "c2") {
		t.Fatal("已移除的容器不应再命中")
	}
	// 其他设备不受影响
	seedAndroids(m, "d2", "c2")
	m.RemoveAndroidByContainer("d1", "c2")
	if len(m.GetAndroidsByDevice("d2")) != 1 {
		t.Fatal("d2 的 c2 不应被 d1 的移除影响")
	}
}

func TestRemoveAndroidsByDevice(t *testing.T) {
	m := newTestManager(t)
	seedAndroids(m, "d1", "c1", "c2")
	seedAndroids(m, "d2", "x1")

	// 多设备场景：只清 d1，d2 保留
	if n := m.RemoveAndroidsByDevice("d1"); n != 2 {
		t.Fatalf("应移除 2 个，得到 %d", n)
	}
	if got := m.GetAndroidsByDevice("d1"); len(got) != 0 {
		t.Fatalf("d1 应被清空，剩 %d 个", len(got))
	}
	if got := m.GetAndroidsByDevice("d2"); len(got) != 1 {
		t.Fatalf("d2 不应被影响，剩 %d 个", len(got))
	}

	// 幂等：重复清已空的设备返回 0
	if n := m.RemoveAndroidsByDevice("d1"); n != 0 {
		t.Fatalf("重复清应返回 0，得到 %d", n)
	}
	// 未存在过的设备：返回 0
	if n := m.RemoveAndroidsByDevice("nope"); n != 0 {
		t.Fatalf("不存在设备应返回 0，得到 %d", n)
	}

	// 清最后一个设备：列表允许变空（显式移除不受 MergeAndroidList 空保护约束）
	if n := m.RemoveAndroidsByDevice("d2"); n != 1 {
		t.Fatalf("应移除 1 个，得到 %d", n)
	}
	if all := m.GetAndroidList(); len(all) != 0 {
		t.Fatalf("清空后列表应为空，剩 %d 个", len(all))
	}
}
