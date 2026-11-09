package main

import "sync"

// screenshotState 截图状态。
// 通过匿名嵌入 App，字段自动提升为 a.screenshotMu 等。
type screenshotState struct {
	screenshotMu   sync.Mutex
	screenshotPath string
	OnScreenshot   func()
}
