package main

import "sync"

// ctrlState 批量控制状态（控制列表/主控被控角色）。
// 通过匿名嵌入 App，字段自动提升为 a.CtrlItems 等。
type ctrlState struct {
	CtrlItems    []ctrlItem
	AllCtrlItems []ctrlItem
	ctrlMu       sync.Mutex
	Controlled   bool   // this window is a controlled slave (no move/resize)
	Hidden       bool   // this window is hidden (background mode, no UI shown)
	PrimaryAddr  string // primary controller address for controlled windows
	SelfAddr     string // this window's control address
}
