package ports

import "github.com/moyunteng/myt/internal/config"

// MappedAddr 计算端口映射模式的连接地址
// 优先从 portBindings 查映射端口，没有则按 indexNum 公式计算
// 返回宿主机IP + 映射端口（或公式端口）
func MappedPort(bindings map[string][]config.PortBinding, containerPort string, indexNum, offset int, deviceIP string) Addr {
	// 优先查 portBindings
	if p := GetMappedPortAny(bindings, containerPort); p > 0 {
		return Addr{Host: deviceIP, Port: p}
	}
	// 兜底：按公式计算
	return Addr{Host: deviceIP, Port: CalcPort(indexNum, offset)}
}

// MYTOS 计算安卓API(MYTOS)的连接地址
func MYTOS(android config.AndroidInfo, deviceIP string) Addr {
	return MappedPort(android.PortBindings, "9082", android.IndexNum, OffsetMYTOS, deviceIP)
}

// RPA 计算RPA的连接地址
func RPA(android config.AndroidInfo, deviceIP string) Addr {
	return MappedPort(android.PortBindings, "9083", android.IndexNum, OffsetRPA, deviceIP)
}

// Video 计算投屏视频的连接地址
func Video(android config.AndroidInfo, deviceIP string) Addr {
	return MappedPort(android.PortBindings, "10000", android.IndexNum, OffsetVideo, deviceIP)
}

// Control 计算投屏控制的连接地址
func Control(android config.AndroidInfo, deviceIP string) Addr {
	return MappedPort(android.PortBindings, "10001", android.IndexNum, OffsetControl, deviceIP)
}

// CameraTCP 计算摄像头TCP的连接地址
func CameraTCP(android config.AndroidInfo, deviceIP string) Addr {
	return MappedPort(android.PortBindings, "10006", android.IndexNum, OffsetCameraTCP, deviceIP)
}

// CameraUDP 计算摄像头UDP的连接地址
func CameraUDP(android config.AndroidInfo, deviceIP string) Addr {
	return MappedPort(android.PortBindings, "10007", android.IndexNum, OffsetCameraUDP, deviceIP)
}

// WebRTC 计算WebRTC的连接地址
func WebRTC(android config.AndroidInfo, deviceIP string) Addr {
	return MappedPort(android.PortBindings, "10008", android.IndexNum, OffsetWebRTC1, deviceIP)
}
