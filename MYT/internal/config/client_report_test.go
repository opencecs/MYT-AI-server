package config

import "testing"

// 清缓存不应重置装机标识：client_report.json 刻意不进 ClearAllCache 的清理列表，
// 否则用户清一次缓存就会在服务端被当成一台新设备重复计数。
func TestClearAllCacheKeepsClientReportState(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.InitDirs(); err != nil {
		t.Fatalf("InitDirs 失败: %v", err)
	}
	want := ReportState{ClientID: "A1B2C3D4E5F6", LastDate: "2026-09-24", LastUID: "10086"}
	if err := m.SaveClientReportState(want); err != nil {
		t.Fatalf("保存上报状态失败: %v", err)
	}

	if err := m.ClearAllCache(); err != nil {
		t.Fatalf("ClearAllCache 失败: %v", err)
	}

	if got := m.GetClientReportState(); got != want {
		t.Errorf("清缓存后上报状态 = %+v，期望 %+v", got, want)
	}
}

// 文件不存在或损坏时返回零值，由调用方重新生成标识，不 panic。
func TestGetClientReportStateMissingFile(t *testing.T) {
	m := NewManager(t.TempDir())
	if got := m.GetClientReportState(); got != (ReportState{}) {
		t.Errorf("缺失文件时状态 = %+v，期望零值", got)
	}
}
