// androidlist 的事件推送集成：实现 eventws.Handlers 接口，把 WS 事件落到本地缓存。
//
// 数据流：eventws 消费 goroutine（串行） → ApplyXxx → config 补丁/Merge → 通知前端。
// 与 REST 刷新（选择/手动）共用 MergeAndroidList 与 onUpdated 通知链，前端零改动。
//
// 通知节流：一次开机序列连发 4-6 帧（create/stop/die/start + boot×4），逐帧 emit 会打前端——
// notifySoon 用 100ms 合并窗口，窗口内多帧只 emit 一次。
package androidlist

import (
	"encoding/json"
	"log"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/eventws"
)

// notifyDebounce 事件通知合并窗口
const notifyDebounce = 100 * time.Millisecond

// OnHello hello 帧：记录设备 SDK 版本（身份字段现阶段只记日志，二期 devicedetail 接入）。
func (s *Service) OnHello(deviceID, host string, h *eventws.HelloFrame) {
	if h == nil || h.SDKVersion == "" {
		return
	}
	log.Printf("[androidlist] 设备 %s WS 建连：sdkVersion=%s proto=%d", deviceID, h.SDKVersion, h.Proto)
}

// OnSnapshot snapshot 帧：整体覆盖该设备的容器列表（与 REST 查询同构转换）。
//
// 模式复查：合并前确认设备仍在当前模式设备列表中——切模式后 eventws 旧连接
// 的在途 snapshot 帧（resync 停连接与帧到达存在毫秒级竞态）会经 MergeAndroidList
// 把旧模式容器数据回灌进刚清空/已切换的列表（MergeAndroidList 不校验设备归属，
// app_service.go 切换注释所述污染路径的兜底）。有连接的设备必在 Sync 时的列表中，
// 正常路径此检查恒通过；代价仅一次内存 map 查找，snapshot 频率低（建连时一帧）。
func (s *Service) OnSnapshot(deviceID, host string, snap *eventws.SnapshotFrame) {
	if snap == nil {
		return
	}
	if s.configManager.GetDevice(deviceID) == nil {
		log.Printf("[androidlist] 设备 %s 不在当前模式设备列表，丢弃 snapshot 帧（模式切换残余）", deviceID)
		return
	}
	ip := hostWithoutPort(host)
	list := make([]config.AndroidInfo, 0, len(snap.Data.List))
	for _, item := range snap.Data.List {
		list = append(list, snapshotItemToCache(item, deviceID, ip))
	}
	s.configManager.MergeAndroidList([]config.DeviceAndroidResult{
		{DeviceID: deviceID, Androids: list},
	}, time.Time{}, nil) // WS snapshot：设备端权威数据，无条件覆盖（无「REST 旧快照回退」问题）
	s.recordSuccess(deviceID)
	log.Printf("[androidlist] 设备 %s snapshot：seq=%d count=%d", deviceID, snap.Seq, len(list))
	s.notifySoon()
}

// OnContainerEvent container lifecycle 事件：补丁状态 + 截图联动。
// containerId 在缓存 → 补丁；不在 → 该设备拉一次全量（事件 data 是列表项子集，拉全量补齐）。
func (s *Service) OnContainerEvent(deviceID, host string, ev *eventws.EventFrame, data eventws.ContainerEventData) {
	if ev == nil || data.ContainerID == "" {
		return
	}

	// 截图联动前置：先查容器的 IP/坑位号（destroy 会删缓存条目，必须先查）。
	// 仅状态转换事件联动（start/stop/die/destroy）——boot 过程容器 running，
	// 截图 1s 轮询在自然抓画面，无需逐事件触发。
	var shotIP string
	var shotIdx int
	var shotFound bool
	switch ev.Action {
	case "start", "stop", "die", "destroy":
		for _, a := range s.configManager.GetAndroidsByDevice(deviceID) {
			if a.ID == data.ContainerID {
				shotIP, shotIdx, shotFound = a.IP, a.IndexNum, true
				break
			}
		}
	}
	// 状态转换后的截图处理（invalidate 清旧图 + 下一轮重抓；立即触发由 fireScreenshotEvent 做）
	fire := func() {
		if shotFound {
			s.fireScreenshotEvent(shotIP, shotIdx)
		}
	}

	switch ev.Action {
	case "destroy":
		if s.configManager.RemoveAndroidByContainer(deviceID, data.ContainerID) {
			log.Printf("[androidlist] 设备 %s 容器销毁：%s", deviceID, data.Name)
			s.notifySoon()
			fire()
		}
		return
	case "oom":
		// 状态不变（running），告警标记——本期记日志
		log.Printf("[androidlist] ⚠ 设备 %s 容器 OOM：%s(%s)", deviceID, data.Name, data.ContainerID)
		return
	}

	status := eventws.ContainerActionToStatus(ev.Action)
	if status == "" {
		return // 未知 action：前向兼容忽略
	}
	if data.Status != "" {
		status = data.Status // 事件 data 自带状态优先（服务端权威）
	}
	if s.configManager.PatchAndroidStatus(deviceID, data.ContainerID, status) {
		log.Printf("[androidlist] 设备 %s 容器状态 %s→%s：%s", deviceID, ev.Action, status, data.Name)
		s.notifySoon()
		fire()
		return
	}
	// 不在缓存（外部创建/快照滞后）→ 拉一次全量补齐（1s 截图轮询随后自动跟上）
	// 新容器事件比离线探测更重要：这次补拉失败不能计入离线退避——设备只是
	// 暂时超时，退避 30s 会让「新建容器」的后续事件全部无查询兜底（老 SDK
	// 无 WS 快照、WS 断开期间，新容器在手动刷新前一直不出现在列表）。
	log.Printf("[androidlist] 设备 %s 收到未知容器事件（%s %s），拉全量补齐", deviceID, ev.Action, data.ContainerID)
	s.queryDeviceAsyncNoBackoff(deviceID, host)
}

