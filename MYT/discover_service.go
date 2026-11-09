package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
	"github.com/moyunteng/myt/internal/v4host"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// DiscoverServiceWrapper 设备发现服务包装（递增间隔轮询：5s→30s）
type DiscoverServiceWrapper struct {
	discoverer        *discover.Discoverer
	configManager     *config.Manager
	eventwsSyncer     func()                // 发现完成后触发 eventws 对账（建连/断开，替代 httpbeat TriggerNow）
	eventwsDisconnect func(deviceID string) // 设备移除时断开 WS 连接
	ipMappingSetter   func(ip, deviceID string)
	onNewDevice       func(deviceID, ip string)
	app               *application.App

	// public 模式线上服务客户端（由 SetMytAgentService 注入 token 来源）
	mytAgentSvc *MytAgentService
	// sdkApiSvc 用于复用共享的网关客户端，避免每次发现都重新登录
	sdkApiSvc *SdkApiServiceWrapper
	// onDeviceListUpdated 设备列表更新后回调（触发 androidlist 立即刷新容器列表）
	onDeviceListUpdated func()
	// v2Refresher v2 模式的"发现"等价物：批量探测主机刷新在线状态（discoverAndUpdate 分支）
	v2Refresher func()
	// v4Scanner v4 模式的发现入口（UDP 7678 广播 + 8088 /health 验证）：
	// 扫描与落库由 V4ServiceWrapper 负责，这里只传模式代际校验钩子（提交前校验）
	v4Scanner func(ctx context.Context, commitValid func() bool) error
	// v4Prober v4 模式手动添加的定向探测（只探 IP，不做 UDP 单播/LocalDevices 过滤）
	v4Prober func(ctx context.Context, ips []string) ([]v4host.Info, []string)
	// onPendingAdded v2 模式待添加主机移入正式列表后的联动（拉容器 + 刷新状态）
	onPendingAdded func(deviceIDs []string)

	// 绑定状态查询（30 分钟定时轮询 + 添加设备时增量）
	paymentSvc *PaymentServiceWrapper
	bindStopCh chan struct{}
	bindWg     sync.WaitGroup
	// bindCovered / lastBindQueryAt：发现链路（public 每轮）补查绑定状态的限频与去重。
	// bindCovered = 本进程已查过绑定状态的设备；lastBindQueryAt = 上次补查时间。
	// 30 分钟全量轮询只覆盖「登录时已在列表」的设备，public 后加入的设备此前要等
	// 下一个 30 分钟周期才有 bind_status（前端授权页表现为长期「未绑定」）。
	bindQueryMu     sync.Mutex
	bindCovered     map[string]struct{}
	lastBindQueryAt time.Time
	// 坑位到期查询（30 分钟定时 + 添加设备增量，并发限 4）
	slotQueryFn func(deviceIDs []string)

	stopCh     chan struct{}
	running    bool
	polling    int32 // atomic: 防止上一轮未完成时重叠
	generation int64 // atomic: 模式切换代际，discoverAndUpdate 写数据前校验，避免旧模式结果污染新模式
	// manualHint/manualPending（atomic）：public 手动刷新撞轮守卫的补跑机制。
	// RefreshDevices 置 manualHint；discoverAndUpdate 撞守卫时转记 manualPending，
	// 本轮结束（defer）时补跑一轮——public 自动轮询窗口 5~30s，手动刷新若被
	// 静默丢弃，用户要等下个轮询周期才能看到结果（公网刷新必达）。
	manualHint    int32
	manualPending int32
	// autoPollSkipFirst：本次 StartAutoPoll 跳过 autoPollLoop 的立即首轮
	// （调用方自行同步执行首轮并需要其返回值时置位，见 RefreshDevices）。
	// 写于 go autoPollLoop() 之前，happens-before 成立，无需原子。
	autoPollSkipFirst bool
	// lastErrEmitAt（atomic unix 秒）：devicelist:error 事件 60s 节流。
	lastErrEmitAt int64
}

