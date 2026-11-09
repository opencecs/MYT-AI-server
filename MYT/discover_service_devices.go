package main

import (
	"log"

	"github.com/moyunteng/myt/internal/config"
)

// --- 设备列表与笔记操作 ---

func (s *DiscoverServiceWrapper) GetDeviceList() []config.DeviceCache {
	return s.configManager.GetDeviceList()
}

func (s *DiscoverServiceWrapper) DeleteDevice(deviceID string) {
	s.configManager.RemoveDevice(deviceID)
	s.configManager.RemovePendingDevice(deviceID)
	s.configManager.CleanAndroidsByDevice(deviceID)
	s.configManager.DeleteDeviceDetail(deviceID)
	if s.eventwsDisconnect != nil {
		s.eventwsDisconnect(deviceID) // 断开该设备的 WS 连接
	}
	log.Printf("[discover] 设备已删除: %s", deviceID)
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
		s.app.Event.Emit("androidlist:updated", s.configManager.GetAndroidList())
		s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
	}
}

func (s *DiscoverServiceWrapper) ToggleDeviceOrder(deviceID string, order int) {
	s.configManager.UpdateDeviceOrder(deviceID, order)
}

// SetDevicesOrder 批量设置设备勾选状态（勾选/取消各一批），一次锁内原子落 order，
// 返回写完后的权威设备列表供前端收敛（理由同 SelectAllDevices：order 是勾选的
// 唯一事实源，逐台 ToggleDeviceOrder 会有几十次 IPC + 中间态快照回灌挤掉勾选）。
// 勾选时只给当前 order==0 的设备分配新 order——已勾选设备保持原次序不动，
// 与 SelectAllDevices「补空不重排」的口径一致。
func (s *DiscoverServiceWrapper) SetDevicesOrder(deviceIDs []string, selected bool) []config.DeviceCache {
	ids := make(map[string]struct{}, len(deviceIDs))
	for _, id := range deviceIDs {
		ids[id] = struct{}{}
	}
	orders := make(map[string]int, len(deviceIDs))
	maxOrder := 0
	for _, d := range s.configManager.GetDeviceList() {
		if d.Order > maxOrder {
			maxOrder = d.Order
		}
		_, inBatch := ids[d.DeviceID]
		if selected && inBatch && d.Order == 0 {
			maxOrder++
			orders[d.DeviceID] = maxOrder
		}
		if !selected && inBatch && d.Order > 0 {
			orders[d.DeviceID] = 0
		}
	}
	s.configManager.UpdateDeviceOrders(orders)
	return s.configManager.GetDeviceList()
}

// SelectAllDevices 勾选全部设备（含离线与待认证），返回勾选后的权威列表。
//
// 不按 State 过滤：order 是「勾选」的唯一事实源，单选取舍从不看在线状态
// （2026-09-05 决策：离线设备勾选合法，批量执行时对不可达设备按台失败回报）。
// 旧实现 SelectAllOnline 只给 State==0 的设备写 order，与前端「全选=全部」冲突，
// 于是离线/401（待认证）设备被心跳回灌按 order 收窄 → 表现为"选上又自动取消"。
// 返回权威列表供前端收敛，替代"乐观改写 + 等事件纠偏"。
func (s *DiscoverServiceWrapper) SelectAllDevices() []config.DeviceCache {
	devices := s.configManager.GetDeviceList()
	maxOrder := 0
	for _, d := range devices {
		if d.Order > maxOrder {
			maxOrder = d.Order
		}
	}
	orders := make(map[string]int, len(devices))
	for _, d := range devices {
		if d.Order == 0 {
			maxOrder++
			orders[d.DeviceID] = maxOrder
		}
	}
	s.configManager.UpdateDeviceOrders(orders)
	return s.configManager.GetDeviceList()
}

// DeselectAll 取消全部设备勾选，返回取消后的权威列表（理由同 SelectAllDevices）。
func (s *DiscoverServiceWrapper) DeselectAll() []config.DeviceCache {
	devices := s.configManager.GetDeviceList()
	orders := make(map[string]int, len(devices))
	for _, d := range devices {
		if d.Order > 0 {
			orders[d.DeviceID] = 0
		}
	}
	s.configManager.UpdateDeviceOrders(orders)
	return s.configManager.GetDeviceList()
}

func (s *DiscoverServiceWrapper) GetDeviceNote(deviceID string) string {
	return s.configManager.GetDeviceNote(deviceID)
}

func (s *DiscoverServiceWrapper) SetDeviceNote(deviceID, note string) {
	s.configManager.SetDeviceNote(deviceID, note)
	log.Printf("[discover] 设备备注已更新: %s", deviceID)
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicenote:updated", map[string]any{"deviceID": deviceID, "note": note})
	}
}

func (s *DiscoverServiceWrapper) GetAllDeviceNotes() map[string]string {
	return s.configManager.GetAllDeviceNotes()
}

func (s *DiscoverServiceWrapper) GetAndroidNote(androidID string) string {
	return s.configManager.GetAndroidNote(androidID)
}

func (s *DiscoverServiceWrapper) SetAndroidNote(androidID, note string) {
	s.configManager.SetAndroidNote(androidID, note)
	log.Printf("[discover] 安卓备注已更新: %s", androidID)
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("androidnote:updated", map[string]any{"androidID": androidID, "note": note})
	}
}

func (s *DiscoverServiceWrapper) GetAllAndroidNotes() map[string]string {
	return s.configManager.GetAllAndroidNotes()
}

func (s *DiscoverServiceWrapper) GetAndroidList() []config.AndroidInfo {
	return s.configManager.GetAndroidList()
}

func (s *DiscoverServiceWrapper) GetRunningAndroids() []config.AndroidInfo {
	return s.configManager.GetRunningAndroids()
}

func (s *DiscoverServiceWrapper) GetSlotExpiry() map[string][]config.SlotExpiry {
	return s.configManager.GetSlotExpiry()
}
