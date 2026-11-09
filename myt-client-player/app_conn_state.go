package main

import (
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// connState 连接生命周期状态（连接/重连/关闭）。
// 通过匿名嵌入 App，字段自动提升为 a.Connected 等。
type connState struct {
	Connected    atomic.Bool
	ConnState    int32
	ConnRetry    int32
	connMu       sync.Mutex
	reconnecting bool
	kicked       atomic.Bool
	MaxRetry     int
	retryDelay   int
	disconnected int32
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	rtspHost     atomic.Pointer[string]
	rtspPort     atomic.Pointer[string]
	httpClient   *http.Client
}

// newConnState 创建连接状态（NewApp 调用）。
func newConnState() connState {
	return connState{
		MaxRetry:   0, // infinite reconnect
		retryDelay: 4,
		shutdownCh: make(chan struct{}),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:       5,
				IdleConnTimeout:    30 * time.Second,
				DisableCompression: true,
				DialContext: (&net.Dialer{
					Timeout: 5 * time.Second,
				}).DialContext,
			},
		},
	}
}
