// v4ws 连接层：单主机（V4 设备）WS 连接的拨号、读循环、消费循环、看门狗、退避重连。
//
// 并发模型（与协议文档 §8「客户端必须持续读」一致）：
//
//	supervisor goroutine：run 循环（拨号 → 读循环 → 断开 → 退避）
//	读循环（connectOnce 内）：ws.Reader → io.ReadAll → frameCh，只收帧入队，零业务逻辑
//	消费 goroutine          ：frameCh 取帧 → seq 检查 → handler 回调
//	看门狗 goroutine        ：空闲检测（见 watchdog 注释）
//
// frameCh 为每轮连接的局部变量：断开即关闭并废弃，重连用全新 channel——
// 协议文档 §10「每次重连都是全新协议会话」由此从结构上保证（旧帧留在旧 channel 被 GC）。
//
// **看门狗的关键差异（与 V3 eventws 的最大区别）**：tobox 的 /ws/events 在空闲时
// **没有任何数据帧**，唯一流量是服务端每 5s 一个协议层 ping。V3 的看门狗只在「收到
// 数据帧」时重置，照搬到 V4 会把健康连接在 90s 后误杀（反复重连、快照反复重建）。
// 因此这里的存活证据有两类：
//
//	① 任意数据帧（hello/snapshot/event）
//	② DialOptions.OnPingReceived 回调（协议层 ping 到达，5s 一次）
//
// 二者都写同一个原子时间戳（lastActivity），看门狗按该时间戳判空闲。超时取 120s
// （≈24 个 ping 周期）：协议文档建议应用层空闲检测 15~30s 只是「容忍 3~6 个 ping
// 丢失」，而客户端读循环若被业务阻塞会由服务端 60s pongWait 主动断开（1006），
// 客户端看门狗只需兜住「服务端静默消失但 TCP 未断」这种极少数情况——取宽值避免误杀。
package v4ws

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

func newConn(host, user, password string, h Handlers) *conn {
	return &conn{
		host:              host,
		user:              user,
		password:          password,
		handlers:          h,
		done:              make(chan struct{}),
		dialTimeout:       dialTimeout,
		watchdogTimeout:   watchdogTimeout,
		backoffInitial:    backoffInitial,
		backoffMax:        backoffMax,
		resyncMinInterval: resyncMinInterval,
	}
}

func (c *conn) reportState(state int) {
	select {
	case <-c.done:
		return // 已停止（移除/重建）：迟到的失败上报不污染状态
	default:
	}
	if c.onState != nil {
		c.onState(state)
	}
}

// log 输出一行事件日志（主机前缀由 manager 的 logHook 加）；无 hook 时静默。
func (c *conn) log(format string, args ...any) {
	if c.logHook != nil {
		c.logHook(fmt.Sprintf(format, args...))
	}
}

// shouldLogFailure 确定性失败日志的节流闸门（每主机每 degradedLogInterval 一条）。
// 只在真正记录时刷新时间戳，否则持续失败会变成「永不记录」的滑动窗口。
func (c *conn) shouldLogFailure() bool {
	c.muLog.Lock()
	defer c.muLog.Unlock()
	if time.Since(c.lastFailLog) < degradedLogInterval {
		return false
	}
	c.lastFailLog = time.Now()
	return true
}

// touch 刷新存活时间戳（数据帧 / ping 到达）
func (c *conn) touch() { c.lastActivity.Store(time.Now().UnixNano()) }

// Stop 关闭连接并终止重连循环（幂等）。
func (c *conn) Stop() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.closeForRetry()
	})
}

// closeForRetry 主动断开当前连接（重连由 run 循环负责）。
func (c *conn) closeForRetry() {
	c.writeMu.Lock()
	ws := c.ws
	c.writeMu.Unlock()
	if ws != nil {
		ws.Close(websocket.StatusNormalClosure, "retry")
	}
}

