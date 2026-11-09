package main

// keymap_engine.go — 键盘映射引擎核心:按键拦截 → 命中映射 → 状态管理。
//
// 拦截点在系统快捷键(Ctrl+C/X/V)之后、funcKey/原始键透传之前;
// 未命中零开销直通;被映射键 consume 并吞掉后续 WM_CHAR(防输入框重复出字)。
// 手势序列实现在 keymap_gesture.go;编辑器会话状态在 keymap_overlay_windows.go。

import "sync"

// kmToggleCode / kmToggleVK — 启用/禁用映射的全局快捷键 F9。
const (
	kmToggleCode = "F9"
	kmToggleVK   = 0x78
	kmRuneScale  = 10000 // 万分比坐标基数
)

// dpadRun 单个虚拟摇杆的运行态。
type dpadRun struct {
	down         bool
	lastX, lastY float64 // 最近触点(源像素)
}

// KeymapEngine 键位映射引擎(每窗口一个)。
type KeymapEngine struct {
	mu     sync.Mutex
	dpadMu sync.Mutex // 摇杆手势串行化(方向变化按序执行)
	app    *App
	store  *keymapStore
	loaded bool

	binding DeviceBinding
	profile *KeymapProfile

	pressed map[string]bool // 按下的已映射键(JS code)
	suppVK  map[int]bool    // 已消费 VK(吞 WM_CHAR)
	dpadRun map[string]*dpadRun

	// OnToggle 由 UI 层注入:F9 切换后回调(Windows 侧 PostMessage 转 UI 线程)。
	OnToggle func(enabled bool)
}

// NewKeymapEngine 构造引擎。
func NewKeymapEngine(app *App) *KeymapEngine {
	return &KeymapEngine{
		app:     app,
		store:   &keymapStore{},
		pressed: map[string]bool{},
		suppVK:  map[int]bool{},
		dpadRun: map[string]*dpadRun{},
	}
}

// newKeymapEngineWithStore 注入 store 的构造(测试用:临时目录,不碰真实配置)。
func newKeymapEngineWithStore(app *App, store *keymapStore) *KeymapEngine {
	e := NewKeymapEngine(app)
	e.store = store
	return e
}

// ensureLoaded 惰性加载设备绑定与激活预设(仅一次,后续热刷新走 Reload)。
// 开关状态不跨会话记忆:每次启动默认关闭(用户预期"打开就是关的",
// 想用按 F9/侧栏开关开)——presetId/hints 照常记忆。
// ⚠️ 调用方必须已持 e.mu(HandleCode/uiState/SnapshotBinding 等全部如此),
// 这里不可再 Lock——sync.Mutex 不可重入,再 Lock 即死锁(曾致开屏无响应)。
func (e *KeymapEngine) ensureLoaded() {
	if e.loaded {
		return
	}
	e.Reload()
	if e.binding.Enabled {
		e.binding.Enabled = false
		e.store.saveBindingLocked(e.binding)
	}
}

// Reload 从磁盘重载绑定与预设(UI 变更后调用)。
func (e *KeymapEngine) Reload() {
	e.binding = e.store.loadBinding()
	if e.binding.PresetID != "" {
		e.profile = e.store.loadPreset(e.binding.PresetID)
	} else {
		e.profile = nil
	}
	e.loaded = true
	logf("[keymap] loaded: preset=%q enabled=%v entries=%d\n",
		e.binding.PresetID, e.binding.Enabled, len(e.entries()))
}

// HandleVK Win32 入口:VK → JS code 后统一处理。
func (e *KeymapEngine) HandleVK(vk int, pressed bool, mods int) bool {
	return e.HandleCode(vkToJSCodeName(vk), pressed, mods)
}

