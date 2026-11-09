package androidlist

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

const (
	maxFailCount        = 2
	offlineSkipDuration = 30 * time.Second
)

type queryTask struct {
	deviceID string
	ip       string
	idx      int
}

type queryOutcome struct {
	idx      int
	deviceID string
	androids []config.AndroidInfo
	err      error
}

type failRecord struct {
	count     int
	skipUntil time.Time
}

type Service struct {
	configManager     *config.Manager
	getDevicePassword func(deviceIP string) string
	onUpdated         func()
	stopCh            chan struct{}
	running           bool
	mu                sync.Mutex
	polling           int32 // atomic: 上一轮查询未完成时跳过（手动刷新与追踪补查重叠防护）
	ctx               context.Context
	cancel            context.CancelFunc

	// 事件通知节流（androidlist_events.go）：100ms 合并窗口
	notifyMu    sync.Mutex
	notifyTimer *time.Timer

	// 截图联动（事件驱动）：容器状态转换事件触发截图缓存失效+立即重抓
	onScreenshotEvent func(deviceIP string, indexNum int)

	selectedMu  sync.RWMutex
	selectedIDs map[string]bool // 前端勾选的设备ID，为空时 SetSelectedDevices 前不查任何设备

	failMu  sync.RWMutex
	failMap map[string]*failRecord // deviceID → 失败记录

	// public 模式网关查询（注入后公网设备走网关，不注入则跳过空 IP 设备）
	publicListAndroids func(deviceID string) ([]config.AndroidInfo, error)

	// v2 模式：容器列表走 Docker 2375（注入回调查询 + 模式判定），见 androidlist_v2.go
	v2ListAndroids func(deviceID, ip string) ([]config.AndroidInfo, error)
	// v4 模式：容器列表走 tobox_player 8088 REST（注入回调 + 模式判定），见 androidlist_v4.go
	v4ListAndroids func(deviceID, ip string) ([]config.AndroidInfo, error)
	deployMode     func() string
}

func NewService(cm *config.Manager) *Service {
	return &Service{
		configManager: cm,
		stopCh:        make(chan struct{}),
		failMap:       make(map[string]*failRecord),
	}
}

func (s *Service) SetPasswordGetter(fn func(deviceIP string) string) {
	s.mu.Lock()
	s.getDevicePassword = fn
	s.mu.Unlock()
}

func (s *Service) SetOnUpdated(fn func()) {
	s.mu.Lock()
	s.onUpdated = fn
	s.mu.Unlock()
}

// SetScreenshotEventCallback 注入截图联动回调（事件驱动）：容器状态转换事件触发
// 截图缓存失效+立即重抓。setup.go 注入 screenshot.Service 的 InvalidateSlot+TriggerNow。
func (s *Service) SetScreenshotEventCallback(fn func(deviceIP string, indexNum int)) {
	s.mu.Lock()
	s.onScreenshotEvent = fn
	s.mu.Unlock()
}

// SetPublicListAndroids 注入公网模式网关查询回调（public 模式下空 IP 设备走此回调查容器列表）。
func (s *Service) SetPublicListAndroids(fn func(deviceID string) ([]config.AndroidInfo, error)) {
	s.mu.Lock()
	s.publicListAndroids = fn
	s.mu.Unlock()
}

// refresh 全量刷新入口（TriggerNow）：查所有选中设备
func (s *Service) refresh() { s.refreshFiltered(nil, false) }

// TriggerNow 立即触发一次刷新（不等待定时器），供设备发现完成后调用以加速容器列表加载。
func (s *Service) TriggerNow() {
	go s.refresh()
}

// SetSelectedDevices 设置前端勾选的设备ID（为空不查）；新选中的设备立即查询一次
func (s *Service) SetSelectedDevices(deviceIDs []string) {
	s.selectedMu.Lock()
	m := make(map[string]bool, len(deviceIDs))
	for _, id := range deviceIDs {
		m[id] = true
	}
	old := s.selectedIDs
	s.selectedIDs = m
	s.selectedMu.Unlock()

	// 找出新选中的设备，立即查询一次
	var newIDs []string
	for id := range m {
		if !old[id] {
			newIDs = append(newIDs, id)
		}
	}
	if len(newIDs) > 0 {
		go s.queryDevicesByID(newIDs)
	}
	log.Printf("[androidlist] SetSelectedDevices: count=%d (was %d) new=%d", len(deviceIDs), len(old), len(newIDs))
}

