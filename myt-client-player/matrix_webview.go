package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// MatrixInjectConfig + parseMatrixInjectConfig 已拆分到 matrix_webview_config.go

// MatrixManager manages the public network matrix screencast input fan-out
// for WebURL mode. It listens for UDP messages from other matrix instances,
// forwards captured page events to target instances via UDP, and handles
// dynamic role switching between primary (capture) and controlled (replay).
//
// Protocol (JSON over UDP, 127.0.0.1):
//   1. Input event:  {"t":"ev","kind":"pointerdown","x":0.5,"y":0.3,...}
//   2. Role switch: {"t":"role","primary":true,"targets":[39401,39402]}
//
// This struct is platform-agnostic (standard library only). The evalFn
// callback is wired by the platform UI to call WebView2 Eval.
type MatrixManager struct {
	listenPort  int
	conn        *net.UDPConn
	targets     []int
	targetAddrs []*net.UDPAddr
	primary     atomic.Bool
	eventCh     chan []byte // buffered: page events → UDP send goroutine
	stopCh      chan struct{}
	stopped     atomic.Bool
	evalFn      func(script string) // platform UI sets this to webChromium.Eval
	recvCount   atomic.Int64        // throttled logging
	sentCount   atomic.Int64
	inject      *MatrixInjectConfig // 被控注入配置（nil=主控或不注入）
	injectMu    sync.Mutex          // 保护 inject 字段并发读写（SW/SH 被页面分辨率上报更新）
	httpClient  *http.Client
	injectCount atomic.Int64
}

// NewMatrixManager creates a new MatrixManager. Does not start listening.
// inject 非空时，被控收到语义事件后 HTTP POST 到 myt-agent inject 接口。
func NewMatrixManager(listenPort int, primary bool, targets []int, inject *MatrixInjectConfig) *MatrixManager {
	m := &MatrixManager{
		listenPort: listenPort,
		targets:    targets,
		eventCh:    make(chan []byte, 256),
		stopCh:     make(chan struct{}),
		inject:     inject,
		httpClient: &http.Client{Timeout: 8 * time.Second},
	}
	m.primary.Store(primary)
	return m
}

// IsPrimary returns the current primary (capture) state.
func (m *MatrixManager) IsPrimary() bool {
	return m.primary.Load()
}

// SetPrimary atomically updates the primary state and notifies the page
// via evalFn to call __mxSetPrimary.
func (m *MatrixManager) SetPrimary(primary bool) {
	old := m.primary.Swap(primary)
	if old == primary {
		return
	}
	logf("[matrix] role switched: primary=%v\n", primary)
	if m.evalFn != nil {
		m.evalFn(fmt.Sprintf("window.__mxSetPrimary(%v)", primary))
	}
}

// SetTargets updates the target port list and re-resolves addresses.
func (m *MatrixManager) SetTargets(targets []int) {
	m.targets = targets
	m.resolveTargets()
}

func (m *MatrixManager) resolveTargets() {
	m.targetAddrs = m.targetAddrs[:0]
	for _, port := range m.targets {
		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			logf("[matrix] failed to resolve target port %d: %v\n", port, err)
			continue
		}
		m.targetAddrs = append(m.targetAddrs, addr)
	}
}

// Start binds the UDP listener and launches the listen + send goroutines.
func (m *MatrixManager) Start() error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", m.listenPort))
	if err != nil {
		return fmt.Errorf("matrix resolve listen addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("matrix listen udp: %w", err)
	}
	m.conn = conn
	m.resolveTargets()
	logf("[matrix] listening on 127.0.0.1:%d, primary=%v, targets=%v\n",
		m.listenPort, m.primary.Load(), m.targets)
	go m.udpListenLoop()
	go m.sendLoop()
	// 主控启动后延迟广播 reset + diag：等被控就绪，清掊切换主控残留触点，
	// 并触发被控执行 __mytControlDiag() 打印通道状态供诊断对照。
	// 场景：switchPublicMatrixPrimary 重启所有窗口，新主控启动后需通知被控复位 + 诊断。
	if m.primary.Load() && len(m.targets) > 0 {
		go func() {
			time.Sleep(time.Second)
			m.BroadcastReset()
			time.Sleep(500 * time.Millisecond)
			m.BroadcastDiag()
		}()
	}
	return nil
}

// Stop shuts down the matrix manager and closes the UDP socket.
func (m *MatrixManager) Stop() {
	if !m.stopped.CompareAndSwap(false, true) {
		return
	}
	close(m.stopCh)
	if m.conn != nil {
		m.conn.Close()
	}
}

