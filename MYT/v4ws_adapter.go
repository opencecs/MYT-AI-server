package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/util"
	"github.com/moyunteng/myt/internal/v4ws"
)

// --- V4 WS 事件 → 本地容器缓存适配层 ---
//
// 数据流：v4ws 消费 goroutine（串行） → OnSnapshot/OnInstanceState → config 补丁/Merge
// → notifySoon（100ms 合并窗口）→ androidlist:updated → 前端。
//
// key 约定（与截图/投屏 key 保持兼容）：
//   - 容器缓存条目 IP 字段 = 主机 IP（V4 设备以 IP 为身份）
//   - IndexNum = 该主机内实例的稳定序号（按 backend/id 排序派生，见 indexFor）
//     → 截图 key 仍是 "IP_indexNum"，与既有截图/投屏链路零改动
//   - ID = backend + "/" + id（联合主键，不同后端可有同名 id）
//   - Name 保留服务端实例名（后端+id 的唯一性由 ID 保证）
//
// 为什么 IndexNum 要派生：V4 没有「坑位号」概念，而截图轮询按 (IP, indexNum) 取图。
// 用「按 backend/id 排序后的序号」做稳定映射——只要实例集合不变，序号就稳定；
// 新增实例可能改变后续序号（截图缓存随之重抓一轮），代价可接受。

// notifyDebounce 事件通知合并窗口（一次开机序列会连发 starting→running 多帧）
const notifyDebounce = 100 * time.Millisecond

// v4EventAdapter 实现 v4ws.Handlers，把事件落到 config 缓存。
type v4EventAdapter struct {
	cm  *config.Manager
	app func(name string, data any)
	// idx 实例序号派生（与 REST 补拉路径共用同一份，保证两条路径写入的
	// 截图 key（IP_indexNum）一致）
	idx *v4Indexer

	mu          sync.Mutex
	notifyTimer *time.Timer

	// shotInvalidate 实例状态变化 → 截图缓存失效 + 立即重抓（setup 注入，
	// 与 V3 的 androidlist.SetScreenshotEventCallback 同语义）。nil 时跳过。
	shotInvalidate func(hostIP, containerID string)
	// clipSink 剪贴板回灌处理器（setup 注入 SdkApiServiceWrapper：防回声 + 写系统
	// 剪贴板 + 推 v4:clipboard 事件，见 v4_clipboard.go）。nil 时只记日志。
	clipSink *SdkApiServiceWrapper
}

// SetScreenshotInvalidator 注入截图失效回调（实例状态变化时清缓存并重抓）。
func (a *v4EventAdapter) SetScreenshotInvalidator(fn func(hostIP, containerID string)) {
	a.mu.Lock()
	a.shotInvalidate = fn
	a.mu.Unlock()
}

// SetClipboardSink 注入剪贴板回灌处理器（onV4ClipboardPush 的宿主，见 v4_clipboard.go）。
func (a *v4EventAdapter) SetClipboardSink(svc *SdkApiServiceWrapper) {
	a.mu.Lock()
	a.clipSink = svc
	a.mu.Unlock()
}
// fireScreenshotInvalidate 触发截图失效（状态转换后画面必然变化）
func (a *v4EventAdapter) fireScreenshotInvalidate(ip, containerID string) {
	a.mu.Lock()
	fn := a.shotInvalidate
	a.mu.Unlock()
	if fn != nil {
		fn(ip, containerID)
	}
}

// newV4EventAdapter 创建适配层。emit 为事件推送函数（setup 注入 app.Event.Emit 的包装，
// 未注入时（nil）只落缓存不推前端）；idx 为共享序号派生器。
func newV4EventAdapter(cm *config.Manager, idx *v4Indexer, emit func(name string, data any)) *v4EventAdapter {
	return &v4EventAdapter{
		cm:  cm,
		app: emit,
		idx: idx,
	}
}

