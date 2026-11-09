package main

// 进程退出码常量（投屏播放器 → 主程序 IPC 信号）
//
// go_legacy.exe 退出时通过 exit code 向 MYT 主程序传递意图：
//   ExitCodeRotate — WebURL 模式下用户点击旋转按钮，请求主程序翻转
//                    orientation 后重新打开投屏窗口。
// MYT 侧 screencast_service.go 和 window_windows.go 据此判断是否为
// 正常退出（非失败），并触发 reopenRotatedProjection。

const (
	// ExitCodeRotate 旋转重开请求：WebURL 模式翻转 orientation 重新打开投屏。
	ExitCodeRotate = 42
)
