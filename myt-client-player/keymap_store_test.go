package main

// keymap_store_test.go — 方案管理存储行为测试(命名去重/导入命名/删除)。
// 通过 baseDir 注入临时目录,不触碰真实 %LOCALAPPDATA%。

import "testing"

func newTestStore(t *testing.T) *keymapStore {
	t.Helper()
	return &keymapStore{baseDir: t.TempDir()}
}

func TestUniqueName(t *testing.T) {
	s := newTestStore(t)
	s.savePreset(&KeymapProfile{ID: "a", Name: "新方案"})
	s.savePreset(&KeymapProfile{ID: "b", Name: "新方案 2"})

	if got := s.uniqueName("王者"); got != "王者" {
		t.Fatalf("不重名应原样返回, got %q", got)
	}
	if got := s.uniqueName("新方案"); got != "新方案 3" {
		t.Fatalf("重名应追加到空闲序号, got %q", got)
	}
	if got := s.uniqueName(""); got != "新方案 3" {
		t.Fatalf("空名应兜底「新方案」再去重, got %q", got)
	}
}

func TestImportPresetNaming(t *testing.T) {
	s := newTestStore(t)
	data := []byte(`{"id":"old","name":"我的方案","entries":[]}`)

	p := s.importPreset(data, "")
	if p == nil {
		t.Fatal("导入应成功")
	}
	if p.Name != "我的方案" {
		t.Fatalf("应保留 JSON 内嵌方案名, got %q", p.Name)
	}
	if p.ID == "old" || p.ID == "" {
		t.Fatalf("导入必须分配新 id, got %q", p.ID)
	}

	p2 := s.importPreset(data, "")
	if p2 == nil || p2.Name != "我的方案 2" {
		t.Fatalf("重复导入应去重命名, got %+v", p2)
	}
}

func TestImportPresetFallbackName(t *testing.T) {
	s := newTestStore(t)
	p := s.importPreset([]byte(`{"entries":[]}`), "")
	if p == nil || p.Name != "导入的方案" {
		t.Fatalf("无内嵌名应兜底「导入的方案」, got %+v", p)
	}
}

func TestDeletePreset(t *testing.T) {
	s := newTestStore(t)
	s.savePreset(&KeymapProfile{ID: "a", Name: "X"})

	if !s.deletePreset("a") {
		t.Fatal("删除存在的预设应返回 true")
	}
	if s.deletePreset("a") {
		t.Fatal("删除不存在的预设应返回 false")
	}
	if s.loadPreset("a") != nil {
		t.Fatal("删除后不应再读到该预设")
	}
}

func TestValidateEntryScaleClamp(t *testing.T) {
	mk := func(scale float64) KeymapEntry {
		return KeymapEntry{Type: kmTypeTap, Keys: []string{"KeyQ"}, X: 100, Y: 100, Scale: scale}
	}
	cases := []struct {
		in, want float64
	}{
		{0, 1},     // 缺省
		{2.5, 2.5}, // 3.0 以内保留
		{3.0, 3.0}, // 边界
		{3.5, 3.0}, // 超上限钳回
		{0.3, 0.6}, // 低于下限钳回
	}
	for _, c := range cases {
		en := mk(c.in)
		if !validateEntry(&en) {
			t.Fatalf("Scale=%v 的合法 tap 条目不应被丢弃", c.in)
		}
		if en.Scale != c.want {
			t.Fatalf("Scale=%v 应钳到 %v, got %v", c.in, c.want, en.Scale)
		}
	}
}

func TestValidateEntryDpadR(t *testing.T) {
	en := KeymapEntry{Type: kmTypeDpad, Keys: []string{"KeyW", "KeyA", "KeyS", "KeyD"}, R: 2500, X: 500, Y: 500}
	if !validateEntry(&en) || en.R != 2500 {
		t.Fatalf("大半径 dpad(R=2500) 应原样保留, got R=%v", en.R)
	}
}

// TestEngineFirstLoadDisablesEnabled 锁「启动默认关闭」与锁路径:
// ① 磁盘残留 enabled=true 时首次加载应强制关闭;② ensureLoaded 在
// 持锁调用方(SnapshotBinding)内不得死锁(历史事故:内部再 Lock 即死锁);
// ③ 会话内 F9 应能正常开(不被 ensureLoaded 反复重置)。
func TestEngineFirstLoadDisablesEnabled(t *testing.T) {
	s := newTestStore(t)
	s.savePreset(&KeymapProfile{ID: "a", Name: "X", Entries: []KeymapEntry{
		{ID: "e1", Type: kmTypeTap, Keys: []string{"KeyQ"}, X: 100, Y: 100},
	}})
	// 磁盘残留 enabled=true + 已选预设
	s.saveBinding(DeviceBinding{PresetID: "a", Enabled: true})

	e := newKeymapEngineWithStore(nil, s)

	// 持锁调用方路径(曾死锁:ensureLoaded 内再 Lock)
	var enabled bool
	var entries int
	e.SnapshotBinding(&enabled, &entries) // 首次加载:强制关
	if enabled {
		t.Fatal("首次加载后应默认关闭(enabled=false)")
	}
	if entries != 1 {
		t.Fatalf("条目数应为 1, got %d", entries)
	}

	// F9 开 → 再 Snapshot(非首次,不得重置)
	if !e.HandleCode("F9", true, 0) {
		t.Fatal("F9 按下应被消费(存在预设)")
	}
	e.SnapshotBinding(&enabled, &entries)
	if !enabled {
		t.Fatal("会话内 F9 开启后应保持开启")
	}
	// 注:不再按映射键(KeyQ)——手势路径经 app.activeScaler,引擎测试
	// 注入的 app 为 nil 会 panic;键位命中的行为由真机验证,本测试只锁
	// 锁路径与开关语义。
}
