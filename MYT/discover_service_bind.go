package main

import (
	"log"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// bindRefreshMinInterval 发现链路补查绑定状态的最小间隔。
// 登录时 30 分钟全量轮询会立即查一次；public 每 5~30s 一轮发现，
// 不设时间窗会变成「每轮都全量打点授权 API」。取 1 分钟：新设备加入后
// 最迟 1 分钟内拿到 bind_status，同时把 API 压力控制在每分钟最多 1 批。
const bindRefreshMinInterval = time.Minute

// 绑定状态/坑位到期的刷新体系（用户决策 2026-09-04 调整后）：
//   - StartBindStatusPoll：30 分钟全量兜底（批量单请求），存量设备绑定状态随时间推进
//   - slot.StartAutoQuery：30 分钟全量兜底（逐台并发限 4），到期状态翻转
//   - queryAuthForNewDevices（discover_service_pending.go）：添加设备时只查新设备
//   - 前端坑位模式选设备时单台按需查
// 授权页不再订阅 devicelist:updated 重查授权——进 tab 读缓存 + 手动刷新调 API。

// StartBindStatusPoll 启动绑定状态 30 分钟轮询（登录后调用）
func (s *DiscoverServiceWrapper) StartBindStatusPoll() {
	s.StopBindStatusPoll()
	s.bindStopCh = make(chan struct{})
	s.queryAllBindStatus()
	ticker := time.NewTicker(30 * time.Minute)
	s.bindWg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		defer s.bindWg.Done()
		defer ticker.Stop()
		for {
			select {
			case <-s.bindStopCh:
				return
			case <-ticker.C:
				s.queryAllBindStatus()
			}
		}
	}()
}

// StopBindStatusPoll 停止绑定状态轮询
func (s *DiscoverServiceWrapper) StopBindStatusPoll() {
	select {
	case <-s.bindStopCh:
	default:
		close(s.bindStopCh)
	}
	s.bindWg.Wait()
}

// queryAllBindStatus 全量查询所有设备的绑定状态（批量单请求）
func (s *DiscoverServiceWrapper) queryAllBindStatus() {
	if s.paymentSvc == nil || !s.paymentSvc.IsLoggedIn() {
		return
	}
	devices := s.configManager.GetDeviceList()
	if len(devices) == 0 {
		return
	}
	var ids []string
	for _, d := range devices {
		ids = append(ids, d.DeviceID)
	}
	s.queryAndUpdateBindStatus(ids)
}

// queryAndUpdateBindStatus 查询指定设备的绑定状态并更新到 configManager
func (s *DiscoverServiceWrapper) queryAndUpdateBindStatus(deviceIDs []string) {
	if s.paymentSvc == nil || len(deviceIDs) == 0 {
		return
	}
	result, err := s.paymentSvc.GetDeviceBindStatus(deviceIDs)
	if err != nil {
		log.Printf("[bind] 查询绑定状态失败: %v", err)
		return
	}
	for _, info := range result {
		s.configManager.UpdateBindStatus(info.DeviceID, info.BindStatus)
	}
	s.markBindCovered(deviceIDs)
	log.Printf("[bind] 定时刷新绑定状态: %d 台设备", len(result))
}

// refreshBindStatusForDevices 发现链路（public 每 5~30s 一轮）的绑定状态补查。
// 目的：public 模式下 bind_status 只由 30 分钟全量轮询写入，新加入列表的设备
// 最长要等 30 分钟才有状态——前端授权页会一直显示「未绑定」。
// 这里在每轮发现后异步补查，但做了两重限频，避免每轮都全量打点授权 API：
//   - 时间窗：距上次补查 < bindRefreshMinInterval 直接跳过
//   - 去重：只查「本进程从未查过」的设备，全部覆盖后自然停发
//
// 异步执行，不阻塞发现轮次；失败只记日志（下轮再试）。
func (s *DiscoverServiceWrapper) refreshBindStatusForDevices(deviceIDs []string) {
	if len(deviceIDs) == 0 || s.paymentSvc == nil || !s.paymentSvc.IsLoggedIn() {
		return
	}
	now := time.Now()
	s.bindQueryMu.Lock()
	if !s.lastBindQueryAt.IsZero() && now.Sub(s.lastBindQueryAt) < bindRefreshMinInterval {
		s.bindQueryMu.Unlock()
		return
	}
	var missing []string
	for _, id := range deviceIDs {
		if _, ok := s.bindCovered[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		s.bindQueryMu.Unlock()
		return
	}
	s.lastBindQueryAt = now
	// 先占位再查：并发/重入时不会对同一批设备重复发请求（查询失败下轮再补）
	for _, id := range missing {
		s.bindCovered[id] = struct{}{}
	}
	s.bindQueryMu.Unlock()

	util.GoSafe(func() {
		result, err := s.paymentSvc.GetDeviceBindStatus(missing)
		if err != nil {
			log.Printf("[bind] 发现链路补查绑定状态失败: %v", err)
			s.unmarkBindCovered(missing) // 失败不占位，下个时间窗再试
			return
		}
		for _, info := range result {
			s.configManager.UpdateBindStatus(info.DeviceID, info.BindStatus)
		}
		log.Printf("[bind] 发现链路补查绑定状态: %d 台（新增 %d 台）", len(result), len(missing))
	})
}

// deviceIDsForBind 当前正式列表里全部设备的 ID（发现链路补查绑定状态的入参）。
func (s *DiscoverServiceWrapper) deviceIDsForBind() []string {
	list := s.configManager.GetDeviceList()
	ids := make([]string, 0, len(list))
	for _, d := range list {
		ids = append(ids, d.DeviceID)
	}
	return ids
}

// markBindCovered 标记这些设备已查过绑定状态（30 分钟全量轮询后调用，
// 避免发现链路把刚查过的设备再查一遍）。
func (s *DiscoverServiceWrapper) markBindCovered(deviceIDs []string) {
	s.bindQueryMu.Lock()
	defer s.bindQueryMu.Unlock()
	if s.bindCovered == nil {
		s.bindCovered = make(map[string]struct{})
	}
	for _, id := range deviceIDs {
		s.bindCovered[id] = struct{}{}
	}
}

// unmarkBindCovered 撤销占位（查询失败时让下个时间窗重试）。
func (s *DiscoverServiceWrapper) unmarkBindCovered(deviceIDs []string) {
	s.bindQueryMu.Lock()
	defer s.bindQueryMu.Unlock()
	for _, id := range deviceIDs {
		delete(s.bindCovered, id)
	}
}
