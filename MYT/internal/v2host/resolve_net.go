package v2host

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// --- 序列号 → IP 的两条系统通道实现（ARP MAC 匹配 / 81 systeminfo 扫描） ---

// ipByARPMAC 用 ARP 表 MAC 反查序列号对应的 IP（ARP 缓存为 IP→MAC 表，反查要全表扫）。
// 解析失败返回空串。
func ipByARPMAC(serial string) string {
	mac := normalizeSerialMAC(serial)
	if mac == "" {
		return ""
	}
	table, err := arpTable()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(table, "\n") {
		if !strings.Contains(strings.ToLower(line), mac) {
			continue
		}
		// 行内第一个 IPv4 即 ARP 条目的 IP（Windows 行形如
		// "  10.10.2.46          c8:5b:40-..."，Unix 行形如 "? (10.10.2.46) at c8:5b..."）
		if ip := firstIPv4(line); ip != "" {
			return ip
		}
	}
	return ""
}

// ipBySystemInfo 串行探测常见内网网段，对每台候选主机拉 81 /systeminfo，
// devCode 与序列号一致即命中。受 ctx 约束（上层可取消）；候选数量有限
// （512 台 × 2.5s 短超时、16 并发，全失败约 80s——正常现场 ARP/设备列表已命中，
// 本路径仅兜底）。全部未命中返回错误（上层照常报「无法解析」）。
func ipBySystemInfo(ctx context.Context, serial string) (string, error) {
	candidates := candidateIPs()
	if len(candidates) == 0 {
		return "", fmt.Errorf("无候选网段")
	}
	sem := make(chan struct{}, 16)
	var mu sync.Mutex
	found := ""
	var wg sync.WaitGroup
	for _, ip := range candidates {
		select {
		case <-ctx.Done():
			wg.Wait()
			return "", ctx.Err()
		default:
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			sys, err := probeSystemInfo(c, ip)
			if err != nil || sys.DevCode != serial {
				return
			}
			mu.Lock()
			if found == "" {
				found = ip
			}
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	if found == "" {
		return "", fmt.Errorf("systeminfo 扫描未命中序列号 %s", serial)
	}
	return found, nil
}

// candidateIPs 取本机所有非环回 IPv4 网段的 .1-.254 候选地址（去重）。
func candidateIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}
			base := []byte(ip4)
			for last := 1; last <= 254; last++ {
				base[3] = byte(last)
				cand := net.IP(base).String()
				if cand == ipNet.IP.String() || seen[cand] {
					continue
				}
				seen[cand] = true
				out = append(out, cand)
			}
		}
	}
	return out
}

// firstIPv4 从一行文本里提取第一个 IPv4 地址（无则空串）。
func firstIPv4(line string) string {
	for _, tok := range strings.Fields(line) {
		if ip := net.ParseIP(tok); ip != nil && ip.To4() != nil {
			return ip.To4().String()
		}
	}
	return ""
}