// OnHello 握手帧：仅记录（V4 无设备身份字段，sdkVersion 用于排查）
func (a *v4EventAdapter) OnHello(host string, h *v4ws.HelloFrame) {
	if h == nil {
		return
	}
	log.Printf("[v4ws] 主机 %s 建连：sdkVersion=%s proto=%d", v4hostOf(host), h.SDKVersion, h.Proto)
}

// OnSnapshot 全量快照：整体覆盖该主机的实例视图。
func (a *v4EventAdapter) OnSnapshot(host string, snap *v4ws.SnapshotFrame) {
	if snap == nil {
		return
	}
	ip := v4hostOf(host)
	if a.cm.GetDeployMode() != "v4" {
		// 模式复查：切模式后旧连接在途 snapshot 会经 MergeAndroidList 把旧模式容器
		// 回灌进刚清空的列表（MergeAndroidList 不校验设备归属）
		log.Printf("[v4ws] 主机 %s 快照到达但当前非 v4 模式，丢弃", ip)
		return
	}
	items := snap.Data.Instances
	idx := a.indexFor(ip, items)
	list := make([]config.AndroidInfo, 0, len(items))
	for _, it := range items {
		list = append(list, v4ItemToCache(it, ip, idx[it.Key()]))
	}
	// WS 快照 = 设备端权威数据，无条件覆盖。
	// 必须用 ReplaceAndroidListForDevice 而非 MergeAndroidList：后者在「合并后整体为空」
	// 时保留旧数据（防 REST 查询失败误清），会让「删掉最后一台实例」的快照留下幽灵条目。
	a.cm.ReplaceAndroidListForDevice(ip, list)
	log.Printf("[v4ws] 主机 %s 快照：seq=%d count=%d", ip, snap.Seq, len(list))
	a.notifySoon()
}

// OnInstanceState 实例状态事件：deleted 移除，其余补丁状态（未命中则拉一次全量）。
func (a *v4EventAdapter) OnInstanceState(host string, ev *v4ws.EventFrame, item v4ws.InstanceItem) {
	ip := v4hostOf(host)
	if item.ID == "" {
		return
	}
	if a.cm.GetDeployMode() != "v4" {
		return
	}
	containerID := v4ws.InstanceKey(item.Backend, item.ID)

	if item.State == v4ws.StateDeleted {
		if a.cm.RemoveAndroidByContainer(ip, containerID) {
			log.Printf("[v4ws] 主机 %s 实例已删除：%s", ip, containerID)
			a.notifySoon()
			a.fireScreenshotInvalidate(ip, containerID)
		}
		return
	}
	status := v4StateToStatus(item.State)
	if status == "" {
		return // 未知状态：前向兼容忽略
	}
	prev := a.cachedStatus(ip, containerID)
	if a.cm.PatchAndroidStatus(ip, containerID, status) {
		log.Printf("[v4ws] 主机 %s 实例 %s 状态 → %s", ip, containerID, status)
		a.notifySoon()
		// 状态真的变了才失效截图（快照重发会重复推同状态，无变化时不清缓存，
		// 否则每次快照都白抓一轮图）
		if prev != status {
			a.fireScreenshotInvalidate(ip, containerID)
		}
		return
	}
	// 不在缓存（外部创建 / 快照滞后）→ 拉一次全量补齐
	log.Printf("[v4ws] 主机 %s 收到未知实例事件（%s %s），拉全量补齐", ip, ev.Action, containerID)
	a.queryAsync(ip)
}

// OnClipboard 剪贴板推送：防回声比对后写系统剪贴板（云机→本机回灌）。
// 主动方向（本机→云机）见 SetAndroidClipboard；防回声由 svc.v4Clip 按实例比对
// 最近一次主动 set 的文本，相同则忽略（那是自己刚写进去被推回来的）。
func (a *v4EventAdapter) OnClipboard(host string, ev *v4ws.EventFrame, data v4ws.ClipboardEventData) {
	ip := v4hostOf(host)
	key := v4ws.InstanceKey(data.Backend, data.ID)
	a.mu.Lock()
	svc := a.clipSink
	a.mu.Unlock()
	if svc == nil {
		log.Printf("[v4ws] 主机 %s 剪贴板推送：%s %d 字符（无回灌落点，忽略）", ip, key, len([]rune(data.Text)))
		return
	}
	onV4ClipboardPush(svc, ip, key, data.Text)
}

