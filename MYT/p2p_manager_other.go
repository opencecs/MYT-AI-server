//go:build !windows

package main

// P2PManager P2P 推流管理器（非 Windows 平台桩：WindowsPusher 仅提供 Windows 资源）
type P2PManager struct{}

// P2PSession P2P 推流会话（非 Windows 平台桩，保持字段引用兼容）
type P2PSession struct{}

// NewP2PManager 创建 P2P 管理器（非 Windows 平台桩）
func NewP2PManager() *P2PManager { return &P2PManager{} }

// Start 启动 P2P 推流（非 Windows 平台不支持）
func (m *P2PManager) Start(deviceIP string, port int, streamName, streamURL string, listenPort int) map[string]interface{} {
	return map[string]interface{}{"success": false, "message": "p2p WindowsPusher is only available on Windows"}
}

// Stop 停止 P2P 推流（非 Windows 平台无会话）
func (m *P2PManager) Stop(deviceIP string, port int, streamName string) map[string]interface{} {
	return map[string]interface{}{"success": false, "message": "session not found"}
}

// Status 查询 P2P 推流状态（非 Windows 平台无会话）
func (m *P2PManager) Status(deviceIP string, port int, streamName string) map[string]interface{} {
	return map[string]interface{}{"running": false, "message": "not running"}
}

// StopAll 停止所有 P2P 会话（非 Windows 平台无会话）
func (m *P2PManager) StopAll() {}
