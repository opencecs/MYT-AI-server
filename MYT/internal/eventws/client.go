// eventws 连接层：单设备 WS 连接的拨号、读循环、消费循环、看门狗、退避重连、404 日志节流。
//
// 并发模型（协议文档 §7 硬性要求——读循环绝不能被业务处理阻塞，否则 60s 心跳超时被踢）：
//
//	supervisor goroutine：run 循环（拨号→读循环→断开→退避）
//	读循环（connectOnce 内） ：conn.Reader → io.ReadAll → frameCh，只收帧入队，零业务逻辑
//	消费 goroutine          ：frameCh 取帧 → seq 检查 → handler 回调（快照合并/状态补丁/emit）
//
// frameCh 为每轮连接的局部变量：断开即关闭并废弃，重连用全新 channel——
// 协议文档 §8「重连后不信任旧连接的任何帧」由此从结构上保证（旧帧留在旧 channel 里被 GC）。
//
// 协议层 ping 由 coder/websocket 在 Reader 中自动回 pong——读循环活着心跳就活着。
package eventws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// 协议文档 §7：90s 未收到任何帧（含 ping）→ 判通道死亡 → 断开重连
	watchdogTimeout = 90 * time.Second
	// 退避 1s → 2s → 4s → … → 10s 封顶。
	// 注：协议文档 §8 示例为 30s——实测 30s 稳态下「设备悄悄恢复」最坏等 38s 才重新上线，
	// 调优为 10s（最坏 18s，平均 ~13s）。退避是客户端自律行为（防高频重连轰炸），
	// 真离线设备收不到拨号、无服务端压力；若需对齐协议文档可回退。
	backoffInitial = 1 * time.Second
	backoffMax     = 10 * time.Second
	// 协议文档 §5：resync 限频 10s（服务端静默丢弃间隔不足的请求，客户端同样自律）
	resyncMinInterval = 10 * time.Second
	// 帧队列缓冲：读循环与消费解耦的蓄水池（一次开机序列 4-6 帧，256 足够吸收消费抖动）
	frameChBuf = 256
	// 拨号超时
	dialTimeout = 8 * time.Second
	// 404 日志节流：老 SDK 按退避节奏重连（2026-09-08 用户决策，与普通断线
	// 同节奏），404 每设备每 10 分钟一条文件日志（否则 300 台老设备刷屏）。
	degradedLogInterval = 10 * time.Minute
)

// Handlers 消费侧回调（manager 注入，androidlist 集成层实现）。全部在消费 goroutine 串行调用。
type Handlers interface {
	OnHello(deviceID, host string, h *HelloFrame)
	OnSnapshot(deviceID, host string, snap *SnapshotFrame)
	OnContainerEvent(deviceID, host string, ev *EventFrame, data ContainerEventData)
	OnBootEvent(deviceID string, ev *EventFrame, data BootEventData, uiStatus string)
	OnStatsEvent(deviceID string, ev *EventFrame, stats []ContainerStatsItem)
	// OnSystemStats system/stats 事件（设备级指标）。raw 与 /info/device 动态字段完全同构
	// （JSON 键一致），消费端直接反序列化为现有结构即可。
	OnSystemStats(deviceID string, ev *EventFrame, raw json.RawMessage)
	// OnTagsChangedEvent system/tagsChanged 事件：标签写操作成功后的标签字典推送
	//（data = 变更后的本机标签名扁平全集，跨容器去重）。
	OnTagsChangedEvent(deviceID string, ev *EventFrame, tags []string)
}

// ErrDegraded 曾表示 404 降级（老 SDK 停止重连 10 分钟）——2026-09-08 用户决策
// 老 SDK 与普通断线同节奏退避重连后移除。保留此注释记录语义变更。

