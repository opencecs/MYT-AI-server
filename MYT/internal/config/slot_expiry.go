package config

// --- 坑位到期 slot_expiry.json ---

func (m *Manager) slotExpiryPath() string {
	return m.ConfigDir() + "/slot_expiry.json"
}

func (m *Manager) GetSlotExpiry() map[string][]SlotExpiry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getSlotExpiryLocked()
}

func (m *Manager) getSlotExpiryLocked() map[string][]SlotExpiry {
	result := make(map[string][]SlotExpiry, len(m.slotExpiry))
	for k, v := range m.slotExpiry {
		slots := make([]SlotExpiry, len(v))
		copy(slots, v)
		result[k] = slots
	}
	return result
}

// MergeSlotExpiry 合并写入坑位到期缓存：对每个 deviceID 键覆盖进现有 map，
// 未出现在本次结果中的设备键保留旧缓存（查询失败的设备不丢历史到期数据）。
func (m *Manager) MergeSlotExpiry(expiry map[string][]SlotExpiry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.slotExpiry == nil {
		m.slotExpiry = make(map[string][]SlotExpiry, len(expiry))
	}
	for k, v := range expiry {
		m.slotExpiry[k] = v
	}
	m.slotExpiryDirty.Store(true)
}

func (m *Manager) GetDeviceSlotExpiry(deviceID string) []SlotExpiry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	slots, ok := m.slotExpiry[deviceID]
	if !ok {
		return nil
	}
	result := make([]SlotExpiry, len(slots))
	copy(result, slots)
	return result
}
