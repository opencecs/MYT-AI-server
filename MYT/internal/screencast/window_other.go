//go:build !darwin

package screencast

// SetDataDir 非 macOS 平台无操作（Windows 上 go_legacy 提取到 exe 同目录，findPlayer 直接搜同目录）
func SetDataDir(dir string) {}

// CheckAccessibilityPermission 非 macOS 平台始终返回 true（无需辅助功能权限）
func CheckAccessibilityPermission() bool {
	return true
}

// OpenAccessibilitySettings 非 macOS 平台无操作
func OpenAccessibilitySettings() {}
