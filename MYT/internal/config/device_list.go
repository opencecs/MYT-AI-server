package config

import (
	"log"
	"sync/atomic"

	"github.com/moyunteng/myt/internal/device/discover"
)

// --- 设备列表（按部署模式隔离）---
// local:  device_list_local.json  — UDP 发现 + 手动添加，含离线设备
// public: device_list_public.json — 网关拉取 + /info 探测，仅在线设备
// v2:     device_list_v2.json     — V2 主机（Docker 2375 探测添加）
// v4:     device_list_v4.json     — V4 设备（发现/连接逻辑待定，见各 TODO）

func (m *Manager) deviceListLocalPath() string {
	return m.ConfigDir() + "/device_list_local.json"
}

func (m *Manager) deviceListPublicPath() string {
	return m.ConfigDir() + "/device_list_public.json"
}

func (m *Manager) deviceListV2Path() string {
	return m.ConfigDir() + "/device_list_v2.json"
}

func (m *Manager) deviceListV4Path() string {
	return m.ConfigDir() + "/device_list_v4.json"
}

// legacyDeviceListPath 旧版共享文件路径，仅用于迁移
func (m *Manager) legacyDeviceListPath() string {
	return m.ConfigDir() + "/device_list.json"
}

// migrateDeviceCache 从旧 config.json 迁移 device_cache，补充默认 State/Order
func migrateDeviceCache(old []DeviceCache) []DeviceCache {
	result := make([]DeviceCache, len(old))
	for i, d := range old {
		result[i] = d
		if result[i].State == 0 && result[i].Order == 0 {
			result[i].State = 1 // 默认离线，等心跳确认
		}
	}
	return result
}

// currentModeLocked 返回当前部署模式（调用方需持锁）
func (m *Manager) currentModeLocked() string {
	if m.config.DeployMode == "" {
		return "local"
	}
	return m.config.DeployMode
}

// deviceListPtrLocked 返回当前模式设备列表的指针（调用方需持写锁）
func (m *Manager) deviceListPtrLocked() *[]DeviceCache {
	switch m.currentModeLocked() {
	case "public":
		return &m.deviceListPublic
	case "v2":
		return &m.deviceListV2
	case "v4":
		return &m.deviceListV4
	}
	return &m.deviceListLocal
}

// deviceListDirtyPtr 返回当前模式列表的脏标记指针
func (m *Manager) deviceListDirtyPtr() *atomic.Bool {
	switch m.currentModeLocked() {
	case "public":
		return &m.deviceListPublicDirty
	case "v2":
		return &m.deviceListV2Dirty
	case "v4":
		return &m.deviceListV4Dirty
	}
	return &m.deviceListLocalDirty
}

// getDeviceListLocked 返回当前模式列表的拷贝（调用方需持读锁）
func (m *Manager) getDeviceListLocked() []DeviceCache {
	list := *m.deviceListPtrLocked()
	result := make([]DeviceCache, len(list))
	copy(result, list)
	return result
}

// getDeviceListLocalLocked 返回 local 列表拷贝（flush 用）
func (m *Manager) getDeviceListLocalLocked() []DeviceCache {
	result := make([]DeviceCache, len(m.deviceListLocal))
	copy(result, m.deviceListLocal)
	return result
}

// getDeviceListPublicLocked 返回 public 列表拷贝（flush 用）
func (m *Manager) getDeviceListPublicLocked() []DeviceCache {
	result := make([]DeviceCache, len(m.deviceListPublic))
	copy(result, m.deviceListPublic)
	return result
}

// getDeviceListV2Locked 返回 v2 列表拷贝（flush 用）
func (m *Manager) getDeviceListV2Locked() []DeviceCache {
	result := make([]DeviceCache, len(m.deviceListV2))
	copy(result, m.deviceListV2)
	return result
}

// getDeviceListV4Locked 返回 v4 列表拷贝（flush 用）
func (m *Manager) getDeviceListV4Locked() []DeviceCache {
	result := make([]DeviceCache, len(m.deviceListV4))
	copy(result, m.deviceListV4)
	return result
}

func (m *Manager) GetDeviceList() []DeviceCache {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getDeviceListLocked()
}

// GetDeviceListAsDiscover 返回 discover.Device 格式的当前设备列表（手动发现被防重入时的降级返回）
func (m *Manager) GetDeviceListAsDiscover() []discover.Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := *m.deviceListPtrLocked()
	result := make([]discover.Device, 0, len(list))
	for _, d := range list {
		result = append(result, discover.Device{
			IP:         d.IP,
			DeviceType: d.DeviceType,
			DeviceID:   d.DeviceID,
			DeviceName: d.DeviceName,
			Version:    d.Version,
		})
	}
	return result
}

func (m *Manager) SetDeviceList(devices []DeviceCache) {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.deviceListPtrLocked() = devices
	m.deviceListDirtyPtr().Store(true)
}

// SetDeviceListAllowEmpty 与 SetDeviceList 相同，但显式放行「空列表落档」一次。
// 仅用于「网关权威返回空列表」（账号下确实没有设备）这类合法清空场景——
// 落档防呆（flushDirty）默认拒绝内存空列表覆盖磁盘已有内容，用于挡住网关异常
// 返回空集合时的误抹；该场景下空是真实结果，必须写盘，否则磁盘残留旧列表、
// 重启后已下架的设备"复活"。
func (m *Manager) SetDeviceListAllowEmpty(devices []DeviceCache) {
	m.mu.Lock()
	defer m.mu.Unlock()
	*m.deviceListPtrLocked() = devices
	m.deviceListDirtyPtr().Store(true)
	if len(devices) == 0 {
		m.allowEmptyFlush.Store(true)
	}
}

// UpsertDevice 新增或更新设备，变化才更新，标记脏
func (m *Manager) UpsertDevice(d DeviceCache) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertDeviceLocked(d)
}

// UpsertDeviceCheck 新增或更新设备，返回是否为新设备
func (m *Manager) UpsertDeviceCheck(d DeviceCache) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.upsertDeviceLocked(d)
}

// upsertDeviceLocked 内部实现，返回 true 表示新设备或信息有变化
func (m *Manager) upsertDeviceLocked(d DeviceCache) bool {
	list := m.deviceListPtrLocked()
	for i, existing := range *list {
		if existing.DeviceID == d.DeviceID {
			if existing.IP == d.IP && existing.DeviceType == d.DeviceType &&
				existing.DeviceName == d.DeviceName && existing.Version == d.Version {
				return false
			}
			(*list)[i].IP = d.IP
			(*list)[i].DeviceType = d.DeviceType
			(*list)[i].DeviceName = d.DeviceName
			(*list)[i].Version = d.Version
			m.deviceListDirtyPtr().Store(true)
			return true
		}
	}

	*list = append(*list, d)
	m.deviceListDirtyPtr().Store(true)
	log.Printf("[config] 新设备加入列表: id=%s ip=%s type=%s, 列表总数: %d", safeID(d.DeviceID), d.IP, d.DeviceType, len(*list))
	return true
}

// --- 后续方法已按职责拆分到：---
//   - device_list_update.go   （Update*/Remove*/SetDeviceType）
//   - device_list_query.go     （GetDevice/GetOnline*）
//   - device_list_sort.go      （SortDeviceListByIP/compareIP/safeID）
