package config

import (
	"time"
)

// --- 事件推送补丁（eventws 增量更新，全部 manager.mu 内聚加锁避免读-改-写竞态） ---

// AndroidStatsPatch 容器 stats 数值补丁（eventws.ContainerStatsItem 的 config 侧形态）。
type AndroidStatsPatch struct {
	ContainerID string
	CPUUsage    float64
	MemUsage    int64
	DiskUsage   int64
	// Tags 标签补丁：nil = 本帧未带标签（不覆盖缓存）；非 nil（含空数组）= 权威覆盖。
	// 空数组语义是「该容器无标签」，不能与「未带」混为一谈。
	Tags []string
}

// PatchAndroidStatus 按容器 ID 补丁状态（container lifecycle / boot 事件用）。
// 返回是否命中（containerId 在缓存中）。
func (m *Manager) PatchAndroidStatus(deviceID, containerID, status string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	hit := false
	list := m.androidListPtrLocked()
	for i := range *list {
		a := &(*list)[i]
		if a.DeviceID == deviceID && a.ID == containerID {
			a.Status = status
			hit = true
		}
	}
	if hit {
		m.androidListDirtyPtr().Store(true)
		// 记录补丁时间：MergeAndroidList 据此跳过随后到达的 REST 旧快照（防状态回退）
		m.androidLastPatchAt[deviceID] = time.Now()
	}
	return hit
}

// PatchAndroidStats 批量补丁容器 stats 数值（container/stats 事件用）。
// Tags 按 nil/非 nil 区分「本帧未带」与「权威覆盖（含空数组=无标签）」。
// 返回命中数（用于决定是否通知前端）。
func (m *Manager) PatchAndroidStats(deviceID string, stats []AndroidStatsPatch) int {
	if len(stats) == 0 {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	index := make(map[string]bool, len(stats))
	for _, s := range stats {
		index[s.ContainerID] = true
	}
	hit := 0
	list := m.androidListPtrLocked()
	for i := range *list {
		a := &(*list)[i]
		if a.DeviceID != deviceID || !index[a.ID] {
			continue
		}
		for _, s := range stats {
			if s.ContainerID == a.ID {
				a.CPUUsage = s.CPUUsage
				a.MemUsage = s.MemUsage
				a.DiskUsage = s.DiskUsage
				if s.Tags != nil {
					a.Tags = s.Tags
				}
				hit++
				break
			}
		}
	}
	if hit > 0 {
		m.androidListDirtyPtr().Store(true)
	}
	return hit
}

// PatchAndroidTags 按容器名批量补丁标签（本客户端写入 /android/label 后的乐观回填：
// 设备端容器状态经 WS 推回前的过渡窗口内，前端以本地补丁值即时渲染）。
// 命中的容器若当前 Tags 为 nil，先置空数组再赋值（保证缓存里有权威的「无标签」语义）。
// 返回命中的容器名（供事件载荷精确定位，未命中 = 容器刚重建/已删除，等权威推送收敛）。
func (m *Manager) PatchAndroidTags(deviceID string, names []string, tags []string) []string {
	if len(names) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	hit := make([]string, 0, len(names))
	list := m.androidListPtrLocked()
	for i := range *list {
		a := &(*list)[i]
		if a.DeviceID == deviceID && nameSet[a.Name] {
			a.Tags = normalizeTags(tags)
			hit = append(hit, a.Name)
		}
	}
	if len(hit) > 0 {
		m.androidListDirtyPtr().Store(true)
	}
	return hit
}

// RemoveAndroidByContainer 按容器 ID 从缓存移除（container destroy 事件用）。
// 返回是否命中。
func (m *Manager) RemoveAndroidByContainer(deviceID, containerID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	list := m.androidListPtrLocked()
	filtered := make([]AndroidInfo, 0, len(*list))
	for _, a := range *list {
		if a.DeviceID == deviceID && a.ID == containerID {
			found = true
			continue
		}
		filtered = append(filtered, a)
	}
	if found {
		*list = filtered
		m.androidListDirtyPtr().Store(true)
	}
	return found
}

// PatchAndroidProfile 按容器 ID 补丁设备档案（V4 configure 成功后的乐观回填：
// REST 列表与 WS 快照之间的窗口内，前端以本地补丁值渲染；WS 下一轮 snapshot 权威校准）。
// 返回是否命中。
func (m *Manager) PatchAndroidProfile(deviceID, containerID string, profile *AndroidProfile) bool {
	if profile == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	hit := false
	list := m.androidListPtrLocked()
	for i := range *list {
		a := &(*list)[i]
		if a.DeviceID == deviceID && a.ID == containerID {
			a.Profile = profile
			hit = true
		}
	}
	if hit {
		m.androidListDirtyPtr().Store(true)
	}
	return hit
}
