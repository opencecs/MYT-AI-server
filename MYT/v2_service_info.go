package main

import (
	"context"
	"fmt"

	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/v2host"
)

// --- V2 主机设备信息（myt_sdk 容器 :81/systeminfo）---
// 两个消费点：
//  1. DeviceInfoModal（设备信息弹窗）：SdkApiServiceWrapper.GetDeviceInfo 的 v2 分支
//     转发到 DeviceInfoByIP，返回与本地 /info/device 同构的 sdkapi.DeviceInfo
//  2. 设备行磁盘/内存条：容器列表查询时顺带拉取，写入 DeviceCache 后推
//     devicestats:updated（指标心跳频道，语义吻合、订阅者只有磁盘/内存条 UI）

// DeviceInfoByIP 组装 V2 主机的设备信息（弹窗展示用，字段口径对齐本地 /info/device）。
// 参数兼容序列号或 IP（前端 v2 的 getDeviceId 传序列号，旧数据传 IP——resolveDevice 兜底）。
func (s *V2ServiceWrapper) DeviceInfoByIP(ident string) (*sdkapi.DeviceInfo, error) {
	dev, err := s.resolveDevice(ident)
	if err != nil {
		return nil, err
	}
	ip := dev.IP
	sys, err := s.client.SystemInfo(context.Background(), ip)
	if err != nil {
		return nil, err
	}

	model, version, deviceID := dev.DeviceType, dev.Version, dev.DeviceID
	if deviceID == "" {
		deviceID = sys.DevCode
	}
	memTotalMB := sys.MemTotal / 1024 / 1024
	memUseMB := int64(float64(sys.MemTotal) * sys.MemPercent / 100 / 1024 / 1024)
	diskTotalMB := sys.DiskTotal / 1024 / 1024
	diskUseMB := int64(float64(sys.DiskTotal) * sys.DiskPercent / 100 / 1024 / 1024)

	// 顺带刷新设备行磁盘/内存条数据（与弹窗同源，一次拉取两处受益）
	if deviceID != "" {
		s.configManager.UpdateDeviceDisk(deviceID, diskUseMB*1024*1024, diskTotalMB*1024*1024)
		s.configManager.UpdateDeviceMem(deviceID, memUseMB*1024*1024, memTotalMB*1024*1024)
		s.emitDeviceStats()
	}

	// MAC：81 接口不提供，从系统 ARP 表取（刚与主机通信过，条目必然新鲜）
	return &sdkapi.DeviceInfo{
		DeviceID: deviceID,
		Model:    model,
		Version:  version,
		HwAddr:   v2host.LookupMAC(ip),
		CPUTemp:  int(sys.Temperatures),
		CPULoad:  fmt.Sprintf("%.1f%%", sys.CPU),
		MemTotal: memTotalMB,
		MemUse:   memUseMB,
		MMCTotal: diskTotalMB,
		MMCUse:   diskUseMB,
		MMCModel: "disk",
	}, nil
}

// attachSystemInfo 容器列表查询的伴随动作：拉系统信息刷新设备行磁盘/内存条。
// 失败静默（伴随数据，不影响容器列表主流程）。
func (s *V2ServiceWrapper) attachSystemInfo(ip string) {
	sys, err := s.client.SystemInfo(context.Background(), ip)
	if err != nil {
		return
	}
	deviceID := s.deviceIDByIP(ip)
	if deviceID == "" {
		if sys.DevCode != "" {
			deviceID = sys.DevCode // 兜底：devCode 与 UDP 发现的 DeviceID 同源
		} else {
			return
		}
	}
	memTotalMB := sys.MemTotal / 1024 / 1024
	memUseMB := int64(float64(sys.MemTotal) * sys.MemPercent / 100 / 1024 / 1024)
	diskTotalMB := sys.DiskTotal / 1024 / 1024
	diskUseMB := int64(float64(sys.DiskTotal) * sys.DiskPercent / 100 / 1024 / 1024)
	s.configManager.UpdateDeviceDisk(deviceID, diskUseMB*1024*1024, diskTotalMB*1024*1024)
	s.configManager.UpdateDeviceMem(deviceID, memUseMB*1024*1024, memTotalMB*1024*1024)
	s.emitDeviceStats()
}

// emitDeviceStats 推送设备指标（devicestats:updated：设备行磁盘/内存条专用频道）
func (s *V2ServiceWrapper) emitDeviceStats() {
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicestats:updated", s.configManager.GetDeviceList())
	}
}
