// v4ws 连接的数据结构与常量：连接对象、事件回调接口、超时/退避参数。
//
// 并发模型（与协议文档 §8「客户端必须持续读」一致）：
//
//	supervisor goroutine：run 循环（拨号 → 读循环 → 断开 → 退避）
//	读循环（connectOnce 内）：ws.Reader → io.ReadAll → frameCh，只收帧入队，零业务逻辑
//	消费 goroutine          ：frameCh 取帧 → seq 检查 → handler 回调
//	看门狗 goroutine        ：空闲检测（见下）
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
// 二者都写同一个原子时间戳，看门狗按该时间戳判空闲。超时取 120s（≈24 个 ping 周期）：
// 协议文档建议应用层空闲检测 15~30s 只是「容忍 3~6 个 ping 丢失」，但客户端读循环若被
// 业务阻塞会由服务端 60s pongWait 主动断开（1006），客户端看门狗只需兜住「服务端静默
// 消失但 TCP 未断」这种极少数情况——取宽值避免任何误杀。
package v4ws

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	// 看门狗空闲超时（兜底）：ping 5s 一次，120s ≈ 24 个周期，见包注释
	watchdogTimeout = 120 * time.Second
	// 退避 1s → 2s → 4s → … → 30s 封顶（协议文档 §10），加 0~20% 抖动防雪崩
	backoffInitial = 1 * time.Second
	backoffMax     = 30 * time.Second
	// resync 限频 10s（服务端按连接限频，间隔不足的请求被静默丢弃，客户端自律避免白发）
	resyncMinInterval = 10 * time.Second
	// 帧队列缓冲：读循环与消费解耦的蓄水池
	frameChBuf = 256
	// 拨号超时
	dialTimeout = 8 * time.Second
	// 确定性失败（401/1008）的日志节流：千台规模下每轮一条会刷屏
	degradedLogInterval = 10 * time.Minute
	// 单帧读取上限：快照随实例数线性增长（实例多时几十 KB），默认 32KB 会 1009 断连
	maxReadLimit = 64 << 20
)

// 连接状态（与 config 设备状态语义一致：0=在线 1=离线 2=认证失败）
const (
	StateOnline   = 0
	StateOffline  = 1
	StateAuthFail = 2
)

// Handlers 消费侧回调（manager 注入，适配层实现）。全部在消费 goroutine 串行调用。
type Handlers interface {
	// OnHello 建连握手帧（协议版本已校验通过才会回调）
	OnHello(host string, h *HelloFrame)
	// OnSnapshot 全量快照（建连首帧 / resync 响应）：整体覆盖该主机的实例视图
	OnSnapshot(host string, snap *SnapshotFrame)
	// OnInstanceState instance/state 事件（含 state="deleted" 的删除事件）
	OnInstanceState(host string, ev *EventFrame, item InstanceItem)
	// OnClipboard clipboard/push 事件（当前仅记日志，见适配层）
	OnClipboard(host string, ev *EventFrame, data ClipboardEventData)
	// OnSystemResync system/resync 事件（后端增删，客户端已自动请求 resync）
	OnSystemResync(host string, ev *EventFrame)
}

// conn 单主机连接。生命周期由 manager 管理，Stop 后不再重连。
//
// host 为拨号地址（ip:8088）同时也是回调/日志用的主机标识——V4 设备以 IP 为 key
// （/health 无设备标识字段）。
type conn struct {
	host     string
	user     string
	password string
	handlers Handlers
	onState  func(state int)
	logHook  func(line string)

	seq       seqTracker
	done      chan struct{}
	closeOnce sync.Once
	// fatal 协议不兼容等不可恢复错误：置位后 run 循环退出，不再重连
	fatal atomic.Bool
	// snapshotSeen 本轮连接是否收到过 snapshot（退避重置判据），每轮 connectOnce 重置
	snapshotSeen atomic.Bool
	// retryImmediate 本轮断开后是否应立即重连（1013 / resync-required）
	retryImmediate atomic.Bool
	// lastActivity 存活证据时间戳（UnixNano）：数据帧与协议层 ping 都刷新它
	lastActivity atomic.Int64

	resyncMu     sync.Mutex
	lastResyncAt time.Time

	muLog       sync.Mutex
	lastFailLog time.Time

	writeMu sync.Mutex
	ws      *websocket.Conn

	// 超时参数（测试可压缩，生产用默认常量）
	dialTimeout       time.Duration
	watchdogTimeout   time.Duration
	backoffInitial    time.Duration
	backoffMax        time.Duration
	resyncMinInterval time.Duration
}
