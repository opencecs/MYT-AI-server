package main

import (
	"github.com/moyunteng/myt/internal/account/slot"
	"github.com/moyunteng/myt/internal/config"
)

// SlotServiceWrapper 坑位服务包装
type SlotServiceWrapper struct {
	service       *slot.Service
	configManager *config.Manager
}

func NewSlotServiceWrapper(s *slot.Service, cm *config.Manager) *SlotServiceWrapper {
	return &SlotServiceWrapper{service: s, configManager: cm}
}

// isV4Mode v4 模式判定（V4 没有坑位/订阅概念，坑位相关查询一律返回空）。
func (s *SlotServiceWrapper) isV4Mode() bool {
	return s.configManager != nil && s.configManager.GetDeployMode() == "v4"
}

// QuerySlotInfo 坑位到期信息查询。
// v4 模式返回空：V4 主机（tobox_player 8088）没有坑位概念，查云端既无意义也会
// 产生无用请求——前端拿到空结果后坑位视图自然退化为空（不报错、不阻塞 UI）。
func (s *SlotServiceWrapper) QuerySlotInfo(deviceIDs []string) []slot.SlotQueryResult {
	if s.isV4Mode() {
		return nil
	}
	return s.service.QuerySlotInfo(deviceIDs)
}

func (s *SlotServiceWrapper) GetCachedSlots(deviceID string) []slot.Slot {
	if s.isV4Mode() {
		return nil
	}
	return s.service.GetCachedSlots(deviceID)
}

// GetSlotCount 坑位数。
//
// v4 模式没有坑位概念，但列表视图（SlotView）按「坑位数」决定渲染多少行——
// 返回 0 会让整张表空掉。这里用「该主机的实例数」作为坑位数：V4 每个实例的
// indexNum 是主机内 1..N 的连续派生序号（见 v4_indexer），因此实例数即覆盖全部
// 实例行的行数，列表视图退化为「一台实例一行」的正常表格（数据层适配，UI 不改）。
func (s *SlotServiceWrapper) GetSlotCount(deviceID string) int {
	if s.isV4Mode() {
		return len(s.configManager.GetAndroidsByDevice(deviceID))
	}
	return s.service.GetSlotCount(deviceID)
}

// StartAutoQuery 低频兜底刷新（30 分钟全量，并发限 4）——存量设备到期状态随时间推进；
// 添加设备时的增量查询（queryAuthForNewDevices）负责新设备立即生效。
// v4 模式不查：V4 没有坑位/订阅概念（设备是 tobox_player 8088 主机），
// 拿去云端查坑位既无意义也会产生无用请求。
func (s *SlotServiceWrapper) StartAutoQuery() {
	s.service.StartAutoQuery(func() []string {
		if s.configManager != nil && s.configManager.GetDeployMode() == "v4" {
			return nil
		}
		return s.configManager.GetAllDeviceIDs()
	})
}

func (s *SlotServiceWrapper) StopAutoQuery() {
	s.service.StopAutoQuery()
}