// queryDevicesByID 按设备ID查询对应的安卓列表（设备ID→设备信息需从 configManager 获取）
func (s *Service) queryDevicesByID(deviceIDs []string) {
	onlineDevices := s.configManager.GetOnlineDevices()
	idToDev := make(map[string]config.DeviceCache, len(onlineDevices))
	for _, d := range onlineDevices {
		idToDev[d.DeviceID] = d
	}
	for _, id := range deviceIDs {
		if dev, ok := idToDev[id]; ok {
			s.QueryDevice(id, dev.IP)
		}
	}
}

// RefreshSelected 手动刷新当前选中的设备（刷新按钮调用）。
//
// 手动刷新是优先级最高且强制的：
//   - force=true：跳过 failMap 离线退避（设备可能已恢复，退避是为轮询省请求的）
//   - 跳过 GetOnlineDevices 在线过滤（状态表可能滞后，设备实际在线只是标记未更新；
//     查询失败自然计入 failMap，不影响后续退避）
//   - 查询前清选中设备的 failMap 记录（让后续轮询立即恢复查询）
//   - CAS 占用冲突时等待抢占而不是静默跳过（见 refreshFiltered）
//
// 同步等待本轮查询完成，返回失败设备 IP 列表——前端据此区分「刷新成功」与
// 「刷新失败仍显示旧缓存」，并把失败设备展示给用户（不再只报数量）。
func (s *Service) RefreshSelected() []string {
	s.selectedMu.RLock()
	n := len(s.selectedIDs)
	s.selectedMu.RUnlock()
	// v4 模式没有「勾选设备」语义（主机级全量）：前端从不调 SetSelectedDevices，
	// selectedIDs 恒空——沿用 n==0 的早退会让 V4 的刷新按钮静默无效（假成功）。
	// v4 一律强制刷新全部主机（refreshFiltered 内部同样忽略 selectedIDs）。
	if n == 0 && !s.isV4Mode() {
		return nil
	}
	failedIPs, _ := s.refreshFiltered(nil, true)
	return failedIPs
}

func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.mu.Unlock()
	// 无周期轮询：容器状态由 eventws 事件推送实时更新（见 androidlist_events.go），
	// REST 查询仅剩三个触发点——选择设备 / 手动刷新 / 设备上线（QueryDevice 一次）。
	// 例外：v2 模式主机无 eventws（无 8000），容器状态变化无实时推送——
	// 加低频兜底轮询（60s，仅勾选设备，有变化才推送），否则新建/删除/开关机
	// 的云机要等用户手动刷新才出现（截图目标列表也随之滞后）。
	if s.isV2Mode() {
		go s.pollV2Loop()
	}
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.cancel()
	close(s.stopCh)
	s.running = false
}