// HandleCode 统一按键处理(JS KeyboardEvent.code 风格)。
// 返回 true = 事件已消费(不再透传给设备键盘)。
func (e *KeymapEngine) HandleCode(code string, pressed bool, mods int) bool {
	if code == "" {
		return false
	}
	e.mu.Lock()
	e.ensureLoaded()

	// 修饰键组合的按下事件直通(保护 Ctrl+C/X/V 等);释放事件始终入引擎维持状态
	if pressed && mods != 0 {
		e.mu.Unlock()
		return false
	}
	if !pressed && !e.pressed[code] && code != kmToggleCode {
		e.mu.Unlock()
		return false
	}

	// F9 全局开关(存在任意预设时接管;否则保持原透传行为)
	if code == kmToggleCode && pressed {
		if len(e.store.listPresetsLocked()) > 0 {
			e.binding.Enabled = !e.binding.Enabled
			e.store.saveBindingLocked(e.binding)
			enabled := e.binding.Enabled
			e.mu.Unlock()
			logf("[keymap] F9 toggle -> %v\n", enabled)
			e.fireToggle(enabled)
			return true
		}
		e.mu.Unlock()
		return false
	}

	if !e.binding.Enabled || len(e.entries()) == 0 {
		e.mu.Unlock()
		return false
	}

	// 命中查找:dpad 匹配四键之一,其余匹配 Keys[0]
	var hit *KeymapEntry
	entries := e.entries()
	for i := range entries {
		if entryHasCode(&entries[i], code) {
			hit = &entries[i]
			break
		}
	}
	if hit == nil {
		e.mu.Unlock()
		return false
	}

	// 自动重复过滤:已按下时再次 keydown 直接吞掉
	if pressed {
		if e.pressed[code] {
			e.mu.Unlock()
			return true
		}
		e.pressed[code] = true
		if vk := jsCodeToVK(code); vk != 0 {
			e.suppVK[vk] = true
		}
	} else {
		delete(e.pressed, code)
		if vk := jsCodeToVK(code); vk != 0 {
			delete(e.suppVK, vk)
		}
	}
	entry := *hit
	e.mu.Unlock()

	// 摇杆走串行通道(防双键竞态),其余异步执行(不阻塞键盘消息循环)
	if entry.Type == kmTypeDpad {
		e.dpadStep(entry, pressed)
	} else {
		go e.runGesture(entry, pressed)
	}
	return true
}

// fireToggle 触发 UI 回调。
func (e *KeymapEngine) fireToggle(enabled bool) {
	if e.OnToggle != nil {
		e.OnToggle(enabled)
	}
}

// entries 返回当前条目切片(调用方须持锁)。
func (e *KeymapEngine) entries() []KeymapEntry {
	if e.profile == nil {
		return nil
	}
	return e.profile.Entries
}

// srcSize 当前视频源尺寸。
func (e *KeymapEngine) srcSize() (int32, int32, bool) {
	scaler := e.app.activeScaler()
	if scaler == nil {
		return 0, 0, false
	}
	w, h := scaler.SourceSize()
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// SuppressChar 该字符对应按键是否被映射消费(吞 WM_CHAR 防输入框重复出字)。
func (e *KeymapEngine) SuppressChar(ch rune) bool {
	code := charToJSCode(ch)
	if code == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pressed[code]
}

// charToJSCode 字符 → JS code(字母/数字/空格,覆盖 P1 单键绑定场景)。
func charToJSCode(ch rune) string {
	switch {
	case ch >= 'A' && ch <= 'Z':
		return "Key" + string(ch)
	case ch >= 'a' && ch <= 'z':
		return "Key" + string(ch-32) // JS code 用大写:KeyA
	case ch >= '0' && ch <= '9':
		return "Digit" + string(ch)
	case ch == ' ':
		return "Space"
	}
	return ""
}

// ─── 引擎级 UI/存储操作(供 dispatchMessage 与覆盖层调用) ────────────────────

// uiState 侧边栏键位面板所需状态。
type keymapUIState struct {
	Enabled bool           `json:"enabled"`
	Hints   bool           `json:"hints"`
	Active  string         `json:"activeId"`
	Presets []presetMeta   `json:"presets"`
	Profile *KeymapProfile `json:"profile"`
}

// uiState 组装当前状态(可直接 JSON)。
func (e *KeymapEngine) uiState() keymapUIState {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureLoaded()
	return keymapUIState{
		Enabled: e.binding.Enabled,
		Hints:   e.binding.HintsOn(),
		Active:  e.binding.PresetID,
		Presets: e.store.listPresetsLocked(),
		Profile: e.profile,
	}
}

// uiHintsLocked 运行态按键提醒开关(覆盖层绘制线程用)。
func (e *KeymapEngine) uiHintsLocked() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureLoaded()
	return e.binding.Enabled && e.binding.HintsOn()
}

