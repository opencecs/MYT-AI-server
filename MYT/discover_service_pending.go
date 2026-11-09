package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/moyunteng/myt/internal/config"
)

// ---------- 待添加设备 ----------

// GetPendingDevices 返回待添加设备列表
func (s *DiscoverServiceWrapper) GetPendingDevices() []config.DeviceCache {
	return s.configManager.GetPendingDevices()
}

// AddPendingDevices 将选中的待添加设备移入正式设备列表
func (s *DiscoverServiceWrapper) AddPendingDevices(deviceIDs string) string {
	var ids []string
	json.Unmarshal([]byte(deviceIDs), &ids)
	added := s.configManager.AddPendingDevices(ids)

	// v2 主机入列联动：拉容器列表 + 刷新状态（与授权查询相互独立）
	if s.configManager.GetDeployMode() == "v2" && s.onPendingAdded != nil {
		s.onPendingAdded(ids)
	}
	// 授权联动查询对所有模式生效：v2 与 v3/local 共用云端授权体系（绑定/坑位）
	s.queryAuthForNewDevices(ids)

	// 更新 IP 映射
	for _, id := range ids {
		if dev := s.configManager.GetDevice(id); dev != nil && s.ipMappingSetter != nil {
			s.ipMappingSetter(dev.IP, dev.DeviceID)
		}
	}

	// 推送更新
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
		s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
	}

	// 立即触发 eventws 对账，让新添加的设备尽快建连并转为在线
	if s.eventwsSyncer != nil {
		s.eventwsSyncer()
	}

	log.Printf("[discover] 已添加 %d 台设备到正式列表", added)
	return fmt.Sprintf("%d", added)
}

// AddAllPendingDevices 将所有待添加设备移入正式设备列表
func (s *DiscoverServiceWrapper) AddAllPendingDevices() string {
	pending := s.configManager.GetPendingDevices()
	added := s.configManager.AddAllPendingDevices()

	ids := make([]string, 0, len(pending))
	for _, dev := range pending {
		ids = append(ids, dev.DeviceID)
		if s.ipMappingSetter != nil {
			s.ipMappingSetter(dev.IP, dev.DeviceID)
		}
	}
	// v2 主机入列联动：拉容器列表 + 刷新状态（与授权查询相互独立）
	if s.configManager.GetDeployMode() == "v2" && s.onPendingAdded != nil {
		s.onPendingAdded(ids)
	}
	// 授权联动查询对所有模式生效：v2 与 v3/local 共用云端授权体系（绑定/坑位）
	s.queryAuthForNewDevices(ids)

	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
		s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
	}

	// 立即触发 eventws 对账
	if s.eventwsSyncer != nil {
		s.eventwsSyncer()
	}

	log.Printf("[discover] 已添加全部 %d 台设备到正式列表", added)
	return fmt.Sprintf("%d", added)
}

// queryAuthForNewDevices 添加设备时查询新设备的授权数据（用户决策 2026-09-03）：
// 绑定状态（批量单请求）+ 坑位到期（逐台，QuerySlotInfo 内部并发限 4）。
// 异步执行不阻塞添加操作；坑位到期数据经 MergeSlotExpiry 合并落缓存并推前端。
func (s *DiscoverServiceWrapper) queryAuthForNewDevices(deviceIDs []string) {
	if len(deviceIDs) == 0 {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		// 绑定状态：批量单请求（设备归属账号）
		if s.paymentSvc != nil && s.paymentSvc.IsLoggedIn() {
			result, err := s.paymentSvc.GetDeviceBindStatus(deviceIDs)
			if err == nil {
				for _, info := range result {
					s.configManager.UpdateBindStatus(info.DeviceID, info.BindStatus)
				}
				// 标记已覆盖：发现链路的限频补查不必再查这批设备
				s.markBindCovered(deviceIDs)
				if s.app != nil && s.app.Event != nil {
					s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
				}
			}
		}
		// 坑位到期：逐台查询（内部并发限 4）
		if s.slotQueryFn != nil {
			s.slotQueryFn(deviceIDs)
		}
		log.Printf("[auth] 已查询 %d 台新设备的授权数据（绑定+坑位到期）", len(deviceIDs))
	}()
}

// ClearPendingDevices 清空待添加设备列表
func (s *DiscoverServiceWrapper) ClearPendingDevices() {
	s.configManager.ClearPendingDevices()
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
	}
}

// DeleteDevices 批量删除设备
func (s *DiscoverServiceWrapper) DeleteDevices(deviceIDs string) string {
	var ids []string
	json.Unmarshal([]byte(deviceIDs), &ids)
	deleted := 0
	for _, id := range ids {
		s.configManager.RemoveDevice(id)
		s.configManager.RemovePendingDevice(id)
		s.configManager.CleanAndroidsByDevice(id)
		s.configManager.DeleteDeviceDetail(id)
		deleted++
	}
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
		s.app.Event.Emit("androidlist:updated", s.configManager.GetAndroidList())
		s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
	}
	log.Printf("[discover] 已批量删除 %d 台设备", deleted)
	return fmt.Sprintf("%d", deleted)
}
