package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// StreamInfo 流信息结构
type StreamInfo struct {
	StreamName   string `json:"streamName"`
	AppName      string `json:"appName"`
	PublisherIP  string `json:"publisherIP"`
	StartTime    string `json:"startTime"`
	VideoBitrate int    `json:"videoBitrate"`
	AudioBitrate int    `json:"audioBitrate"`
}

// PullerInfo 拉流客户端信息
type PullerInfo struct {
	DeviceIP   string `json:"deviceIP"`
	Type       string `json:"type"`
	StreamName string `json:"streamName"`
}

// LalStatResponse LAL 统计接口响应结构
type LalStatResponse struct {
	ErrorCode int    `json:"error_code"`
	Desp      string `json:"desp"`
	Data      struct {
		Groups []struct {
			AppName    string `json:"app_name"`
			StreamName string `json:"stream_name"`
			PubSession *struct {
				SessionId     string `json:"session_id"`
				Protocol      string `json:"protocol"`
				BaseType      string `json:"base_type"`
				RemoteAddr    string `json:"remote_addr"`
				StartTime     string `json:"start_time"`
				ReadBytesSum  int    `json:"read_bytes_sum"`
				WroteBytesSum int    `json:"wrote_bytes_sum"`
				BitrateKbits  int    `json:"bitrate_kbits"`
			} `json:"pub"`
			SubSessions []struct {
				StreamName string `json:"stream_name"`
				RemoteAddr string `json:"remote_addr"`
				Type       string `json:"type"`
			} `json:"subs"`
		} `json:"groups"`
	} `json:"data"`
}

// getLocalIP 获取本机局域网IP（通用回退）。
// 注意：在 TUN 代理（fake-ip）机器上 dial 8.8.8.8 会返回 198.18.x；网卡遍历
// 也可能命中 WSL/Hyper-V 虚拟网卡（172.17.x）。面向设备的分发地址必须用
// localIPForDevice（按目标设备路由取源地址），本函数仅做显示/兜底。
func getLocalIP() string {
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		conn.Close()
		if ip := localAddr.IP.To4(); ip != nil && !isTunOrReserved(ip) {
			return ip.String()
		}
		// dial 命中代理网段：回退到网卡遍历
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		ipnet, ok := address.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip := ipnet.IP.To4()
		if ip == nil || isTunOrReserved(ip) {
			continue
		}
		// 优先返回私网地址（RFC1918）——设备在同一局域网
		if isPrivateLAN(ip) {
			return ip.String()
		}
	}
	// 没有私网地址时兜底：任意非保留 IPv4
	for _, address := range addrs {
		ipnet, ok := address.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip := ipnet.IP.To4()
		if ip != nil && !isTunOrReserved(ip) {
			return ip.String()
		}
	}
	return ""
}

// localIPForDevice 返回面向目标设备的本机源地址（UDP dial 不发包、只查路由表）。
// 推流分发（modifydev path / P2P srt 地址）必须用它：设备要能路由回来。
// 路由失败或结果异常（TUN 段）时回退 getLocalIP。
func localIPForDevice(deviceIP string) string {
	if deviceIP == "" {
		return getLocalIP()
	}
	if ip := net.ParseIP(deviceIP); ip == nil {
		return getLocalIP()
	}
	if conn, err := net.Dial("udp", net.JoinHostPort(deviceIP, "554")); err == nil {
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		conn.Close()
		if ip := localAddr.IP.To4(); ip != nil && !isTunOrReserved(ip) && !ip.IsLoopback() {
			return ip.String()
		}
	}
	return getLocalIP()
}

// isTunOrReserved 判断是否 TUN 代理网段（198.18.0.0/15，IANA benchmark 段，
// Clash 等代理的 fake-ip 默认网段）或链路本地/组播等不可路由地址。
func isTunOrReserved(ip net.IP) bool {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// 198.18.0.0/15 = 198.18.0.0 - 198.19.255.255
	return ip[0] == 198 && (ip[1] == 18 || ip[1] == 19)
}

// isPrivateLAN 判断 RFC1918 私网地址
func isPrivateLAN(ip net.IP) bool {
	if ip[0] == 10 {
		return true
	}
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return true
	}
	if ip[0] == 192 && ip[1] == 168 {
		return true
	}
	return false
}

// streamReadString 从 map 中安全读取字符串
func streamReadString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	value, ok := m[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return fmt.Sprintf("%.0f", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// streamReadFloat 从 map 中安全读取浮点数
func streamReadFloat(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	value, ok := m[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

// streamReadMap 从 map 中安全读取子 map
func streamReadMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	value, ok := m[key]
	if !ok || value == nil {
		return nil
	}
	if child, ok := value.(map[string]interface{}); ok {
		return child
	}
	return nil
}

// httpGet 发送 HTTP GET 请求
func httpGet(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	return client.Get(url)
}

// readBody 读取 HTTP 响应体
func readBody(resp *http.Response) (string, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