func (s *Service) QueryDevice(deviceID, ip string) {
	// public 模式设备无局域网 IP：经注入的网关查询补拉（与 refreshFiltered 公网路径同构）。
	// 未知容器事件的兜底补拉（queryDeviceAsync）此前在此早退——事件到达但 containerId
	// 不在缓存时公网设备的状态会静默丢失。
	if ip == "" {
		s.queryPublicDevice(deviceID)
		return
	}
	// v2 模式：主机是裸 Docker（无 SDK API），容器列表走注入的 v2 回调
	if s.isV2Mode() {
		s.queryV2Device(deviceID, ip)
		return
	}
	// v4 模式：tobox_player 8088 REST（WS 未连上/事件缺口时的兜底补拉）
	if s.isV4Mode() {
		s.queryV4Device(deviceID, ip)
		return
	}
	start := time.Now()
	util.AcquireDeviceHTTP()
	defer util.ReleaseDeviceHTTP()
	// 容器多时 /android 响应大（每容器含 CPU/Mem/端口绑定），2s 会误超时→刷新静默失败→UI 显示旧缓存
	client := sdkapi.NewClientWithTimeout(ip, 8*time.Second)
	client.SetContext(s.ctx)
	s.mu.Lock()
	pwFn := s.getDevicePassword
	s.mu.Unlock()
	if pwFn != nil {
		client.SetPassword(pwFn(ip))
	}
	androids, err := client.ListAndroids()
	if err != nil {
		log.Printf("[androidlist] 新设备查询失败 device=%s ip=%s err=%v 耗时=%v", deviceID, ip, err, time.Since(start))
		return
	}
	list := make([]config.AndroidInfo, len(androids))
	for j, a := range androids {
		list[j] = config.AndroidInfo{
			ID: a.ID, Name: a.Name, Status: a.Status,
			Image: a.Image, IndexNum: a.IndexNum,
			DeviceID: deviceID, IP: ip,
			NetworkName: a.NetworkName, PortBindings: convertPortBindings(a.PortBindings),
			ContainerIP: a.IPAddress, ModelPath: a.ModelPath,
			CPUUsage: a.CPUUsage, MemUsage: a.MemUsage, MemLimit: a.MemLimit, DiskUsage: a.DiskUsage,
			Tags: a.Tags,
		}
		// DEBUG: 记录特定设备的安卓数据(QueryDevice)
		if util.DebugEnabled && ip == "10.10.11.46" {
			log.Printf("[androidlist:DEBUG:QueryDevice] ip=%s index=%d: id=%s name=%s status=%s networkName=%s containerIP=%s",
				ip, a.IndexNum, a.ID, a.Name, a.Status, a.NetworkName, a.IPAddress)
		}
	}
	s.configManager.MergeAndroidList([]config.DeviceAndroidResult{
		{DeviceID: deviceID, Androids: list},
	}, start, nil)
	// 新设备查询成功，清除失败记录
	s.failMu.Lock()
	delete(s.failMap, deviceID)
	s.failMu.Unlock()
	log.Printf("[androidlist] ★ 新设备查询完成 device=%s ip=%s count=%d 耗时=%v", deviceID, ip, len(list), time.Since(start))
	s.mu.Lock()
	updateFn := s.onUpdated
	s.mu.Unlock()
	if updateFn != nil {
		updateFn()
	}
}

// queryPublicDevice 公网设备补拉：经注入的 publicListAndroids 网关查询（未注入维持 no-op）。
// 结果与本地路径同样走 MergeAndroidList + recordSuccess + 通知——公网容器缓存
// 的 IP 字段约定存 device_id，注入方（setup）已保证该格式。
func (s *Service) queryPublicDevice(deviceID string) {
	s.mu.Lock()
	publicListAndroids := s.publicListAndroids
	s.mu.Unlock()
	if publicListAndroids == nil {
		return
	}
	start := time.Now()
	androids, err := publicListAndroids(deviceID)
	if err != nil {
		log.Printf("[androidlist] public 补拉失败 device=%s err=%v 耗时=%v", deviceID, err, time.Since(start))
		return
	}
	s.configManager.MergeAndroidList([]config.DeviceAndroidResult{
		{DeviceID: deviceID, Androids: androids},
	}, start, nil)
	s.recordSuccess(deviceID)
	log.Printf("[androidlist] ★ public 补拉完成 device=%s count=%d 耗时=%v", deviceID, len(androids), time.Since(start))
	s.mu.Lock()
	updateFn := s.onUpdated
	s.mu.Unlock()
	if updateFn != nil {
		updateFn()
	}
}

