package config

// --- 设备备注 device_notes.json ---

func (m *Manager) deviceNotesPath() string {
	return m.ConfigDir() + "/device_notes.json"
}

func (m *Manager) GetDeviceNote(deviceID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deviceNotes[deviceID]
}

func (m *Manager) SetDeviceNote(deviceID, note string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if note == "" {
		delete(m.deviceNotes, deviceID)
	} else {
		m.deviceNotes[deviceID] = note
	}
	m.deviceNotesDirty.Store(true)
}

func (m *Manager) GetAllDeviceNotes() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getDeviceNotesLocked()
}

func (m *Manager) getDeviceNotesLocked() map[string]string {
	result := make(map[string]string, len(m.deviceNotes))
	for k, v := range m.deviceNotes {
		result[k] = v
	}
	return result
}

// --- 安卓备注 android_notes.json ---

func (m *Manager) androidNotesPath() string {
	return m.ConfigDir() + "/android_notes.json"
}

func (m *Manager) GetAndroidNote(androidID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.androidNotes[androidID]
}

func (m *Manager) SetAndroidNote(androidID, note string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if note == "" {
		delete(m.androidNotes, androidID)
	} else {
		m.androidNotes[androidID] = note
	}
	m.androidNotesDirty.Store(true)
}

func (m *Manager) GetAllAndroidNotes() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getAndroidNotesLocked()
}

func (m *Manager) getAndroidNotesLocked() map[string]string {
	result := make(map[string]string, len(m.androidNotes))
	for k, v := range m.androidNotes {
		result[k] = v
	}
	return result
}

// CleanOrphanNotes 清理不存在的安卓实例备注
func (m *Manager) CleanOrphanNotes(validIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	validSet := make(map[string]bool, len(validIDs))
	for _, id := range validIDs {
		validSet[id] = true
	}
	changed := false
	for id := range m.androidNotes {
		if !validSet[id] {
			delete(m.androidNotes, id)
			changed = true
		}
	}
	if changed {
		m.androidNotesDirty.Store(true)
	}
}
