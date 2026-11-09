package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Manager struct {
	mu     sync.RWMutex
	root   string
	config *AppConfig

	// 十个独立数据体系（内存优先+异步落档）
	// 设备列表按部署模式隔离：local/public/v2/v4 各自独立存储，切换不丢数据
	deviceListLocal  []DeviceCache // config/device_list_local.json
	deviceListPublic []DeviceCache // config/device_list_public.json
	deviceListV2     []DeviceCache // config/device_list_v2.json
	deviceListV4     []DeviceCache // config/device_list_v4.json
	// 待添加列表同样按部署模式隔离（与正式列表对称），避免 v2 扫描看到 local 上次的扫描结果
	pendingDevicesLocal  []DeviceCache            // config/pending_devices_local.json
	pendingDevicesPublic []DeviceCache            // config/pending_devices_public.json
	pendingDevicesV2     []DeviceCache            // config/pending_devices_v2.json
	pendingDevicesV4     []DeviceCache            // config/pending_devices_v4.json
	deviceNotes          map[string]string        // config/device_notes.json
	// 容器列表同样按部署模式隔离：local/public/v2/v4 各自独立存储
	// （容器 key 前缀随模式变化：local="IP_indexNum"，public="device_id_indexNum"，
	// 混存会让切模式后的截图/投屏 key 不匹配，且清空某一模式会污染另一模式）
	androidListLocal  []AndroidInfo            // config/android_list_local.json
	androidListPublic []AndroidInfo            // config/android_list_public.json
	androidListV2     []AndroidInfo            // config/android_list_v2.json
	androidListV4     []AndroidInfo            // config/android_list_v4.json
	androidNotes      map[string]string        // config/android_notes.json
	slotExpiry           map[string][]SlotExpiry  // config/slot_expiry.json
	deviceDetails        map[string]*DeviceDetail // config/device_details.json
	deviceGroups         []DeviceGroup            // config/device_groups.json
	slotTags             map[string][]string      // config/slot_tags.json（键 "deviceID#indexNum"）
	// 容器最近一次 WS 补丁时间（仅内存，不落档）：MergeAndroidList 用它防 REST 查询
	// 整体替换覆盖期间到达的 WS 补丁（见 android_list.go）。
	androidLastPatchAt map[string]time.Time

	// 脏标记：atomic 无锁读写，flush 期间不阻塞服务
	deviceListLocalDirty      atomic.Bool
	deviceListPublicDirty     atomic.Bool
	deviceListV2Dirty         atomic.Bool
	deviceListV4Dirty         atomic.Bool
	pendingDevicesLocalDirty  atomic.Bool
	pendingDevicesPublicDirty atomic.Bool
	pendingDevicesV2Dirty     atomic.Bool
	pendingDevicesV4Dirty     atomic.Bool
	deviceNotesDirty          atomic.Bool
	androidListLocalDirty     atomic.Bool
	androidListPublicDirty    atomic.Bool
	androidListV2Dirty        atomic.Bool
	androidListV4Dirty        atomic.Bool
	androidNotesDirty         atomic.Bool
	slotExpiryDirty           atomic.Bool
	deviceDetailsDirty        atomic.Bool
	deviceGroupsDirty         atomic.Bool
	slotTagsDirty             atomic.Bool

	// allowEmptyFlush 放行「用空设备列表覆盖磁盘」这一次落档。
	// 落档防呆（flushDirty）会拒绝内存空列表覆盖磁盘已有内容，以避免网关异常
	// 返回空列表时把设备整表抹掉；但用户逐台删除至空是合法意图，若同样被挡，
	// 磁盘会一直留着旧列表、重启后设备"复活"。故显式删空时置位，落档一次后复位。
	allowEmptyFlush atomic.Bool

	flushStopCh chan struct{}
	flushWg     sync.WaitGroup
}

