package config

// --- 设备查询 ---

// GetDevice 获取单个设备信息
func (m *Manager) GetDevice(deviceID string) *DeviceCache {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := m.deviceListPtrLocked()
	for _, d := range *list {
		if d.DeviceID == deviceID {
			cp := d
			return &cp
		}
	}
	return nil
}

// GetDeviceByIP 按 IP 查设备（v2 旧数据兜底：早期版本添加的条目 DeviceID 为空、
// 以 IP 为主键，前端传 IP 过来时按 IP 匹配）。
func (m *Manager) GetDeviceByIP(ip string) *DeviceCache {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := m.deviceListPtrLocked()
	for _, d := range *list {
		if d.IP == ip {
			cp := d
			return &cp
		}
	}
	return nil
}

// UpdateDeviceStateByIP 按 IP 更新在线状态（v2 旧数据兜底：条目 DeviceID 为空、
// 无法按 ID 定位时）。返回是否命中并更新。
func (m *Manager) UpdateDeviceStateByIP(ip string, state int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i := range *list {
		d := &(*list)[i]
		if d.IP == ip && d.State != state {
			d.State = state
			m.deviceListDirtyPtr().Store(true)
			return true
		}
	}
	return false
}

// FillDeviceIDByIP 回填空 DeviceID（v2 旧数据自愈）：UDP/扫描拿到序列号后，
// 按 IP 找到 DeviceID 为空的条目填入序列号并标脏落档。返回是否发生了回填。
func (m *Manager) FillDeviceIDByIP(ip, deviceID string) bool {
	if ip == "" || deviceID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i := range *list {
		d := &(*list)[i]
		if d.IP == ip && d.DeviceID == "" {
			d.DeviceID = deviceID
			m.deviceListDirtyPtr().Store(true)
			return true
		}
	}
	return false
}

func (m *Manager) GetOnlineDevices() []DeviceCache {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := m.deviceListPtrLocked()
	var result []DeviceCache
	for _, d := range *list {
		if d.State == 0 {
			result = append(result, d)
		}
	}
	return result
}

func (m *Manager) GetOnlineDeviceIPs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := m.deviceListPtrLocked()
	var ips []string
	for _, d := range *list {
		if d.State == 0 {
			ips = append(ips, d.IP)
		}
	}
	return ips
}

func (m *Manager) GetOnlineDeviceIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := m.deviceListPtrLocked()
	var ids []string
	for _, d := range *list {
		if d.State == 0 {
			ids = append(ids, d.DeviceID)
		}
	}
	return ids
}

func (m *Manager) GetAllDeviceIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := m.deviceListPtrLocked()
	var ids []string
	for _, d := range *list {
		ids = append(ids, d.DeviceID)
	}
	return ids
}
