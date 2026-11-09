package main

import "log"

// --- 设备密码管理 ---

// UpdateDevicePasswords 更新设备密码（启动时从配置恢复）
func (s *SdkApiServiceWrapper) UpdateDevicePasswords(passwords map[string]string) {
	s.passwordsMu.Lock()
	s.devicePasswords = passwords
	s.passwordsMu.Unlock()
}

// GetDevicePassword 获取指定设备的密码（通过 IP 查找）
func (s *SdkApiServiceWrapper) GetDevicePassword(deviceIP string) string {
	s.passwordsMu.RLock()
	defer s.passwordsMu.RUnlock()
	if id, ok := s.ipToDeviceID[deviceIP]; ok {
		return s.devicePasswords[id]
	}
	return ""
}

// GetAllDevicePasswords 获取所有设备密码
func (s *SdkApiServiceWrapper) GetAllDevicePasswords() map[string]string {
	s.passwordsMu.RLock()
	defer s.passwordsMu.RUnlock()
	cp := make(map[string]string)
	for k, v := range s.devicePasswords {
		cp[k] = v
	}
	return cp
}

// SetDevicePassword 调用设备API设置/修改密码
func (s *SdkApiServiceWrapper) SetDevicePassword(deviceID, deviceIP, currentPwd, newPwd string) error {
	client := s.newClient(deviceIP)
	if currentPwd != "" {
		client.SetPassword(currentPwd)
	}
	if err := client.SetDevicePassword(newPwd, newPwd); err != nil {
		return err
	}
	s.passwordsMu.Lock()
	s.devicePasswords[deviceID] = newPwd
	s.ipToDeviceID[deviceIP] = deviceID
	s.passwordsMu.Unlock()
	if s.cm != nil {
		if err := s.cm.SetDevicePasswordInConfig(deviceID, newPwd); err != nil {
			log.Printf("[sdkapi] 持久化设备密码失败: %v", err)
		}
	}
	return nil
}

// CloseDevicePassword 调用设备API关闭密码认证
func (s *SdkApiServiceWrapper) CloseDevicePassword(deviceID, deviceIP, currentPwd string) error {
	client := s.newClient(deviceIP)
	if currentPwd != "" {
		client.SetPassword(currentPwd)
	}
	if err := client.CloseAuth(); err != nil {
		return err
	}
	s.passwordsMu.Lock()
	delete(s.devicePasswords, deviceID)
	s.passwordsMu.Unlock()
	if s.cm != nil {
		if err := s.cm.RemoveDevicePasswordFromConfig(deviceID); err != nil {
			log.Printf("[sdkapi] 移除设备密码失败: %v", err)
		}
	}
	return nil
}
