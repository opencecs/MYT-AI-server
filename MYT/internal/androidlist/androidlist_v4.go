package androidlist

import (
	"log"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// --- v4 模式容器查询（V4 主机 = tobox_player 8088 REST）---
//
// 与 v2 分支同构：查询回调由 setup 注入（main 包持有 V4 REST 客户端，
// internal 包不反向依赖）。实时状态仍以 v4ws 的 WS 事件为准，REST 查询是
// 「WS 未连上 / 事件缺口」时的兜底（注入方在 setup 里按模式路由）。

// SetV4ListAndroids 注入 v4 模式容器查询回调。
func (s *Service) SetV4ListAndroids(fn func(deviceID, ip string) ([]config.AndroidInfo, error)) {
	s.mu.Lock()
	s.v4ListAndroids = fn
	s.mu.Unlock()
}

// isV4ModeLocked 当前是否 v4 模式（调用方需持 s.mu 或明确无并发写）
func (s *Service) isV4ModeLocked() bool {
	return s.deployMode != nil && s.deployMode() == "v4"
}

// isV4Mode 当前是否 v4 模式（自带加锁）
func (s *Service) isV4Mode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isV4ModeLocked()
}

// queryV4Device v4 主机容器补拉：走注入的 v4ListAndroids 回调（8088 GET /android）。
// 结果与本地路径同构：MergeAndroidList + 成败记录 + onUpdated 通知。
func (s *Service) queryV4Device(deviceID, ip string) {
	s.mu.Lock()
	v4ListAndroids := s.v4ListAndroids
	s.mu.Unlock()
	if v4ListAndroids == nil {
		return
	}
	start := time.Now()
	androids, err := v4ListAndroids(deviceID, ip)
	if err != nil {
		log.Printf("[androidlist] v4 容器查询失败 device=%s ip=%s err=%v 耗时=%v", deviceID, ip, err, time.Since(start))
		s.recordFail(deviceID)
		return
	}
	s.configManager.MergeAndroidList([]config.DeviceAndroidResult{
		{DeviceID: deviceID, Androids: androids},
	}, start, nil)
	s.recordSuccess(deviceID)
	log.Printf("[androidlist] ★ v4 容器查询完成 device=%s ip=%s count=%d 耗时=%v", deviceID, ip, len(androids), time.Since(start))
	s.mu.Lock()
	updateFn := s.onUpdated
	s.mu.Unlock()
	if updateFn != nil {
		updateFn()
	}
}