// fireScreenshotEvent 触发截图联动：清该坑位缓存（避免显示过期画面）+ 立即重抓一轮。
func (s *Service) fireScreenshotEvent(deviceIP string, indexNum int) {
	s.mu.Lock()
	fn := s.onScreenshotEvent
	s.mu.Unlock()
	if fn != nil {
		fn(deviceIP, indexNum)
	}
}

// OnBootEvent boot 事件：开机过程状态映射（queued/booting→starting 等）。
// 状态权威原则：container 事件是最终真相，boot 只是过程展示——
// boot 映射的目标状态若与 container 已确立的状态冲突，以先到者为准（seq 顺序保证不乱序）。
func (s *Service) OnBootEvent(deviceID string, ev *eventws.EventFrame, data eventws.BootEventData, uiStatus string) {
	if data.ContainerID == "" {
		return
	}
	if s.configManager.PatchAndroidStatus(deviceID, data.ContainerID, uiStatus) {
		log.Printf("[androidlist] 设备 %s 开机进度 %s→%s", deviceID, ev.Action, uiStatus)
		s.notifySoon()
	}
}

// OnStatsEvent container/stats 事件：批量补丁容器数值 + 标签（幂等周期数据）。
// tags 兜底由设备端保证（nil→[]），客户端侧保留 nil 语义：老设备帧无 tags 字段
// → 不覆盖缓存（避免把已有标签抹掉）；非 nil（含空数组）→ 权威覆盖。
func (s *Service) OnStatsEvent(deviceID string, ev *eventws.EventFrame, stats []eventws.ContainerStatsItem) {
	patches := make([]config.AndroidStatsPatch, 0, len(stats))
	for _, st := range stats {
		patches = append(patches, config.AndroidStatsPatch{
			ContainerID: st.ContainerID,
			CPUUsage:    st.CPUUsage,
			MemUsage:    st.MemUsage,
			DiskUsage:   st.DiskUsage,
			Tags:        st.Tags,
		})
	}
	if s.configManager.PatchAndroidStats(deviceID, patches) > 0 {
		s.notifySoon()
	}
}

// OnSystemStats system/stats 事件（设备级指标）：androidlist 不消费，由 devicedetail 处理。
func (s *Service) OnSystemStats(deviceID string, ev *eventws.EventFrame, raw json.RawMessage) {}

// OnTagsChangedEvent system/tagsChanged 事件：设备端标签写操作成功后的秒级推送
//（data=变更后的本机标签名扁平全集，跨容器去重、不含容器归属）。
//
// 落库策略（结构核实结论，设备端 wsevents.HelloIdentity.Tags / tagsChanged 载荷均为
// DistinctTagNames 扁平字典）：扁平全集无法按容器覆盖缓存 Tags（会破坏容器归属），
// 容器级标签的权威源仍是 snapshot（逐容器 tags）与 container/stats 周期推送。
// 因此本事件只作「字典变化信号」：触发该设备一次异步全量刷新，让新标签随
// /android 快照秒级落库（快照自带每容器 tags），不再等 20s stats 周期。
// 刷新本身幂等（MergeAndroidList 权威覆盖），高频写标签最坏多拉几次快照，可接受。
func (s *Service) OnTagsChangedEvent(deviceID string, ev *eventws.EventFrame, tags []string) {
	// 设备须在当前模式列表中（与 OnSnapshot 的模式复查同理：切模式后旧连接残余帧不回灌）
	if s.configManager.GetDevice(deviceID) == nil {
		return
	}
	log.Printf("[androidlist] 设备 %s 标签字典变化：%d 个标签，触发全量刷新", deviceID, len(tags))
	s.queryDeviceAsync(deviceID, s.deviceQueryHost(deviceID))
}

