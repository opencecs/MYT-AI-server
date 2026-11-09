package config

import (
	"log"
	"strconv"
)

// FlexInt 兼容 JSON 中的数字和字符串（魔云腾 API 类型不稳定）
type FlexInt int

func (f *FlexInt) UnmarshalJSON(data []byte) error {
	s := string(data)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') {
		n, err := strconv.Atoi(s[1 : len(s)-1])
		if err != nil {
			*f = 0
			return nil
		}
		*f = FlexInt(n)
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		*f = 0
		return nil
	}
	*f = FlexInt(n)
	return nil
}

func (f FlexInt) Int() int { return int(f) }

// DeviceDetail 设备详情（来自 /info/device），内存缓存+异步落档
type DeviceDetail struct {
	DeviceID  string  `json:"deviceId"`
	IP        string  `json:"ip"`
	Model     string  `json:"model"`
	Version   string  `json:"version"`
	// SDKVersion SDK 版本数值（hello 的 "v208" → 208）。hello 实时写入，
	// 升级轮询读它避免逐台 HTTP 探测；system/stats 事件不带此字段，
	// HandleSystemStats 保留清单必须含它（否则 20s 心跳洗成 0）
	SDKVersion int    `json:"sdkVersion,omitempty"`
	MMCTotal   FlexInt `json:"mmctotal"`
	MMCUse    FlexInt `json:"mmcuse"`
	MMCModel  string  `json:"mmcmodel"`
	MMCTemp   string  `json:"mmctemp"`
	MMCRead   string  `json:"mmcread"`
	MMCWrite  string  `json:"mmcwrite"`
	CPUTemp   FlexInt `json:"cputemp"`
	CPULoad   string  `json:"cpuload"`
	MemTotal  string  `json:"memtotal"`
	MemUse    string  `json:"memuse"`
	Speed     string  `json:"speed"`
	SysUptime string  `json:"sysuptime"`
}

func (m *Manager) deviceDetailsPath() string {
	return m.ConfigDir() + "/device_details.json"
}

func (m *Manager) GetDeviceDetail(deviceID string) *DeviceDetail {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.deviceDetails[deviceID]; ok {
		cp := *d
		return &cp
	}
	return nil
}

func (m *Manager) GetAllDeviceDetails() map[string]*DeviceDetail {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getAllDeviceDetailsLocked()
}

func (m *Manager) getAllDeviceDetailsLocked() map[string]*DeviceDetail {
	result := make(map[string]*DeviceDetail, len(m.deviceDetails))
	for k, v := range m.deviceDetails {
		result[k] = v
	}
	return result
}

func (m *Manager) SetDeviceDetail(deviceID string, detail *DeviceDetail) {
	m.mu.Lock()
	m.deviceDetails[deviceID] = detail
	m.mu.Unlock()
	m.deviceDetailsDirty.Store(true)
}

func (m *Manager) DeleteDeviceDetail(deviceID string) {
	m.mu.Lock()
	delete(m.deviceDetails, deviceID)
	m.mu.Unlock()
	m.deviceDetailsDirty.Store(true)
}

func (m *Manager) loadDeviceDetails() {
	var details map[string]*DeviceDetail
	if err := m.readJSON(m.deviceDetailsPath(), &details); err != nil {
		return
	}
	if details != nil {
		m.deviceDetails = details
	}
	log.Printf("[config] 加载设备详情: %d 条", len(m.deviceDetails))
}