type AppConfig struct {
	Token              string            `json:"token,omitempty"`
	DeviceCache        []DeviceCache     `json:"device_cache,omitempty"` // 旧字段，仅用于迁移
	AuthUsername       string            `json:"auth_username,omitempty"`
	AuthPassword       string            `json:"auth_password,omitempty"`
	DataStoragePath    string            `json:"data_storage_path,omitempty"`
	ImageStoragePath   string            `json:"image_storage_path,omitempty"`
	AutoStart          bool              `json:"auto_start"`
	CloseBehavior      string            `json:"close_behavior,omitempty"`
	LogLevel           string            `json:"log_level,omitempty"`
	DevicePasswords    map[string]string `json:"device_passwords,omitempty"`
	AutoCheckUpdate    bool              `json:"auto_check_update"`
	AutoDownloadUpdate bool              `json:"auto_download_update"`
	AutoUpgradeSDK     bool              `json:"auto_upgrade_sdk"`
	NoSidebar          bool              `json:"no_sidebar"`
	GpuAccel           bool              `json:"gpu_accel"`  // 投屏 GPU 硬件加速解码（默认 false，关闭后走 CPU 软解）
	SingleCastScale    float64           `json:"single_cast_scale,omitempty"` // 单台投屏窗口大小（相对原生布局的倍率，0=默认 1.0）
	MCPEnabled         bool              `json:"mcpEnabled"` // MCP 调试服务开关（默认 true，仅开发构建生效；见 NewManager 默认值说明）
	MCPToken           string            `json:"mcpToken"`   // MCP server 鉴权 token（首次启用时自动生成并持久化）
	MCPHost            string            `json:"mcpHost"`    // MCP server 监听地址（空 = 默认 127.0.0.1；可填 0.0.0.0 或内网 IP 供局域网访问）
	// 部署模式："local"（默认，局域网直连设备）/ "public"（走线上服务转发）
	// / "v2"（直连云机主机 Docker）/ "v4"（本地 V4，发现与连接逻辑待定）
	DeployMode string `json:"deploy_mode,omitempty"`
}

type DeviceCache struct {
	DeviceID   string `json:"device_id"`
	IP         string `json:"ip"`
	DeviceType string `json:"device_type"`
	DeviceName string `json:"device_name"`
	Version    string `json:"version"`
	State      int    `json:"state"` // 0=在线, 1=离线, 2=认证失败
	Order      int    `json:"order"` // 0=未勾选, >0=勾选顺序
	DiskUsed   int64  `json:"disk_used,omitempty"`
	DiskTotal  int64  `json:"disk_total,omitempty"`
	MemUsed    int64  `json:"mem_used,omitempty"`
	MemTotal   int64  `json:"mem_total,omitempty"`
	BindStatus int    `json:"bind_status,omitempty"` // 0=未绑定, 1=已绑定(当前账号), 2=被其他账号绑定
	// SuperSdkReady 超级 SDK（81 myt_sdk 容器）就绪状态，仅 v2 模式维护：
	// true=81 可达；false=未就绪。不用 omitempty——bool 零值 false 被抹掉后
	// 前端拿 undefined，会与「未知」混淆，导致未就绪提示静默不显示。
	SuperSdkReady bool `json:"super_sdk_ready"`
}

// PortBinding Docker 端口映射条目（与 sdkapi.PortBinding 一致）
type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type AndroidInfo struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Status       string                   `json:"status"`
	Image        string                   `json:"image"`
	IndexNum     int                      `json:"indexNum"`
	DeviceID     string                   `json:"deviceId"`
	IP           string                   `json:"ip"`
	NetworkName  string                   `json:"networkName"`
	PortBindings map[string][]PortBinding `json:"portBindings,omitempty"`
	ContainerIP  string                   `json:"containerIP,omitempty"`
	ModelPath    string                   `json:"modelPath,omitempty"`
	CPUUsage     float64                  `json:"cpuUsage,omitempty"`  // 容器 CPU 占用百分比（running 时有效）
	MemUsage     int64                    `json:"memUsage,omitempty"`  // 容器内存使用量（字节，running 时有效）
	MemLimit     int64                    `json:"memLimit,omitempty"`  // 容器内存上限（字节，0=无限制）
	DiskUsage    int64                    `json:"diskUsage,omitempty"` // 容器磁盘使用量（字节，running 时有效）
	// Tags 容器标签（设备端权威数据，随 /android 快照与 container/stats 周期推送下发）。
	// 旧本地坑位标签（slot_tags.json）已停止消费，标签唯一来源是设备端此字段。
	Tags []string `json:"tags,omitempty"`
	// Profile 设备档案（V4 专属：WS snapshot / configure 后下发，经纬度/运营商/手机号/root 等）。
	// V3 恒为 nil；弹窗预填与展示都读此字段。
	Profile *AndroidProfile `json:"profile,omitempty"`
}

