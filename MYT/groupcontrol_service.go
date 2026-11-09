package main

import (
	"github.com/moyunteng/myt/internal/config"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// GroupControlServiceWrapper 群控服务（分组管理 + 批量操作入口）
type GroupControlServiceWrapper struct {
	cm  *config.Manager
	app *application.App
}

func NewGroupControlServiceWrapper(cm *config.Manager) *GroupControlServiceWrapper {
	return &GroupControlServiceWrapper{cm: cm}
}

func (s *GroupControlServiceWrapper) SetConfigManager(cm *config.Manager) {
	s.cm = cm
}

func (s *GroupControlServiceWrapper) SetApp(app *application.App) {
	s.app = app
}

// emitGroupsUpdated 分组变更后推送事件，前端各 Tab 监听后刷新本地分组
func (s *GroupControlServiceWrapper) emitGroupsUpdated() {
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicegroups:updated", s.cm.GetDeviceGroups())
	}
}

// --- 分组管理 ---

// GetDeviceGroups 获取所有分组
func (s *GroupControlServiceWrapper) GetDeviceGroups() []config.DeviceGroup {
	return s.cm.GetDeviceGroups()
}

// CreateDeviceGroup 创建分组
func (s *GroupControlServiceWrapper) CreateDeviceGroup(name string) config.DeviceGroup {
	g := s.cm.CreateDeviceGroup(name)
	s.emitGroupsUpdated()
	return g
}

// UpdateDeviceGroup 更新分组名称
func (s *GroupControlServiceWrapper) UpdateDeviceGroup(groupID, name string) bool {
	ok := s.cm.UpdateDeviceGroup(groupID, name)
	if ok {
		s.emitGroupsUpdated()
	}
	return ok
}

// DeleteDeviceGroup 删除分组
func (s *GroupControlServiceWrapper) DeleteDeviceGroup(groupID string) bool {
	ok := s.cm.DeleteDeviceGroup(groupID)
	if ok {
		s.emitGroupsUpdated()
	}
	return ok
}

// AddDeviceToGroup 将设备加入分组
func (s *GroupControlServiceWrapper) AddDeviceToGroup(groupID, deviceID string) bool {
	ok := s.cm.AddDeviceToGroup(groupID, deviceID)
	if ok {
		s.emitGroupsUpdated()
	}
	return ok
}

// RemoveDeviceFromGroup 将设备从分组中移除
func (s *GroupControlServiceWrapper) RemoveDeviceFromGroup(groupID, deviceID string) bool {
	ok := s.cm.RemoveDeviceFromGroup(groupID, deviceID)
	if ok {
		s.emitGroupsUpdated()
	}
	return ok
}

// SetGroupDevices 设置分组的设备列表
func (s *GroupControlServiceWrapper) SetGroupDevices(groupID string, deviceIDs []string) bool {
	ok := s.cm.SetGroupDevices(groupID, deviceIDs)
	if ok {
		s.emitGroupsUpdated()
	}
	return ok
}

// GetDevicesByGroup 获取分组下的设备列表
func (s *GroupControlServiceWrapper) GetDevicesByGroup(groupID string) []config.DeviceCache {
	return s.cm.GetDevicesByGroup(groupID)
}

// GetUngroupedDevices 获取未分组的设备列表
func (s *GroupControlServiceWrapper) GetUngroupedDevices() []config.DeviceCache {
	return s.cm.GetUngroupedDevices()
}
