package v2host

import (
	"strconv"
	"strings"

	"github.com/moyunteng/myt/internal/config"
)

// IsCloudPhone 是否云机容器（区别于 myt_sdk 等宿主机附属容器）：
// 云机创建时必带 idx label（坑位号），V2 Web 的解析器不过滤，
// 但 myt_sdk 混进云机列表会成为无法操作的脏卡片，这里按 idx 严格过滤。
func IsCloudPhone(c ContainerInfo) bool {
	if c.Labels != nil && strings.TrimSpace(c.Labels["idx"]) != "" {
		return true
	}
	// 兜底：81 dc_api/v1/create 建的容器可能不带 idx label（与 V2 Web 手工建容器的
	// label 体系不同），但命名约定一致——{主机序列号}_{坑位}_{序号}-{端口}，
	// 名称含 "_<数字>_" 段的 dobox 容器视为云机（排除 myt_sdk/mytsdk 等系统容器）
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	return strings.Contains(name, "dobox") || nameSuffixSlot(name) > 0
}

// nameSuffixSlot 从容器名末段提取坑位号（"xxx_4_2" → 4 不成立——末段是序号；
// 这里只识别名称中含 "_<数字>_" 中段的云机命名，返回中段数字，非云机命名返回 0）。
func nameSuffixSlot(name string) int {
	parts := strings.Split(name, "_")
	if len(parts) < 3 {
		return 0
	}
	for i := 1; i < len(parts)-1; i++ {
		if n, err := strconv.Atoi(parts[i]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// ToAndroidInfo 把 Docker 容器条目映射为客户端通用的 AndroidInfo（云机）。
// 映射口径与 V2 Web 端 analysisDeviceList 一致：
//   - 名称取 Names[0]（去掉前导 "/"）
//   - 状态取 Docker State（running/exited/...，与坑位表 a.status === 'running' 口径一致）
//   - 坑位号优先取容器 label "idx"，否则取名称末段 "_<数字>"，取不到为 0
//   - IP 填主机 IP（容器对外通信都经主机端口映射）
func ToAndroidInfo(c ContainerInfo, hostDeviceID string, hostIP string) config.AndroidInfo {
	a := config.AndroidInfo{
		ID:       c.ID,
		Name:     containerName(c),
		Status:   c.State,
		Image:    c.Image,
		IndexNum: slotIndex(c),
		DeviceID: hostDeviceID,
		IP:       hostIP,
	}
	if len(c.Ports) > 0 {
		a.PortBindings = make(map[string][]config.PortBinding, len(c.Ports))
		for _, p := range c.Ports {
			if p.PublicPort == 0 {
				continue // 未发布端口（仅容器内部监听）不进映射
			}
			key := strconv.Itoa(p.PrivatePort) + "/" + p.Type
			a.PortBindings[key] = append(a.PortBindings[key], config.PortBinding{
				HostIP:   p.IP,
				HostPort: strconv.Itoa(p.PublicPort),
			})
		}
	}
	return a
}

// containerName 容器名：Names[0] 去掉前导 "/"（Docker 命名惯例）
func containerName(c ContainerInfo) string {
	if len(c.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

// slotIndex 坑位号：label "idx" 优先，其次名称末段 "_<数字>"（如 myt_3 → 3），均无则 0
func slotIndex(c ContainerInfo) int {
	if c.Labels != nil {
		if v, ok := c.Labels["idx"]; ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		}
	}
	name := containerName(c)
	if i := strings.LastIndex(name, "_"); i >= 0 && i+1 < len(name) {
		if n, err := strconv.Atoi(name[i+1:]); err == nil {
			return n
		}
	}
	return 0
}
