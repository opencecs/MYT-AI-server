package config

import (
	"sync"
	"testing"
)

// --- 设备列表 CRUD ---

func TestDeviceListCRUD(t *testing.T) {
	m := tempManager(t)

	devices := []DeviceCache{
		{DeviceID: "dev1", IP: "10.10.0.1", DeviceType: "R1S", DeviceName: "r1s_v3", State: 0, Order: 1},
		{DeviceID: "dev2", IP: "10.10.0.2", DeviceType: "Q1", DeviceName: "q1_v3", State: 1, Order: 0},
	}
	m.SetDeviceList(devices)
	got := m.GetDeviceList()
	if len(got) != 2 {
		t.Fatalf("GetDeviceList() len = %d, want 2", len(got))
	}
	if got[0].IP != "10.10.0.1" {
		t.Errorf("got[0].IP = %q, want %q", got[0].IP, "10.10.0.1")
	}

	var found *DeviceCache
	for i := range got {
		if got[i].DeviceID == "dev2" {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatal("device dev2 not found in list")
	}
	if found.IP != "10.10.0.2" {
		t.Errorf("dev2 IP = %q, want %q", found.IP, "10.10.0.2")
	}
}

func TestUpdateDeviceState(t *testing.T) {
	m := tempManager(t)

	m.SetDeviceList([]DeviceCache{
		{DeviceID: "dev1", IP: "10.10.0.1", State: 0},
	})

	m.UpdateDeviceState("dev1", 1)

	list := m.GetDeviceList()
	var found *DeviceCache
	for i := range list {
		if list[i].DeviceID == "dev1" {
			found = &list[i]
			break
		}
	}
	if found == nil {
		t.Fatal("dev1 not found after UpdateDeviceState")
	}
	if found.State != 1 {
		t.Errorf("State = %d, want 1", found.State)
	}

	m.UpdateDeviceState("nonexistent", 0)
}

// --- 备注测试 ---

func TestDeviceNotes(t *testing.T) {
	m := tempManager(t)

	m.SetDeviceNote("dev1", "测试设备")
	if got := m.GetDeviceNote("dev1"); got != "测试设备" {
		t.Errorf("GetDeviceNote = %q, want %q", got, "测试设备")
	}
	if got := m.GetDeviceNote("nonexistent"); got != "" {
		t.Errorf("GetDeviceNote(nonexistent) = %q, want empty", got)
	}
}

func TestAndroidNotes(t *testing.T) {
	m := tempManager(t)

	m.SetAndroidNote("android1", "测试云机")
	if got := m.GetAndroidNote("android1"); got != "测试云机" {
		t.Errorf("GetAndroidNote = %q, want %q", got, "测试云机")
	}
}

// --- 安卓列表测试 ---

func TestAndroidList(t *testing.T) {
	m := tempManager(t)

	androids := []AndroidInfo{
		{Name: "c1", Status: "running", IndexNum: 1, DeviceID: "dev1"},
		{Name: "c2", Status: "stopped", IndexNum: 2, DeviceID: "dev1"},
	}
	m.SetAndroidList(androids)
	got := m.GetAndroidList()
	if len(got) != 2 {
		t.Fatalf("GetAndroidList() len = %d, want 2", len(got))
	}
}

// --- 坑位到期测试 ---

func TestSlotExpiry(t *testing.T) {
	m := tempManager(t)

	expiry := map[string][]SlotExpiry{
		"dev1": {{Index: 1, ExpireTime: "2026-12-31", State: 0}},
	}
	m.MergeSlotExpiry(expiry)
	got := m.GetSlotExpiry()
	if len(got) != 1 {
		t.Fatalf("GetSlotExpiry() len = %d, want 1", len(got))
	}
	if got["dev1"][0].ExpireTime != "2026-12-31" {
		t.Errorf("ExpireTime = %q, want %q", got["dev1"][0].ExpireTime, "2026-12-31")
	}
}

// --- 并发安全测试 ---

func TestConcurrentAccess(t *testing.T) {
	m := tempManager(t)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			m.SetDeviceList([]DeviceCache{{DeviceID: "dev", IP: "10.10.0.1", Order: i}})
		}(i)
		go func() {
			defer wg.Done()
			_ = m.GetDeviceList()
		}()
	}
	wg.Wait()
}

// --- 设备密码测试 ---

func TestDevicePasswords(t *testing.T) {
	m := tempManager(t)

	if err := m.SetDevicePasswordInConfig("dev1", "secret123"); err != nil {
		t.Fatalf("SetDevicePasswordInConfig failed: %v", err)
	}
	pwdMap := m.GetDevicePasswordsMap()
	if pwdMap["dev1"] != "secret123" {
		t.Errorf("GetDevicePasswordsMap[dev1] = %q, want %q", pwdMap["dev1"], "secret123")
	}

	if err := m.SetDevicePasswordInConfig("dev1", ""); err != nil {
		t.Fatalf("SetDevicePasswordInConfig(empty) failed: %v", err)
	}
	pwdMap = m.GetDevicePasswordsMap()
	if _, exists := pwdMap["dev1"]; exists {
		t.Error("dev1 should be deleted after setting empty password")
	}
}
