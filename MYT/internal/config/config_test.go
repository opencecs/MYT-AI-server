package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// tempManager 创建临时目录的 Manager
func tempManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m := NewManager(dir)
	if err := m.InitDirs(); err != nil {
		t.Fatalf("InitDirs failed: %v", err)
	}
	return m
}

// --- 目录结构测试 ---

func TestInitDirs(t *testing.T) {
	m := tempManager(t)

	expectedDirs := []string{
		filepath.Join(m.root, "config"),
		filepath.Join(m.root, "data", "images"),
		filepath.Join(m.root, "data", "model"),
		filepath.Join(m.root, "cache"),
		filepath.Join(m.root, "logs"),
	}
	for _, d := range expectedDirs {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			t.Errorf("directory %s not created", d)
		}
	}
}

func TestDirHelpers(t *testing.T) {
	m := tempManager(t)

	if m.ConfigDir() != filepath.Join(m.root, "config") {
		t.Errorf("ConfigDir = %q, want %q", m.ConfigDir(), filepath.Join(m.root, "config"))
	}
	if m.DataDir() != filepath.Join(m.root, "data") {
		t.Errorf("DataDir = %q, want %q", m.DataDir(), filepath.Join(m.root, "data"))
	}
	if m.CacheDir() != filepath.Join(m.root, "cache") {
		t.Errorf("CacheDir = %q, want %q", m.CacheDir(), filepath.Join(m.root, "cache"))
	}
	if m.LogsDir() != filepath.Join(m.root, "logs") {
		t.Errorf("LogsDir = %q, want %q", m.LogsDir(), filepath.Join(m.root, "logs"))
	}
	if m.RootDir() != m.root {
		t.Errorf("RootDir = %q, want %q", m.RootDir(), m.root)
	}
}

// --- 基础配置读写测试 ---

