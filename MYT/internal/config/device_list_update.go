package config

// --- 设备状态/字段更新 ---

func (m *Manager) UpdateDeviceState(deviceID string, state int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			if d.State != state {
				(*list)[i].State = state
				m.deviceListDirtyPtr().Store(true)
			}
			return
		}
	}
}

// GetDeviceState 获取设备当前在线状态 (0=在线, 1=离线)，不存在返回 -1
func (m *Manager) GetDeviceState(deviceID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := m.deviceListPtrLocked()
	for _, d := range *list {
		if d.DeviceID == deviceID {
			return d.State
		}
	}
	return -1
}

func (m *Manager) UpdateDeviceIP(deviceID, ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			if d.IP != ip {
				(*list)[i].IP = ip
				m.deviceListDirtyPtr().Store(true)
			}
			return
		}
	}
}

// UpdateDeviceFields 更新设备字段（心跳结果），标记脏
func (m *Manager) UpdateDeviceFields(deviceID, ip, deviceType, deviceName, version string, state int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			changed := false
			if ip != "" && d.IP != ip {
				(*list)[i].IP = ip
				changed = true
			}
			if deviceType != "" && d.DeviceType != deviceType {
				(*list)[i].DeviceType = deviceType
				changed = true
			}
			if deviceName != "" && d.DeviceName != deviceName {
				(*list)[i].DeviceName = deviceName
				changed = true
			}
			if version != "" && d.Version != version {
				(*list)[i].Version = version
				changed = true
			}
			if d.State != state {
				(*list)[i].State = state
				changed = true
			}
			if changed {
				m.deviceListDirtyPtr().Store(true)
			}
			return
		}
	}
}

func (m *Manager) RemoveDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			*list = append((*list)[:i], (*list)[i+1:]...)
			m.deviceListDirtyPtr().Store(true)
			// 删到空是用户的显式意图，放行本次空列表落档——否则落档防呆
			// （config.go flushDirty）会把这次删除挡下，磁盘留着旧列表，
			// 重启后设备"复活"。
			if len(*list) == 0 {
				m.allowEmptyFlush.Store(true)
			}
			break
		}
	}
	// 设备已删除，其坑位标签不再有归属：一并清理，避免 config 无界增长
	m.removeDeviceSlotTagsLocked(deviceID)
}

func (m *Manager) UpdateDeviceOrder(deviceID string, order int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyDeviceOrdersLocked(map[string]int{deviceID: order})
}

// UpdateDeviceOrders 一次锁内批量写入多台设备的 order。
// 「全选/全不选」必须原子：逐台 UpdateDeviceOrder 会让并发的 devicelist:updated
// 快照读到"写了一半"的中间态，前端按 order 回灌勾选时就把没写到的那批挤掉。
func (m *Manager) UpdateDeviceOrders(orders map[string]int) {
	if len(orders) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyDeviceOrdersLocked(orders)
}

// applyDeviceOrdersLocked 批量落 order 的唯一实现，调用方须持有 m.mu 写锁。
func (m *Manager) applyDeviceOrdersLocked(orders map[string]int) {
	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if order, ok := orders[d.DeviceID]; ok && d.Order != order {
			(*list)[i].Order = order
			m.deviceListDirtyPtr().Store(true)
		}
	}
}

func (m *Manager) UpdateDeviceDisk(deviceID string, used, total int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			if d.DiskUsed != used || d.DiskTotal != total {
				(*list)[i].DiskUsed = used
				(*list)[i].DiskTotal = total
				m.deviceListDirtyPtr().Store(true)
			}
			return
		}
	}
}

func (m *Manager) UpdateDeviceMem(deviceID string, used, total int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			if d.MemUsed != used || d.MemTotal != total {
				(*list)[i].MemUsed = used
				(*list)[i].MemTotal = total
				m.deviceListDirtyPtr().Store(true)
			}
			return
		}
	}
}

// UpdateDeviceSuperSdkReady 更新 v2 设备超级 SDK 就绪状态（81 探测/容器刷新时维护）。
// 防抖规则统一收口在此：**新值 false 且现值 true → 不覆盖**（「探测超时不降级
// 已就绪状态」——81 探测失败不区分「未安装」与「不可达」，单次超时/网络抖动
// 不得把已装设备翻成「需安装」误导用户重装；true→false 目前无合法来源，全仓
// 无卸载超级 SDK 路径，显式卸载落地时须新增专用写入方法绕过本防抖）。
// 新值 true 无条件写（false→true 快速恢复）；首次写入（新设备无记录）照常落，
// 未装设备仍能提示「需安装」。
// deviceID 不命中（空 ID 旧数据）静默返回；值未变不写避免反复标脏落档。
func (m *Manager) UpdateDeviceSuperSdkReady(deviceID string, ready bool) {
	m.updateDeviceSuperSdkReady(deviceID, ready)
}

// UpdateDeviceSuperSdkReadyChecked 同 UpdateDeviceSuperSdkReady，返回是否发生了
// 状态变化（扫描路径用：变化才计入 listChanged 触发事件广播）。
func (m *Manager) UpdateDeviceSuperSdkReadyChecked(deviceID string, ready bool) bool {
	return m.updateDeviceSuperSdkReady(deviceID, ready)
}

func (m *Manager) updateDeviceSuperSdkReady(deviceID string, ready bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			// 防抖：探测路径的 false 不降级已就绪的 true（见方法注释）
			if !ready && d.SuperSdkReady {
				return false
			}
			if d.SuperSdkReady != ready {
				(*list)[i].SuperSdkReady = ready
				m.deviceListDirtyPtr().Store(true)
				return true
			}
			return false
		}
	}
	return false
}

// UpdateBindStatus 更新设备绑定状态
func (m *Manager) UpdateBindStatus(deviceID string, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID {
			if d.BindStatus != status {
				(*list)[i].BindStatus = status
				m.deviceListDirtyPtr().Store(true)
			}
			return
		}
	}
}

// SetDeviceType 更新指定设备的型号，返回是否有变化（用于公网异步补全型号）。
func (m *Manager) SetDeviceType(deviceID, deviceType string) bool {
	if deviceType == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.deviceListPtrLocked()
	for i, d := range *list {
		if d.DeviceID == deviceID && d.DeviceType != deviceType {
			(*list)[i].DeviceType = deviceType
			m.deviceListDirtyPtr().Store(true)
			return true
		}
	}
	return false
}