// AndroidProfile 云机设备档案（V4 tobox_player 的 DeviceProfile）。
//
// 单一事实源：internal/v4host 与 internal/v4ws 都以类型别名引用本定义
// （两包分别对接 REST body 与 WS snapshot，字段名一致=snake_case），不做值拷贝映射。
// 本包是依赖叶子（所有 internal 包都可 import），放这里避免 config↔v4host 循环。
type AndroidProfile struct {
	Latitude        float64 `json:"latitude"`
	Longitude       float64 `json:"longitude"`
	Country         string  `json:"country"`
	OperatorNumeric string  `json:"operator_numeric"`
	OperatorName    string  `json:"operator_name"`
	CellID          string  `json:"cell_id"`
	PhoneNumber     string  `json:"phone_number"`
	Root            bool    `json:"root"`
}

type SlotExpiry struct {
	Index      int    `json:"index"`
	ExpireTime string `json:"expire_time"`
	State      int    `json:"state"` // 0=正常, 1=即将到期, 2=已过期
}

func NewManager(rootDir string) *Manager {
	return &Manager{
		root: rootDir,
		// MCPEnabled 默认 true：旧 config.json 缺失该字段时 json.Unmarshal 不覆盖内存默认值，
		// 因此「无字段」自然落在 true（开发构建默认开启 MCP 调试）；用户显式关闭后落盘 false 并保持。
		config:        &AppConfig{AutoCheckUpdate: true, MCPEnabled: true},
		deviceNotes:   make(map[string]string),
		androidNotes:  make(map[string]string),
		slotExpiry:    make(map[string][]SlotExpiry),
		deviceDetails:      make(map[string]*DeviceDetail),
		slotTags:           make(map[string][]string),
		androidLastPatchAt: make(map[string]time.Time),
		flushStopCh:        make(chan struct{}),
	}
}

