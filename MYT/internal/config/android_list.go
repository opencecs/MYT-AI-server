package config

import (
	"log"
	"os"
	"sync/atomic"
	"time"
)

// --- 安卓（容器）列表：按部署模式隔离 ---
// local:  android_list_local.json  — 本地 UDP 发现设备的容器（key="IP_indexNum"）
// public: android_list_public.json — 公网设备经网关透传的容器（key="device_id_indexNum"）
// v2:     android_list_v2.json     — V2 主机（Docker 2375）的容器
// v4:     android_list_v4.json     — V4 设备（发现逻辑待定，见 discover_service_core.go）
//
// 隔离的必要性：容器 key 前缀随模式变化（local="IP_indexNum"，public="device_id_indexNum"），
// 混存会让切模式后的截图/投屏 key 不匹配；且清空某一模式会污染另一模式。
// 旧版共享 android_list.json 仅用于加载时迁移（见 migrateLegacyAndroidListLocked）。

func (m *Manager) androidListLocalPath() string {
	return m.ConfigDir() + "/android_list_local.json"
}

func (m *Manager) androidListPublicPath() string {
	return m.ConfigDir() + "/android_list_public.json"
}

func (m *Manager) androidListV2Path() string {
	return m.ConfigDir() + "/android_list_v2.json"
}

func (m *Manager) androidListV4Path() string {
	return m.ConfigDir() + "/android_list_v4.json"
}

// legacyAndroidListPath 旧版共享文件路径，仅用于加载时迁移
func (m *Manager) legacyAndroidListPath() string {
	return m.ConfigDir() + "/android_list.json"
}

// androidListPtrLocked 返回当前模式容器列表的指针（调用方需持写锁）
func (m *Manager) androidListPtrLocked() *[]AndroidInfo {
	switch m.currentModeLocked() {
	case "public":
		return &m.androidListPublic
	case "v2":
		return &m.androidListV2
	case "v4":
		return &m.androidListV4
	}
	return &m.androidListLocal
}

// androidListDirtyPtr 返回当前模式容器列表的脏标记指针
func (m *Manager) androidListDirtyPtr() *atomic.Bool {
	switch m.currentModeLocked() {
	case "public":
		return &m.androidListPublicDirty
	case "v2":
		return &m.androidListV2Dirty
	case "v4":
		return &m.androidListV4Dirty
	}
	return &m.androidListLocalDirty
}

// migrateLegacyAndroidListLocked 迁移旧版共享 android_list.json（调用方需持写锁）。
//
// 仅当「当前模式对应文件不存在」时迁移——旧文件是三模式混写，无法区分条目来源，
// 迁入当前模式是尽力而为（与 device_list 的 local 迁移同一取舍：不丢用户可见数据优先）。
// 迁移后旧文件改名 .bak（不删除：用户可人工取回，出问题也能回溯）。
func (m *Manager) migrateLegacyAndroidListLocked() {
	legacy := m.legacyAndroidListPath()
	if _, err := os.Stat(legacy); err != nil {
		return
	}
	var legacyList []AndroidInfo
	if err := m.readJSON(legacy, &legacyList); err != nil {
		log.Printf("[config] 旧 android_list.json 解析失败，跳过迁移: %v", err)
		return
	}

	// 当前模式已有独立文件：不动旧文件，也不做任何写入——
	// 各模式已加载各自的文件，旧文件里的数据不再被任何模式使用（保留原样备查）。
	targetPath := m.androidListPathForMode()
	if _, err := os.Stat(targetPath); err == nil {
		log.Printf("[config] 旧 android_list.json 未迁移（%s 已存在，旧文件 %d 条数据不再使用）", targetPath, len(legacyList))
		return
	}

	*m.androidListPtrLocked() = legacyList
	m.androidListDirtyPtr().Store(true)
	// 改名而非删除：迁移是一次性动作，改名后下次启动不会重复迁移，但数据仍可人工找回
	if err := os.Rename(legacy, legacy+".bak"); err != nil {
		log.Printf("[config] 旧 android_list.json 改名为 .bak 失败（迁移已完成，下次启动可能重复迁移）: %v", err)
	} else {
		log.Printf("[config] 迁移旧 android_list.json → %s（%d 个容器），旧文件已改名 .bak", targetPath, len(legacyList))
	}
}

