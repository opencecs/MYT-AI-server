package main

// keymap_store.go — 键位预设库与设备绑定的持久化(本地 JSON,原子写入)。
//
// 目录结构(%LOCALAPPDATA%\MYT\player\keymap):
//   presets/<presetID>.json   命名预设(完整键位方案)
//   presets/index.json        预设元数据(id/名称/更新时间)
//   <ip>_<port>.json          设备绑定 {presetId, enabled}

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

// presetMeta 预设列表条目(index.json 元素)。
type presetMeta struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Updated int64  `json:"updated"`
}

type keymapStore struct {
	mu      sync.Mutex
	baseDir string // 根目录覆盖(测试注入临时目录);空 = kmBaseDir()
}

// baseDirOr 存储根目录(可注入)。
func (s *keymapStore) baseDirOr() string {
	if s.baseDir != "" {
		return s.baseDir
	}
	return kmBaseDir()
}

func (s *keymapStore) presetsDir() string { return filepath.Join(s.baseDirOr(), "presets") }

func (s *keymapStore) ensureDirs() { os.MkdirAll(s.presetsDir(), 0o755) }

// listPresets 返回全部预设元数据(按更新时间倒序)。
func (s *keymapStore) listPresets() []presetMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listPresetsLocked()
}

func (s *keymapStore) listPresetsLocked() []presetMeta {
	s.ensureDirs()
	ents, err := os.ReadDir(s.presetsDir())
	if err != nil {
		return nil
	}
	var out []presetMeta
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || e.Name() == "index.json" {
			continue
		}
		var p KeymapProfile
		if s.loadFileLocked(filepath.Join(s.presetsDir(), e.Name()), &p) && p.ID != "" {
			out = append(out, presetMeta{ID: p.ID, Name: p.Name, Updated: p.Updated})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out
}

// uniqueName 方案名去重:与现有预设重名时追加「 2」「 3」…
// (2~999 试遍仍冲突则用时间戳,保证唯一)。
func (s *keymapStore) uniqueName(base string) string {
	if base == "" {
		base = "新方案"
	}
	taken := map[string]bool{}
	for _, m := range s.listPresets() {
		taken[m.Name] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; i < 1000; i++ {
		n := base + " " + strconv.Itoa(i)
		if !taken[n] {
			return n
		}
	}
	return base + " " + strconv.FormatInt(time.Now().Unix(), 10)
}

// loadPreset 按 id 读取预设;不存在返回 nil。
func (s *keymapStore) loadPreset(id string) *KeymapProfile {
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var p KeymapProfile
	if s.loadFileLocked(s.presetPath(id), &p) && p.ID == id {
		return &p
	}
	return nil
}

// savePreset 写入预设(逐条校验规范化)并刷新 index。
func (s *keymapStore) savePreset(p *KeymapProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.savePresetLocked(p)
}

func (s *keymapStore) savePresetLocked(p *KeymapProfile) {
	s.ensureDirs()
	p.Version = 1
	p.Updated = time.Now().Unix()
	clean := make([]KeymapEntry, 0, len(p.Entries))
	for i := range p.Entries {
		if validateEntry(&p.Entries[i]) {
			clean = append(clean, p.Entries[i])
		}
	}
	p.Entries = clean
	s.saveFileLocked(s.presetPath(p.ID), p)
	s.rebuildIndexLocked()
}

// deletePreset 删除预设文件并刷新 index;返回是否删除了文件。
func (s *keymapStore) deletePreset(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.presetPath(id))
	s.rebuildIndexLocked()
	return err == nil
}

// loadBinding 读当前设备绑定;文件不存在返回零值。
func (s *keymapStore) loadBinding() DeviceBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b DeviceBinding
	s.loadFileLocked(filepath.Join(s.baseDirOr(), kmDeviceKey()+".json"), &b)
	return b
}

// saveBinding 写当前设备绑定。
func (s *keymapStore) saveBinding(b DeviceBinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveBindingLocked(b)
}

func (s *keymapStore) saveBindingLocked(b DeviceBinding) {
	s.ensureDirs()
	s.saveFileLocked(filepath.Join(s.baseDirOr(), kmDeviceKey()+".json"), b)
}

// loadPresetLocked 按 id 读预设(调用方须持锁)。
func (s *keymapStore) loadPresetLocked(id string) *KeymapProfile {
	if id == "" {
		return nil
	}
	var p KeymapProfile
	if s.loadFileLocked(s.presetPath(id), &p) && p.ID == id {
		return &p
	}
	return nil
}

// exportPreset 把预设复制为可分享的导出文件,返回路径。
func (s *keymapStore) exportPreset(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p KeymapProfile
	if !s.loadFileLocked(s.presetPath(id), &p) || p.ID == "" {
		return ""
	}
	path := filepath.Join(s.baseDirOr(),
		"export_"+sanitizeFileName(p.Name)+"_"+strconv.FormatInt(time.Now().Unix(), 10)+".json")
	if s.saveFileLocked(path, p) {
		return path
	}
	return ""
}

// importPreset 从 JSON 字节导入为新预设(新 id)。
// name 为空时保留 JSON 内嵌方案名(再兜底「导入的方案」);结果做重名去重。
func (s *keymapStore) importPreset(data []byte, name string) *KeymapProfile {
	var p KeymapProfile
	if json.Unmarshal(data, &p) != nil {
		return nil
	}
	p.ID = newKeymapID()
	if name == "" {
		name = p.Name
	}
	if name == "" {
		name = "导入的方案"
	}
	p.Name = s.uniqueName(name)
	s.savePreset(&p)
	return s.loadPreset(p.ID)
}

// ─── 文件原子读写(调用方须持锁) ─────────────────────────────────────────────

func (s *keymapStore) presetPath(id string) string {
	return filepath.Join(s.presetsDir(), id+".json")
}

func (s *keymapStore) loadFileLocked(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func (s *keymapStore) saveFileLocked(path string, v any) bool {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return false
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) != nil {
		return false
	}
	return os.Rename(tmp, path) == nil
}

func (s *keymapStore) rebuildIndexLocked() {
	s.saveFileLocked(filepath.Join(s.presetsDir(), "index.json"), s.listPresetsLocked())
}

func sanitizeFileName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r >= 0x4e00:
			out = append(out, r)
		case r == '-' || r == '_':
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "preset"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return string(out)
}

func newKeymapID() string {
	return "p" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
