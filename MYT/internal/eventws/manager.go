// eventws 管理层：按设备管理连接生命周期、事件回调分发。
//
// 在线性判定（替代 httpbeat）：WS 心跳即存活信号——
//
//	建连且收到 snapshot（事件流健康）  → 在线（State 0）
//	拨号失败（401 → State 2 认证失败） / 90s 无帧看门狗 / 断连 → 离线（State 1）
//	404 老 SDK → 同普通断线退避重连（2026-09-08 用户决策：与离线重连同节奏，
//	  设备升级出 WS 端点后秒级恢复；404 日志按设备节流防刷屏）
//
// 新设备发现（discover）→ Connect：立即建连，snapshot 随后覆盖本地状态。
// public 模式设备（ip 为空）不建连——沿用 androidlist/devicedetail 的跳过模式。
package eventws

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/util"
)

// deviceWSPort SDK WS 事件端点端口（与设备管理 API 同端口）。
const deviceWSPort = "8000"

// dedupForceInterval 离线态重复上报的强制转发间隔：prev/state/config 三者一致
// 的离线重复上报（404 每轮 1~10s）正常去重，但超过此间隔强制转发一次——
// 给 setup 的 HTTP 复核链路自愈机会（防复核死锁，见 reportDeviceState 注释）。
// 仅限离线态：在线/认证态的重复转发只重放无意义的级联，不参与兜底。
// 30s 的依据：复核成功记忆 60s 是探测频率的实际上限，此值只决定「离线误判
// 的最长持续时间」——SDK 降级/回退重启期间首次探测必失败（写离线），设备
// 起来后 404 上报持续到达，等本间隔到期转发一次即恢复在线。真离线设备的
// 探测负载 = 每 30s 一次经 AcquireDeviceHTTP（16 并发+20ms 间隔）排队，可控。
const dedupForceInterval = 30 * time.Second

// 设备状态值（与 config 设备状态语义一致：0=在线 1=离线 2=认证失败）
const (
	stateOnline   = 0
	stateOffline  = 1
	stateAuthFail = 2
)

// logHookMinInterval logHandler（前端 console 日志）转发间隔下限。
// 千台规模事件流可达 10 条/s，每条都是一次 WebView eval（IPC）——前端消费端
// （App.tsx）本就按 333ms 节流，Go 侧 400ms 与之对齐（略宽保证前端零丢弃）。
const logHookMinInterval = 400 * time.Millisecond

// Service 按设备管理 WS 连接。
type Service struct {
	mu      sync.Mutex
	conns   map[string]*conn // deviceID → 连接
	stopped bool

	// 在线性上报：连接状态变化时写 config 设备状态 + 通知前端（setup 注入）
	reportState func(deviceID string, state int)
	lastState   map[string]int // deviceID → 上次上报状态（去重）
	// 上次实际转发时间：重复上报超过 dedupForceInterval 强制转发（复核死锁自愈）
	lastForward map[string]time.Time
	// config 实际状态查询（在线性对账用，setup 注入 cm.GetDeviceState）
	stateQueryer func(deviceID string) int

	// 事件日志钩子：每帧/连接事件一行日志（前端 console 调试，setup 注入）。
	// 转发经 forwardLog 全局节流（logHookMinInterval），超量静默丢弃。
	logHandler func(deviceID, line string)

	// logHandler 节流状态（全部连接共享一个出口）
	logMu      sync.Mutex
	lastLogFor time.Time

	// 事件回调路由：androidlist（容器事件）+ devicedetail（设备指标）
	androidHandlers    Handlers
	systemStatsHandler func(deviceID string, raw json.RawMessage)
	helloHandler       func(deviceID string, h *HelloFrame)

	// 测试缝：覆盖拨号端口（缺省 deviceWSPort）。仅同包测试使用。
	dialPortOverride string

	// public（公网网关透传）形态：publicToken 非空 = 启用。公网设备（IP 为空）
	// 经网关 /v1/devices/{id}/passthrough/ws/events 透传建连（实测 101，帧协议
	// 与本地同构——消费层零改动复用）。gatewayHost 由 setup 注入（cloudapi 网关）。
	publicToken   func() (string, error)
	publicGateway string

	getPassword func(deviceIP string) string
}

