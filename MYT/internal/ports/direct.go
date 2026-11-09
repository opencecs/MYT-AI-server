package ports

import "github.com/moyunteng/myt/internal/config"

// DirectAddr 计算macvlan直连模式的连接地址
// 直接用容器IP + 容器内部端口
// 调用前必须先用 IsMacvlanDirect 判断

// MYTOSDirect 安卓API(MYTOS)直连地址
func MYTOSDirect(android config.AndroidInfo) Addr {
	return Addr{Host: android.ContainerIP, Port: PortMYTOS}
}

// RPADirect RPA直连地址
func RPADirect(android config.AndroidInfo) Addr {
	return Addr{Host: android.ContainerIP, Port: PortRPA}
}

// VideoDirect 投屏视频直连地址
func VideoDirect(android config.AndroidInfo) Addr {
	return Addr{Host: android.ContainerIP, Port: PortVideo}
}

// ControlDirect 投屏控制直连地址
func ControlDirect(android config.AndroidInfo) Addr {
	return Addr{Host: android.ContainerIP, Port: PortControl}
}

// CameraTCPDirect 摄像头TCP直连地址
func CameraTCPDirect(android config.AndroidInfo) Addr {
	return Addr{Host: android.ContainerIP, Port: PortCameraTCP}
}

// CameraUDPDirect 摄像头UDP直连地址
func CameraUDPDirect(android config.AndroidInfo) Addr {
	return Addr{Host: android.ContainerIP, Port: PortCameraUDP}
}

// WebRTCDirect WebRTC直连地址
func WebRTCDirect(android config.AndroidInfo) Addr {
	return Addr{Host: android.ContainerIP, Port: PortWebRTC}
}