// refreshFiltered 查询在线设备；onlyIDs==nil 查全部选中设备，否则只查指定设备（操作追踪补查）。
//
// force=true（手动刷新）：最高优先级且强制——跳过 failMap 退避、跳过在线状态过滤
// （查全部设备含 State≠0）、清选中设备 failMap、与轮询 CAS 冲突时等待抢占而非静默跳过。
// force=false（轮询/TriggerNow/设备上线/追踪补查）：维持原有保守行为。
//
// 返回 (失败设备 IP 列表, 成功设备数)。未勾选/退避跳过的设备不计入失败——
// 调用方（前端）用 IP 列表提示哪些设备刷新失败、可能仍显示旧缓存。
func (s *Service) refreshFiltered(onlyIDs map[string]bool, force bool) ([]string, int) {
	if !atomic.CompareAndSwapInt32(&s.polling, 0, 1) {
		if !force {
			return nil, 0
		}
		// 手动刷新优先级最高：等待在途轮询完成后强制执行自己的查询，
		// 宁可重复查询也不静默跳过（跳过会导致前端弹「已刷新」假成功）。
		if !s.waitForPollingIdle(pollingPreemptTimeout) {
			log.Printf("[androidlist] 手动刷新等待在途轮询超时（%v），放弃本轮", pollingPreemptTimeout)
			return nil, 0
		}
		if !atomic.CompareAndSwapInt32(&s.polling, 0, 1) {
			return nil, 0
		}
	}
	defer atomic.StoreInt32(&s.polling, 0)
	start := time.Now()

	// WS 补丁竞态防护基准：晚于该时刻被 WS 补丁过的设备，本轮 REST 结果不覆盖缓存
	queryStartedAt := start

	// force 跳过在线过滤：取全部设备（状态表可能滞后，设备实际在线只是标记未更新）
	var devices []config.DeviceCache
	if force {
		devices = s.configManager.GetDeviceList()
	} else {
		devices = s.configManager.GetOnlineDevices()
	}
	if len(devices) == 0 {
		return nil, 0
	}

	now := time.Now()
	var tasks []queryTask
	skipped := 0

	// 只刷新选中的设备（TriggerNow 查所有选中设备，操作追踪只查 dirty 设备）
	// v4 模式没有「勾选设备」语义（主机级全量，UI 也不调 SetSelectedDevices）：
	// 残留的勾选集合（切模式前留下的）若参与过滤，会让 V4 主机被整体跳过。
	s.selectedMu.RLock()
	selectedIDs := s.selectedIDs
	s.selectedMu.RUnlock()
	if s.isV4Mode() {
		selectedIDs = nil
	}

	// force 时清选中设备的失败退避记录：设备可能已恢复，且让后续轮询立即恢复查询
	if force {
		s.clearFailForSelected(selectedIDs)
	}

	for i, dev := range devices {
		// 选中过滤：未勾选的设备跳过
		if len(selectedIDs) > 0 && !selectedIDs[dev.DeviceID] {
			skipped++
			continue
		}
		// 追踪过滤：只补查指定设备
		if onlyIDs != nil && !onlyIDs[dev.DeviceID] {
			continue
		}
		// 离线退避：跳过正在退避的设备（force 豁免——手动刷新必须真查）
		if !force {
			s.failMu.RLock()
			rec, ok := s.failMap[dev.DeviceID]
			s.failMu.RUnlock()
			if ok && rec.skipUntil.After(now) {
				skipped++
				continue
			}
		}
		tasks = append(tasks, queryTask{deviceID: dev.DeviceID, ip: dev.IP, idx: i})
	}

	if len(tasks) == 0 {
		return nil, 0
	}

	taskCh := make(chan queryTask, len(tasks))
	outCh := make(chan queryOutcome, len(tasks))

	s.mu.Lock()
	publicListAndroids := s.publicListAndroids
	v2ListAndroids := s.v2ListAndroids
	v4ListAndroids := s.v4ListAndroids
	v2Mode := s.isV2ModeLocked()
	v4Mode := s.isV4ModeLocked()
	s.mu.Unlock()

	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	queryWorkers := 16
	var wg sync.WaitGroup
	for i := 0; i < queryWorkers; i++ {
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] goroutine recovered: %v", r)
				}
			}()
			defer wg.Done()
			for t := range taskCh {
				if t.ip == "" {
					// 公网模式：走网关查询（通过注入的 publicListAndroids）
					if publicListAndroids != nil {
						androids, err := publicListAndroids(t.deviceID)
						if err != nil {
							outCh <- queryOutcome{idx: t.idx, deviceID: t.deviceID, err: err}
							continue
						}
						outCh <- queryOutcome{idx: t.idx, deviceID: t.deviceID, androids: androids}
					}
					continue
				}
				if v2Mode {
					// v2 模式：主机是裸 Docker（无 SDK API），容器列表走注入的 v2 回调
					if v2ListAndroids != nil {
						androids, err := v2ListAndroids(t.deviceID, t.ip)
						outCh <- queryOutcome{idx: t.idx, deviceID: t.deviceID, androids: androids, err: err}
					}
					continue
				}
				if v4Mode {
					// v4 模式：tobox_player 8088 REST（/android 聚合全部 backend）
					if v4ListAndroids != nil {
						androids, err := v4ListAndroids(t.deviceID, t.ip)
						outCh <- queryOutcome{idx: t.idx, deviceID: t.deviceID, androids: androids, err: err}
					}
					continue
				}
				client := sdkapi.NewClientWithTimeout(t.ip, 8*time.Second)
				client.SetContext(s.ctx)
				if s.getDevicePassword != nil {
					client.SetPassword(s.getDevicePassword(t.ip))
				}
				androids, err := client.ListAndroids()
				if err != nil {
					outCh <- queryOutcome{idx: t.idx, deviceID: t.deviceID, err: err}
					continue
				}
				list := make([]config.AndroidInfo, len(androids))
				for j, a := range androids {
					list[j] = config.AndroidInfo{
						ID: a.ID, Name: a.Name, Status: a.Status,
						Image: a.Image, IndexNum: a.IndexNum,
						DeviceID: t.deviceID, IP: t.ip,
						NetworkName: a.NetworkName, PortBindings: convertPortBindings(a.PortBindings),
						ContainerIP: a.IPAddress, ModelPath: a.ModelPath,
						CPUUsage: a.CPUUsage, MemUsage: a.MemUsage, MemLimit: a.MemLimit, DiskUsage: a.DiskUsage,
						Tags: a.Tags,
					}
					// DEBUG: 记录特定设备的安卓数据
					if util.DebugEnabled && t.ip == "10.10.11.46" {
						log.Printf("[androidlist:DEBUG] ip=%s index=%d: id=%s name=%s status=%s networkName=%s containerIP=%s portBindingsCount=%d",
							t.ip, a.IndexNum, a.ID, a.Name, a.Status, a.NetworkName, a.IPAddress, len(a.PortBindings))
					}
				}
				outCh <- queryOutcome{idx: t.idx, deviceID: t.deviceID, androids: list}
			}
		}()
	}
	wg.Wait()
	close(outCh)

	var success, fail int32
	results := make([]config.DeviceAndroidResult, len(devices))
	failedIPs := make([]string, 0)
	ipByID := make(map[string]string, len(devices))
	for _, d := range devices {
		ipByID[d.DeviceID] = d.IP
	}
	for o := range outCh {
		if o.err != nil {
			atomic.AddInt32(&fail, 1)
			s.recordFail(o.deviceID)
			failedIPs = append(failedIPs, ipByID[o.deviceID])
			continue
		}
		results[o.idx] = config.DeviceAndroidResult{DeviceID: o.deviceID, Androids: o.androids}
		atomic.AddInt32(&success, 1)
		s.recordSuccess(o.deviceID)
	}

	s.configManager.MergeAndroidList(results, queryStartedAt, nil)
	log.Printf("[androidlist] 刷新完成 force=%v online=%d skipped=%d success=%d fail=%d 耗时=%v",
		force, len(devices), skipped, success, fail, time.Since(start))
	s.mu.Lock()
	updateFn := s.onUpdated
	s.mu.Unlock()
	if updateFn != nil {
		updateFn()
	}
	return failedIPs, int(success)
}

