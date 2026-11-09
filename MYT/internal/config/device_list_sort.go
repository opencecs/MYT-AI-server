package config

import "strings"

// --- 设备列表按 IP 排序 ---

func (m *Manager) SortDeviceListByIP() {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	if sortDevicesByIP(*list) {
		m.deviceListDirtyPtr().Store(true)
	}
}

func sortDevicesByIP(devices []DeviceCache) bool {
	changed := false
	for i := 0; i < len(devices); i++ {
		for j := i + 1; j < len(devices); j++ {
			if compareIP(devices[i].IP, devices[j].IP) > 0 {
				devices[i], devices[j] = devices[j], devices[i]
				changed = true
			}
		}
	}
	return changed
}

func compareIP(a, b string) int {
	return CompareIP(a, b)
}

// CompareIP 按数值比较 IPv4（10.10.1.9 < 10.10.1.10），非法 IP 退回字符串比较。
// 导出版本，供其它包复用（如截图轮询排序）。
func CompareIP(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	for i := 0; i < 4; i++ {
		va, vb := 0, 0
		if i < len(partsA) {
			for _, c := range partsA[i] {
				va = va*10 + int(c-'0')
			}
		}
		if i < len(partsB) {
			for _, c := range partsB[i] {
				vb = vb*10 + int(c-'0')
			}
		}
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}
	return 0
}

// safeID 截断设备 ID 用于日志
func safeID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