func NewDiscoverServiceWrapper(d *discover.Discoverer, cm *config.Manager) *DiscoverServiceWrapper {
	return &DiscoverServiceWrapper{
		discoverer:    d,
		configManager: cm,
		stopCh:        make(chan struct{}),
		bindStopCh:    make(chan struct{}),
		bindCovered:   make(map[string]struct{}),
	}
}

// SetEventwsSyncer 注入 eventws 对账触发器（发现完成后建连新设备，替代 httpbeat TriggerNow）
func (s *DiscoverServiceWrapper) SetEventwsSyncer(fn func()) {
	s.eventwsSyncer = fn
}

// SetEventwsDisconnect 注入设备移除时的 WS 断开回调
func (s *DiscoverServiceWrapper) SetEventwsDisconnect(fn func(deviceID string)) {
	s.eventwsDisconnect = fn
}

func (s *DiscoverServiceWrapper) SetPaymentService(ps *PaymentServiceWrapper) {
	s.paymentSvc = ps
}

// SetSlotQueryFn 注入坑位到期查询函数（手动刷新时触发全量查询）
func (s *DiscoverServiceWrapper) SetSlotQueryFn(fn func(deviceIDs []string)) {
	s.slotQueryFn = fn
}

// SetMytAgentService 注入 MytAgentService（public 模式提供 session_token）
func (s *DiscoverServiceWrapper) SetMytAgentService(svc *MytAgentService) {
	s.mytAgentSvc = svc
}

// SetSdkApiService 注入 SdkApiServiceWrapper（复用共享网关客户端）
func (s *DiscoverServiceWrapper) SetSdkApiService(svc *SdkApiServiceWrapper) {
	s.sdkApiSvc = svc
}

// SetOnDeviceListUpdated 注入设备列表更新回调（供 androidlist 立即刷新）
func (s *DiscoverServiceWrapper) SetOnDeviceListUpdated(fn func()) {
	s.onDeviceListUpdated = fn
}

// SetV2Refresher 注入 v2 模式的主机状态刷新回调（v2 无 UDP 发现，探测即发现）
func (s *DiscoverServiceWrapper) SetV2Refresher(fn func()) {
	s.v2Refresher = fn
}

// SetOnPendingAdded 注入 v2 模式待添加主机入列后的联动回调
func (s *DiscoverServiceWrapper) SetOnPendingAdded(fn func(deviceIDs []string)) {
	s.onPendingAdded = fn
}

// SetV4Scanner 注入 v4 模式的发现入口（setup 注入 v4Svc.ScanForDiscover）
func (s *DiscoverServiceWrapper) SetV4Scanner(fn func(ctx context.Context, commitValid func() bool) error) {
	s.v4Scanner = fn
}

// SetV4Prober 注入 v4 模式手动添加的定向探测（setup 注入 v4Svc.ProbeV4IPs）
func (s *DiscoverServiceWrapper) SetV4Prober(fn func(ctx context.Context, ips []string) ([]v4host.Info, []string)) {
	s.v4Prober = fn
}

func (s *DiscoverServiceWrapper) SetIPMappingSetter(fn func(ip, deviceID string)) {
	s.ipMappingSetter = fn
}

func (s *DiscoverServiceWrapper) SetOnNewDevice(fn func(deviceID, ip string)) {
	s.onNewDevice = fn
}

func (s *DiscoverServiceWrapper) SetApp(app *application.App) {
	s.app = app
}

// RefreshNow 立即触发一次设备发现（按当前部署模式：local 走 UDP，public 走线上）。
// public 模式下同时确保自动轮询已启动。
func (s *DiscoverServiceWrapper) RefreshNow() {
	if s.configManager.GetDeployMode() == "public" && !s.running {
		s.StartAutoPoll()
		return // StartAutoPoll 内部会立即发现一次
	}
	go s.discoverAndUpdate()
}

