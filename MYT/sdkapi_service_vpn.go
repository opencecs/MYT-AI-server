package main

import (
	"time"

	"github.com/moyunteng/myt/internal/sdkapi"
)

// VPN 客户端管理（转发到设备端 SDK API /mytVpn/*）

// GetVpnStatus 查询设备 VPN 客户端状态
func (s *SdkApiServiceWrapper) GetVpnStatus(deviceIP string) (*sdkapi.VpnStatus, error) {
	return s.newClient(deviceIP).GetVpnStatus()
}

// StartVpn 启动设备 VPN 客户端
func (s *SdkApiServiceWrapper) StartVpn(deviceIP string) error {
	return s.newClient(deviceIP).StartVpn()
}

// StopVpn 停止设备 VPN 客户端
func (s *SdkApiServiceWrapper) StopVpn(deviceIP string) error {
	return s.newClient(deviceIP).StopVpn()
}

// ReinstallVpn 重装设备 VPN 客户端（安装/重安装，超时 5 分钟）
func (s *SdkApiServiceWrapper) ReinstallVpn(deviceIP string) error {
	return s.newClientWithTimeout(deviceIP, 5*time.Minute).ReinstallVpn()
}

// UninstallVpn 卸载设备 VPN 客户端
func (s *SdkApiServiceWrapper) UninstallVpn(deviceIP string) error {
	return s.newClient(deviceIP).UninstallVpn()
}