// run supervisor 循环：拨号 → 读循环 → 断开 → 退避重试。
func (c *conn) run() {
	backoff := c.backoffInitial
	for {
		select {
		case <-c.done:
			return
		default:
		}

		healthy, retryImmediate := c.connectOnce()

		if c.fatal.Load() {
			log.Printf("[v4ws] %s 协议不兼容（hello.proto != %d），停止重连", c.host, ProtoVersion)
			c.reportState(StateOffline)
			return
		}
		// 收到过 snapshot → 事件流健康，退避重置（协议文档 §10）
		if healthy || retryImmediate {
			backoff = c.backoffInitial
		}

		// 0~20% 抖动（retryImmediate 用初始退避的抖动，避免溢出时高频重连风暴）
		base := backoff
		delay := base + time.Duration(rand.Int64N(int64(base)/5+1))
		if delay > c.backoffMax {
			delay = c.backoffMax
		}
		if !retryImmediate {
			backoff *= 2
			if backoff > c.backoffMax {
				backoff = c.backoffMax
			}
		}

		select {
		case <-c.done:
			return
		case <-time.After(delay):
		}
	}
}

// dialURL 拨号地址：ws://host/ws/events（host 已含端口 8088）
func (c *conn) dialURL() string {
	return "ws://" + c.host + "/ws/events"
}

// dialOptions 拨号参数：Basic Auth（/health 之外的端点都要求）+ ping 回调（存活证据）。
func (c *conn) dialOptions() *websocket.DialOptions {
	h := http.Header{}
	if c.user != "" {
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.password)))
	}
	return &websocket.DialOptions{
		HTTPHeader: h,
		// 关键：tobox 空闲无数据帧，ping 是唯一存活证据——收到 ping 即刷新看门狗
		OnPingReceived: func(context.Context, []byte) bool {
			c.touch()
			return true // 返回 true 让库自动回 pong（协议文档 §8：客户端必须回 pong）
		},
	}
}

// connectOnce 一轮连接生命周期。
// 返回 healthy（本轮是否收到过 snapshot，用于退避重置）与 retryImmediate（是否立即重连）。
func (c *conn) connectOnce() (healthy, retryImmediate bool) {
	c.snapshotSeen.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), c.dialTimeout)
	ws, resp, dialErr := websocket.Dial(ctx, c.dialURL(), c.dialOptions())
	cancel()
	if dialErr != nil {
		c.handleDialFailure(dialErr, resp)
		return false, false
	}

	c.writeMu.Lock()
	c.ws = ws
	c.writeMu.Unlock()
	c.touch()
	defer func() {
		c.writeMu.Lock()
		c.ws = nil
		c.writeMu.Unlock()
		ws.Close(websocket.StatusNormalClosure, "bye")
	}()

	ws.SetReadLimit(maxReadLimit)

	frameCh := make(chan []byte, frameChBuf) // 每轮全新：旧帧随旧 channel 废弃
	consumerDone := make(chan struct{})
	go c.consume(frameCh, consumerDone)

	// 看门狗：数据帧与 ping 都刷新 lastActivity，超时则主动断开读循环
	watchStop := make(chan struct{})
	watchDone := make(chan struct{})
	go c.watchdog(ws, watchStop, watchDone)

	readErr := c.readLoop(ws, frameCh)
	close(watchStop)
	<-watchDone

	close(frameCh) // → consumer 退出
	<-consumerDone

	c.handleReadEnd(readErr)
	return c.snapshotSeen.Load(), c.retryImmediate.Swap(false)
}

// readLoop 读循环：只收帧入队，零业务逻辑。任意数据帧刷新存活时间戳。
func (c *conn) readLoop(ws *websocket.Conn, frameCh chan<- []byte) error {
	for {
		_, r, err := ws.Reader(context.Background())
		if err != nil {
			return err
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		c.touch()
		select {
		case frameCh <- data:
		case <-c.done:
			return nil
		}
	}
}

// watchdog 空闲检测：lastActivity 超时则关闭连接（读循环随之返回错误）。
// 检测间隔取超时的 1/4（下限 50ms，便于测试压缩）。
func (c *conn) watchdog(ws *websocket.Conn, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	interval := c.watchdogTimeout / 4
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-c.done:
			return
		case <-ticker.C:
			last := time.Unix(0, c.lastActivity.Load())
			if time.Since(last) < c.watchdogTimeout {
				continue
			}
			log.Printf("[v4ws] %s %s 无任何帧与 ping——判通道死亡，断开重连",
				c.host, c.watchdogTimeout)
			c.log("⏰ %s 无帧无 ping——判通道死亡，断开重连", c.watchdogTimeout)
			c.reportState(StateOffline)
			ws.Close(websocket.StatusGoingAway, "watchdog")
			return
		}
	}
}

