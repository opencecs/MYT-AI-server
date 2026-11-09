// v4ws 管理层：按主机（V4 设备 IP）管理 WS 连接生命周期、事件回调路由、状态上报。
//
// 与 V3 eventws 的差异：
//   - key 是**主机 IP**（V4 /health 无设备标识字段，IP 即身份），V3 是 deviceId
//   - 没有 public 网关形态（V4 只有局域网直连）
//   - 状态语义即 V4 的**在线性判据**（用户口径 2026-10）：连上并收到 snapshot = 在线，
//     断开/重连中/失败 = 离线（由 main 包的 v4StateReporter 写回 config.State 并推前端）；
//     离线时另有一次 /health 兜底复核，覆盖「WS 通道单独故障但设备在线」的场景。
package v4ws

import (
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/util"
	"github.com/moyunteng/myt/internal/v4host"
)

// logHookMinInterval 日志钩子（前端 console）转发间隔下限：与 V3 同款节流，
// 避免每条帧一次 WebView eval。
const logHookMinInterval = 400 * time.Millisecond

// Service 按主机管理 WS 连接。
type Service struct {
	mu      sync.Mutex
	conns   map[string]*conn // host（ip:port）→ 连接
	stopped bool

	user     string
	password string

	handlers Handlers
	// onStateChange 连接状态变化回调（host, state）——V4 的在线性判据，
	// main 包注入 v4StateReporter 写回 config.State 并推 devicedlist:updated
	onStateChange func(host string, state int)
	// logHandler 事件日志钩子（host, line）
	logHandler func(host, line string)

	logMu      sync.Mutex
	lastLogFor time.Time

	// dialPortOverride 测试缝：覆盖拨号端口（缺省由调用方给 host 时带上）
	dialPortOverride string
}

// NewService 创建 V4 WS 服务。user/password 为 Basic Auth 凭据（tobox 默认 admin/myt）。
func NewService(user, password string) *Service {
	return &Service{
		conns:    make(map[string]*conn),
		user:     user,
		password: password,
	}
}

// SetHandlers 注入事件回调（适配层实现 Handlers 接口）。
func (s *Service) SetHandlers(h Handlers) {
	s.mu.Lock()
	s.handlers = h
	s.mu.Unlock()
}

// SetStateChangeHandler 注入连接状态变化回调。
func (s *Service) SetStateChangeHandler(fn func(host string, state int)) {
	s.mu.Lock()
	s.onStateChange = fn
	s.mu.Unlock()
}

// SetLogHandler 注入事件日志钩子（前端 console 调试用，全局节流）。
func (s *Service) SetLogHandler(fn func(host, line string)) {
	s.mu.Lock()
	s.logHandler = fn
	s.mu.Unlock()
}

// SetCredentials 更新 Basic Auth 凭据（设备密码变更后调用）。
func (s *Service) SetCredentials(user, password string) {
	s.mu.Lock()
	s.user, s.password = user, password
	s.mu.Unlock()
}

// forwardLog 节流转发日志钩子（全部连接共享一个出口）。
func (s *Service) forwardLog(fn func(host, line string), host, line string) {
	s.logMu.Lock()
	if time.Since(s.lastLogFor) < logHookMinInterval {
		s.logMu.Unlock()
		return
	}
	s.lastLogFor = time.Now()
	s.logMu.Unlock()
	fn(host, line)
}

// --- Handlers 路由实现（conn 只与本接口打交道）---

func (s *Service) OnHello(host string, h *HelloFrame) {
	s.mu.Lock()
	fn := s.handlers
	s.mu.Unlock()
	if fn != nil {
		fn.OnHello(host, h)
	}
}

func (s *Service) OnSnapshot(host string, snap *SnapshotFrame) {
	s.mu.Lock()
	fn := s.handlers
	s.mu.Unlock()
	if fn != nil {
		fn.OnSnapshot(host, snap)
	}
}

