package screenshot

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

const (
	pollInterval              = 1000 * time.Millisecond
	fetchTimeout              = 2000 * time.Millisecond
	minScreenshotWorkers      = 32
	unchangedBackoffThreshold = 5                      // 连续5次未变化后进入退避
	unchangedBackoffDuration  = 3 * time.Second        // 退避3秒
	notifyFlushInterval       = 150 * time.Millisecond // 变化推送的最小合批间隔
)

// isDeviceID 判断标识符是 device_id 还是局域网 IP。
// device_id 可能以数字开头（如 "8l617..."），不能用首字符判断。
// 正确方式：不是合法 IP 的就是 device_id。
func isDeviceID(s string) bool {
	if s == "" {
		return false
	}
	return net.ParseIP(s) == nil
}

// screenshotTransport 截图专用 Transport：每设备多端口并发，无连接上限
var screenshotTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   500 * time.Millisecond,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 20,
	IdleConnTimeout:     60 * time.Second,
}

var httpClient = &http.Client{
	Timeout:   0,
	Transport: screenshotTransport,
}

// ContainerTarget 截图轮询目标（定义见 screenshot_target.go）
//
// TargetFetcher 自定义抓图函数（定义见 screenshot_target.go）

// Service 截图后台轮询服务
type Service struct {
	mu          sync.RWMutex
	cache       map[string]*CacheItem
	stopCh      chan struct{}
	dispatching int32 // atomic: 0=空闲, 1=正在轮询
	running     bool

	getContainers     func() []ContainerTarget
	onUpdated         func(changedKeys []string)
	selectedDeviceIDs map[string]bool // 前端勾选的设备ID，为空时getContainers返回空

	// publicSnap 公网模式截图回调（注入后公网设备走网关透传截图，不注入则直连）
	publicSnap func(deviceID string, indexNum int) ([]byte, error)

	// targetFetcher 自定义抓图回调（V4 走 8088 截图接口）：目标带 InstanceID 时优先走它
	targetFetcher TargetFetcher

	// onceKeys/onceMu GetScreenshot 未命中同步抓图的 per-key 去重锁
	//（未命中并发同 key 只抓一次，其余等结果读缓存；见 screenshot_cache.go fetchSingleOnce）
	onceMu   sync.Mutex
	onceKeys map[string]*sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
}

func NewService() *Service {
	return &Service{
		cache:  make(map[string]*CacheItem),
		stopCh: make(chan struct{}),
	}
}

func (s *Service) SetContainerGetter(fn func() []ContainerTarget) {
	s.mu.Lock()
	s.getContainers = fn
	s.mu.Unlock()
}

// SetPublicSnap 注入公网模式截图回调（public 模式下空 IP 设备走网关透传截图）。
func (s *Service) SetPublicSnap(fn func(deviceID string, indexNum int) ([]byte, error)) {
	s.mu.Lock()
	s.publicSnap = fn
	s.mu.Unlock()
}

// SetTargetFetcher 注入自定义抓图回调（V4 走 8088 截图接口）。
// 仅对带 InstanceID 的目标生效，其余目标仍走 9082/公网网关。
func (s *Service) SetTargetFetcher(fn TargetFetcher) {
	s.mu.Lock()
	s.targetFetcher = fn
	s.mu.Unlock()
}

// getTargetFetcher 读取自定义抓图回调（持锁读，避免与注入并发竞态）
func (s *Service) getTargetFetcher() TargetFetcher {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.targetFetcher
}

func (s *Service) SetOnUpdated(fn func(changedKeys []string)) {
	s.mu.Lock()
	s.onUpdated = fn
	s.mu.Unlock()
}

// SetSelectedDevices 设置前端勾选的设备ID列表，为空时不轮询任何设备
func (s *Service) SetSelectedDevices(deviceIDs []string) {
	s.mu.Lock()
	m := make(map[string]bool, len(deviceIDs))
	for _, id := range deviceIDs {
		m[id] = true
	}
	old := len(s.selectedDeviceIDs)
	s.selectedDeviceIDs = m
	s.mu.Unlock()
	log.Printf("[screenshot] SetSelectedDevices: count=%d (was %d) ids=%v", len(deviceIDs), old, deviceIDs)
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

	util.Debugf("[screenshot] 服务启动，轮询间隔 %v", pollInterval)
	util.GoSafe(s.pollLoop)
}

// TriggerNow 立即触发一次截图轮询（非阻塞）
func (s *Service) TriggerNow() {
	util.GoSafe(s.dispatchPoll)
}

// RefreshNow 同步执行一轮截图轮询（上一轮仍在进行则直接返回）。
// 刷新按钮用：列表刷新完成后串行重抓截图，返回时截图已是最新。
func (s *Service) RefreshNow() {
	s.dispatchPoll()
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

// pollLoop 带自恢复的外层循环：内层 pollLoopOnce panic（被 recover 捕获）后
// sleep 一小段再重建 ticker 继续轮询，避免单次 panic 让后台轮询协程静默退出、
// 整个截图系统无日志停摆。recover 后 sleep 防止持续性 panic 退化成 hot-loop；
// 正常退出（Stop）则不再重启。
func (s *Service) pollLoop() {
	for {
		panicked := true
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] screenshot pollLoop recovered, restart: %v", r)
				} else {
					panicked = false
				}
			}()
			s.pollLoopOnce()
		}()
		if !panicked {
			return
		}
		time.Sleep(time.Second)
	}
}

func (s *Service) pollLoopOnce() {
	s.dispatchPoll()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.dispatchPoll()
		}
	}
}