// androidListPathForMode 返回当前模式容器列表的文件路径
func (m *Manager) androidListPathForMode() string {
	switch m.currentModeLocked() {
	case "public":
		return m.androidListPublicPath()
	case "v2":
		return m.androidListV2Path()
	case "v4":
		return m.androidListV4Path()
	}
	return m.androidListLocalPath()
}

func (m *Manager) GetAndroidList() []AndroidInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getAndroidListLocked()
}

// getAndroidListLocked 返回当前模式容器列表的拷贝（调用方需持读锁）
func (m *Manager) getAndroidListLocked() []AndroidInfo {
	list := *m.androidListPtrLocked()
	result := make([]AndroidInfo, len(list))
	copy(result, list)
	return result
}

// --- flush 用的按模式快照（各模式互不影响）---

func (m *Manager) getAndroidListLocalLocked() []AndroidInfo {
	result := make([]AndroidInfo, len(m.androidListLocal))
	copy(result, m.androidListLocal)
	return result
}

func (m *Manager) getAndroidListPublicLocked() []AndroidInfo {
	result := make([]AndroidInfo, len(m.androidListPublic))
	copy(result, m.androidListPublic)
	return result
}

func (m *Manager) getAndroidListV2Locked() []AndroidInfo {
	result := make([]AndroidInfo, len(m.androidListV2))
	copy(result, m.androidListV2)
	return result
}

func (m *Manager) getAndroidListV4Locked() []AndroidInfo {
	result := make([]AndroidInfo, len(m.androidListV4))
	copy(result, m.androidListV4)
	return result
}

func (m *Manager) SetAndroidList(androids []AndroidInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.androidListPtrLocked() = androids
	m.androidListDirtyPtr().Store(true)
}

// DeviceAndroidResult 单个设备的安卓查询结果
type DeviceAndroidResult struct {
	DeviceID string
	Androids []AndroidInfo
}

// androidsPatchedAfter 判断设备在时刻 t 之后是否被 WS 补丁过（调用方需持 m.mu，
// PatchAndroidStatus 写入、MergeAndroidList 读取）。
func (m *Manager) androidsPatchedAfter(deviceID string, t time.Time) bool {
	return m.androidLastPatchAt[deviceID].After(t)
}

