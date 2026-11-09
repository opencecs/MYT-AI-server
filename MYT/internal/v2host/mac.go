package v2host

import (
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// --- 主机 MAC 地址查询 ---
// V2 的 81 接口（systeminfo 等）不提供 MAC，V2 Web 全套也不展示。
// 替代来源：系统 ARP 表——客户端与主机通信后（Docker API/发现探测）ARP 缓存
// 必然有条目，且主机与本机同网段时 ARP 地址即主机网卡真实 MAC。

var macRe = regexp.MustCompile(`([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}`)

// LookupMAC 查系统 ARP 表中该 IP 的 MAC 地址（小写冒号分隔），查不到返回空串。
// 调用前先与该 IP 有过通信（探测/截图请求）可保证 ARP 条目新鲜。
func LookupMAC(ip string) string {
	var out []byte
	var err error
	if runtime.GOOS == "windows" {
		out, err = exec.Command("arp", "-a", ip).Output()
	} else {
		// macOS/Linux：arp -n <ip>（不发起 DNS/网络解析）
		out, err = exec.Command("arp", "-n", ip).Output()
	}
	if err != nil {
		return ""
	}
	return parseMACLine(string(out), ip)
}

// parseMACLine 从 arp 输出中找目标 IP 所在行的 MAC，规范化为 xx:xx:xx:xx:xx:xx
func parseMACLine(text, ip string) string {
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, ip) {
			continue
		}
		if m := macRe.FindString(line); m != "" {
			return normalizeMAC(m)
		}
	}
	return ""
}

// normalizeMAC 统一为小写冒号分隔（Windows 是 "-" 分隔，且可能被截断成
// "r713-c8dc-..." 的路由器摘要格式——正则已排除该形态，这里只做分隔符替换）
func normalizeMAC(m string) string {
	m = strings.ReplaceAll(m, "-", ":")
	return strings.ToLower(m)
}