// pollingPreemptTimeout 手动刷新等待在途轮询完成的上限
const pollingPreemptTimeout = 10 * time.Second

// waitForPollingIdle 轮询等待 s.polling 归零（最多 timeout）。手动刷新 CAS 失败后的
// 强制抢占路径：sleep 200ms 间隔探测，避免引入条件变量的复杂度（冲突窗口仅秒级）。
func (s *Service) waitForPollingIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&s.polling) == 0 {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return atomic.LoadInt32(&s.polling) == 0
}

// clearFailForSelected 清除选中设备的失败退避记录（手动刷新前调用）：
// 防上轮退避残留让用户「点了刷新却因 skipUntil 没查」，并让后续轮询立即恢复。
func (s *Service) clearFailForSelected(selectedIDs map[string]bool) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	for id := range selectedIDs {
		delete(s.failMap, id)
	}
}

func (s *Service) recordFail(deviceID string) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	rec, ok := s.failMap[deviceID]
	if !ok {
		rec = &failRecord{}
		s.failMap[deviceID] = rec
	}
	rec.count++
	if rec.count >= maxFailCount {
		rec.skipUntil = time.Now().Add(offlineSkipDuration)
		log.Printf("[androidlist] ★ 设备API离线退避: device=%s (连续%d次失败, 跳过%v)", deviceID, rec.count, offlineSkipDuration)
	}
}

func (s *Service) recordSuccess(deviceID string) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	delete(s.failMap, deviceID)
}

func convertPortBindings(src map[string][]sdkapi.PortBinding) map[string][]config.PortBinding {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]config.PortBinding, len(src))
	for k, v := range src {
		bindings := make([]config.PortBinding, len(v))
		for i, b := range v {
			bindings[i] = config.PortBinding{HostIP: b.HostIP, HostPort: b.HostPort}
		}
		dst[k] = bindings
	}
	return dst
}