// 编译期断言：Service 实现 Handlers（作为 conn 的回调路由器）
var _ Handlers = (*Service)(nil)

func NewService() *Service {
	return &Service{
		conns:       make(map[string]*conn),
		lastState:   make(map[string]int),
		lastForward: make(map[string]time.Time),
	}
}

// SetPasswordGetter 注入设备密码查询（与 androidlist/devicedetail 共用同一 getter）。
func (s *Service) SetPasswordGetter(fn func(deviceIP string) string) {
	s.mu.Lock()
	s.getPassword = fn
	s.mu.Unlock()
}

// SetPublicDialer 注入公网网关透传形态（public 模式设备建连用）。
// getToken 返回智能体网关 session JWT（每次拨号现取）；gatewayHost 为 B 端网关
// host:port（容忍带 http(s):// 前缀，内部剥掉——cloudapi.GatewayURL 是带 scheme
// 的完整地址，dialURL 会自行拼 ws://，双重 scheme 是非法 URL 必然拨号失败）。
// 未注入时公网设备（IP 为空）维持不建连的现状。
func (s *Service) SetPublicDialer(getToken func() (string, error), gatewayHost string) {
	gatewayHost = strings.TrimPrefix(strings.TrimPrefix(gatewayHost, "https://"), "http://")
	gatewayHost = strings.TrimSuffix(gatewayHost, "/")
	s.mu.Lock()
	s.publicToken = getToken
	s.publicGateway = gatewayHost
	s.mu.Unlock()
}

// SetHandlers 注入容器事件回调（androidlist 集成层实现 Handlers 接口）。
func (s *Service) SetHandlers(h Handlers) {
	s.mu.Lock()
	s.androidHandlers = h
	s.mu.Unlock()
}

// SetSystemStatsHandler 注入设备级指标回调（devicedetail：raw 与 /info/device 动态字段同构）。
func (s *Service) SetSystemStatsHandler(fn func(deviceID string, raw json.RawMessage)) {
	s.mu.Lock()
	s.systemStatsHandler = fn
	s.mu.Unlock()
}

// SetHelloHandler 注入 hello 身份字段回调（devicedetail：deviceId/固件版本/机型）。
func (s *Service) SetHelloHandler(fn func(deviceID string, h *HelloFrame)) {
	s.mu.Lock()
	s.helloHandler = fn
	s.mu.Unlock()
}

// SetLogHandler 注入事件日志钩子（每帧/连接事件一行，前端 console 调试用）。
// 转发全局节流：至少间隔 logHookMinInterval（约 2.5 条/s）——事件流本身可达
// 10 条/s（124 台 × stats 20s），节流后 IPC/eval 开销与前端消费速率对齐。
func (s *Service) SetLogHandler(fn func(deviceID, line string)) {
	s.mu.Lock()
	s.logHandler = fn
	s.mu.Unlock()
}

// forwardLog 节流转发日志钩子（全部连接共享出口）。
func (s *Service) forwardLog(fn func(deviceID, line string), deviceID, host, line string) {
	s.logMu.Lock()
	if time.Since(s.lastLogFor) < logHookMinInterval {
		s.logMu.Unlock()
		return
	}
	s.lastLogFor = time.Now()
	s.logMu.Unlock()
	fn(deviceID, host+" "+line)
}

// --- Handlers 路由实现（转发到注入的回调；conn 只与本接口打交道） ---

func (s *Service) OnHello(deviceID, host string, h *HelloFrame) {
	s.mu.Lock()
	helloFn := s.helloHandler
	s.mu.Unlock()
	if helloFn != nil {
		helloFn(deviceID, h)
	}
}

