package protocol

import (
	"net"
	"strings"

	"github.com/moyunteng/myt/internal/util"
)

type DeviceReply struct {
	IP         string
	DeviceType string
	DeviceID   string
	DeviceName string
}

func ParseReply(data []byte, remoteAddr net.Addr) *DeviceReply {
	text := strings.TrimSpace(string(data))
	parts := strings.Split(text, ":")
	if len(parts) < 3 {
		return nil
	}

	ip := ""
	if udpAddr, ok := remoteAddr.(*net.UDPAddr); ok {
		ip = udpAddr.IP.String()
	}
	if ip == "" {
		return nil
	}

	name := parts[2]

	// 发现层全量放行：_v3 / _v2 / 裸名（C1、q1_10 等旧设备）都构造 Device 返回，
	// 模式过滤由调用方按模式负责——local 只留 v3（discover.LocalDevices）；
	// v2 模式由扫描白名单把关（MYT/v2_service_scan.go 的 v2ScanModelWhitelist）。
	// 截断版本后缀后面的内容（如 r1s_v3_cecs → r1s_v3；裸名无后缀，原样返回）
	if idx := strings.Index(name, "_v3"); idx >= 0 {
		name = name[:idx+len("_v3")]
	} else if idx := strings.Index(name, "_v2"); idx >= 0 {
		name = name[:idx+len("_v2")]
	}

	return &DeviceReply{
		IP:         ip,
		DeviceType: strings.ToUpper(extractDeviceType(name)),
		DeviceID:   parts[1],
		DeviceName: name,
	}
}

// extractDeviceType 从设备名称中提取型号（如 q1_v3 → Q1, R1S_v3 → R1S）
func extractDeviceType(name string) string {
	if idx := strings.Index(name, "_"); idx > 0 {
		return name[:idx]
	}
	return name
}

func ParseVersion(name string) string {
	if strings.Contains(name, "_v3") {
		return "v3"
	}
	if strings.Contains(name, "_v2") {
		return "v2"
	}
	if strings.Contains(name, "_10") {
		return "v1"
	}
	return "v0"
}

// ScreenshotPort 设备截图服务端口
const ScreenshotPort = 30105

func MagicBytes() []byte {
	return []byte(util.LgCloudMagic)
}

func Port() int {
	return util.LgCloudPort
}
