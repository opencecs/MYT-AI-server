package main

import (
	"context"
	"log"
	"time"

	"github.com/moyunteng/myt/internal/v2host"
)

// --- V2 序列号 → 主机 IP 解析（v2 模式维护类操作用） ---
//
// v2 设备标识是序列号（UDP 发现 DeviceID = devCode），SSH/8000 等直连通道需要
// 真实主机 IP。解析链（ResolveIPBySerial）：设备列表（序列号→IP）→ 系统 ARP 表
// （MAC 反查）→ 81 /systeminfo 候选网段扫描。旧数据条目以 IP 为主键（GetDevice
// 查不到序列号）时直接视为已解析，与 resolveDevice 的兜底口径一致。

// resolveV2HostIP 导出给 maintenanceSvc 注入（SetV2HostIPResolver）：
// 把 v2 设备标识解析为可直连的主机 IP。
func (s *V2ServiceWrapper) resolveV2HostIP(serial string) (string, error) {
	if dev := s.configManager.GetDevice(serial); dev != nil && dev.IP != "" {
		return dev.IP, nil
	}
	// 兜底链走 v2host（ARP MAC 反查 + 81 systeminfo 候选扫描）；设备列表
	// 映射在 config.Manager，internal 包拿不到——只有落到这里才真的扫网段
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return v2host.ResolveIPBySerial(ctx, serial, nil)
}

// ResolveV2HostIPForTest 留给未来远程验证脚本用（Wails 绑定可直接调）。
func (s *V2ServiceWrapper) ResolveV2HostIPForTest(serial string) (string, error) {
	ip, err := s.resolveV2HostIP(serial)
	if err != nil {
		log.Printf("[v2] 序列号解析失败 %s: %v", serial, err)
		return "", err
	}
	log.Printf("[v2] 序列号 %s 解析为 %s", serial, ip)
	return ip, nil
}