func (s *Service) OnSnapshot(deviceID, host string, snap *SnapshotFrame) {
	s.mu.Lock()
	h := s.androidHandlers
	s.mu.Unlock()
	if h != nil {
		h.OnSnapshot(deviceID, host, snap)
	}
}

func (s *Service) OnContainerEvent(deviceID, host string, ev *EventFrame, data ContainerEventData) {
	s.mu.Lock()
	h := s.androidHandlers
	s.mu.Unlock()
	if h != nil {
		h.OnContainerEvent(deviceID, host, ev, data)
	}
}

func (s *Service) OnBootEvent(deviceID string, ev *EventFrame, data BootEventData, uiStatus string) {
	s.mu.Lock()
	h := s.androidHandlers
	s.mu.Unlock()
	if h != nil {
		h.OnBootEvent(deviceID, ev, data, uiStatus)
	}
}

func (s *Service) OnStatsEvent(deviceID string, ev *EventFrame, stats []ContainerStatsItem) {
	s.mu.Lock()
	h := s.androidHandlers
	s.mu.Unlock()
	if h != nil {
		h.OnStatsEvent(deviceID, ev, stats)
	}
}

func (s *Service) OnSystemStats(deviceID string, ev *EventFrame, raw json.RawMessage) {
	s.mu.Lock()
	fn := s.systemStatsHandler
	s.mu.Unlock()
	if fn != nil {
		fn(deviceID, raw)
	}
}

func (s *Service) OnTagsChangedEvent(deviceID string, ev *EventFrame, tags []string) {
	s.mu.Lock()
	h := s.androidHandlers
	s.mu.Unlock()
	if h != nil {
		h.OnTagsChangedEvent(deviceID, ev, tags)
	}
}

// SetStateReporter 注入在线性上报回调（替代 httpbeat 的状态判定）。
// state: 0=在线（WS 心跳健康）1=离线（拨号失败/90s 无帧/断连/404）2=认证失败。
func (s *Service) SetStateReporter(fn func(deviceID string, state int)) {
	s.mu.Lock()
	s.reportState = fn
	s.mu.Unlock()
}

// SetStateQueryer 注入 config 实际状态查询（在线性对账去重用，setup 注入 cm.GetDeviceState）。
// 作用：config 状态被外部（认证链路/迁移）写歪时，心跳帧发现不一致即重报纠正。
func (s *Service) SetStateQueryer(fn func(deviceID string) int) {
	s.mu.Lock()
	s.stateQueryer = fn
	s.mu.Unlock()
}

// reportDeviceState 上报设备状态（两级去重：本服务上次上报值 + config 实际值）。
// config 实际值由 SetStateQueryer 注入——状态被外部（认证链路/迁移）写歪时，
// 心跳帧发现不一致立即重报纠正，避免「连接活着却显示离线」。
// 注意：known 必须显式判断——map 缺 key 的零值恰等于 stateOnline(0)，
// 直接比值会把新设备的首次在线上报静默吞掉（多设备场景必现）。
//
// 时间兜底（dedupForceInterval，仅离线态）：prev/state/config 三者一致的
// 离线重复上报超过 30 秒仍强制转发一次——覆盖「复核失败写离线后的死锁」：
// 设备 SDK 降级重启期间复核失败写 State=1，起来后 WS 404 持续报 1 与
// config(1) 一致被去重永吞，复核链路失去触发点，设备永远显示离线。转发给
// setup 的复核一次即自愈。在线/认证态不参与兜底（重复转发只重放级联无收益）。
func (s *Service) reportDeviceState(deviceID string, state int) {
	s.mu.Lock()
	fn := s.reportState
	query := s.stateQueryer
	prev, known := s.lastState[deviceID]
	s.lastState[deviceID] = state
	lastFwd := s.lastForward[deviceID]
	s.mu.Unlock()
	if known && prev == state && (query == nil || query(deviceID) == state) {
		// 时间兜底只对离线态生效（dedupForceInterval）——它存在的唯一目的是给
		// setup 的 HTTP 复核链路自愈触发点。在线/认证态的重复转发没有任何自愈
		// 收益，只会每 5 分钟重放一次上线级联（全量广播 + QueryDevice +
		// TriggerNow）——300 台在线设备 ≈ 每秒 1 条全量 devicelist:updated，
		// 正是事件铁律禁止的「周期数据混入列表事件」。
		if state != stateOffline || time.Since(lastFwd) < dedupForceInterval {
			return // 高频重复：去重（404 每轮 1~10s 退避上报；在线心跳帧 20s/帧）
		}
		// 超过 30s 的离线重复上报：强制转发一次（复核链路自愈——降级/回退
		// 重启期间首次探测失败写离线后，这里是最快的恢复触发点）
	}
	if fn != nil {
		s.mu.Lock()
		s.lastForward[deviceID] = time.Now()
		s.mu.Unlock()
		fn(deviceID, state)
	}
}