// deviceQueryHost 标签字典变化触发的全量刷新用的设备地址：local 取缓存条目 IP；
// public 设备（IP 为空）传 deviceID——QueryDevice 对空 ip 走网关透传补拉路径。
func (s *Service) deviceQueryHost(deviceID string) string {
	for _, a := range s.configManager.GetAndroidsByDevice(deviceID) {
		if a.IP != "" {
			return a.IP
		}
	}
	return deviceID
}

// snapshotItemToCache eventws.SnapshotItem → config.AndroidInfo
// （与 REST 查询的 sdkapi.androidInfo → config.AndroidInfo 转换同构，androidlist.go:225-234）。
func snapshotItemToCache(item eventws.SnapshotItem, deviceID, ip string) config.AndroidInfo {
	var portBindings map[string][]config.PortBinding
	if len(item.PortBindings) > 0 {
		portBindings = make(map[string][]config.PortBinding, len(item.PortBindings))
		for k, v := range item.PortBindings {
			bindings := make([]config.PortBinding, 0, len(v))
			for _, b := range v {
				bindings = append(bindings, config.PortBinding{HostIP: b.HostIP, HostPort: b.HostPort})
			}
			portBindings[k] = bindings
		}
	}
	return config.AndroidInfo{
		ID: item.ID, Name: item.Name, Status: item.Status,
		Image: item.Image, IndexNum: item.IndexNum,
		DeviceID: deviceID, IP: ip,
		NetworkName: item.NetworkName, PortBindings: portBindings,
		ContainerIP: item.IPAddress, ModelPath: item.ModelPath,
		CPUUsage: item.CPUUsage, MemUsage: item.MemUsage, MemLimit: item.MemLimit, DiskUsage: item.DiskUsage,
		Tags: item.Tags,
	}
}

// queryDeviceAsync 异步拉一次单设备全量（未知容器事件补齐用）。
func (s *Service) queryDeviceAsync(deviceID, host string) {
	ip := hostWithoutPort(host)
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		s.QueryDevice(deviceID, ip)
	}()
}

// queryDeviceAsyncNoBackoff 未知容器事件的补拉变体：失败不记退避、且豁免已有退避。
//
// 新容器事件比离线探测更重要——设备可能只是暂时超时，若这次补拉计入退避
// （或沿用上一轮轮询残留的 30s 退避），老 SDK（无 WS 快照）/WS 断开期间
// 新建容器在手动刷新前一直无法出现在列表。实现：查询前清该设备退避记录，
// 保证 QueryDevice 一定真发 HTTP（local 本地路径本身不查也不写 failMap，
// 这里只需处理 refreshFiltered 残留的记录）。
func (s *Service) queryDeviceAsyncNoBackoff(deviceID, host string) {
	s.mu.Lock()
	ctx := s.ctx
	s.mu.Unlock()
	if ctx == nil {
		return
	}
	ip := hostWithoutPort(host)
	s.recordSuccess(deviceID)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		s.QueryDevice(deviceID, ip)
	}()
}

// notifySoon 事件通知节流：100ms 合并窗口内多次调用只触发一次 onUpdated。
func (s *Service) notifySoon() {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	if s.notifyTimer != nil {
		return // 已有待触发的通知
	}
	s.notifyTimer = time.AfterFunc(notifyDebounce, func() {
		s.notifyMu.Lock()
		s.notifyTimer = nil
		s.mu.Lock()
		updateFn := s.onUpdated
		s.mu.Unlock()
		s.notifyMu.Unlock()
		if updateFn != nil {
			updateFn()
		}
	})
}

// hostWithoutPort 去除 host 中的端口（eventws 回调给的是 ip:port 拨号形态）。
func hostWithoutPort(host string) string {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}

// compile-time 接口实现断言
var _ eventws.Handlers = (*Service)(nil)