// consume 消费循环：解析帧 → seq 规则 → 分发 handler。
func (c *conn) consume(frameCh <-chan []byte, done chan<- struct{}) {
	defer close(done)
	for data := range frameCh {
		f, err := parseFrame(data)
		if err != nil || f == nil {
			continue // 解析失败/未知帧：忽略（前向兼容），连接不动
		}
		switch f.Kind {
		case frameHello:
			c.onHello(f.Hello)
		case frameSnapshot:
			c.seq.onSnapshot(f.Snapshot.Seq)
			c.snapshotSeen.Store(true)
			// 在线上报的唯一判据是「收到快照」（用户口径）：hello 只说明协议握手通过，
			// 快照到达才证明事件流真正可用。重连成功同样经此翻回在线。
			c.reportState(StateOnline)
			c.log("📦 快照: %d 台实例 seq=%d", len(f.Snapshot.Data.Instances), f.Snapshot.Seq)
			if c.handlers != nil {
				c.handlers.OnSnapshot(c.host, f.Snapshot)
			}
		case frameEvent:
			c.onEvent(f)
		case frameResyncRequired:
			// 服务端队列溢出，随后会以 1013 关闭连接——不在本连接上发 resync（马上要关），
			// 直接断开走重连（协议文档 §10 第 3 条）
			log.Printf("[v4ws] %s 收到 resync-required（事件队列溢出），直接重连重建快照", c.host)
			c.log("🔄 服务端事件溢出（resync-required），直接重连")
			c.retryImmediate.Store(true)
			c.closeForRetry()
			return
		}
	}
}

// onHello 握手帧：校验协议版本，不兼容则置 fatal（停止重连）。
func (c *conn) onHello(h *HelloFrame) {
	c.seq.onHello()
	if h == nil {
		return
	}
	if h.Proto != ProtoVersion {
		c.fatal.Store(true)
		log.Printf("[v4ws] %s hello.proto=%d 与本客户端支持的 %d 不兼容，停止重连（需升级客户端）",
			c.host, h.Proto, ProtoVersion)
		c.log("🚫 协议版本不兼容（proto=%d），停止重连", h.Proto)
		c.closeForRetry()
		return
	}
	c.log("✅ 建连 hello: proto=%d sdk=%s", h.Proto, h.SDKVersion)
	// 注意：这里**不**上报在线——在线的判据是「收到快照」（见 consume 的 frameSnapshot），
	// hello 只代表协议握手通过；快照没到（服务端后端全挂会 1011）仍应算离线。
	if c.handlers != nil {
		c.handlers.OnHello(c.host, h)
	}
}

// onEvent 增量事件：seq 检查后按 event/action 分发。
func (c *conn) onEvent(f *Frame) {
	if f.Event == nil {
		return
	}
	apply, gap := c.seq.onEvent(f.Event.Seq)
	if gap {
		// 断档：中间有事件丢失 → 补拉快照（限频 10s，快照幂等）
		log.Printf("[v4ws] %s seq 断档（实收 %d），请求 resync", c.host, f.Event.Seq)
		c.SendResync()
	}
	if !apply {
		return
	}
	c.logEvent(f.Event)
	if c.handlers == nil {
		return
	}
	switch f.Event.Event {
	case EventInstance:
		if f.Event.Action != ActionState {
			return // instance 只有 state 动作，其余前向兼容忽略
		}
		item, err := ParseInstanceStateData(f.Event.Data)
		if err != nil {
			return
		}
		c.handlers.OnInstanceState(c.host, f.Event, item)
	case EventClipboard:
		data, err := ParseClipboardData(f.Event.Data)
		if err != nil {
			return
		}
		c.handlers.OnClipboard(c.host, f.Event, data)
	case EventSystem:
		if f.Event.Action == ActionResync {
			// 后端集合变化（/backend/add、/backend/:name delete）：实例集合与 backend
			// 字段都可能整体变化，需重拉快照
			c.SendResync()
			c.handlers.OnSystemResync(c.host, f.Event)
		}
	}
}
