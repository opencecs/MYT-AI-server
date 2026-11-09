package config

import "path/filepath"

// ReportState 客户端启动上报的本地状态（config/client_report.json）。
//
// 刻意用独立文件而非并入 AppConfig：
//  1. 语义内聚，统计状态不混入主配置；
//  2. 不进 ClearAllCache 的清理列表 —— 清缓存不应重置装机标识（device_id 需永久复用）。
type ReportState struct {
	ClientID string `json:"client_id"`        // 12 位大写 hex，首次生成后永久复用
	LastDate string `json:"last_report_date"` // 最近一次成功上报的日期（UTC+8，YYYY-MM-DD）
	LastUID  string `json:"last_report_uid"`  // 最近一次成功上报携带的 uid
}

func (m *Manager) clientReportPath() string {
	return filepath.Join(m.root, "config", "client_report.json")
}

// GetClientReportState 读取上报状态；文件不存在或损坏时返回零值。
func (m *Manager) GetClientReportState() ReportState {
	var state ReportState
	if err := m.readJSON(m.clientReportPath(), &state); err != nil {
		return ReportState{}
	}
	return state
}

// SaveClientReportState 写入上报状态。
func (m *Manager) SaveClientReportState(state ReportState) error {
	return m.writeJSON(m.clientReportPath(), state)
}