func TestLoadNoConfig(t *testing.T) {
	m := tempManager(t)
	// config.json 不存在时应返回 nil
	if err := m.Load(); err != nil {
		t.Errorf("Load() with no file = %v, want nil", err)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	m := tempManager(t)

	m.SetToken("test-token-123")
	m.SetAuthCredentials("admin", "pass123")
	if err := m.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// 重新创建 Manager 加载
	m2 := NewManager(m.root)
	if err := m2.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if m2.GetToken() != "test-token-123" {
		t.Errorf("Token = %q, want %q", m2.GetToken(), "test-token-123")
	}
	uname, pwd := m2.GetAuthCredentials()
	if uname != "admin" || pwd != "pass123" {
		t.Errorf("AuthCredentials = (%q, %q), want (admin, pass123)", uname, pwd)
	}
}

func TestSetAndClearAuthCredentials(t *testing.T) {
	m := tempManager(t)

	m.SetAuthCredentials("user1", "pwd1")
	uname, pwd := m.GetAuthCredentials()
	if uname != "user1" || pwd != "pwd1" {
		t.Errorf("got (%q, %q), want (user1, pwd1)", uname, pwd)
	}

	m.ClearAuthCredentials()
	uname, pwd = m.GetAuthCredentials()
	if uname != "" || pwd != "" {
		t.Errorf("after Clear: got (%q, %q), want empty", uname, pwd)
	}
}

// --- 基础配置读写测试 ---

func TestFlushDirty(t *testing.T) {
	m := tempManager(t)

	// 设置数据但不启动 flush loop
	m.SetDeviceList([]DeviceCache{
		{DeviceID: "dev1", IP: "10.10.0.1"},
	})
	m.SetDeviceNote("dev1", "note1")

	// 手动触发 flush
	m.flushDirty()

	// 验证文件已写入
	data, err := os.ReadFile(m.deviceListLocalPath())
	if err != nil {
		t.Fatalf("device_list.json not written: %v", err)
	}
	var devices []DeviceCache
	if err := json.Unmarshal(data, &devices); err != nil {
		t.Fatalf("parse device_list.json: %v", err)
	}
	if len(devices) != 1 || devices[0].DeviceID != "dev1" {
		t.Errorf("device_list.json content unexpected: %v", devices)
	}

	data, err = os.ReadFile(m.deviceNotesPath())
	if err != nil {
		t.Fatalf("device_notes.json not written: %v", err)
	}
	var notes map[string]string
	if err := json.Unmarshal(data, &notes); err != nil {
		t.Fatalf("parse device_notes.json: %v", err)
	}
	if notes["dev1"] != "note1" {
		t.Errorf("device_notes.json content unexpected: %v", notes)
	}
}

func TestFlushLoop(t *testing.T) {
	m := tempManager(t)

	m.StartFlushLoop()
	defer m.StopFlushLoop()

	m.SetDeviceList([]DeviceCache{
		{DeviceID: "dev1", IP: "10.10.0.1"},
	})

	// 等待 flush loop 写入（最多等 6 秒）
	deadline := time.After(6 * time.Second)
	for {
		data, err := os.ReadFile(m.deviceListLocalPath())
		if err == nil && len(data) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("flush loop did not write device_list.json within 6s")
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func TestFlushNoDirty(t *testing.T) {
	m := tempManager(t)
	// 没有脏数据时 flush 应该直接返回
	m.flushDirty()
	// 不应写入任何文件（device_list.json 不应存在）
	if _, err := os.Stat(m.deviceListLocalPath()); !os.IsNotExist(err) {
		t.Error("device_list.json should not exist when no dirty data")
	}
}

// --- 记住凭证测试 ---

func TestLoadAll(t *testing.T) {
	m := tempManager(t)

	// 先写入数据
	m.SetDeviceList([]DeviceCache{{DeviceID: "dev1", IP: "10.10.0.1"}})
	m.SetDeviceNote("dev1", "note1")
	m.SetAndroidList([]AndroidInfo{{Name: "c1", Status: "running"}})
	m.SetAndroidNote("c1", "android-note")
	m.MergeSlotExpiry(map[string][]SlotExpiry{"dev1": {{Index: 1}}})
	m.flushDirty()

	// 新建 Manager 加载
	m2 := NewManager(m.root)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("LoadAll() = %v", err)
	}

	devices := m2.GetDeviceList()
	if len(devices) != 1 || devices[0].DeviceID != "dev1" {
		t.Errorf("loaded device list unexpected: %v", devices)
	}
	if m2.GetDeviceNote("dev1") != "note1" {
		t.Errorf("loaded device note = %q, want %q", m2.GetDeviceNote("dev1"), "note1")
	}
	androids := m2.GetAndroidList()
	if len(androids) != 1 || androids[0].Name != "c1" {
		t.Errorf("loaded android list unexpected: %v", androids)
	}
	if m2.GetAndroidNote("c1") != "android-note" {
		t.Errorf("loaded android note = %q, want %q", m2.GetAndroidNote("c1"), "android-note")
	}
}

// --- 旧格式迁移测试 ---

func TestMigrateDeviceCache(t *testing.T) {
	m := tempManager(t)

	// 写入旧格式的 config.json
	oldConfig := map[string]interface{}{
		"device_cache": []map[string]interface{}{
			{"device_id": "old1", "ip": "10.10.0.100", "device_type": "P1", "device_name": "p1_v3", "state": 0, "order": 1},
		},
	}
	data, _ := json.MarshalIndent(oldConfig, "", "  ")
	os.WriteFile(filepath.Join(m.root, "config", "config.json"), data, 0644)

	if err := m.Load(); err != nil {
		t.Fatalf("Load() with old format = %v", err)
	}

	got := m.GetDeviceList()
	if len(got) != 1 || got[0].DeviceID != "old1" {
		t.Errorf("migrated device list = %v, want old1", got)
	}
}

// --- 镜像缓存测试 ---

func TestMirrorCache(t *testing.T) {
	m := tempManager(t)

	items := []util.MirrorItem{
		{ID: "img1", Name: "测试镜像", URL: "http://example.com/img.tar"},
	}
	m.SetMirrorCache(items)
	got := m.GetMirrorCache()
	if len(got) != 1 || got[0].ID != "img1" {
		t.Errorf("GetMirrorCache = %v, want img1", got)
	}
}

// --- 记住凭证测试 ---

func TestRememberedCredentials(t *testing.T) {
	m := tempManager(t)

	m.SetRememberedCredentials("user@test.com", "mypassword")
	uname, pwd := m.GetRememberedCredentials()
	if uname != "user@test.com" || pwd != "mypassword" {
		t.Errorf("got (%q, %q), want (user@test.com, mypassword)", uname, pwd)
	}

	m.ClearRememberedCredentials()
	uname, pwd = m.GetRememberedCredentials()
	if uname != "" || pwd != "" {
		t.Errorf("after clear: got (%q, %q), want empty", uname, pwd)
	}
}
