package config

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// DeviceGroup 设备分组
// Mode 标记分组所属部署模式（local/public），公网与本地分组隔离，
// 避免公网设备分组与本地设备分组互相污染。
// v2 与 local 共用同一个分组桶：两种模式发现的是同一批物理主机
// （UDP 序列号相同——v3 回复 lgcloud:{serial}:{model}_v3，v2 回复
// lgcloudpro:{serial}:{model}，DeviceID 同为序列号），设备表按同 ID
// 合并，分组按 DeviceID 挂成员天然跨模式有效；若按 "v2" 再隔离，
// 用户在 local 建的分组切到 v2 就全部消失（历史问题）。
type DeviceGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	DeviceIDs []string  `json:"deviceIds"`
	Mode      string    `json:"mode,omitempty"` // "local" / "public"（v2 归一化为 local），空视为 local
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// normalizeGroupMode 分组桶归一：v2 与 local 同桶（物理设备同一批），public 独立。
func normalizeGroupMode(mode string) string {
	if mode == "public" {
		return "public"
	}
	return "local"
}

func newGroupID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- device_groups.json ---

func (m *Manager) deviceGroupsPath() string {
	return m.ConfigDir() + "/device_groups.json"
}

func (m *Manager) getDeviceGroupsLocked() []DeviceGroup {
	result := make([]DeviceGroup, len(m.deviceGroups))
	copy(result, m.deviceGroups)
	return result
}

// GetDeviceGroups 返回当前部署模式的分组（public 独立桶；v2 与 local 共桶）
func (m *Manager) GetDeviceGroups() []DeviceGroup {
	mode := normalizeGroupMode(m.GetDeployMode()) // 先取 mode，避免在锁内重复 RLock
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]DeviceGroup, 0, len(m.deviceGroups))
	for _, g := range m.deviceGroups {
		gMode := normalizeGroupMode(g.Mode) // 兼容旧数据（空→local）与历史 "v2" 分组
		if gMode == mode {
			result = append(result, g)
		}
	}
	return result
}

// CreateDeviceGroup 创建分组（自动绑定当前部署模式；v2 归一化为 local）
func (m *Manager) CreateDeviceGroup(name string) DeviceGroup {
	mode := normalizeGroupMode(m.GetDeployMode()) // 先取 mode，避免在写锁内调 GetDeployMode 死锁
	m.mu.Lock()
	defer m.mu.Unlock()

	g := DeviceGroup{
		ID:        newGroupID(),
		Name:      name,
		DeviceIDs: []string{},
		Mode:      mode,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	m.deviceGroups = append(m.deviceGroups, g)
	m.deviceGroupsDirty.Store(true)
	return g
}

// UpdateDeviceGroup 更新分组名称
func (m *Manager) UpdateDeviceGroup(groupID, name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.deviceGroups {
		if g.ID == groupID {
			m.deviceGroups[i].Name = name
			m.deviceGroups[i].UpdatedAt = time.Now()
			m.deviceGroupsDirty.Store(true)
			return true
		}
	}
	return false
}

// DeleteDeviceGroup 删除分组
func (m *Manager) DeleteDeviceGroup(groupID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.deviceGroups {
		if g.ID == groupID {
			m.deviceGroups = append(m.deviceGroups[:i], m.deviceGroups[i+1:]...)
			m.deviceGroupsDirty.Store(true)
			return true
		}
	}
	return false
}

// AddDeviceToGroup 将设备加入分组（幂等，已存在则跳过）
func (m *Manager) AddDeviceToGroup(groupID, deviceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.deviceGroups {
		if g.ID == groupID {
			for _, id := range g.DeviceIDs {
				if id == deviceID {
					return true // 已存在
				}
			}
			m.deviceGroups[i].DeviceIDs = append(m.deviceGroups[i].DeviceIDs, deviceID)
			m.deviceGroups[i].UpdatedAt = time.Now()
			m.deviceGroupsDirty.Store(true)
			return true
		}
	}
	return false
}

// RemoveDeviceFromGroup 将设备从分组中移除
func (m *Manager) RemoveDeviceFromGroup(groupID, deviceID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.deviceGroups {
		if g.ID == groupID {
			for j, id := range g.DeviceIDs {
				if id == deviceID {
					m.deviceGroups[i].DeviceIDs = append(m.deviceGroups[i].DeviceIDs[:j], m.deviceGroups[i].DeviceIDs[j+1:]...)
					m.deviceGroups[i].UpdatedAt = time.Now()
					m.deviceGroupsDirty.Store(true)
					return true
				}
			}
			return false
		}
	}
	return false
}

// SetGroupDevices 设置分组的设备列表（覆盖）
func (m *Manager) SetGroupDevices(groupID string, deviceIDs []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, g := range m.deviceGroups {
		if g.ID == groupID {
			m.deviceGroups[i].DeviceIDs = deviceIDs
			m.deviceGroups[i].UpdatedAt = time.Now()
			m.deviceGroupsDirty.Store(true)
			return true
		}
	}
	return false
}

// GetDevicesByGroup 获取分组下的设备列表（返回 DeviceCache）
func (m *Manager) GetDevicesByGroup(groupID string) []DeviceCache {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var group *DeviceGroup
	for _, g := range m.deviceGroups {
		if g.ID == groupID {
			group = &g
			break
		}
	}
	if group == nil {
		return nil
	}

	deviceMap := make(map[string]DeviceCache, len(*m.deviceListPtrLocked()))
	for _, d := range *m.deviceListPtrLocked() {
		deviceMap[d.DeviceID] = d
	}

	var result []DeviceCache
	for _, id := range group.DeviceIDs {
		if d, ok := deviceMap[id]; ok {
			result = append(result, d)
		}
	}
	return result
}

// GetUngroupedDevices 获取未分组的设备列表
func (m *Manager) GetUngroupedDevices() []DeviceCache {
	m.mu.RLock()
	defer m.mu.RUnlock()

	grouped := make(map[string]bool)
	for _, g := range m.deviceGroups {
		for _, id := range g.DeviceIDs {
			grouped[id] = true
		}
	}

	var result []DeviceCache
	for _, d := range *m.deviceListPtrLocked() {
		if !grouped[d.DeviceID] {
			result = append(result, d)
		}
	}
	return result
}

// RemoveDeviceFromAllGroups 将设备从所有分组中移除（设备删除时调用）
func (m *Manager) RemoveDeviceFromAllGroups(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	changed := false
	for i, g := range m.deviceGroups {
		for j, id := range g.DeviceIDs {
			if id == deviceID {
				m.deviceGroups[i].DeviceIDs = append(m.deviceGroups[i].DeviceIDs[:j], m.deviceGroups[i].DeviceIDs[j+1:]...)
				m.deviceGroups[i].UpdatedAt = time.Now()
				changed = true
				break
			}
		}
	}
	if changed {
		m.deviceGroupsDirty.Store(true)
	}
}
