package main

import "log"

// --- 坑位标签（绑定「设备 + 坑位号」，见 internal/config/slot_tag.go）---

// GetAllSlotTags 返回全部坑位标签，键为 "deviceID#indexNum"
func (s *DiscoverServiceWrapper) GetAllSlotTags() map[string][]string {
	return s.configManager.GetAllSlotTags()
}

// SetSlotTags 覆盖写入某坑位的标签（传空数组 = 清除该坑位标签）
func (s *DiscoverServiceWrapper) SetSlotTags(deviceID string, indexNum int, tags []string) {
	s.configManager.SetSlotTags(deviceID, indexNum, tags)
	log.Printf("[discover] 坑位标签已更新: %s#%d", deviceID, indexNum)
	if s.app != nil && s.app.Event != nil {
		// 回传归一化后的权威值（去重/去空白由 config 层完成），前端直接以事件为准
		s.app.Event.Emit("slottag:updated", map[string]any{
			"deviceID": deviceID,
			"indexNum": indexNum,
			"tags":     s.configManager.GetSlotTags(deviceID, indexNum),
		})
	}
}