// conn 单设备连接。生命周期由 manager 管理，Stop 后不再重连。
// host 为回调/日志用的设备地址标识：local 形态是 ip:8000（OnSnapshot 等回调
// 经 hostWithoutPort 提取纯 IP 落缓存）；public 形态是 device_id（公网容器缓存
// 的 IP 字段约定存 device_id——截图 key 与网关透传标识都依赖它）。
// onState 由 manager 注入：连接状态变化时上报（0 在线 / 1 离线 / 2 认证失败）。
// logHook 由 manager 注入：每帧/连接事件输出一行日志（前端 console 调试用）。
type conn struct {
	deviceID    string
	host        string
	getPassword func(ip string) string
	handlers    Handlers
	onState     func(state int)
	logHook     func(line string)

	// public（公网网关透传）形态——数据源差异的全部落点：
	// gatewayPath 非空 = public；getToken 每次拨号现取 Bearer JWT
	// （15min 过期，重连自动换新）。local 形态两者为零值，逻辑不变。
	gatewayPath string
	getToken    func() (string, error)

	seq       seqTracker
	done      chan struct{}
	closeOnce sync.Once

	resyncMu     sync.Mutex
	lastResyncAt time.Time

	mu404      sync.Mutex // 拨号失败类日志节流（404/401/403/网络断，见 degradedLogInterval）
	last404Log time.Time

	writeMu sync.Mutex // websocket 单写者保护（仅 resync 写路径）
	ws      *websocket.Conn
}

func newConn(deviceID, host string, getPassword func(string) string, h Handlers) *conn {
	return &conn{
		deviceID:    deviceID,
		host:        host,
		getPassword: getPassword,
		handlers:    h,
		done:        make(chan struct{}),
	}
}

// newPublicConn 公网形态构造：gatewayHost 为网关地址（host:port），拨号路径
// /v1/devices/{deviceID}/passthrough/ws/events（网关透传到真实设备，实测 101）。
// host 字段存 device_id 保证回调语义（见 conn.host 注释）。
func newPublicConn(deviceID, gatewayHost string, getToken func() (string, error), h Handlers) *conn {
	c := newConn(deviceID, deviceID, nil, h)
	c.gatewayPath = gatewayHost + "/v1/devices/" + deviceID + "/passthrough"
	c.getToken = getToken
	return c
}

func (c *conn) reportState(state int) {
	select {
	case <-c.done:
		return // 已停止（漂移重建/移除）：迟到的失败上报不污染状态，新连接负责
	default:
	}
	if c.onState != nil {
		c.onState(state)
	}
}

// log 输出一行事件日志（设备前缀已加）；无 hook 时静默。
func (c *conn) log(format string, args ...any) {
	if c.logHook != nil {
		c.logHook(fmt.Sprintf(format, args...))
	}
}

// shouldLogDialFailure 拨号失败类日志（404/401/403/网络断）的节流闸门：
// 每设备每 degradedLogInterval 至多一条。仅在实际记录时刷新时间戳——
// 每轮失败都刷新会把间隔变成滑动窗口（持续失败时首条之后永不再记录）。
// 401/403 是确定性失败（密码变更/缓存串号），重连退避下每设备每 10s 一条，
// 千台规模不节流会刷屏（2026-09-18 实测 4.2 万行/105 分钟）。
func (c *conn) shouldLogDialFailure() bool {
	c.mu404.Lock()
	defer c.mu404.Unlock()
	if time.Since(c.last404Log) < degradedLogInterval {
		return false
	}
	c.last404Log = time.Now()
	return true
}

// Stop 关闭连接并终止重连循环（幂等）。
func (c *conn) Stop() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.writeMu.Lock()
		ws := c.ws
		c.writeMu.Unlock()
		if ws != nil {
			ws.Close(websocket.StatusNormalClosure, "client stop")
		}
	})
}

// run supervisor 循环：拨号 → 读循环 → 断开 → 退避重试（404 老 SDK 同此节奏）。
func (c *conn) run() error {
	backoff := backoffInitial
	for {
		select {
		case <-c.done:
			return nil
		default:
		}

		streamOK, _ := c.connectOnce() // 错误一律退避重连（404/网络断/读错误同此节奏）

		// snapshot 帧收到过 → 事件流健康，重置退避；否则累积（防「连上即断」高频轰炸）
		if streamOK {
			backoff = backoffInitial
		} else {
			backoff *= 2
			if backoff > backoffMax {
				backoff = backoffMax
			}
		}

		select {
		case <-c.done:
			return nil
		case <-time.After(backoff):
		}
	}
}

