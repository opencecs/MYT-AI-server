//go:build linux

package main

import (
	"github.com/moyunteng/myt/internal/config"
)

// CameraMicServiceWrapper 是 Linux 桩（对齐 camera_mic_service_darwin.go 模式）：
// 摄像头+麦克风推流依赖 Windows 专用二进制（ffmpeg.exe + libx264-165.dll +
// libgcc_s_seh-1.dll + libwinpthread-1.dll + WindowsPusher.exe），Linux 上不可用。
// setup.go 无条件构造此类型并注册为 Wails service，故需保留类型与方法签名，
// 运行时统一返回"不支持"。
type CameraMicServiceWrapper struct{}

func NewCameraMicServiceWrapper() *CameraMicServiceWrapper {
	return &CameraMicServiceWrapper{}
}

func (s *CameraMicServiceWrapper) SetConfigManager(cm *config.Manager) {}

func (s *CameraMicServiceWrapper) StartCameraMic(deviceIP string, indexNum int, camName, micName string) map[string]interface{} {
	return map[string]interface{}{"success": false, "message": "Linux 不支持摄像头推流"}
}

func (s *CameraMicServiceWrapper) StopCameraMic(id int) map[string]interface{} {
	return map[string]interface{}{"success": true, "message": "会话不存在"}
}

func (s *CameraMicServiceWrapper) StopAll() {}

func (s *CameraMicServiceWrapper) ListCameraDevices() map[string]interface{} {
	return map[string]interface{}{"success": false, "message": "Linux 不支持摄像头推流", "cameras": []string{}, "mics": []string{}}
}

func (s *CameraMicServiceWrapper) IsCameraMicRunning(deviceIP string, indexNum int) map[string]interface{} {
	return map[string]interface{}{"running": false}
}