// OnSystemResync 后端增删：v4ws 已自动请求 resync，此处只记日志。
func (a *v4EventAdapter) OnSystemResync(host string, ev *v4ws.EventFrame) {
	log.Printf("[v4ws] 主机 %s 后端集合变化（system/resync seq=%d），已请求重发快照", v4hostOf(host), ev.Seq)
}

// cachedStatus 读缓存里该实例的当前状态（截图失效判定用：状态未变就不清缓存）
func (a *v4EventAdapter) cachedStatus(ip, containerID string) string {
	for _, it := range a.cm.GetAndroidsByDevice(ip) {
		if it.ID == containerID {
			return it.Status
		}
	}
	return ""
}

// queryAsync 异步拉一次该主机全量（未知实例事件补齐用）。
// 不在消费 goroutine 里同步做 HTTP：读循环/消费循环一旦被阻塞，服务端 10s 写超时会踢线。
func (a *v4EventAdapter) queryAsync(ip string) {
	util.GoSafe(func() {
		if a.cm.GetDeployMode() != "v4" {
			return
		}
		start := time.Now()
		res, err := newV4InstanceClient(ip).List(nil)
		if err != nil {
			log.Printf("[v4ws] 主机 %s REST 补拉失败: %v", ip, err)
			return
		}
		list := v4InstancesToCache(res.Instances, ip, a.idx.Assign(ip, v4InstanceKeys(res.Instances)))
		// REST 全量也是权威事实（成功返回即代表该主机的完整实例集合）
		a.cm.ReplaceAndroidListForDevice(ip, list)
		log.Printf("[v4ws] 主机 %s REST 补拉完成 count=%d 耗时=%v", ip, len(list), time.Since(start))
		a.notifySoon()
	})
}

// notifySoon 100ms 合并窗口内只推一次 androidlist:updated。
func (a *v4EventAdapter) notifySoon() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.notifyTimer != nil {
		return
	}
	a.notifyTimer = time.AfterFunc(notifyDebounce, func() {
		a.mu.Lock()
		a.notifyTimer = nil
		a.mu.Unlock()
		if a.app == nil {
			return
		}
		a.app("androidlist:updated", a.cm.GetAndroidList())
	})
}

// indexFor 为一批实例派生稳定 indexNum（委托共享 v4Indexer，与 REST 路径同源）
func (a *v4EventAdapter) indexFor(ip string, items []v4ws.InstanceItem) map[string]int {
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.Key())
	}
	return a.idx.Assign(ip, keys)
}

// v4ItemToCache v4ws.InstanceItem → config.AndroidInfo（与 REST 路径共用 v4CacheItem）
func v4ItemToCache(it v4ws.InstanceItem, ip string, indexNum int) config.AndroidInfo {
	return v4CacheItem(v4ItemFields{
		Key: it.Key(), Name: it.Name, FallbackID: it.ID, State: it.State, Variant: it.Variant,
		MemoryMB: it.MemoryMB, DiskUsedMB: it.DiskUsedMB, Profile: it.Profile,
	}, ip, indexNum)
}

// v4StateToStatus V4 实例状态 → 前端容器状态词汇。
// stopped→exited（「已关机」）、starting/stopping/running 原样、unknown 保留原文。
// 状态词汇必须落在 utils/containerStatus 的枚举里，否则前端 badge 显示原文。
func v4StateToStatus(state string) string {
	switch strings.ToLower(state) {
	case "stopped":
		return "exited"
	case "starting":
		return "starting"
	case "running":
		return "running"
	case "stopping":
		return "stopping"
	case "unknown":
		return "unknown"
	default:
		return strings.ToLower(state)
	}
}

// v4hostOf 从拨号 host（ip:8088）剥出纯 IP——V4 以 IP 为设备 key。
func v4hostOf(host string) string { return v4HostIP(host) }