// Connect 为设备建连（幂等）：已有连接且拨号目标一致则复用；目标漂移
// （设备重启换 IP / local↔public 形态切换）则停旧建新——刷新修正 IP 后
// WS 事件流必须跟随恢复，不能停留在拨旧地址的僵尸连接上。
// ip 非空 → local 直连；ip 为空且配置了公网拨号 → 网关透传建连，否则不连。
func (s *Service) Connect(deviceID, ip string) {
	if deviceID == "" {
		return
	}
	var stale *conn
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if existing, ok := s.conns[deviceID]; ok {
		if existing.targetIP() == ip {
			s.mu.Unlock()
			return // 已有连接且目标一致：复用
		}
		// 目标漂移：注销旧连接，锁外 Stop（ws.Close 可能阻塞），按新目标重建
		stale = existing
		delete(s.conns, deviceID)
	}
	var c *conn
	if ip == "" {
		tokenFn, gateway := s.publicToken, s.publicGateway
		if tokenFn == nil || gateway == "" {
			s.mu.Unlock()
			if stale != nil {
				stale.Stop() // 公网拨号未配置：旧 local 连接同样不再保留
			}
			return // 公网拨号未配置：维持不建连现状
		}
		c = newPublicConn(deviceID, gateway, tokenFn, s)
	} else {
		port := s.dialPortOverride
		if port == "" {
			port = deviceWSPort
		}
		c = newConn(deviceID, ip+":"+port, s.getPassword, s) // manager 自身即 Handlers（路由器）
	}
	c.onState = func(state int) { s.reportDeviceState(deviceID, state) }
	if s.logHandler != nil {
		fn := s.logHandler
		id, host := deviceID, c.host
		c.logHook = func(line string) { s.forwardLog(fn, id, host, line) }
	}
	s.conns[deviceID] = c
	s.mu.Unlock()

	if stale != nil {
		log.Printf("[eventws] 设备 %s 拨号目标漂移（%s → %s），重建连接", deviceID, stale.targetIP(), ip)
		stale.Stop()
	}

	util.GoSafe(func() {
		err := c.run()
		if err != nil {
			log.Printf("[eventws] 设备 %s(%s) 连接循环异常退出: %v", deviceID, ip, err)
		}
		s.cleanupConn(deviceID, c)
	})
}

// cleanupConn 连接循环退出后的注销：只清理仍是当前注册的那条连接。
// 目标漂移重建后，旧连接的 run() 可能迟到几秒才退出——无条件 delete 会
// 误删新连接的注册（设备看似无连接，下轮 Sync 再建一条 → 双连接泄漏）。
func (s *Service) cleanupConn(deviceID string, c *conn) {
	s.mu.Lock()
	if s.conns[deviceID] == c {
		delete(s.conns, deviceID)
	}
	s.mu.Unlock()
}

