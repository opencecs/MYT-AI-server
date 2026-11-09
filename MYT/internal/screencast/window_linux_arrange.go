//go:build linux

package screencast

// ArrangePlayerWindows Linux 桩：no-op（对齐调用方签名 screencast_service.go:301）。
// Linux 上 go_legacy 播放器不可用（NewProjectionWindow 返回 error），没有可排列的
// 投屏窗口。
func ArrangePlayerWindows(scale float64, manual bool) {}
