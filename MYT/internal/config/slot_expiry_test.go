package config

import "testing"

// TestMergeSlotExpiryPreservesOtherDevices 验证合并语义：
// 已有 A、B 两台设备的缓存，合并结果只含 B 时，A 的旧缓存条目保留不丢。
func TestMergeSlotExpiryPreservesOtherDevices(t *testing.T) {
	m := tempManager(t)

	m.MergeSlotExpiry(map[string][]SlotExpiry{
		"devA": {{Index: 1, ExpireTime: "100", State: 0}},
		"devB": {{Index: 2, ExpireTime: "200", State: 1}},
	})

	// 只回传 devB 的查询结果（模拟 devA 查询失败被排除）
	m.MergeSlotExpiry(map[string][]SlotExpiry{
		"devB": {{Index: 2, ExpireTime: "999", State: 2}},
	})

	got := m.GetSlotExpiry()
	if len(got) != 2 {
		t.Fatalf("GetSlotExpiry() len = %d, want 2 (devA 应保留)", len(got))
	}
	if got["devA"][0].ExpireTime != "100" {
		t.Errorf("devA ExpireTime = %q, want 保留旧值 100", got["devA"][0].ExpireTime)
	}
	if got["devB"][0].ExpireTime != "999" {
		t.Errorf("devB ExpireTime = %q, want 覆盖为新值 999", got["devB"][0].ExpireTime)
	}
}