// Disconnect 断开指定设备的连接（设备移除时用）。
func (s *Service) Disconnect(deviceID string) {
	s.mu.Lock()
	c, ok := s.conns[deviceID]
	if ok {
		delete(s.conns, deviceID)
	}
	s.mu.Unlock()
	if ok {
		c.Stop()
	}
}

// Sync 对账设备列表：新设备建连（拨号即探测），不在列表中的断开；
// 设备 IP 漂移（重启换 IP 后经刷新修正）由 Connect 停旧建新，事件流跟随恢复。
// 传入全部 local 设备（含 State=1 的）：拨通+snapshot→reportState(0 在线)，
// 拨号失败→reportState(1 离线)。挂在 discover 发现完成 / 设备增删钩子上。
// public 模式（IP 为空的设备）在配置了公网拨号时同样建连（网关透传）。
func (s *Service) Sync(devices []config.DeviceCache) {
	s.mu.Lock()
	publicOn := s.publicToken != nil && s.publicGateway != ""
	stopped := s.stopped
	s.lastForward = make(map[string]time.Time) // 刷新解锁：去重时间兜底归零，下次上报立即转发触发复核
	// lastState 一并清空：跨模式残留的上次上报值会让新模式的首次状态上报被
	// 误判为「重复上报」去重吞掉（如 local 下报过在线、切 public 后设备实际
	// 离线，残留 prev=0 会吞掉离线上报，设备显示假在线直到 30s 离线兜底）。
	s.lastState = make(map[string]int)
	s.mu.Unlock()

	online := make(map[string]string, len(devices)) // deviceID → ip（空串=公网形态）
	for _, d := range devices {
		if d.IP != "" || publicOn {
			online[d.DeviceID] = d.IP
		}
	}
	if stopped {
		return
	}
	s.mu.Lock()
	var toStop []*conn
	for id, c := range s.conns {
		if _, ok := online[id]; !ok {
			toStop = append(toStop, c)
			delete(s.conns, id)
		}
	}
	s.mu.Unlock()

	for _, c := range toStop {
		c.Stop()
	}
	for id, ip := range online {
		s.Connect(id, ip) // 幂等：目标一致复用，漂移停旧建新（内部持锁判断）
	}
}

// StopAll 断开全部连接但不置位 stopped（服务继续可用）。
// 供模式切换到 v2 时全停：v2 主机是裸 Docker（无 8000 eventws 端点），
// 不能对 v2 列表 Sync 拨号，只需停掉 local/public 残留连接防止它们继续
// 写状态/回灌事件。与 Stop 的区别：stopped 仍为 false，后续 Sync 可正常重建。
func (s *Service) StopAll() {
	s.mu.Lock()
	conns := make([]*conn, 0, len(s.conns))
	for _, c := range s.conns {
		conns = append(conns, c)
	}
	s.conns = make(map[string]*conn)
	s.lastForward = make(map[string]time.Time) // 去重时间兜底归零，与 Sync 对账语义一致
	s.lastState = make(map[string]int)         // 跨模式残留状态值一并清空，防去重吞掉新模式首次上报
	s.mu.Unlock()
	for _, c := range conns {
		c.Stop()
	}
}

// Start 启动服务（预留：当前无全局 goroutine，连接按需建立）。
func (s *Service) Start() {}

// Stop 断开全部连接（程序退出时调用）。
func (s *Service) Stop() {
	s.mu.Lock()
	s.stopped = true
	conns := make([]*conn, 0, len(s.conns))
	for _, c := range s.conns {
		conns = append(conns, c)
	}
	s.conns = make(map[string]*conn)
	s.mu.Unlock()
	for _, c := range conns {
		c.Stop()
	}
}

// Connected 返回设备当前是否有活跃 WS 连接（供诊断/二期兜底判断用）。
func (s *Service) Connected(deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.conns[deviceID]
	return ok
}