// RefreshDevices 设备列表「刷新」按钮：同步跑一轮 discoverAndUpdate 并等待完成
// （local ≈2s UDP + 增量合并；public = 网关拉取）。与 RefreshNow 的区别：
// 阻塞到本轮结束，让前端刷新按钮的 loading 态覆盖真实扫描窗口；
// 语义 = 增量合并（已存在设备同步 IP 漂移/型号，新设备进待添加），不是纯扫描。
// 返回失败原因给前端 toast（public 的网关登录/拉取失败不再静默）。
func (s *DiscoverServiceWrapper) RefreshDevices() error {
	if s.configManager.GetDeployMode() == "public" && !s.running {
		// StartAutoPoll 内部首轮被跳过（autoPollSkipFirst）：改为下方同步执行
		// 一轮并返回其错误——保证点击有实际动作且失败原因可见（此前纯异步，
		// 首轮失败前端毫无感知）。
		s.autoPollSkipFirst = true
		s.StartAutoPoll()
		return s.discoverAndUpdate()
	}
	atomic.StoreInt32(&s.manualHint, 1) // 撞轮守卫时转记 manualPending 补跑（见 core.go）
	return s.discoverAndUpdate()
}

// emitDiscoverError 公网拉取失败的错误事件（60s 节流）。轮询/补跑路径的失败
// 无法经 RefreshDevices 返回值送达前端（只有手动刷新路径有同步返回）——
// 不 emit 的话网关故障时用户只看到空列表，没有任何失败提示。
// 节流避免 5~30s 轮询每轮失败都弹 toast。
func (s *DiscoverServiceWrapper) emitDiscoverError(msg string) {
	if s.app == nil || s.app.Event == nil {
		return
	}
	now := time.Now().Unix()
	last := atomic.LoadInt64(&s.lastErrEmitAt)
	if now-last < 60 || !atomic.CompareAndSwapInt64(&s.lastErrEmitAt, last, now) {
		return
	}
	s.app.Event.Emit("devicelist:error", msg)
}

// BumpGeneration 递增模式代际，使进行中的旧模式发现轮次丢弃结果。
// 用于 SetDeployMode 切换模式后，避免上一轮旧模式发现把数据写回新模式的列表。
func (s *DiscoverServiceWrapper) BumpGeneration() {
	atomic.AddInt64(&s.generation, 1)
}

// StartAutoPoll 启动自动轮询（递增间隔：5s→30s）。
// 仅 public 模式使用：持续从线上网关拉取设备列表。
// local 模式走 RefreshNow 单次发现，新设备进 pending 待用户添加。
func (s *DiscoverServiceWrapper) StartAutoPoll() {
	s.StopAutoPoll()
	s.stopCh = make(chan struct{})
	s.running = true

	go s.autoPollLoop()
}

func (s *DiscoverServiceWrapper) StopAutoPoll() {
	if s.running {
		close(s.stopCh)
		s.running = false
	}
}

func (s *DiscoverServiceWrapper) autoPollLoop() {
	// 启动时立即发现一次（RefreshDevices 已同步执行首轮时跳过，避免重复跑）
	if s.autoPollSkipFirst {
		s.autoPollSkipFirst = false
	} else {
		s.discoverAndUpdate()
	}

	// 递增间隔：前 30 秒每 5 秒，之后每 30 秒
	const fastInterval = 5 * time.Second
	const slowInterval = 30 * time.Second
	const fastPhase = 30 * time.Second

	startTime := time.Now()
	switchedToSlow := false
	ticker := time.NewTicker(fastInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.discoverAndUpdate()
			if !switchedToSlow && time.Since(startTime) > fastPhase {
				switchedToSlow = true
				ticker.Reset(slowInterval)
				log.Printf("[discover] 进入稳定期，轮询间隔调整为 %v", slowInterval)
			}
		}
	}
}
