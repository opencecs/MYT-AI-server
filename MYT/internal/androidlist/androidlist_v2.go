package androidlist

import (
	"log"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// --- v2 模式容器查询（V2 主机 = 裸 Docker 2375，无 SDK API）---
// 回调与模式判定由 setup 注入（main 包持有 v2host 客户端，internal 不反向依赖）。

// SetV2ListAndroids 注入 v2 模式容器查询回调（签名对齐 v2host 客户端映射）。
func (s *Service) SetV2ListAndroids(fn func(deviceID, ip string) ([]config.AndroidInfo, error)) {
	s.mu.Lock()
	s.v2ListAndroids = fn
	s.mu.Unlock()
}

// SetDeployModeGetter 注入部署模式读取器（v2 分支判定用）。
func (s *Service) SetDeployModeGetter(fn func() string) {
	s.mu.Lock()
	s.deployMode = fn
	s.mu.Unlock()
}

// isV2ModeLocked 当前是否 v2 模式（调用方需持 s.mu 或明确无并发写）
func (s *Service) isV2ModeLocked() bool {
	return s.deployMode != nil && s.deployMode() == "v2"
}

// isV2Mode 当前是否 v2 模式（自带加锁）
func (s *Service) isV2Mode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isV2ModeLocked()
}

// queryV2Device v2 主机容器补拉：走注入的 v2ListAndroids 回调（Docker /containers/json）。
// 结果与本地路径同构：MergeAndroidList + 成败记录 + onUpdated 通知。
func (s *Service) queryV2Device(deviceID, ip string) {
	s.mu.Lock()
	v2ListAndroids := s.v2ListAndroids
	s.mu.Unlock()
	if v2ListAndroids == nil {
		return
	}
	start := time.Now()
	androids, err := v2ListAndroids(deviceID, ip)
	if err != nil {
		log.Printf("[androidlist] v2 容器查询失败 device=%s ip=%s err=%v 耗时=%v", deviceID, ip, err, time.Since(start))
		s.recordFail(deviceID)
		return
	}
	s.configManager.MergeAndroidList([]config.DeviceAndroidResult{
		{DeviceID: deviceID, Androids: androids},
	}, start, map[string]string{deviceID: ip})
	s.recordSuccess(deviceID)
	log.Printf("[androidlist] ★ v2 容器查询完成 device=%s ip=%s count=%d 耗时=%v", deviceID, ip, len(androids), time.Since(start))
	s.mu.Lock()
	updateFn := s.onUpdated
	s.mu.Unlock()
	if updateFn != nil {
		updateFn()
	}
}

// v2PollInterval v2 兜底轮询周期（主机无 eventws，容器状态变化无实时推送）。
// 81 dc list 单台实测 ~3s，勾选设备并行拉取。
const v2PollInterval = 60 * time.Second

// pollV2Loop v2 容器列表低频兜底轮询：仅勾选设备（与截图轮询范围一致），
// 列表快照与缓存比对、有变化才合并推送（无变化不触发前端全量重渲染）。
func (s *Service) pollV2Loop() {
	// 首轮 5s 后跑：启动时勾选恢复（SetSelectedDevices 立即查询）可能早于设备
	// 上线（GetOnlineDevices 为空被跳过），不能等满 60s 才兜底
	first := time.NewTimer(5 * time.Second)
	defer first.Stop()
	select {
	case <-s.stopCh:
		return
	case <-first.C:
		s.pollV2Once()
	}
	ticker := time.NewTicker(v2PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.pollV2Once()
		}
	}
}

// pollV2Once 一轮轮询：并行拉勾选设备的容器列表，有变化才合并推送
func (s *Service) pollV2Once() {
	if !s.isV2Mode() {
		return
	}
	s.mu.Lock()
	v2ListAndroids := s.v2ListAndroids
	s.mu.Unlock()
	// selectedIDs 由 selectedMu 守护（与 s.mu 不同锁），按各自字段取锁读
	s.selectedMu.RLock()
	selected := make(map[string]bool, len(s.selectedIDs))
	for id := range s.selectedIDs {
		selected[id] = true
	}
	s.selectedMu.RUnlock()
	if v2ListAndroids == nil || len(selected) == 0 {
		return
	}
	queryStartedAt := time.Now()

	// 勾选设备 ∩ 在线设备（离线主机 81 不可达，跳过省请求）
	targets := make([]config.DeviceCache, 0, len(selected))
	for _, d := range s.configManager.GetOnlineDevices() {
		if selected[d.DeviceID] && d.IP != "" {
			targets = append(targets, d)
		}
	}
	if len(targets) == 0 {
		return
	}

	type pollResult struct {
		deviceID string
		androids []config.AndroidInfo
		changed  bool
	}
	results := make(chan pollResult, len(targets))
	var wg sync.WaitGroup
	for _, d := range targets {
		wg.Add(1)
		go func(d config.DeviceCache) {
			defer wg.Done()
			androids, err := v2ListAndroids(d.DeviceID, d.IP)
			if err != nil {
				return // 拉取失败不动缓存（merge 语义保守：查询失败不能误清）
			}
			results <- pollResult{deviceID: d.DeviceID, androids: androids, changed: !s.v2SnapshotEqual(d.DeviceID, androids)}
		}(d)
	}
	wg.Wait()
	close(results)

	changedDevices := make([]config.DeviceAndroidResult, 0, len(targets))
	for r := range results {
		if r.changed {
			changedDevices = append(changedDevices, config.DeviceAndroidResult{DeviceID: r.deviceID, Androids: r.androids})
		}
	}
	if len(changedDevices) == 0 {
		return
	}
	log.Printf("[androidlist] v2 轮询检出变化: %d/%d 台", len(changedDevices), len(targets))
	s.configManager.MergeAndroidList(changedDevices, queryStartedAt, nil)
	s.mu.Lock()
	updateFn := s.onUpdated
	s.mu.Unlock()
	if updateFn != nil {
		updateFn()
	}
}

// v2SnapshotEqual 对比拉到的容器列表与缓存是否一致（数量 + 逐台关键状态）。
// 端口绑定/分辨率等派生字段不参与对比——状态不变即视为无变化。
func (s *Service) v2SnapshotEqual(deviceID string, androids []config.AndroidInfo) bool {
	cached := s.configManager.GetAndroidsByDevice(deviceID)
	if len(cached) != len(androids) {
		return false
	}
	cachedByName := make(map[string]config.AndroidInfo, len(cached))
	for _, a := range cached {
		cachedByName[a.Name] = a
	}
	for _, a := range androids {
		c, ok := cachedByName[a.Name]
		if !ok {
			return false
		}
		if c.Status != a.Status || c.IndexNum != a.IndexNum || c.ID != a.ID {
			return false
		}
	}
	return true
}