// HandlePageEvent processes a message from the WebView2 page MessageCallback.
//   - {mx:1,...} 捕获事件：去掉 mx 后透传入队 UDP 发送（仅主控实例转发）。
//     C 方案下 JS 侧已在事件内带 t 字段（"touch"/"key"/"nav"），Go 不再强制 t:"ev"。
//   - {mx:2,...}/{mx:3,...} 探针消息：bridge 激活 / 捕获计数 / 回放计数,
//     直接记日志（主控被控都会产生，必须先于 primary 门处理）
func (m *MatrixManager) HandlePageEvent(raw string) {
	var msg map[string]any
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return
	}
	mx, ok := msg["mx"].(float64)
	if !ok {
		return
	}
	if mx == 2 || mx == 3 {
		delete(msg, "mx")
		if data, err := json.Marshal(msg); err == nil {
			logf("[matrix] page probe: %s\n", data)
		}
		return
	}
	if mx == 4 {
		// 被控 video 真实分辨率上报：videoWidth/Height 已是实际显示方向的分辨率，
		// 直接用作 inject 的 sw/sh（横屏 1920x1088 → sw=1920 sh=1088），无需互换。
		// 修正写死的 1080x1920 导致的坐标偏移（myt-agent 用 sw/sh 把归一化坐标转设备坐标）。
		vw, _ := msg["vw"].(float64)
		vh, _ := msg["vh"].(float64)
		if m.inject != nil && vw > 0 && vh > 0 {
			sw, sh := int(vw), int(vh)
			m.injectMu.Lock()
			changed := m.inject.SW != sw || m.inject.SH != sh
			if changed {
				m.inject.SW = sw
				m.inject.SH = sh
			}
			m.injectMu.Unlock()
			if changed {
				logf("[matrix-inject] video 分辨率更新: %dx%d\n", sw, sh)
			}
		}
		return
	}
	if mx != 1 || !m.primary.Load() {
		return
	}
	delete(msg, "mx")
	// 若 JS 未带 t 字段（旧逻辑兼容），默认 ev
	if _, ok := msg["t"]; !ok {
		msg["t"] = "ev"
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case m.eventCh <- data:
	default:
	}
}

// udpListenLoop reads UDP datagrams and dispatches to event/role handlers.
func (m *MatrixManager) udpListenLoop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-m.stopCh:
			return
		default:
		}
		n, err := m.conn.Read(buf)
		if err != nil {
			select {
			case <-m.stopCh:
				return
			default:
				logf("[matrix] udp read error: %v\n", err)
				return
			}
		}
		m.handleUDPMessage(buf[:n])
	}
}

func (m *MatrixManager) handleUDPMessage(data []byte) {
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	t, _ := msg["t"].(string)
	switch t {
	case "touch", "key", "nav", "gesture": // C 方案语义事件：被控优先 HTTP inject，无 inject 配置则回退页面试
		if m.inject != nil {
			m.injectEvent(data)
		} else {
			m.replayToPage(data)
		}
	case "reset": // 触点复位：被控无需 HTTP，回退页面清理
		m.replayToPage(data)
	case "ev": // v5 旧协议（合成 DOM 事件），保留向后兼容
		m.replayToPage(data)
	case "role":
		m.handleRoleMessage(msg)
	}
}

// replayToPage forwards the event JSON to the page via __mxReplay Eval.
// 兼容 v5 "ev" 旧协议与 C 方案 "touch/key/nav/reset" 语义事件（无 inject 配置时的回退）。
func (m *MatrixManager) replayToPage(data []byte) {
	count := m.recvCount.Add(1)
	if count%100 == 1 {
		logf("[matrix] received event #%d\n", count)
	}
	if m.evalFn == nil {
		return
	}
	js := fmt.Sprintf("window.__mxReplay(%q)", string(data))
	m.evalFn(js)
}

// injectEvent 把 UDP 语义事件转成 myt-agent inject 接口格式并 HTTP POST。
// UDP {t:"touch",action:"down",x,y,tid} → POST event:{type:"touch_down",nx,ny,tid}
// UDP {t:"nav",action:"home"}            → POST event:{type:"nav",nav:"goHome"}
// UDP {t:"key",keycode:66}               → POST event:{type:"key",keycode:66}
// injectEvent/convertInjectEvent/postInject/handleRoleMessage/sendLoop/sendToTargets/SendNav/BroadcastReset/BroadcastDiag 已拆分到 matrix_webview_inject.go