// dialURL 拨号地址——数据源差异的唯一落点：public 走网关透传路径，local 直连设备。
func (c *conn) dialURL() string {
	if c.gatewayPath != "" {
		return "ws://" + c.gatewayPath + "/ws/events"
	}
	return "ws://" + c.host + "/ws/events"
}

// connectOnce 一轮连接生命周期。返回 streamOK：snapshot 是否成功收到（事件流健康，用于退避重置）。
func (c *conn) connectOnce() (streamOK bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	ws, resp, dialErr := websocket.Dial(ctx, c.dialURL(), c.dialOptions())
	cancel()
	if dialErr != nil {
		// 协议文档 §2：404=老 SDK；401/403/503 走普通退避重试。
		// 老 SDK（WS 端点不存在）同样走退避重连（2026-09-08 用户决策：与普通
		// 断线同节奏，设备升级出 WS 端点后秒级恢复事件推送）——404/401/403 都是
		// 确定性失败，秒级重试的握手开销与日志必须节流（shouldLogDialFailure，
		// 每 10 分钟一条）。
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			if c.shouldLogDialFailure() {
				log.Printf("[eventws] 设备 %s(%s) WS 404——老 SDK（无 /ws/events 端点），按退避节奏重连探测升级", c.deviceID, c.host)
				// 前端 console 日志随文件日志同频节流（否则每 1~10s 一条刷屏）
				c.log("🔄 WS 404（老 SDK），退避重连探测升级")
			}
			c.reportState(stateOffline) // 状态上报经 manager 去重 + setup 侧 HTTP 复核
			return false, nil
		}
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			if c.shouldLogDialFailure() {
				log.Printf("[eventws] 设备 %s(%s) WS 401——认证失败（设备密码变更？）", c.deviceID, c.host)
				c.log("🔑 401 认证失败（设备密码变更？），退避重试")
			}
			c.reportState(stateAuthFail) // 对齐 httpbeat 语义：认证失败 State=2
			return false, nil
		}
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			// deviceId 头与设备实际 ID 不匹配（客户端缓存串了设备/IP 复用）——
			// 按 403 响应体提示清理缓存。文档 §2：code 62。
			if c.shouldLogDialFailure() {
				log.Printf("[eventws] 设备 %s(%s) WS 403——deviceId 不匹配（客户端缓存过期？IP 复用？）", c.deviceID, c.host)
				c.log("🚫 403 deviceId 不匹配（本机记录=%s，设备实际 ID 不同），退避重试", c.deviceID)
			}
			c.reportState(stateOffline)
			return false, nil
		}
		// 拨号失败（超时/网络断/URL 非法等）：文件日志必须可见——否则设备被
		// 判离线时无从排查（曾因网关地址带 scheme 拼出非法 URL 静默失败全离线）。
		// 与 404 同频节流（每设备每 10 分钟一条）：千台规模的离线设备退避循环
		// 每轮一条会刷屏（40 台离线 ≈ 4 行/s 持续）。故障恢复后自然消失。
		if c.shouldLogDialFailure() {
			log.Printf("[eventws] 设备 %s 拨号 %s 失败: %v", c.deviceID, c.dialURL(), dialErr)
		}
		c.reportState(stateOffline) // 拨号失败：设备不可达
		return false, nil
	}

	c.writeMu.Lock()
	c.ws = ws
	c.writeMu.Unlock()
	defer func() {
		c.writeMu.Lock()
		c.ws = nil
		c.writeMu.Unlock()
		ws.Close(websocket.StatusNormalClosure, "bye")
	}()

	// 快照帧随容器数线性增长（37 容器实测 ~40KB，默认 32KB 限制直接 1009 断连）。
	// 放宽到 64MB——单帧 JSON 上限远低于此，socket 缓冲不受此值影响。
	ws.SetReadLimit(64 << 20)

	frameCh := make(chan []byte, frameChBuf) // 每轮全新：旧帧随旧 channel 废弃

	consumerDone := make(chan struct{})
	go c.consume(frameCh, consumerDone)

	// 看门狗：任意帧到达重置；超时主动断开读循环（Reader 返回错误）
	watchdog := time.AfterFunc(watchdogTimeout, func() {
		log.Printf("[eventws] 设备 %s(%s) 90s 无帧——判通道死亡，断开重连", c.deviceID, c.host)
		c.log("⏰ 90s 无帧——判通道死亡，断开重连")
		c.reportState(stateOffline) // 心跳停止：判离线
		ws.Close(websocket.StatusGoingAway, "watchdog")
	})

	readErr := c.readLoop(ws, watchdog, frameCh, &streamOK)
	watchdog.Stop()
	if streamOK && readErr != nil {
		// 之前事件流健康、现在读循环报错（TCP 断/服务端关闭）→ 判离线（重连成功后恢复在线）
		c.reportState(stateOffline)
		c.log("❌ 连接断开（%v），退避后重连", readErr)
	} else if readErr != nil {
		c.log("❌ 连接失败（%v），退避后重试", readErr)
	}
	close(frameCh) // → consumer 退出
	<-consumerDone
	return streamOK, readErr
}

