package cdp

// session.go — 持久 CDP 会话：单条长连接 WS 上多路复用命令请求与事件订阅。
//
// 与短连接 Call（client.go）的区别：Network/Console/Dialog/Tracing 等域的
// 数据以「事件」形式推送（Network.requestWillBeSent、Runtime.consoleAPICalled、
// Page.javascriptDialogOpening、Tracing.dataCollected…），短连接读完命令响应
// 即断开、拿不到事件，故浏览器类工具统一走 Session。
//
// 并发模型：pending 表 + mutex 串行写帧；后台 goroutine 读帧，
// 按 id 分发响应、按 method 广播事件给订阅者（每订阅者独立缓冲 chan）。
// 连接断开时 pending 全部唤醒报错；下次 Call 自动重拨一次（WebView2 窗口
// 刷新后旧连接失效的场景）。

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Event CDP 事件帧（method + params 原始 JSON）。
type Event struct {
	Method string
	Params json.RawMessage
}

// Session 单 target 的持久 CDP 会话。
type Session struct {
	client *Client
	wsURL  string

	mu      sync.Mutex
	conn    *websocket.Conn
	nextID  int64
	pending map[int64]chan callResult
	subs    map[int]*subEntry
	subSeq  int
	closed  bool
}

type callResult struct {
	result json.RawMessage
	err    error
}

type subEntry struct {
	ch chan Event
}

// Dial 建立持久会话（显式启动；失败返回错误，由调用方决定降级策略）。
func (c *Client) Dial(wsURL string) (*Session, error) {
	s := &Session{
		client:  c,
		wsURL:   wsURL,
		pending: map[int64]chan callResult{},
		subs:    map[int]*subEntry{},
	}
	if err := s.dial(); err != nil {
		return nil, err
	}
	return s, nil
}

// dial 连接 WS 并启动读循环。
func (s *Session) dial() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, s.wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 CDP WebSocket 失败: %w", err)
	}
	conn.SetReadLimit(64 << 20) // trace 数据可达数 MB
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	go s.readLoop(conn)
	return nil
}

// readLoop 读帧并分发：响应帧投递到 pending，事件帧广播给订阅者。
func (s *Session) readLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.Read(context.Background())
		if err != nil {
			s.failAll(err)
			return
		}
		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID != nil {
			s.mu.Lock()
			ch := s.pending[*msg.ID]
			delete(s.pending, *msg.ID)
			s.mu.Unlock()
			if ch != nil {
				if msg.Error != nil {
					ch <- callResult{err: fmt.Errorf("CDP 命令失败: %s", msg.Error.Message)}
				} else {
					ch <- callResult{result: msg.Result}
				}
				close(ch)
			}
			continue
		}
		if msg.Method != "" {
			s.broadcast(Event{Method: msg.Method, Params: msg.Params})
		}
	}
}

// failAll 连接断开：唤醒所有 pending、标记断连（下次 Call 重拨）。
func (s *Session) failAll(cause error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.conn = nil
	for id, ch := range s.pending {
		ch <- callResult{err: fmt.Errorf("CDP 连接断开: %w", cause)}
		close(ch)
		delete(s.pending, id)
	}
}

// broadcast 把事件广播给所有订阅者（ chan 满则丢弃，避免慢订阅者阻塞读循环）。
func (s *Session) broadcast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		select {
		case sub.ch <- ev:
		default: // 订阅者缓冲满：丢事件（ring buffer 采集场景可接受）
		}
	}
}

// Subscribe 订阅全部事件，返回取消函数与事件 chan（缓冲 4096）。
func (s *Session) Subscribe() (<-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan Event, 4096)
	s.subSeq++
	id := s.subSeq
	s.subs[id] = &subEntry{ch: ch}
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subs, id)
	}
}

// Call 在持久连接上执行一条 CDP 命令（多路复用，可并发）。
// 连接断开时自动重拨一次再试。
func (s *Session) Call(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := s.doCall(ctx, method, params)
	if err != nil && s.reconnect() {
		rctx, rcancel := context.WithTimeout(context.Background(), timeout)
		defer rcancel()
		res, err = s.doCall(rctx, method, params)
	}
	return res, err
}

// doCall 单次请求-响应（调用方持有超时控制）。
func (s *Session) doCall(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("CDP 会话已关闭")
	}
	if s.conn == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("CDP 连接未建立")
	}
	s.nextID++
	id := s.nextID
	ch := make(chan callResult, 1)
	s.pending[id] = ch
	conn := s.conn
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, err
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, fmt.Errorf("发送 CDP 命令失败: %w", err)
	}
	s.mu.Unlock()

	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("CDP 命令 %s 失败: %w", method, r.err)
		}
		return r.result, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, fmt.Errorf("CDP 命令 %s 超时: %w", method, ctx.Err())
	}
}

// reconnect 断线重拨（已在连接中或已关闭时返回 false）。
func (s *Session) reconnect() bool {
	s.mu.Lock()
	if s.closed || s.conn != nil {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	return s.dial() == nil
}

// Close 关闭会话（幂等）。
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	conn := s.conn
	s.conn = nil
	for id, ch := range s.pending {
		ch <- callResult{err: fmt.Errorf("CDP 会话已关闭")}
		close(ch)
		delete(s.pending, id)
	}
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
}
