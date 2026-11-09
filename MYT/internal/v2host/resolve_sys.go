package v2host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
)

// --- 序列号 MAC 规范化 + 系统调用封装（ARP 全表 / 81 systeminfo HTTP） ---
// 序列号即主机网卡 MAC（与 UDP 发现的 DeviceID 同源），ARP 反查与 systeminfo
// 候选扫描共用这套底层封装。

// normalizeSerialMAC 把序列号规范化为小写冒号分隔 MAC（识别不了返回空串）。
func normalizeSerialMAC(serial string) string {
	s := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(serial)), "-", ":")
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return ""
	}
	for _, p := range parts {
		if len(p) != 2 || !isHex(p) {
			return ""
		}
	}
	return s
}

// isHex 判断两字符是否均为十六进制位
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// arpTable 读系统 ARP 全表（Windows `arp -a` / Unix `arp -an`）。
func arpTable() (string, error) {
	var cmdName string
	var args []string
	if runtime.GOOS == "windows" {
		cmdName, args = "arp", []string{"-a"}
	} else {
		cmdName, args = "arp", []string{"-an"}
	}
	out, err := exec.Command(cmdName, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// probeSystemInfo 拉单台主机的 81 /systeminfo（复用 NewClient 的 sdkHC 短超时）。
// 走一次性的 Client（sdkHC 15s 超时，扫描候选时外层 ctx 再压到 5s）。
func probeSystemInfo(ctx context.Context, ip string) (*SystemInfo, error) {
	c := NewClient()
	req, err := http.NewRequestWithContext(ctx, "GET", c.sdkBaseURL(ip)+"/systeminfo", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdkHC.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return decodeSystemInfo(body)
}

// decodeSystemInfo 解析 /systeminfo 响应（与 Client.SystemInfo 同构）。
func decodeSystemInfo(body []byte) (*SystemInfo, error) {
	var wrapper struct {
		Code int        `json:"code"`
		Msg  SystemInfo `json:"msg"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, err
	}
	if wrapper.Code != 200 {
		return nil, fmt.Errorf("code %d", wrapper.Code)
	}
	return &wrapper.Msg, nil
}
