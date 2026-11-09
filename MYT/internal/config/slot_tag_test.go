package config

import "testing"

// --- 坑位标签 ---

func TestSlotTagsCRUD(t *testing.T) {
	m := tempManager(t)

	m.SetSlotTags("dev1", 3, []string{"抖音", "养号"})
	got := m.GetSlotTags("dev1", 3)
	if len(got) != 2 || got[0] != "抖音" || got[1] != "养号" {
		t.Fatalf("GetSlotTags = %v, want [抖音 养号]", got)
	}

	if got := m.GetSlotTags("dev1", 4); got != nil {
		t.Errorf("未打标签的坑位应返回 nil, got %v", got)
	}

	// 覆盖写入
	m.SetSlotTags("dev1", 3, []string{"快手"})
	if got := m.GetSlotTags("dev1", 3); len(got) != 1 || got[0] != "快手" {
		t.Errorf("覆盖写入后 = %v, want [快手]", got)
	}

	// 空集合 = 删除该坑位标签
	m.SetSlotTags("dev1", 3, nil)
	if got := m.GetSlotTags("dev1", 3); got != nil {
		t.Errorf("清空后应返回 nil, got %v", got)
	}
}

func TestSlotTagsNormalize(t *testing.T) {
	m := tempManager(t)

	m.SetSlotTags("dev1", 1, []string{" 抖音 ", "", "抖音", "  ", "养号"})
	got := m.GetSlotTags("dev1", 1)
	if len(got) != 2 || got[0] != "抖音" || got[1] != "养号" {
		t.Fatalf("归一化后 = %v, want [抖音 养号]（去空白项 + 去重 + 保留原顺序）", got)
	}

	// 全是空白项 = 等价于清空
	m.SetSlotTags("dev1", 2, []string{"  ", ""})
	if got := m.GetSlotTags("dev1", 2); got != nil {
		t.Errorf("全空白项应视为清空, got %v", got)
	}
}

func TestSlotTagsKeyIsolation(t *testing.T) {
	m := tempManager(t)

	m.SetSlotTags("dev1", 1, []string{"A"})
	m.SetSlotTags("dev1", 2, []string{"B"})
	m.SetSlotTags("dev2", 1, []string{"C"})

	all := m.GetAllSlotTags()
	if len(all) != 3 {
		t.Fatalf("GetAllSlotTags len = %d, want 3", len(all))
	}
	if all[SlotTagKey("dev1", 1)][0] != "A" || all[SlotTagKey("dev1", 2)][0] != "B" || all[SlotTagKey("dev2", 1)][0] != "C" {
		t.Errorf("坑位标签互相串位: %v", all)
	}

	// 返回值必须是副本：外部改写不得污染内部状态
	all[SlotTagKey("dev1", 1)][0] = "被篡改"
	if got := m.GetSlotTags("dev1", 1); got[0] != "A" {
		t.Errorf("GetAllSlotTags 未返回副本，内部状态被外部改写: %v", got)
	}
}

func TestSlotTagsPersist(t *testing.T) {
	m := tempManager(t)
	m.SetSlotTags("dev1", 5, []string{"抖音"})
	// 走真实落档路径（flushDirty）：若 config.go 少接了 slotTagsDirty/snapshot/entries
	// 任一环，此处不会写盘，重启后标签即丢失 —— 本用例同时是接线完整性回归
	m.flushDirty()

	// 模拟重启：新 Manager 从同一目录加载
	reloaded := NewManager(m.root)
	if err := reloaded.LoadAll(); err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}
	got := reloaded.GetSlotTags("dev1", 5)
	if len(got) != 1 || got[0] != "抖音" {
		t.Fatalf("重启后标签丢失: %v", got)
	}
}

func TestRemoveDeviceSlotTags(t *testing.T) {
	m := tempManager(t)

	// "dev" 是 "dev1" 的前缀：前缀清理不能误伤（键以 "#" 分隔）
	m.SetSlotTags("dev", 1, []string{"X"})
	m.SetSlotTags("dev1", 1, []string{"Y"})
	m.SetSlotTags("dev2", 1, []string{"Z"})

	m.RemoveDeviceSlotTags("dev1")

	if got := m.GetSlotTags("dev1", 1); got != nil {
		t.Errorf("dev1 标签未清理: %v", got)
	}
	if got := m.GetSlotTags("dev", 1); len(got) != 1 || got[0] != "X" {
		t.Errorf("前缀相近的 dev 被误删: %v", got)
	}
	if got := m.GetSlotTags("dev2", 1); len(got) != 1 || got[0] != "Z" {
		t.Errorf("dev2 被误删: %v", got)
	}
}

func TestRemoveDeviceClearsSlotTags(t *testing.T) {
	m := tempManager(t)

	m.SetDeviceList([]DeviceCache{{DeviceID: "dev1", IP: "10.10.0.1", State: 0}})
	m.SetSlotTags("dev1", 1, []string{"抖音"})

	m.RemoveDevice("dev1")

	if got := m.GetSlotTags("dev1", 1); got != nil {
		t.Errorf("删设备后标签未清理: %v", got)
	}
	if len(m.GetDeviceList()) != 0 {
		t.Errorf("设备未被删除")
	}
}