// setHints 开关运行态按键提醒(侧边栏)。
func (e *KeymapEngine) setHints(on bool) {
	e.mu.Lock()
	e.ensureLoaded()
	v := on
	e.binding.Hints = &v
	e.store.saveBindingLocked(e.binding)
	e.mu.Unlock()
}

// setEnabled 开关映射(侧边栏开关)。
func (e *KeymapEngine) setEnabled(on bool) {
	e.mu.Lock()
	e.ensureLoaded()
	e.binding.Enabled = on
	e.store.saveBindingLocked(e.binding)
	e.mu.Unlock()
	e.fireToggle(on)
}

// selectPreset 切换激活预设(热生效,先抬起旧触点防粘滞)。
func (e *KeymapEngine) selectPreset(id string) {
	e.releaseAll()
	e.mu.Lock()
	e.ensureLoaded()
	e.binding.PresetID = id
	e.store.saveBindingLocked(e.binding)
	e.mu.Unlock()
	e.Reload()
}

// deletePreset 删除预设;若删的是激活预设则解绑并抬起其触点(防粘滞)。
func (e *KeymapEngine) deletePreset(id string) {
	e.mu.Lock()
	e.ensureLoaded()
	wasActive := e.binding.PresetID == id
	if wasActive {
		e.binding.PresetID = ""
		e.profile = nil
		e.store.saveBindingLocked(e.binding)
	}
	e.mu.Unlock()
	if wasActive {
		e.releaseAll() // releaseAll 自取锁,须在解锁后调用
	}
	e.store.deletePreset(id)
	e.Reload()
}

// createPreset 新建空预设并激活;名称为空用「新方案」,重名自动追加序号。
func (e *KeymapEngine) createPreset(name string) *KeymapProfile {
	if name == "" {
		name = "新方案"
	}
	name = e.store.uniqueName(name)
	p := &KeymapProfile{ID: newKeymapID(), Name: name}
	e.store.savePreset(p)
	e.selectPreset(p.ID)
	return p
}

// renamePreset 重命名预设。
func (e *KeymapEngine) renamePreset(id, name string) {
	if id == "" || name == "" {
		return
	}
	e.mu.Lock()
	e.ensureLoaded()
	p := e.store.loadPresetLocked(id)
	e.mu.Unlock()
	if p == nil {
		return
	}
	p.Name = name
	e.store.savePreset(p)
	e.Reload()
}

// saveProfile 保存预设内容(编辑器「完成」);未激活任何预设时自动激活。
func (e *KeymapEngine) saveProfile(p *KeymapProfile) {
	if p.ID == "" {
		p.ID = newKeymapID()
	}
	if p.Name == "" {
		p.Name = "未命名方案"
	}
	e.store.savePreset(p)
	e.mu.Lock()
	e.ensureLoaded()
	if e.binding.PresetID == "" {
		e.binding.PresetID = p.ID
		e.store.saveBindingLocked(e.binding)
	}
	e.mu.Unlock()
	e.Reload()
}

// releaseAllPublic 连接断开时抬起全部触点(防粘滞)。
func (e *KeymapEngine) releaseAllPublic() { e.releaseAll() }

// exportCurrent 导出当前激活预设为可分享 JSON 文件,返回路径(空=无可导出)。
func (e *KeymapEngine) exportCurrent() string {
	e.mu.Lock()
	e.ensureLoaded()
	id := e.binding.PresetID
	e.mu.Unlock()
	if id == "" {
		return ""
	}
	return e.store.exportPreset(id)
}

// importFromJSON 从 JSON 文本导入为新预设并激活(保留内嵌方案名,重名加序号)。
func (e *KeymapEngine) importFromJSON(data []byte) {
	if p := e.store.importPreset(data, ""); p != nil {
		e.selectPreset(p.ID)
	}
}

// SnapshotBinding 读取当前开关与条目数(stateJSON 用)。
func (e *KeymapEngine) SnapshotBinding(enabled *bool, entries *int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureLoaded()
	*enabled = e.binding.Enabled
	*entries = len(e.entries())
}

// activeProfileCopy 返回当前激活预设的深拷贝(编辑器工作副本)。
func (e *KeymapEngine) activeProfileCopy() *KeymapProfile {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureLoaded()
	if e.profile == nil {
		return nil
	}
	p := *e.profile
	p.Entries = make([]KeymapEntry, len(e.profile.Entries))
	copy(p.Entries, e.profile.Entries)
	return &p
}