// MergeAndroidList 增量合并：只替换成功获取的设备数据。
//
// WS 补丁竞态防护（queryStartedAt 非零时生效）：若某设备在查询发起后被 WS 补丁过
// （lastPatchAt 晚于 queryStartedAt），跳过该设备的替换——REST 结果是补丁前快照，
// 采用它会让刚被 WS 更新的状态回退。查询结果晚于补丁时（下次刷新/轮询）正常替换。
// queryStartedAt 为零值时维持原语义（无防护，v2 无 WS 不受影响）。
//
// 单设备空结果保护（resultIPs）：某设备本次结果为空但缓存中该设备原有条目非空时，
// 不覆盖、保留旧数据。mytsdk 忙时间歇返回 code=200 + data:[]（v2 实测），按成功
// 处理会让单台主机的容器列表被整体清空（截图全丢、列表闪断，用户需手动刷新）。
// 真实删光容器的场景由 ReplaceAndroidListForDevice（WS 快照/权威全量）与
// RemoveAndroidsByDevice（显式删除/离线清理）负责，不走本保护。结果非空时正常
// 覆盖，连续成功后缓存自然收敛。resultIPs: 设备ID → 查询来源 IP（仅失败日志用，
// 缺省时日志不打 IP）。
func (m *Manager) MergeAndroidList(results []DeviceAndroidResult, queryStartedAt time.Time, resultIPs map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 收集本次成功获取的设备ID
	updated := make(map[string]bool, len(results))
	for _, r := range results {
		if r.DeviceID == "" {
			continue
		}
		updated[r.DeviceID] = true
	}

	// 保留未更新设备的旧数据
	merged := make([]AndroidInfo, 0)
	for _, a := range *m.androidListPtrLocked() {
		if !updated[a.DeviceID] {
			merged = append(merged, a)
		}
	}

	// 追加本次获取的新数据（含空结果——设备确实没有安卓实例）。
	// 例外：空结果但缓存中该设备原有条目非空 → 判为「疑似误报空」，保留旧数据
	// 不覆盖（对齐老版 v2.html「失败置离线、数据保留」语义；v2 mytsdk 忙时间歇
	// 返回空列表，覆盖即清空用户可见列表）。
	for _, r := range results {
		if !updated[r.DeviceID] {
			continue
		}
		if !queryStartedAt.IsZero() && m.androidsPatchedAfter(r.DeviceID, queryStartedAt) {
			log.Printf("[MergeAndroidList] 设备 %s 在查询发起后被 WS 补丁过，跳过整体替换（防旧快照回退）", r.DeviceID)
			continue
		}
		if len(r.Androids) == 0 {
			// 缓存中该设备原本就无条目：空结果是真实事实（新设备/已删光），照常写入
			prev := m.androidsCountByDeviceLocked(r.DeviceID)
			if prev == 0 {
				continue
			}
			log.Printf("[MergeAndroidList] 设备 %s（ip=%s）返回空列表但缓存有 %d 条旧数据，疑似误报，保留旧数据不清空",
				r.DeviceID, resultIPs[r.DeviceID], prev)
			// 该设备旧条目在上方按 updated 已被丢弃，这里按原序补回
			for _, a := range *m.androidListPtrLocked() {
				if a.DeviceID == r.DeviceID {
					merged = append(merged, a)
				}
			}
			continue
		}
		merged = append(merged, r.Androids...)
	}

	if len(merged) == 0 && len(*m.androidListPtrLocked()) > 0 {
		log.Printf("[MergeAndroidList] 警告：合并后列表为空（旧=%d, results=%d），保留旧数据", len(*m.androidListPtrLocked()), len(results))
		m.androidListDirtyPtr().Store(true)
		return
	}

	*m.androidListPtrLocked() = merged
	m.androidListDirtyPtr().Store(true)
}

// androidsCountByDeviceLocked 统计缓存中指定设备的容器条目数（调用方需持 m.mu）
func (m *Manager) androidsCountByDeviceLocked(deviceID string) int {
	n := 0
	for _, a := range *m.androidListPtrLocked() {
		if a.DeviceID == deviceID {
			n++
		}
	}
	return n
}

// ReplaceAndroidListForDevice 用权威全量替换某台设备的容器缓存（允许替换为空）。
//
// 与 MergeAndroidList 的区别：后者在「合并后整体为空」时保留旧数据——那个保护服务于
// REST 查询失败/网关异常不能误清的场景。而 WS 快照 / 明确的 REST 全量是权威事实，
// 设备上最后一台实例被删除后必须清空；用 Merge 会让幽灵条目永久残留
// （V4 实例墙删除最后一台时必现）。
//
// 只替换 deviceID 对应的条目，其他设备的缓存原样保留。
func (m *Manager) ReplaceAndroidListForDevice(deviceID string, androids []AndroidInfo) {
	if deviceID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.androidListPtrLocked()
	kept := make([]AndroidInfo, 0, len(*list)+len(androids))
	for _, a := range *list {
		if a.DeviceID != deviceID {
			kept = append(kept, a)
		}
	}
	kept = append(kept, androids...)
	*list = kept
	m.androidListDirtyPtr().Store(true)
}

// RemoveAndroidsByDevice 清除指定设备的全部容器缓存（设备离线时调用）。
// 显式移除路径，不受 MergeAndroidList 的「合并为空保留旧数据」保护约束——
// 那个保护只服务于"查询失败不能误清"的 merge 语义，离线是明确的业务事实。
// 返回移除条目数（0=该设备本无缓存，调用方免发通知）。
func (m *Manager) RemoveAndroidsByDevice(deviceID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.androidListPtrLocked()
	kept := make([]AndroidInfo, 0, len(*list))
	for _, a := range *list {
		if a.DeviceID != deviceID {
			kept = append(kept, a)
		}
	}
	removed := len(*list) - len(kept)
	if removed == 0 {
		return 0
	}
	*list = kept
	m.androidListDirtyPtr().Store(true)
	return removed
}

