package main

import (
	"sync"
	"sync/atomic"
	"time"
)

// stallState stall 看门狗状态。
// 通过匿名嵌入 App，字段自动提升为 a.stallMu 等。
type stallState struct {
	stallMu       sync.Mutex
	stallTimer    *time.Timer
	lastFrame     atomic.Int64
}
