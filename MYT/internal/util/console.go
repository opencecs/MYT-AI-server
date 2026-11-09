package util

import "log"

// AttachConsole 在 Windows 上为 GUI 程序附加控制台窗口
// GUI 程序（-H windowsgui）默认没有控制台，调用此函数可同时显示 GUI 和控制台
func AttachConsole() {
	attachConsoleWindows()
	log.Println("[util] 控制台已附加")
}