// dispatchPoll 用 worker 池抓取截图，即时推送变化
// 非阻塞：上一轮未完成则跳过本轮
func (s *Service) dispatchPoll() {
	if !atomic.CompareAndSwapInt32(&s.dispatching, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&s.dispatching, 0)

	s.mu.RLock()
	getFn := s.getContainers
	selectedIDs := s.selectedDeviceIDs
	s.mu.RUnlock()
	if getFn == nil {
		return
	}

	start := time.Now()
	allTargets := getFn()

	// 按前端勾选的设备ID过滤
	var targets []ContainerTarget
	if len(selectedIDs) > 0 {
		for _, t := range allTargets {
			if selectedIDs[t.DeviceID] {
				targets = append(targets, t)
			}
		}
	}
	if len(targets) == 0 {
		return
	}

	seen := make(map[string]bool, len(targets))
	var unique []ContainerTarget
	now := time.Now()
	skippedOffline := 0
	skippedUnchanged := 0
	for _, t := range targets {
		key := fmt.Sprintf("%s_%d", t.DeviceIP, t.IndexNum)
		if !seen[key] {
			seen[key] = true
			// 离线退避 + 未变化退避：跳过正在退避的目标
			s.mu.RLock()
			if item, ok := s.cache[key]; ok {
				if item.Status == "offline" && now.Before(item.SkipUntil) {
					s.mu.RUnlock()
					skippedOffline++
					continue
				}
				if now.Before(item.unchangedSkipUntil) {
					s.mu.RUnlock()
					skippedUnchanged++
					continue
				}
			}
			s.mu.RUnlock()
			unique = append(unique, t)
		}
	}

	workers := util.ParallelWorkers("io")
	if workers < minScreenshotWorkers {
		workers = minScreenshotWorkers
	}

	// worker 产出的变化 key 经此通道交给下面的合批推送协程
	changedCh := make(chan string, len(unique))
	notifyDone := make(chan struct{})

	go func() {
		// close(notifyDone) 必须在任何退出路径都执行（defer 兜底）：通知协程
		// 若 panic 退出且未 close，dispatchPoll 会永久阻塞在 <-notifyDone，
		// dispatching 复位永不执行 → 之后所有轮询在 CAS 处被跳过，截图系统
		// 无日志停摆、前端永久显示「开机中」（该事故已修，不可回退）。
		defer close(notifyDone)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] screenshot notify goroutine recovered: %v", r)
			}
		}()
		// 合批推送：变化 key 攒成一批一次通知。必须合批而非逐 key 通知——逐 key 会让
		// 前端「一事件一查询」，且前端 versionRef 在取回数据前不更新，并发查询都把同样
		// 的 key 判为未同步 → N 张图变化产生 N 次全量取图（O(N²) IPC），设备多且画面
		// 持续变化时内存被 IPC 载荷打爆（该事故已修，不可回退）。
		// 但合批不能等整轮结束：一轮里有不可达容器时，整轮要等 DialContext 500ms /
		// fetchTimeout 2000ms 才收工（实测轮耗时 p95=501ms、max=2907ms），期间已经
		// 抓好的新图全压在通道里，前端要等最慢的那台才有反应 —— 表现为截图成片迟到、
		// 一次涌出几十张。改成每 notifyFlushInterval 落一次批，慢目标不再拖累快结果。
		var batch []string
		flush := func() {
			if len(batch) == 0 {
				return
			}
			keys := batch
			batch = nil
			s.mu.RLock()
			fn := s.onUpdated
			s.mu.RUnlock()
			if fn != nil {
				fn(keys)
			}
		}
		ticker := time.NewTicker(notifyFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case key, ok := <-changedCh:
				if !ok {
					// changedCh 关闭 = 本轮数据全部收完：落最后一批后退出。
					// close(notifyDone) 由函数入口的 defer 统一负责（显式再
					// close 会与 defer 双重关闭 panic，该事故已修，不可回退）。
					flush()
					return
				}
				batch = append(batch, key)
			case <-ticker.C:
				flush()
			}
		}
	}()

	taskCh := make(chan ContainerTarget, len(unique))
	for _, t := range unique {
		taskCh <- t
	}
	close(taskCh)

	var okCount, errCount, offlineCount int32
	var totalChanged int32
	var wg sync.WaitGroup
	for i := 0; i < workers && i < len(unique); i++ {
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] goroutine recovered: %v", r)
				}
			}()
			defer wg.Done()
			for t := range taskCh {
				key := fmt.Sprintf("%s_%d", t.DeviceIP, t.IndexNum)
				oldVer := s.getItemVersion(key)
				s.fetchSingle(t, key)
				newVer := s.getItemVersion(key)
				if oldVer != newVer {
					atomic.AddInt32(&totalChanged, 1)
					changedCh <- key
				}
				s.mu.RLock()
				if item, ok := s.cache[key]; ok {
					switch item.Status {
					case "ok":
						atomic.AddInt32(&okCount, 1)
					case "offline":
						atomic.AddInt32(&offlineCount, 1)
					default:
						atomic.AddInt32(&errCount, 1)
					}
				}
				s.mu.RUnlock()
			}
		}()
	}
	wg.Wait()
	close(changedCh)
	<-notifyDone

	util.Debugf("[screenshot] 轮询完成 targets=%d skipOffline=%d skipUnchanged=%d ok=%d err=%d offline=%d changed=%d 耗时=%v",
		len(unique), skippedOffline, skippedUnchanged, okCount, errCount, offlineCount, totalChanged, time.Since(start))
	s.cleanupStale()
}

func (s *Service) getItemVersion(key string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if item, ok := s.cache[key]; ok {
		return item.Version
	}
	return 0
}

// fetchSingle + truncatePreview 已拆分到 screenshot_fetch.go
