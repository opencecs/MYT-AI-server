package v2host

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// --- 主机心跳（对齐 V2 壳子：常驻 UDP 单播测活）---
// 与设备发现同协议（UDP 7678 + lgcloud 魔术字节），单机一个包、有回复即在线。
// 不走 2375——心跳不依赖 Docker 端口，防火墙只放行 7678 也能判活。

const (
	heartbeatTimeout = 1500 * time.Millisecond // 单机测活超时
	heartbeatWorkers = 16                      // 并发测活数
	heartbeatPort    = 7678                    // 设备发现/心跳端口（util.LgCloudPort，避免引依赖直书）
)

// HeartbeatBatch 并发 UDP 单播测活。返回 ip → 心跳结果（在线状态 + 回复中的序列号）。
func (c *Client) HeartbeatBatch(ctx context.Context, ips []string) map[string]HeartbeatResult {
	result := make(map[string]HeartbeatResult, len(ips))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, heartbeatWorkers)

	for _, ip := range ips {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			online, serial := c.heartbeatOne(ctx, addr)
			mu.Lock()
			result[addr] = HeartbeatResult{Online: online, DeviceID: serial}
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	return result
}

// HeartbeatResult 单机心跳结果：在线状态 + UDP 回复里的设备序列号
// （lgcloudpro:{serial}:{model}，用于回填旧数据里缺失的 DeviceID）。
type HeartbeatResult struct {
	Online   bool
	DeviceID string
}

// heartbeatOne 单机测活：UDP 单播 lgcloud 魔术字节，收到回复即在线，
// 并从回复解析设备序列号（lgcloudpro:{serial}:{model}）。
// Connected UDP socket：Read 只收对端回包，无串包问题。
func (c *Client) heartbeatOne(ctx context.Context, ip string) (bool, string) {
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(ip, strconv.Itoa(heartbeatPort)))
	if err != nil {
		return false, ""
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(util.LgCloudMagic)); err != nil {
		return false, ""
	}
	conn.SetReadDeadline(time.Now().Add(heartbeatTimeout))
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil || n <= 0 {
		return false, ""
	}
	return true, parseHeartbeatSerial(string(buf[:n]))
}

// parseHeartbeatSerial 从 UDP 回复解析设备序列号（lgcloudpro:{serial}:{model}）。
func parseHeartbeatSerial(reply string) string {
	s := strings.TrimSpace(reply)
	if !strings.HasPrefix(s, "lgcloudpro:") {
		return ""
	}
	parts := strings.Split(s, ":")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}
