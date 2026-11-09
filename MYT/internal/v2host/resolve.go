package v2host

import (
	"context"
	"fmt"
	"log"
)

// --- V2 序列号 → 主机 IP 解析 ---
//
// V2 模式前端传来的设备标识是序列号（UDP 发现的 DeviceID = devCode），而 SSH/
// 8000 等直连通道需要真实主机 IP。解析链：系统 ARP 表 → 81 /systeminfo 的
// devCode 字段。设备列表（config.Manager）里的映射属于 main 包（V2ServiceWrapper），
// internal 包不能反向依赖——那里先解析，兜不住的再落到这里的两条 HTTP/系统通道。

// ResolveIPBySerial 由序列号解析 V2 主机真实 IP。
// arpTable 由调用方传入（main 包持 config.Manager，序列号→IP 优先走设备列表；
// lookup 不到传 nil，仅用系统通道兜底）。
// 两通道都失败返回明确错误（上层直接展示给用户）。
func ResolveIPBySerial(ctx context.Context, serial string, arpTable func(serial string) string) (string, error) {
	if serial == "" {
		return "", fmt.Errorf("设备标识为空，无法解析 V2 主机 IP")
	}
	if arpTable != nil {
		if ip := arpTable(serial); ip != "" {
			return ip, nil
		}
	}
	// ARP 表：客户端此前与主机通信过（扫描/心跳/容器查询）则有条目；
	// MAC 序列号解析失败返回空串（条目缺失/跨网段），继续走 systeminfo 兜底
	if ip := ipByARPMAC(serial); ip != "" {
		log.Printf("[v2host] 序列号 %s 经 ARP 表解析为 %s", serial, ip)
		return ip, nil
	}
	// 81 /systeminfo：串行探测候选网段，devCode 与序列号一致即命中。
	// 两段常见内网网段（V2 部署现场以 10.10.2.x / 192.168.1.x 居多，命中率低时
	// 也只是慢——有 2.5s 短超时兜底，不会挂死）。
	if ip, err := ipBySystemInfo(ctx, serial); err == nil && ip != "" {
		log.Printf("[v2host] 序列号 %s 经 systeminfo 解析为 %s", serial, ip)
		return ip, nil
	}
	return "", fmt.Errorf("无法解析 V2 主机 IP（序列号 %s）", serial)
}