// runnableStatuses 可运行状态集合（截图/投屏目标口径）。
// 对齐前端 frontend/src/utils/containerStatus.ts 的 RUNNABLE_STATUSES +
// normalizeStatus：running / starting / restarting / resetting，paused 归一
// starting。不含 created（未开机，前端明确拍板不可见，抓图必失败）、
// exited / dead / removing（关机/删除类）。
// 背景：v2 容器 state 抖动（Docker 汇报滞后/瞬时 exited）时，按精确
// Status=="running" 过滤会把截图目标整批剔除 → 前端清图闪断。放宽为
// 「可运行集合」后目标不随抖动消失，抓不到由截图退避机制兜住。
var runnableStatuses = map[string]bool{
	"running":    true,
	"starting":   true,
	"restarting": true,
	"resetting":  true,
	"paused":     true, // 前端 paused 归一 starting（pause 后 ADB/9082 仍可用）
}

// isRunnableStatus 状态是否在可运行集合内（未知状态按不可运行处理）
func isRunnableStatus(status string) bool {
	return runnableStatuses[status]
}

// GetRunningAndroids 返回「可运行」容器的截图/投屏目标集合。
//
// 口径（2026-11 v2 稳定性修复）：不再精确 Status=="running"，放宽为
// runnableStatuses 可运行集合——v2 容器 state 抖动时整批剔除会让截图全丢。
// 同时要求容器有可达地址（IP 或端口映射），纯名称条目抓不到图不入选。
// 关机/删除类（exited/dead/removing/created）维持排除。
func (m *Manager) GetRunningAndroids() []AndroidInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []AndroidInfo
	for _, a := range *m.androidListPtrLocked() {
		if !isRunnableStatus(a.Status) {
			continue
		}
		if a.IP == "" && len(a.PortBindings) == 0 && a.ContainerIP == "" {
			continue
		}
		result = append(result, a)
	}
	return result
}

func (m *Manager) GetAndroidsByDevice(deviceID string) []AndroidInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []AndroidInfo
	for _, a := range *m.androidListPtrLocked() {
		if a.DeviceID == deviceID {
			result = append(result, a)
		}
	}
	return result
}

func (m *Manager) CleanAndroidsByDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.androidListPtrLocked()
	filtered := make([]AndroidInfo, 0, len(*list))
	for _, a := range *list {
		if a.DeviceID != deviceID {
			filtered = append(filtered, a)
		}
	}
	*list = filtered
	m.androidListDirtyPtr().Store(true)
}

// PruneAndroidsOutsideDeviceList 丢弃「设备不在当前模式设备列表里」的容器缓存，
// 返回丢弃条数。用于切换部署模式后清理跨模式残留：
// 容器列表已按模式隔离，切换不再整表清空，但旧模式连接的残余 WS 事件
// （snapshot / container lifecycle）会写进新模式的数据桶——MergeAndroidList
// 只按 DeviceID 替换，不校验该设备是否属于当前模式，故切换后按当前模式
// 设备列表裁剪一次即可，当前模式自己的缓存（设备仍在列表）原样保留。
func (m *Manager) PruneAndroidsOutsideDeviceList() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	known := make(map[string]struct{})
	for _, d := range *m.deviceListPtrLocked() {
		known[d.DeviceID] = struct{}{}
	}
	list := m.androidListPtrLocked()
	kept := make([]AndroidInfo, 0, len(*list))
	for _, a := range *list {
		if _, ok := known[a.DeviceID]; ok {
			kept = append(kept, a)
		}
	}
	removed := len(*list) - len(kept)
	if removed == 0 {
		return 0
	}
	*list = kept
	m.androidListDirtyPtr().Store(true)
	return removed
}
