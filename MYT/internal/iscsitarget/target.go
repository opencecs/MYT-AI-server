package iscsitarget

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const BlockSize = 512

const maxConnections = 64
const idleTimeout = 60 * time.Second

type Session struct {
	ID           uint32
	Conn         net.Conn
	IQN          string
	InitiatorIQN string
	StatSN       uint32
	DataSN       uint32
	ExpCmdSN     uint32
	MaxCmdSN     uint32
	Active       bool
	LUNManager   LUNProvider
	HeaderDig    bool
	DataDig      bool
	MaxRecvLen   uint32
	IsDiscovery  bool
	nextTTT      uint32 // R2T 的 Target Transfer Tag 递增生成器
	done         chan struct{} // session 关闭时 close，供外部等待
}

// TargetEntry 一个 IQN 对应的 LUN 提供者
type TargetEntry struct {
	IQN  string
	LUNs LUNProvider
}

type Target struct {
	Portal      string
	Listener    net.Listener
	entries     map[string]*TargetEntry
	entryList   []*TargetEntry
	activeConns map[string]*Session // IQN -> active session
	mu          sync.RWMutex
	running     atomic.Bool
	nextSID     atomic.Uint32
	connCount   atomic.Int32
}

func NewTarget(portal string, entries []*TargetEntry) *Target {
	m := make(map[string]*TargetEntry, len(entries))
	for _, e := range entries {
		m[e.IQN] = e
	}
	return &Target{
		Portal:      portal,
		entries:     m,
		entryList:   append([]*TargetEntry{}, entries...),
		activeConns: make(map[string]*Session),
	}
}

// AddEntry 动态添加一个 IQN 条目（磁盘创建后调用）
func (t *Target) AddEntry(entry *TargetEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[entry.IQN] = entry
	t.entryList = append(t.entryList, entry)
	log.Printf("[iSCSI Target] added entry: IQN=%s", entry.IQN)
}

// RemoveEntry 动态移除一个 IQN 条目（磁盘删除后调用）
func (t *Target) RemoveEntry(iqn string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, iqn)
	filtered := make([]*TargetEntry, 0, len(t.entryList))
	for _, e := range t.entryList {
		if e.IQN != iqn {
			filtered = append(filtered, e)
		}
	}
	t.entryList = filtered
	log.Printf("[iSCSI Target] removed entry: IQN=%s", iqn)
}

// GetEntries 返回当前所有条目的快照
func (t *Target) GetEntries() []*TargetEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return append([]*TargetEntry{}, t.entryList...)
}

// GetActiveSessions 返回每个 IQN 的活跃连接数
// 注意：当前架构每个 IQN 只允许一个活跃 session（新连接会踢旧连接），
// 因此每个 IQN 的计数始终为 1。如果未来支持多 session，需改为 map[string][]*Session。
func (t *Target) GetActiveSessions() map[string]int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]int, len(t.activeConns))
	for iqn := range t.activeConns {
		if t.activeConns[iqn] != nil {
			result[iqn] = 1
		}
	}
	return result
}

// GetActiveIQNs 返回有活跃连接的 IQN 列表
func (t *Target) GetActiveIQNs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var result []string
	for iqn := range t.activeConns {
		result = append(result, iqn)
	}
	return result
}

// CloseSession 关闭指定 IQN 的活跃 session，等待 session 完全退出后返回
func (t *Target) CloseSession(iqn string) error {
	t.mu.Lock()
	session := t.activeConns[iqn]
	if session == nil {
		t.mu.Unlock()
		return fmt.Errorf("no active session for %s", iqn)
	}
	session.Conn.Close()
	delete(t.activeConns, iqn)
	t.mu.Unlock()
	<-session.done
	return nil
}

// CloseSessionsByIQN 关闭指定 IQN 的所有活跃 session，返回关闭的 session 数
func (t *Target) CloseSessionsByIQN(iqn string) int {
	t.mu.Lock()
	session := t.activeConns[iqn]
	if session == nil {
		t.mu.Unlock()
		return 0
	}
	session.Conn.Close()
	delete(t.activeConns, iqn)
	t.mu.Unlock()
	<-session.done
	return 1
}

func (t *Target) Start() error {
	listener, err := net.Listen("tcp", t.Portal)
	if err != nil {
		return fmt.Errorf("listen %s: %w", t.Portal, err)
	}
	t.Listener = listener
	t.running.Store(true)
	iqns := make([]string, 0, len(t.entryList))
	for _, e := range t.entryList {
		iqns = append(iqns, e.IQN)
	}
	log.Printf("[iSCSI Target] listening on %s (IQNs: %s)", t.Portal, strings.Join(iqns, ", "))
	go t.acceptLoop()
	return nil
}

func (t *Target) Stop() {
	t.running.Store(false)
	if t.Listener != nil {
		t.Listener.Close()
	}
}

func (t *Target) IsRunning() bool { return t.running.Load() }

func (t *Target) acceptLoop() {
	for t.running.Load() {
		conn, err := t.Listener.Accept()
		if err != nil {
			if t.running.Load() {
				log.Printf("[iSCSI Target] accept error: %v", err)
			}
			continue
		}
		if t.connCount.Load() >= maxConnections {
			log.Printf("[iSCSI Target] rejecting connection from %s: max connections reached", conn.RemoteAddr())
			conn.Close()
			continue
		}
		t.connCount.Add(1)
		go t.handleConnection(conn)
	}
}

func (t *Target) handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		t.connCount.Add(-1)
	}()

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("[iSCSI Target] new connection from %s", remoteAddr)

	// TCP keepalive：10s 间隔探测，快速检测断线
	// TCP_NODELAY：禁用 Nagle 算法，避免小 PDU（如 R2T 48字节）被延迟 40-200ms
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(10 * time.Second)
		tc.SetNoDelay(true)
	}

	session := &Session{
		Conn:       conn,
		MaxRecvLen: DefaultMaxRecvDataSegmentLength,
		done:       make(chan struct{}),
	}

	// Cleanup activeConns on session end
	defer func() {
		if session.IQN != "" {
			t.mu.Lock()
			if t.activeConns[session.IQN] == session {
				delete(t.activeConns, session.IQN)
			}
			t.mu.Unlock()
		}
		close(session.done)
	}()

	if err := session.handleLogin(t); err != nil {
		log.Printf("[iSCSI Target] login failed from %s: %v", remoteAddr, err)
		return
	}

	log.Printf("[iSCSI Target] login success from %s (initiator: %s, target: %s)", remoteAddr, session.InitiatorIQN, session.IQN)
	session.handleFullFeature(t)
}

// --- 后续方法已按职责拆分到：---
//   - target_login.go   （handleLogin / sendLoginReject）
//   - target_session.go （handleFullFeature / handleNopOut / handleLogout / handleText / buildSendTargetsResponse / handleScsiTaskManagement）
//   - target_scsi.go     （handleSCSICmd / limitData / sendDataIn / sendSCSIResponse）