func (m *Manager) InitDirs() error {
	dirs := []string{
		filepath.Join(m.root, "config"),
		filepath.Join(m.root, "data", "images"),
		filepath.Join(m.root, "data", "model"),
		filepath.Join(m.root, "data", "upload"),
		filepath.Join(m.root, "data", "cloudMachineBackup"),
		filepath.Join(m.root, "data", "BackupModel"),
		filepath.Join(m.root, "data", "updateSDK"),
		filepath.Join(m.root, "cache"),
		filepath.Join(m.root, "logs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) ConfigDir() string    { return filepath.Join(m.root, "config") }
func (m *Manager) DataDir() string      { return filepath.Join(m.root, "data") }
func (m *Manager) CacheDir() string     { return filepath.Join(m.root, "cache") }
func (m *Manager) LogsDir() string      { return filepath.Join(m.root, "logs") }
func (m *Manager) RootDir() string      { return m.root }
func (m *Manager) UpdateSDKDir() string { return filepath.Join(m.root, "data", "updateSDK") }

func (m *Manager) CloudMachineBackupDir() string {
	return filepath.Join(m.root, "data", "cloudMachineBackup")
}

func (m *Manager) UploadDir() string { return filepath.Join(m.root, "data", "upload") }

// StartFlushLoop 启动异步落档 goroutine（每5秒检查脏标记）
func (m *Manager) StartFlushLoop() {
	m.flushWg.Add(1)
	go func() {
		defer m.flushWg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.flushDirty()
			case <-m.flushStopCh:
				m.flushDirty() // 退出前最后一次落档
				return
			}
		}
	}()
}

func (m *Manager) StopFlushLoop() {
	close(m.flushStopCh)
	m.flushWg.Wait()
}
func (m *Manager) flushDirty() {
	// 原子检查哪些数据脏，无锁
	needDeviceListLocal := m.deviceListLocalDirty.Load()
	needDeviceListPublic := m.deviceListPublicDirty.Load()
	needDeviceListV2 := m.deviceListV2Dirty.Load()
	needDeviceListV4 := m.deviceListV4Dirty.Load()
	needPendingLocal := m.pendingDevicesLocalDirty.Load()
	needPendingPublic := m.pendingDevicesPublicDirty.Load()
	needPendingV2 := m.pendingDevicesV2Dirty.Load()
	needPendingV4 := m.pendingDevicesV4Dirty.Load()
	needDeviceNotes := m.deviceNotesDirty.Load()
	needAndroidLocal := m.androidListLocalDirty.Load()
	needAndroidPublic := m.androidListPublicDirty.Load()
	needAndroidV2 := m.androidListV2Dirty.Load()
	needAndroidV4 := m.androidListV4Dirty.Load()
	needAndroidNotes := m.androidNotesDirty.Load()
	needSlotExpiry := m.slotExpiryDirty.Load()
	needDeviceDetails := m.deviceDetailsDirty.Load()
	needDeviceGroups := m.deviceGroupsDirty.Load()
	needSlotTags := m.slotTagsDirty.Load()

	if !needDeviceListLocal && !needDeviceListPublic && !needDeviceListV2 && !needDeviceListV4 && !needPendingLocal && !needPendingPublic && !needPendingV2 && !needPendingV4 && !needDeviceNotes && !needAndroidLocal && !needAndroidPublic && !needAndroidV2 && !needAndroidV4 && !needAndroidNotes && !needSlotExpiry && !needDeviceDetails && !needDeviceGroups && !needSlotTags {
		return
	}

	// RLock 只读拷贝快照，不阻塞其他并发读取
	m.mu.RLock()
	snapshot := map[string]interface{}{}
	if needDeviceListLocal {
		snapshot["deviceListLocal"] = m.getDeviceListLocalLocked()
	}
	if needDeviceListPublic {
		snapshot["deviceListPublic"] = m.getDeviceListPublicLocked()
	}
	if needDeviceListV2 {
		snapshot["deviceListV2"] = m.getDeviceListV2Locked()
	}
	if needDeviceListV4 {
		snapshot["deviceListV4"] = m.getDeviceListV4Locked()
	}
	if needPendingLocal {
		snapshot["pendingDevicesLocal"] = m.getPendingLocalLocked()
	}
	if needPendingPublic {
		snapshot["pendingDevicesPublic"] = m.getPendingPublicLocked()
	}
	if needPendingV2 {
		snapshot["pendingDevicesV2"] = m.getPendingV2Locked()
	}
	if needPendingV4 {
		snapshot["pendingDevicesV4"] = m.getPendingV4Locked()
	}
	if needDeviceNotes {
		snapshot["deviceNotes"] = m.getDeviceNotesLocked()
	}
	if needAndroidLocal {
		snapshot["androidListLocal"] = m.getAndroidListLocalLocked()
	}
	if needAndroidPublic {
		snapshot["androidListPublic"] = m.getAndroidListPublicLocked()
	}
	if needAndroidV2 {
		snapshot["androidListV2"] = m.getAndroidListV2Locked()
	}
	if needAndroidV4 {
		snapshot["androidListV4"] = m.getAndroidListV4Locked()
	}
	if needAndroidNotes {
		snapshot["androidNotes"] = m.getAndroidNotesLocked()
	}
	if needSlotExpiry {
		snapshot["slotExpiry"] = m.getSlotExpiryLocked()
	}
	if needDeviceDetails {
		snapshot["deviceDetails"] = m.getAllDeviceDetailsLocked()
	}
	if needDeviceGroups {
		snapshot["deviceGroups"] = m.getDeviceGroupsLocked()
	}
	if needSlotTags {
		snapshot["slotTags"] = m.getSlotTagsLocked()
	}
	m.mu.RUnlock()

	// 写磁盘（无锁，不阻塞任何服务），写完后原子清脏标记
	type flushEntry struct {
		key      string
		path     string
		dirtyPtr *atomic.Bool
	}
	entries := []flushEntry{
		{"deviceListLocal", m.deviceListLocalPath(), &m.deviceListLocalDirty},
		{"deviceListPublic", m.deviceListPublicPath(), &m.deviceListPublicDirty},
		{"deviceListV2", m.deviceListV2Path(), &m.deviceListV2Dirty},
		{"deviceListV4", m.deviceListV4Path(), &m.deviceListV4Dirty},
		{"pendingDevicesLocal", m.pendingDevicesLocalPath(), &m.pendingDevicesLocalDirty},
		{"pendingDevicesPublic", m.pendingDevicesPublicPath(), &m.pendingDevicesPublicDirty},
		{"pendingDevicesV2", m.pendingDevicesV2Path(), &m.pendingDevicesV2Dirty},
		{"pendingDevicesV4", m.pendingDevicesV4Path(), &m.pendingDevicesV4Dirty},
		{"deviceNotes", m.deviceNotesPath(), &m.deviceNotesDirty},
		{"androidListLocal", m.androidListLocalPath(), &m.androidListLocalDirty},
		{"androidListPublic", m.androidListPublicPath(), &m.androidListPublicDirty},
		{"androidListV2", m.androidListV2Path(), &m.androidListV2Dirty},
		{"androidListV4", m.androidListV4Path(), &m.androidListV4Dirty},
		{"androidNotes", m.androidNotesPath(), &m.androidNotesDirty},
		{"slotExpiry", m.slotExpiryPath(), &m.slotExpiryDirty},
		{"deviceDetails", m.deviceDetailsPath(), &m.deviceDetailsDirty},
		{"deviceGroups", m.deviceGroupsPath(), &m.deviceGroupsDirty},
		{"slotTags", m.slotTagsPath(), &m.slotTagsDirty},
	}
	for _, e := range entries {
		v, ok := snapshot[e.key]
		if !ok {
			continue
		}
		// 落档防呆：内存中的设备列表为空时，不覆盖磁盘上已有的内容。
		// 网关异常（限流/超时/返回空集合）或发现轮次被中途打断时，SetDeviceList
		// 会拿到一个空切片，5s 后落档就把用户的设备列表写成 []，重启即永久丢失。
		// 磁盘已有非空内容说明「空」更可能是异常产物，跳过本次写入并保留脏标记，
		// 待内存重新拿到真实列表后再落档。
		// 显式删空（用户逐台删除至最后一台）经 allowEmptyFlush 放行，见其注释。
		if isDeviceListKey(e.key) && isEmptyDeviceList(v) &&
			!m.allowEmptyFlush.Load() && m.hasNonEmptyOnDisk(e.path) {
			log.Printf("落档 %s 跳过：内存列表为空而磁盘已有内容（防误写空，待数据恢复后再落档）", e.key)
			continue
		}
		if err := m.writeJSON(e.path, v); err != nil {
			log.Printf("落档 %s 失败: %v", e.key, err)
			continue
		}
		e.dirtyPtr.Store(false)
	}
	// 放行标记是「一次性」的：本轮已按放行意图落档（或本轮无设备列表待写），
	// 复位后后续异常空列表重新受防呆保护。
	m.allowEmptyFlush.Store(false)
}

// isDeviceListKey 判断落档条目是否为「正式设备列表」。
// 待添加列表（pendingDevices*）虽同为 []DeviceCache，但为空是常态
// （切模式后本就为空、扫描前为空），不能参与防呆，否则永远无法落档。
func isDeviceListKey(key string) bool {
	switch key {
	case "deviceListLocal", "deviceListPublic", "deviceListV2", "deviceListV4":
		return true
	}
	return false
}

// isEmptyDeviceList 判断待落档值是否为空设备列表。
func isEmptyDeviceList(v interface{}) bool {
	list, ok := v.([]DeviceCache)
	return ok && len(list) == 0
}

// hasNonEmptyOnDisk 判断磁盘上该文件是否已有非空设备列表。
// 读取失败（文件不存在/损坏）一律视为「无内容」——此时不应拦截，
// 否则新建环境的第一次落档会被永久跳过。
func (m *Manager) hasNonEmptyOnDisk(path string) bool {
	var devices []DeviceCache
	if err := m.readJSON(path, &devices); err != nil {
		return false
	}
	return len(devices) > 0
}

func (m *Manager) writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (m *Manager) readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Load 从 config.json 加载基础配置
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.root, "config", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := json.Unmarshal(data, m.config); err != nil {
		return err
	}

	// 旧格式迁移：device_cache 在 config.json 内（迁移到 local 列表）
	if len(m.config.DeviceCache) > 0 {
		m.deviceListLocal = migrateDeviceCache(m.config.DeviceCache)
		m.deviceListLocalDirty.Store(true)
		m.config.DeviceCache = nil
		_ = m.writeConfigLocked()
	}

	return nil
}

