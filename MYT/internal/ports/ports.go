package ports

import (
	"strconv"
	"strings"

	"github.com/moyunteng/myt/internal/config"
)

// 容器内部端口常量
const (
	PortADB       = 5555  // ADB
	PortMYTOS     = 9082  // 安卓API/MYTOS（截图、点击、文件等）
	PortRPA       = 9083  // RPA
	PortVideo     = 10000 // 投屏视频
	PortControl   = 10001 // 投屏控制
	PortCameraTCP = 10006 // 摄像头TCP
	PortCameraUDP = 10007 // 摄像头UDP
	PortWebRTC    = 10008 // WebRTC
)

// 非桥接端口映射偏移
const (
	OffsetADB       = 0
	OffsetMYTOS     = 1
	OffsetRPA       = 2
	OffsetVideo     = 3
	OffsetControl   = 4
	OffsetCameraTCP = 5
	OffsetCameraUDP = 6
	OffsetWebRTC1   = 7 // WebRTC TCP
	OffsetWebRTC2   = 8 // WebRTC UDP
)

// BasePort 非桥接端口计算基数
const BasePort = 30000

// IsMacvlanDirect 判断是否为 macvlan 直连模式（桥接）
// 条件：networkName 含 "myt" 且 portBindings 为空
// 此时容器有独立IP，直接用容器IP+容器内部端口访问
// mytBridge 虽然名字含 "myt" 但有 portBindings，属于非桥接（端口映射模式）
func IsMacvlanDirect(networkName string, portBindings map[string][]config.PortBinding) bool {
	if !strings.Contains(strings.ToLower(networkName), "myt") {
		return false
	}
	return len(portBindings) == 0
}

// CalcPort 非桥接模式按 indexNum 计算映射端口
func CalcPort(indexNum, offset int) int {
	if indexNum <= 0 {
		indexNum = 1
	}
	return BasePort + (indexNum-1)*100 + offset
}

// Addr 表示一个网络地址（IP+端口）
type Addr struct {
	Host string
	Port int
}

// String 返回 "host:port" 格式
func (a Addr) String() string {
	return formatAddr(a.Host, a.Port)
}

func formatAddr(host string, port int) string {
	return host + ":" + strconv.Itoa(port)
}

// GetMappedPort 从 portBindings 查询指定容器端口的映射端口
// key 格式为 "9082/tcp"，返回0表示无映射
func GetMappedPort(bindings map[string][]config.PortBinding, key string) int {
	if len(bindings[key]) == 0 {
		return 0
	}
	port, _ := strconv.Atoi(bindings[key][0].HostPort)
	return port
}

// GetMappedPortAny 从 portBindings 查询映射端口，同时匹配 tcp 和 udp
func GetMappedPortAny(bindings map[string][]config.PortBinding, containerPort string) int {
	for _, proto := range []string{"tcp", "udp"} {
		if p := GetMappedPort(bindings, containerPort+"/"+proto); p > 0 {
			return p
		}
	}
	return 0
}
