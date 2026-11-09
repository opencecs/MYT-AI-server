package main

// clipboardState 剪贴板同步状态。
// 通过匿名嵌入 App，字段自动提升为 a.ClipboardWriter 等。
type clipboardState struct {
	ClipboardWriter func(text string) // 将文本写入本地剪贴板
	clipStopCh      chan struct{}
}
