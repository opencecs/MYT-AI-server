package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 容器列表按部署模式隔离：各模式独立文件、切模式互不覆盖、切回来数据还在。
// 背景：容器 key 前缀随模式变化（local="IP_indexNum"，public="device_id_indexNum"），
// 共用一份 android_list.json 会让切模式后的截图/投屏 key 不匹配，且清空会互相污染。

func readAndroidFile(t *testing.T, path string) []AndroidInfo {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	var list []AndroidInfo
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	return list
}

// 各模式容器写入不同文件，切换后读取的是各自模式的数据
func TestAndroidListIsolatedPerMode(t *testing.T) {
	m := tempManager(t)

	if err := m.SetDeployMode("local"); err != nil {
		t.Fatal(err)
	}
	m.SetAndroidList([]AndroidInfo{{ID: "local-c1", DeviceID: "ip-1"}})

	if err := m.SetDeployMode("public"); err != nil {
		t.Fatal(err)
	}
	m.SetAndroidList([]AndroidInfo{{ID: "pub-c1", DeviceID: "dev-1"}})

	if err := m.SetDeployMode("v2"); err != nil {
		t.Fatal(err)
	}
	m.SetAndroidList([]AndroidInfo{{ID: "v2-c1", DeviceID: "host-1"}})

	m.flushDirty()

	if got := readAndroidFile(t, m.androidListLocalPath()); len(got) != 1 || got[0].ID != "local-c1" {
		t.Fatalf("local 文件内容不符: %v", got)
	}
	if got := readAndroidFile(t, m.androidListPublicPath()); len(got) != 1 || got[0].ID != "pub-c1" {
		t.Fatalf("public 文件内容不符: %v", got)
	}
	if got := readAndroidFile(t, m.androidListV2Path()); len(got) != 1 || got[0].ID != "v2-c1" {
		t.Fatalf("v2 文件内容不符: %v", got)
	}

	// 切回 local：读到的仍是 local 自己那份（不被 public/v2 覆盖，也没被清空）
	if err := m.SetDeployMode("local"); err != nil {
		t.Fatal(err)
	}
	if got := m.GetAndroidList(); len(got) != 1 || got[0].ID != "local-c1" {
		t.Fatalf("切回 local 应保留原容器数据，实际 %v", got)
	}
}

// 各模式独立加载：LoadAll 恢复四份数据，互不串台
func TestAndroidListLoadAllPerMode(t *testing.T) {
	m := tempManager(t)
	write := func(path string, list []AndroidInfo) {
		data, err := json.Marshal(list)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(m.androidListLocalPath(), []AndroidInfo{{ID: "l"}})
	write(m.androidListPublicPath(), []AndroidInfo{{ID: "p"}})
	write(m.androidListV2Path(), []AndroidInfo{{ID: "v2"}})
	write(m.androidListV4Path(), []AndroidInfo{{ID: "v4"}})

	m2 := NewManager(m.root)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}
	checks := []struct {
		mode string
		want string
	}{
		{"local", "l"}, {"public", "p"}, {"v2", "v2"}, {"v4", "v4"},
	}
	for _, c := range checks {
		if err := m2.SetDeployMode(c.mode); err != nil {
			t.Fatal(err)
		}
		got := m2.GetAndroidList()
		if len(got) != 1 || got[0].ID != c.want {
			t.Fatalf("%s 模式加载结果不符: %v", c.mode, got)
		}
	}
}

// 旧共享 android_list.json 迁移：当前模式文件不存在时迁入当前模式，旧文件改名 .bak
func TestMigrateLegacyAndroidList(t *testing.T) {
	m := tempManager(t)
	legacy := m.legacyAndroidListPath()
	data, _ := json.Marshal([]AndroidInfo{{ID: "legacy-c1", DeviceID: "dev-1"}})
	if err := os.WriteFile(legacy, data, 0644); err != nil {
		t.Fatal(err)
	}

	m2 := NewManager(m.root)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}

	// 默认模式 local：旧数据应迁入 local 桶
	if got := m2.GetAndroidList(); len(got) != 1 || got[0].ID != "legacy-c1" {
		t.Fatalf("旧数据未迁入当前模式: %v", got)
	}
	// 迁移后旧文件改名 .bak，不再存在原文件（避免下次启动重复迁移）
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("迁移后旧 android_list.json 应已改名，实际仍存在")
	}
	if _, err := os.Stat(legacy + ".bak"); err != nil {
		t.Fatalf("旧文件应改名为 .bak 保留: %v", err)
	}
	// 迁移结果需落档到 local 文件
	m2.flushDirty()
	if got := readAndroidFile(t, m2.androidListLocalPath()); len(got) != 1 || got[0].ID != "legacy-c1" {
		t.Fatalf("迁移数据未落档到 local 文件: %v", got)
	}
}

// 当前模式已有独立文件时不迁移旧文件（不覆盖已有数据），旧文件保留待其它模式迁移
func TestMigrateLegacyAndroidListSkipsWhenModeFileExists(t *testing.T) {
	m := tempManager(t)
	if err := m.SetDeployMode("local"); err != nil {
		t.Fatal(err)
	}
	m.SetAndroidList([]AndroidInfo{{ID: "current"}})
	m.flushDirty()

	legacy := m.legacyAndroidListPath()
	data, _ := json.Marshal([]AndroidInfo{{ID: "legacy-c1"}})
	if err := os.WriteFile(legacy, data, 0644); err != nil {
		t.Fatal(err)
	}

	m2 := NewManager(m.root)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}
	if got := m2.GetAndroidList(); len(got) != 1 || got[0].ID != "current" {
		t.Fatalf("当前模式已有文件时不应被旧数据覆盖: %v", got)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("未迁移时旧文件应原样保留: %v", err)
	}
}

// 切换模式后裁剪跨模式残留容器，当前模式自己的容器原样保留
func TestPruneAndroidsOutsideDeviceList(t *testing.T) {
	m := tempManager(t)
	m.SetDeviceList([]DeviceCache{{DeviceID: "dev1"}})
	m.SetAndroidList([]AndroidInfo{
		{ID: "keep", DeviceID: "dev1"},
		{ID: "drop", DeviceID: "dev-other"},
	})

	if n := m.PruneAndroidsOutsideDeviceList(); n != 1 {
		t.Fatalf("应裁剪 1 个跨模式残留容器，实际 %d", n)
	}
	got := m.GetAndroidList()
	if len(got) != 1 || got[0].ID != "keep" {
		t.Fatalf("裁剪结果不符: %v", got)
	}
	// 再次裁剪无残留，返回 0（调用方据此免发通知）
	if n := m.PruneAndroidsOutsideDeviceList(); n != 0 {
		t.Fatalf("无残留时应返回 0，实际 %d", n)
	}
}

// ClearAllCache 清掉全部模式容器文件
func TestClearAllCacheRemovesAllAndroidFiles(t *testing.T) {
	m := tempManager(t)
	for _, mode := range []string{"local", "public", "v2", "v4"} {
		if err := m.SetDeployMode(mode); err != nil {
			t.Fatal(err)
		}
		m.SetAndroidList([]AndroidInfo{{ID: mode + "-c"}})
	}
	m.flushDirty()

	paths := []string{
		m.androidListLocalPath(), m.androidListPublicPath(),
		m.androidListV2Path(), m.androidListV4Path(),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("前置条件：%s 应存在: %v", filepath.Base(p), err)
		}
	}

	if err := m.ClearAllCache(); err != nil {
		t.Fatalf("ClearAllCache 失败: %v", err)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s 应被清理", filepath.Base(p))
		}
	}
}
