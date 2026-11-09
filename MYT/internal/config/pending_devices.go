package config

import (
	"sync/atomic"
)

// --- 待添加设备列表（按部署模式隔离，与正式设备列表对称）---
// local:  pending_devices_local.json  — local 模式 UDP 发现（v3 设备）
// public: pending_devices_public.json — public 模式（保留对称位，当前扫描不产生）
// v2:     pending_devices_v2.json     — v2 模式扫描/手动添加（V2 主机）
// v4:     pending_devices_v4.json     — v4 模式（发现逻辑待定，见 TODO）
// 旧版共享 pending_devices.json 仅用于加载时识别并丢弃（见 LoadAll）。

func (m *Manager) pendingDevicesLocalPath() string {
	return m.ConfigDir() + "/pending_devices_local.json"
}

func (m *Manager) pendingDevicesPublicPath() string {
	return m.ConfigDir() + "/pending_devices_public.json"
}

func (m *Manager) pendingDevicesV2Path() string {
	return m.ConfigDir() + "/pending_devices_v2.json"
}

func (m *Manager) pendingDevicesV4Path() string {
	return m.ConfigDir() + "/pending_devices_v4.json"
}

// legacyPendingDevicesPath 旧版共享文件路径，仅用于加载时识别丢弃
func (m *Manager) legacyPendingDevicesPath() string {
	return m.ConfigDir() + "/pending_devices.json"
}

// pendingDevicesPtrLocked 返回当前模式待添加列表的指针（调用方需持锁）
func (m *Manager) pendingDevicesPtrLocked() *[]DeviceCache {
	switch m.currentModeLocked() {
	case "public":
		return &m.pendingDevicesPublic
	case "v2":
		return &m.pendingDevicesV2
	case "v4":
		return &m.pendingDevicesV4
	}
	return &m.pendingDevicesLocal
}

// pendingDevicesDirtyPtr 返回当前模式待添加列表的脏标记指针
func (m *Manager) pendingDevicesDirtyPtr() *atomic.Bool {
	switch m.currentModeLocked() {
	case "public":
		return &m.pendingDevicesPublicDirty
	case "v2":
		return &m.pendingDevicesV2Dirty
	case "v4":
		return &m.pendingDevicesV4Dirty
	}
	return &m.pendingDevicesLocalDirty
}

// GetPendingDevices 返回当前模式的待添加设备列表。
// 防御过滤：跳过已存在于当前模式正式列表的设备（避免历史残留/写入入口竞态
// 导致「已在正式列表的设备仍出现在待添加弹窗」），但不清底层存储——
// 只影响读取视图，正式列表条目仍由 AddPendingDevices 的移动语义负责。
func (m *Manager) GetPendingDevices() []DeviceCache {
	m.mu.RLock()
	defer m.mu.RUnlock()
	existing := make(map[string]struct{})
	for _, d := range *m.deviceListPtrLocked() {
		existing[d.DeviceID] = struct{}{}
	}
	list := m.getPendingDevicesLocked()
	result := list[:0:0]
	for _, d := range list {
		if _, ok := existing[d.DeviceID]; !ok {
			result = append(result, d)
		}
	}
	return result
}

func (m *Manager) getPendingDevicesLocked() []DeviceCache {
	list := *m.pendingDevicesPtrLocked()
	result := make([]DeviceCache, len(list))
	copy(result, list)
	return result
}

// getPendingLocalLocked 返回 local 待添加列表拷贝（flush 用）
func (m *Manager) getPendingLocalLocked() []DeviceCache {
	result := make([]DeviceCache, len(m.pendingDevicesLocal))
	copy(result, m.pendingDevicesLocal)
	return result
}

// getPendingPublicLocked 返回 public 待添加列表拷贝（flush 用）
func (m *Manager) getPendingPublicLocked() []DeviceCache {
	result := make([]DeviceCache, len(m.pendingDevicesPublic))
	copy(result, m.pendingDevicesPublic)
	return result
}

// getPendingV2Locked 返回 v2 待添加列表拷贝（flush 用）
func (m *Manager) getPendingV2Locked() []DeviceCache {
	result := make([]DeviceCache, len(m.pendingDevicesV2))
	copy(result, m.pendingDevicesV2)
	return result
}

// getPendingV4Locked 返回 v4 待添加列表拷贝（flush 用）
func (m *Manager) getPendingV4Locked() []DeviceCache {
	result := make([]DeviceCache, len(m.pendingDevicesV4))
	copy(result, m.pendingDevicesV4)
	return result
}

// IsPendingDevice 检查设备是否在当前模式的待添加列表中
func (m *Manager) IsPendingDevice(deviceID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, d := range *m.pendingDevicesPtrLocked() {
		if d.DeviceID == deviceID {
			return true
		}
	}
	return false
}

// UpsertPendingDevice 新增或更新当前模式的待添加设备
func (m *Manager) UpsertPendingDevice(d DeviceCache) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.pendingDevicesPtrLocked()
	for i, existing := range *list {
		if existing.DeviceID == d.DeviceID {
			// 更新信息
			(*list)[i].IP = d.IP
			(*list)[i].DeviceType = d.DeviceType
			(*list)[i].DeviceName = d.DeviceName
			(*list)[i].Version = d.Version
			(*list)[i].SuperSdkReady = d.SuperSdkReady // v2 准入放宽：待添加也带 81 就绪标注
			return
		}
	}
	*list = append(*list, d)
	m.pendingDevicesDirtyPtr().Store(true)
}

// AddPendingDevices 将选中的待添加设备移入正式设备列表
func (m *Manager) AddPendingDevices(deviceIDs []string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	idSet := make(map[string]bool, len(deviceIDs))
	for _, id := range deviceIDs {
		idSet[id] = true
	}

	added := 0
	var remaining []DeviceCache
	list := m.pendingDevicesPtrLocked()
	for _, d := range *list {
		if idSet[d.DeviceID] {
			m.upsertDeviceLocked(d)
			added++
		} else {
			remaining = append(remaining, d)
		}
	}

	if added > 0 {
		*list = remaining
		m.pendingDevicesDirtyPtr().Store(true)
	}
	return added
}

// AddAllPendingDevices 将所有待添加设备移入正式设备列表
func (m *Manager) AddAllPendingDevices() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.pendingDevicesPtrLocked()
	for _, d := range *list {
		m.upsertDeviceLocked(d)
	}
	added := len(*list)
	*list = nil
	m.pendingDevicesDirtyPtr().Store(true)
	return added
}

// ClearPendingDevices 清空当前模式的待添加设备列表
func (m *Manager) ClearPendingDevices() {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.pendingDevicesPtrLocked()
	if len(*list) > 0 {
		*list = nil
		m.pendingDevicesDirtyPtr().Store(true)
	}
}

// RemovePendingDevice 从当前模式的待添加列表中移除指定设备
func (m *Manager) RemovePendingDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.pendingDevicesPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			*list = append((*list)[:i], (*list)[i+1:]...)
			m.pendingDevicesDirtyPtr().Store(true)
			return
		}
	}
}

// PendingDevicesCount 返回当前模式的待添加设备数量
func (m *Manager) PendingDevicesCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(*m.pendingDevicesPtrLocked())
}
