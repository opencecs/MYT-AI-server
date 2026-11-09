package main

import (
	"log"

	"github.com/moyunteng/myt/internal/config"
)

// --- IP 映射 ---

// UpdateIPMapping 更新 IP → deviceID 映射（心跳刷新后调用）
func (s *SdkApiServiceWrapper) UpdateIPMapping(ip, deviceID string) {
	s.passwordsMu.Lock()
	s.ipToDeviceID[ip] = deviceID
	s.passwordsMu.Unlock()
}

// GetDeviceIDByIP 通过 IP 查找 deviceID
func (s *SdkApiServiceWrapper) GetDeviceIDByIP(deviceIP string) string {
	s.passwordsMu.RLock()
	defer s.passwordsMu.RUnlock()
	return s.ipToDeviceID[deviceIP]
}

// UpdateDevicePasswordForIP 通过 IP 更新设备密码（认证弹窗成功后调用）
func (s *SdkApiServiceWrapper) UpdateDevicePasswordForIP(deviceIP, password string) error {
	deviceID := s.GetDeviceIDByIP(deviceIP)
	s.passwordsMu.Lock()
	if deviceID != "" {
		s.devicePasswords[deviceID] = password
	}
	s.ipToDeviceID[deviceIP] = deviceID
	s.passwordsMu.Unlock()
	if s.cm != nil && deviceID != "" {
		if err := s.cm.SetDevicePasswordInConfig(deviceID, password); err != nil {
			log.Printf("[sdkapi] 持久化设备密码失败: %v", err)
		}
	}
	return nil
}

// RebuildIPMappings 从设备列表重建 IP → deviceID 映射（启动时调用）
func (s *SdkApiServiceWrapper) RebuildIPMappings(devices []config.DeviceCache) {
	s.passwordsMu.Lock()
	for _, d := range devices {
		s.ipToDeviceID[d.IP] = d.DeviceID
	}
	s.passwordsMu.Unlock()
}