func (s *Service) OnInstanceState(host string, ev *EventFrame, item InstanceItem) {
	s.mu.Lock()
	fn := s.handlers
	s.mu.Unlock()
	if fn != nil {
		fn.OnInstanceState(host, ev, item)
	}
}

func (s *Service) OnClipboard(host string, ev *EventFrame, data ClipboardEventData) {
	s.mu.Lock()
	fn := s.handlers
	s.mu.Unlock()
	if fn != nil {
		fn.OnClipboard(host, ev, data)
	}
}

func (s *Service) OnSystemResync(host string, ev *EventFrame) {
	s.mu.Lock()
	fn := s.handlers
	s.mu.Unlock()
	if fn != nil {
		fn.OnSystemResync(host, ev)
	}
}

// Connect 为主机建连（幂等）：已有连接直接复用（V4 主机的拨号目标 = IP，
// IP 变了就是另一台设备，由 Sync 负责停旧建新）。
//
// host 形如 "10.10.2.142"（无端口时补 DefaultPort）或 "10.10.2.142:8088"。
func (s *Service) Connect(host string) {
	if host == "" {
		return
	}
	addr := normalizeHost(host)
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if _, ok := s.conns[addr]; ok {
		s.mu.Unlock()
		return // 已有连接：复用
	}
	c := newConn(addr, s.user, s.password, s)
	if s.dialPortOverride != "" {
		c.host = stripPort(addr) + ":" + s.dialPortOverride
	}
	c.onState = func(state int) {
		s.mu.Lock()
		fn := s.onStateChange
		s.mu.Unlock()
		if fn != nil {
			fn(host, state)
		}
	}
	if s.logHandler != nil {
		fn := s.logHandler
		c.logHook = func(line string) { s.forwardLog(fn, host, line) }
	}
	s.conns[addr] = c
	s.mu.Unlock()

	util.GoSafe(func() {
		c.run()
		s.cleanupConn(addr, c)
	})
}

// cleanupConn 连接循环退出后的注销：只清理仍是当前注册的那条连接。
func (s *Service) cleanupConn(addr string, c *conn) {
	s.mu.Lock()
	if s.conns[addr] == c {
		delete(s.conns, addr)
	}
	s.mu.Unlock()
}

// Disconnect 断开指定主机（设备移除时用）。
func (s *Service) Disconnect(host string) {
	addr := normalizeHost(host)
	s.mu.Lock()
	c, ok := s.conns[addr]
	if ok {
		delete(s.conns, addr)
	}
	s.mu.Unlock()
	if ok {
		c.Stop()
	}
}

// Sync 按主机列表对账：不在列表中的断开，新主机建连（幂等）。
// 传入 V4 当前设备列表的 IP（在线/离线都传：WS 拨号本身就是探测）。
func (s *Service) Sync(hosts []string) {
	wanted := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		if h == "" {
			continue
		}
		wanted[normalizeHost(h)] = true
	}

	s.mu.Lock()
	var toStop []*conn
	for addr, c := range s.conns {
		if !wanted[addr] {
			toStop = append(toStop, c)
			delete(s.conns, addr)
		}
	}
	stopped := s.stopped
	s.mu.Unlock()

	for _, c := range toStop {
		c.Stop()
	}
	if stopped {
		return
	}
	for addr := range wanted {
		s.Connect(addr)
	}
}

// StopAll 断开全部连接但不置位 stopped（服务继续可用）。模式切换时调用。
func (s *Service) StopAll() {
	s.mu.Lock()
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

// Stop 断开全部连接并禁止后续建连（程序退出时调用）。
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

// Connected 返回主机当前是否有活跃连接。
func (s *Service) Connected(host string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.conns[normalizeHost(host)]
	return ok
}

// ConnCount 当前连接数（诊断用）。
func (s *Service) ConnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// normalizeHost 补默认端口（V4 SDK 监听 8088，与 /health 同端口）。
func normalizeHost(host string) string {
	return v4host.NormalizeHost(host)
}

// stripPort 去掉 host 的端口部分。
func stripPort(host string) string {
	return v4host.StripPort(host)
}
