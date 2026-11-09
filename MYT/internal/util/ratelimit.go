package util

import (
	"sync"
	"time"
)

// MaxDeviceHTTPConcurrency 全局设备 HTTP 并发上限
const MaxDeviceHTTPConcurrency = 16

// minHTTPInterval 两个 HTTP 请求之间的最小间隔
// 限制每秒新建 TCP 连接数，防止脉冲式并发压垮嵌入式设备 TCP listen backlog
const minHTTPInterval = 20 * time.Millisecond

var (
	deviceHTTPSem   = make(chan struct{}, MaxDeviceHTTPConcurrency)
	httpRateMu      sync.Mutex
	lastHTTPAcquire time.Time
)

// AcquireDeviceHTTP 获取一个设备 HTTP 并发槽位（阻塞）
// 同时限制请求发起速率，避免瞬间建立大量 TCP 连接
func AcquireDeviceHTTP() {
	deviceHTTPSem <- struct{}{}
	httpRateMu.Lock()
	now := time.Now()
	if !lastHTTPAcquire.IsZero() {
		wait := minHTTPInterval - now.Sub(lastHTTPAcquire)
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	lastHTTPAcquire = time.Now()
	httpRateMu.Unlock()
}

// ReleaseDeviceHTTP 释放设备 HTTP 并发槽位
func ReleaseDeviceHTTP() {
	<-deviceHTTPSem
}
