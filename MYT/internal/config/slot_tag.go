package config

import (
	"fmt"
	"strings"
)

// --- 坑位标签 slot_tags.json ---
//
// 标签绑定「设备 + 坑位号」而非容器实例：坑位上的云机重置 / 换镜像 / 删除后标签仍在
// （容器 ID 会随重建变化，坑位号不会）。存储键格式 "deviceID#indexNum"。
//
// 不按部署模式隔离：标签是「这台设备这个坑位」的属性，与用户从哪个入口查看无关；
// 本地 / 公网两种模式下同一台设备的 deviceID 若不同，天然各自独立。

func (m *Manager) slotTagsPath() string {
	return m.ConfigDir() + "/slot_tags.json"
}

// SlotTagKey 坑位标签的存储键（设备 ID + 坑位号）
func SlotTagKey(deviceID string, indexNum int) string {
	return fmt.Sprintf("%s#%d", deviceID, indexNum)
}

// GetSlotTags 读取单个坑位的标签（无标签返回 nil）
func (m *Manager) GetSlotTags(deviceID string, indexNum int) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneTags(m.slotTags[SlotTagKey(deviceID, indexNum)])
}

// SetSlotTags 覆盖写入单个坑位的标签（空集合 = 删除该坑位标签）
func (m *Manager) SetSlotTags(deviceID string, indexNum int, tags []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := SlotTagKey(deviceID, indexNum)
	cleaned := normalizeTags(tags)
	if len(cleaned) == 0 {
		delete(m.slotTags, key)
	} else {
		m.slotTags[key] = cleaned
	}
	m.slotTagsDirty.Store(true)
}

// GetAllSlotTags 返回全部坑位标签（键为 "deviceID#indexNum"）
func (m *Manager) GetAllSlotTags() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getSlotTagsLocked()
}

func (m *Manager) getSlotTagsLocked() map[string][]string {
	result := make(map[string][]string, len(m.slotTags))
	for k, v := range m.slotTags {
		result[k] = cloneTags(v)
	}
	return result
}

// RemoveDeviceSlotTags 设备被删除时清理其名下全部坑位标签
func (m *Manager) RemoveDeviceSlotTags(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeDeviceSlotTagsLocked(deviceID)
}

// removeDeviceSlotTagsLocked 调用者已持锁（供 RemoveDevice 在锁内复用，避免重入死锁）
func (m *Manager) removeDeviceSlotTagsLocked(deviceID string) {
	if deviceID == "" {
		return
	}
	prefix := deviceID + "#"
	changed := false
	for k := range m.slotTags {
		if strings.HasPrefix(k, prefix) {
			delete(m.slotTags, k)
			changed = true
		}
	}
	if changed {
		m.slotTagsDirty.Store(true)
	}
}

// normalizeTags 去空白项、按原顺序去重；标签名内的首尾空格一并去掉
func normalizeTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		name := strings.TrimSpace(tag)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

func cloneTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	result := make([]string, len(tags))
	copy(result, tags)
	return result
}