// Save 保存基础配置到 config.json
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeConfigLocked()
}
func (m *Manager) GetConfig() AppConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.config
}

// LoadAll 从文件恢复所有数据体系到内存（启动时调用）
func (m *Manager) LoadAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 设备列表按模式隔离加载：device_list_local.json / device_list_public.json
	// 兼容旧 device_list.json：迁移为 local（旧版本只有 local 模式）
	var localDevices []DeviceCache
	if err := m.readJSON(m.deviceListLocalPath(), &localDevices); err == nil {
		m.deviceListLocal = localDevices
	} else if err := m.readJSON(m.legacyDeviceListPath(), &localDevices); err == nil {
		m.deviceListLocal = localDevices
		m.deviceListLocalDirty.Store(true)
		log.Printf("[config] 迁移旧 device_list.json → device_list_local.json（%d 台）", len(localDevices))
	}
	var publicDevices []DeviceCache
	if err := m.readJSON(m.deviceListPublicPath(), &publicDevices); err == nil {
		m.deviceListPublic = publicDevices
	}
	var v2Devices []DeviceCache
	if err := m.readJSON(m.deviceListV2Path(), &v2Devices); err == nil {
		m.deviceListV2 = v2Devices
	}
	var v4Devices []DeviceCache
	if err := m.readJSON(m.deviceListV4Path(), &v4Devices); err == nil {
		m.deviceListV4 = v4Devices
	}
	// 待添加列表按模式隔离加载（与正式列表对称）
	var pendingLocal []DeviceCache
	if err := m.readJSON(m.pendingDevicesLocalPath(), &pendingLocal); err == nil {
		m.pendingDevicesLocal = pendingLocal
	}
	var pendingPublic []DeviceCache
	if err := m.readJSON(m.pendingDevicesPublicPath(), &pendingPublic); err == nil {
		m.pendingDevicesPublic = pendingPublic
	}
	var pendingV2 []DeviceCache
	if err := m.readJSON(m.pendingDevicesV2Path(), &pendingV2); err == nil {
		m.pendingDevicesV2 = pendingV2
	}
	var pendingV4 []DeviceCache
	if err := m.readJSON(m.pendingDevicesV4Path(), &pendingV4); err == nil {
		m.pendingDevicesV4 = pendingV4
	}
	// 旧版共享 pending_devices.json：直接丢弃（不迁移）。
	// 待添加列表是扫描产生的临时缓存（重新扫描秒级再生），且旧文件是三模式混写、
	// 无法区分条目来源模式——迁入任一 mode 都会把另一个模式的陈旧扫描结果带回来
	// （正是本次要修的"v2 显示 v3 上次扫描结果"），所以选择丢弃并记日志。
	var legacyPending []DeviceCache
	if err := m.readJSON(m.legacyPendingDevicesPath(), &legacyPending); err == nil && len(legacyPending) > 0 {
		log.Printf("[config] 丢弃旧 pending_devices.json（%d 台，模式混写无法区分来源，重新扫描即可再生）", len(legacyPending))
	}

	// device_notes.json
	dNotes := make(map[string]string)
	if err := m.readJSON(m.deviceNotesPath(), &dNotes); err == nil {
		m.deviceNotes = dNotes
	}

	// 容器列表按模式隔离加载：android_list_local.json / android_list_public.json / ...
	var androidLocal []AndroidInfo
	if err := m.readJSON(m.androidListLocalPath(), &androidLocal); err == nil {
		m.androidListLocal = androidLocal
	}
	var androidPublic []AndroidInfo
	if err := m.readJSON(m.androidListPublicPath(), &androidPublic); err == nil {
		m.androidListPublic = androidPublic
	}
	var androidV2 []AndroidInfo
	if err := m.readJSON(m.androidListV2Path(), &androidV2); err == nil {
		m.androidListV2 = androidV2
	}
	var androidV4 []AndroidInfo
	if err := m.readJSON(m.androidListV4Path(), &androidV4); err == nil {
		m.androidListV4 = androidV4
	}
	// 旧版共享 android_list.json：仅在当前模式对应文件不存在时迁入当前模式，
	// 迁移后旧文件改名 .bak（见 migrateLegacyAndroidListLocked）。
	m.migrateLegacyAndroidListLocked()

	// android_notes.json
	notes := make(map[string]string)
	if err := m.readJSON(m.androidNotesPath(), &notes); err == nil {
		m.androidNotes = notes
	}

	// slot_expiry.json
	expiry := make(map[string][]SlotExpiry)
	if err := m.readJSON(m.slotExpiryPath(), &expiry); err == nil {
		m.slotExpiry = expiry
	}

	// device_details.json
	m.loadDeviceDetails()

	// device_groups.json
	var groups []DeviceGroup
	if err := m.readJSON(m.deviceGroupsPath(), &groups); err == nil {
		m.deviceGroups = groups
	}

	// slot_tags.json
	var tags map[string][]string
	if err := m.readJSON(m.slotTagsPath(), &tags); err == nil && tags != nil {
		m.slotTags = tags
	}

	return nil
}
