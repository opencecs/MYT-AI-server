package v4host

import "fmt"

// --- V4 主机地址工具（v4host / v4ws 共用，避免两处各写一份端口判断）---

// HasPort 判断 host 是否已带端口（V4 是 IPv4 局域网场景，只需覆盖 "1.2.3.4:8088"）。
func HasPort(host string) bool {
	for i := len(host) - 1; i >= 0; i-- {
		switch c := host[i]; {
		case c == ':':
			return true
		case c == '.' || c == '-':
			continue
		case c >= '0' && c <= '9':
			continue
		default:
			return false
		}
	}
	return false
}

// StripPort 去掉 host 的端口部分（无端口时原样返回）。
func StripPort(host string) string {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}

// NormalizeHost 补默认端口（V4 SDK 监听 8088，与 /health 同端口）。
// 已带端口或带 scheme 的输入原样返回（带 scheme 的由调用方自行处理）。
func NormalizeHost(host string) string {
	if host == "" || HasPort(host) {
		return host
	}
	return fmt.Sprintf("%s:%d", host, DefaultPort)
}