// readLoop 读循环：只收帧入队，零业务逻辑（§7）。每帧重置看门狗。
func (c *conn) readLoop(ws *websocket.Conn, watchdog *time.Timer, frameCh chan<- []byte, streamOK *bool) error {
	for {
		_, r, err := ws.Reader(context.Background())
		if err != nil {
			return err
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		watchdog.Reset(watchdogTimeout)
		// 任意数据帧 = 存活证据（WS 心跳即在线性）：上报在线。
		// manager 侧两级去重——config 已在线则零副作用；被外部写歪时此处自愈。
		// 旧实现只在首个 snapshot 上报一次，之后无人纠正。
		c.reportState(stateOnline)
		if !*streamOK {
			// snapshot 到达才算事件流真正健康（hello 只证明握手通）——用于退避重置。
			// 修复「连上即断」场景（如读超限）下 hello 就重置退避导致的 1s 高频重连轰炸。
			if f, perr := parseFrame(data); perr == nil && f != nil && f.Kind == frameSnapshot {
				*streamOK = true
			}
		}
		select {
		case frameCh <- data:
		case <-c.done:
			return nil
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
			c.seq.onHello()
			c.log("✅ 建连 hello: sdk=%s 版本=%s 机型=%s", f.Hello.SDKVersion, f.Hello.DeviceVersion, f.Hello.Model)
			if c.handlers != nil {
				c.handlers.OnHello(c.deviceID, c.host, f.Hello)
			}
		case frameSnapshot:
			c.seq.onSnapshot(f.Snapshot.Seq)
			c.log("📦 快照: %d 台容器 seq=%d", f.Snapshot.Data.Count, f.Snapshot.Seq)
			if c.handlers != nil {
				c.handlers.OnSnapshot(c.deviceID, c.host, f.Snapshot)
			}
		case frameEvent:
			c.logEvent(f)
			c.onEvent(f)
		case frameResyncRequired:
			c.log("🔄 服务端数据可能不全（docker 流重连过），请求 resync")
			// 服务端数据可能不全（docker 流断线重连过）→ 拉新快照
			c.SendResync()
		}
	}
}

// logEvent 事件帧的 console 日志（按 event/action 类型细分描述，stats 带数据内容）。
func (c *conn) logEvent(f *Frame) {
	if c.logHook == nil || f.Event == nil {
		return
	}
	switch f.Event.Event {
	case EventContainer:
		switch f.Event.Action {
		case "stats":
			// 容器指标：台数 + 首台样本（37 台全打太长）
			if stats, err := ParseContainerStatsData(f.Event.Data); err == nil && len(stats) > 0 {
				s0 := stats[0]
				c.log("📊 容器指标: %d 台 | 样本 %s cpu=%.1f%% mem=%dMB disk=%dGB | seq=%d",
					len(stats), shortID(s0.ContainerID), s0.CPUUsage, s0.MemUsage>>20, s0.DiskUsage>>30, f.Event.Seq)
			} else {
				c.log("📊 容器指标 stats: seq=%d", f.Event.Seq)
			}
		default:
			var d ContainerEventData
			if json.Unmarshal(f.Event.Data, &d) == nil {
				c.log("🐳 容器 %s: %s → %s（%s）seq=%d", f.Event.Action, d.Name, d.Status, shortID(d.ContainerID), f.Event.Seq)
			} else {
				c.log("🐳 容器 %s seq=%d", f.Event.Action, f.Event.Seq)
			}
		}
	case EventBoot:
		var d BootEventData
		name := ""
		if json.Unmarshal(f.Event.Data, &d) == nil {
			name = shortID(d.ContainerID)
		}
		c.log("🚀 开机 %s: %s seq=%d", f.Event.Action, name, f.Event.Seq)
	case EventSystem:
		if f.Event.Action == "stats" {
			// 设备指标：按帧形态分支（panel 帧=全字段输出；disk 帧=新 SDK 磁盘三项）
			panel, disk, err := ParseSystemStatsFrame(f.Event.Data)
			switch {
			case err != nil:
				c.log("🖥 设备指标 stats 解析失败: %v | seq=%d", err, f.Event.Seq)
			case disk != nil:
				c.log("💾 设备磁盘: total=%dGB used=%dGB free=%dGB | seq=%d",
					disk.DiskTotal>>30, disk.DiskUsed>>30, disk.DiskFree>>30, f.Event.Seq)
			default:
				// panel 帧：数据心跳本体，全字段输出
				var m map[string]any
				if json.Unmarshal(panel, &m) == nil {
					c.log("🖥 设备指标: cputemp=%v°C cpuload=%v memuse=%vMB mmc=%v/%v mmcread=%v mmcwrite=%v mmctemp=%v sysuptime=%v | seq=%d",
						m["cputemp"], m["cpuload"], m["memuse"], m["mmcuse"], m["mmctotal"],
						m["mmcread"], m["mmcwrite"], m["mmctemp"], m["sysuptime"], f.Event.Seq)
				} else {
					c.log("🖥 设备指标 stats: seq=%d", f.Event.Seq)
				}
			}
		} else if f.Event.Action == "tagsChanged" {
			// 标签字典变化：带全集条数（空全集=标签被清光）
			if tags, err := ParseTagsChangedData(f.Event.Data); err == nil {
				c.log("🏷 标签字典变化: %d 个标签 | seq=%d", len(tags), f.Event.Seq)
			} else {
				c.log("🏷 system/%s: seq=%d", f.Event.Action, f.Event.Seq)
			}
		} else {
			c.log("🖥 system/%s: seq=%d", f.Event.Action, f.Event.Seq)
		}
	default:
		c.log("❓ 未知事件 %s/%s seq=%d", f.Event.Event, f.Event.Action, f.Event.Seq)
	}
}

// shortID 容器 ID 显示缩短（前 8 位）。
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// onEvent 增量事件：seq 检查后按 event 类型分发。
func (c *conn) onEvent(f *Frame) {
	apply, gap := c.seq.onEvent(f.Event.Seq)
	if gap {
		// 断档：应用当前帧 + 补拉快照（统一走 §6 规则，stats 缺口不做豁免——
		// resync 限频 10s、响应 3ms、快照幂等，代价可忽略）
		c.SendResync()
	}
	if !apply {
		return
	}
	if c.handlers == nil {
		return
	}
	switch f.Event.Event {
	case EventContainer:
		if f.Event.Action == "stats" {
			// stats：data 是数组，幂等周期数据，丢帧无须 resync
			if stats, err := ParseContainerStatsData(f.Event.Data); err == nil {
				c.handlers.OnStatsEvent(c.deviceID, f.Event, stats)
			}
			return
		}
		if data, err := ParseContainerEventData(f.Event.Data); err == nil {
			c.handlers.OnContainerEvent(c.deviceID, c.host, f.Event, data)
		}
	case EventBoot:
		uiStatus := BootActionToStatus(f.Event.Action)
		if uiStatus == "" {
			return // 未知 boot action：忽略
		}
		if data, err := ParseBootEventData(f.Event.Data); err == nil {
			c.handlers.OnBootEvent(c.deviceID, f.Event, data, uiStatus)
		}
	case EventSystem:
		switch f.Event.Action {
		case "resync":
			// system/resync 动作语义等同 resync-required（docker 流重连过）
			c.SendResync()
		case "stats":
			// system/stats：设备级指标周期推送（与 /info/device 动态字段同构）
			if c.handlers != nil {
				c.handlers.OnSystemStats(c.deviceID, f.Event, f.Event.Data)
			}
		case "tagsChanged":
			// system/tagsChanged：标签字典变化（data=变更后的本机标签扁平全集）
			if tags, err := ParseTagsChangedData(f.Event.Data); err == nil {
				c.handlers.OnTagsChangedEvent(c.deviceID, f.Event, tags)
			}
		}
	default:
		// 未知事件类型：忽略（前向兼容）
	}
}

// SendResync 发送 resync 动作（限频 10s，间隔不足静默跳过——服务端同样限频，不重试）。
func (c *conn) SendResync() {
	c.resyncMu.Lock()
	if time.Since(c.lastResyncAt) < resyncMinInterval {
		c.resyncMu.Unlock()
		return
	}
	c.lastResyncAt = time.Now()
	c.resyncMu.Unlock()

	c.writeMu.Lock()
	ws := c.ws
	c.writeMu.Unlock()
	if ws == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := ws.Write(ctx, websocket.MessageText, []byte(`{"action":"resync"}`))
	if err != nil {
		log.Printf("[eventws] 设备 %s(%s) resync 发送失败: %v", c.deviceID, c.host, err)
	}
}

// dialOptions 构造拨号头：BasicAuth（设备配了密码时，admin+密码，与 REST 同一套）+ deviceId。
func (c *conn) dialOptions() *websocket.DialOptions {
	h := http.Header{}
	h.Set("deviceId", c.deviceID)
	if c.getToken != nil {
		// public（网关透传）：Bearer JWT，每次拨号现取——token 15min 过期，
		// 重连时自动换新。取失败则带空鉴权头拨号，网关回 401 → State 2 待认证。
		if tok, err := c.getToken(); err == nil && tok != "" {
			h.Set("Authorization", "Bearer "+tok)
		}
		return &websocket.DialOptions{HTTPHeader: h}
	}
	if c.getPassword != nil {
		// 密码 getter 契约是纯设备 IP（ipToDeviceID 按纯 IP 索引）——
		// 传拨号 host（ip:port）会永远查不到密码，配密码设备全部 401。
		if pw := c.getPassword(deviceIPOf(c.host)); pw != "" {
			h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:"+pw)))
		}
	}
	return &websocket.DialOptions{HTTPHeader: h}
}

// deviceIPOf 从拨号 host（ip:port 形态）剥离端口，返回纯设备 IP。
func deviceIPOf(host string) string {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}

// targetIP 连接的拨号目标 IP：local 形态剥端口取纯 IP；public 形态无直连
// IP（host 存 device_id），恒空串——与 Sync 对公网设备传入的空 IP 自然相等。
// Connect 用它比对目标是否漂移（设备重启换 IP / local↔public 形态切换）。
func (c *conn) targetIP() string {
	if c.gatewayPath != "" {
		return ""
	}
	return deviceIPOf(c.host)
}
