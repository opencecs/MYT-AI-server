package main

import (
	"sync"
	"sync/atomic"
	"time"
)

// frameGate 丢包感知帧门控状态机。
//
// 背景（pc-client/logs_stress 根因分析报告）：RTSP 链路丢包 3~4% 时，TCP 队头
// 阻塞 → naluCh(容量 8) 满 → P/B 帧被丢 → 参考链断裂 → 解码出「解码成功但内容
// 损坏」的花屏帧。该类帧有像素能渲染，现有 UV=0 探针抓不到，照常上屏，直到
// 下一个 IDR（4~11s）才恢复。
//
// 策略：检测到丢包（gortsplib OnPacketsLost 或 RTP 解码错误）即置「参考链污染」
// 标志并冻结渲染——后续 P/B 帧不投递（画面保留最后一帧，空帧丢弃路径本就保上
// 一帧）；下一个 IDR 到来时清标志、清空积压 P/B 后正常投递（复用 startDecode-
// Pipeline 现有 IDR 清理路径）。宁可短暂卡住不要花屏。
//
// 兜底：IDR 迟迟不来时不能永久黑屏。注意不能复用 stall 看门狗——lastFrame 在
// RTSP 读循环里照常刷新，stall 判定不了「有流但被门控冻结」；也不能用 5s 阈值
// （见 onStallTick 注释：与设备 heartbeat#5 周期踩线会引发重连风暴）。因此门控
// 自带看门狗：冻结超过 gateIDRTimeout 仍无 IDR → 自动放行渲染（恢复有帧即渲
// 染的旧行为，最坏情况是画面花屏但不黑屏）。
//
// 线程安全：标记位全部 atomic；OnPacketsLost 回调在 gortsplib 读循环 goroutine
// 触发，Poll/Guard/Release 在帧管线/看门狗 goroutine 触发。
type frameGate struct {
	mu           sync.Mutex // 保护 armed 看门狗定时器与 armedAt
	armed        *time.Timer
	armedAt      time.Time
	contaminated atomic.Bool // 参考链污染：P/B 帧不投递，等 IDR 重同步
	firstIDRSeen atomic.Bool // 会话内是否已见过 IDR（首个 IDR 前不门控，不挡冷启动首帧）
	droppedCount atomic.Int32
	gateEnabled  atomic.Bool // 门控总开关（连接建立时开启，断连关闭）
}

// gateIDRTimeout 冻结等待 IDR 的最长时限。GOP 实测 4~11s（压测报告），取 8s
// 居中偏紧：若 IDR 迟到则放行渲染恢复旧行为（花屏但不永久黑屏）。该阈值只管
// 门控自身，与 stall 看门狗（15s，靠 lastFrame）独立，互不干扰。
const gateIDRTimeout = 8 * time.Second

var gate frameGate

// gateOn 开启门控（连接成功时调用）。每个新会话参考链从 IDR 起步，清历史状态。
func gateOn() {
	gate.mu.Lock()
	if gate.armed != nil {
		gate.armed.Stop()
		gate.armed = nil
	}
	gate.mu.Unlock()
	gate.contaminated.Store(false)
	gate.droppedCount.Store(0)
	gate.firstIDRSeen.Store(false)
	gate.gateEnabled.Store(true)
}

// gateOff 关闭门控（断连时调用）：停看门狗、复位全部标志。
func gateOff() {
	gate.mu.Lock()
	if gate.armed != nil {
		gate.armed.Stop()
		gate.armed = nil
	}
	gate.mu.Unlock()
	gate.contaminated.Store(false)
	gate.firstIDRSeen.Store(false)
	gate.gateEnabled.Store(false)
}

// gateOnPacketsLost gortsplib 丢包回调：丢失 RTP 包说明参考链可能已断裂（丢的
// 可能只是当前帧的后续分片，也可能是 P 帧参考数据——事后无法区分），统一置
// 污染标志并冻结。lost 为本次丢失的包数。
func gateOnPacketsLost(lost uint64) {
	gateMarkContaminated("rtp-lost", int(lost))
}

// gateOnDecodeError RTP 解码错误计数（rtsp_setup.go 的 rtpDec.Decode 静默 return
// 路径）：分片重组失败同样意味着参考链数据缺口，与丢包等价处理。
func gateOnDecodeError() {
	gateMarkContaminated("rtp-decode-error", 1)
}

// gateMarkContaminated 置污染标志并武装 IDR 看门狗。已处于污染态时不重复打日志
// （丢包常连片出现），仅续期看门狗。
func gateMarkContaminated(reason string, lost int) {
	if !gate.gateEnabled.Load() {
		return
	}
	// 首个 IDR 之前（会话冷启动阶段）参考链尚未建立，解码器靠 IDR 自建上下文，
	// 期间丢包不产生花屏（没有参考帧可污染），不门控，不挡首帧。
	if !gate.firstIDRSeen.Load() {
		return
	}
	if gate.contaminated.CompareAndSwap(false, true) {
		logf("[gate] 参考链污染（%s 丢 %d 包），冻结渲染等 IDR 重同步\n", reason, lost)
	}
	gate.mu.Lock()
	if gate.armed != nil {
		gate.armed.Reset(gateIDRTimeout)
	} else {
		gate.armed = time.AfterFunc(gateIDRTimeout, gateGuardTick)
		gate.armedAt = time.Now()
	}
	gate.mu.Unlock()
}

// gateGuardTick 门控看门狗：冻结超过 gateIDRTimeout 仍无 IDR（IDR 迟到或设备
// 端长 GOP），放行渲染恢复「有帧即渲染」的旧行为——最坏退化为旧行为（花屏），
// 不永久黑屏。阈值刻意不踩 stall 体系的 5s 心跳周期（见 onStallTick 注释）。
func gateGuardTick() {
	gate.mu.Lock()
	gate.armed = nil
	started := gate.armedAt
	gate.mu.Unlock()
	if !gate.contaminated.Load() {
		return
	}
	logf("[gate] 冻结 %.1fs 仍无 IDR，放行渲染（回退旧行为，等下个 IDR）\n",
		time.Since(started).Seconds())
	gate.contaminated.Store(false)
}

// gatePoll 帧投递前查询：返回 true 表示该帧应当被门控拦截（不投递渲染）。
// 拦截范围只有非 IDR 帧；IDR 一律放行并在 gateOnIDR 里做重同步。
func gatePoll(isKey bool) bool {
	if !gate.gateEnabled.Load() {
		return false
	}
	return gate.contaminated.Load() && !isKey
}

// gateOnIDR IDR 到达：清污染标志、停看门狗，恢复投递。返回 true 表示刚经历了
// 一次门控恢复（供调用方打日志）。
func gateOnIDR() bool {
	gate.firstIDRSeen.Store(true)
	wasContaminated := gate.contaminated.Swap(false)
	if wasContaminated {
		gate.mu.Lock()
		if gate.armed != nil {
			gate.armed.Stop()
			gate.armed = nil
		}
		gate.mu.Unlock()
		logf("[gate] IDR 到达，清参考链污染，恢复渲染（期间丢弃 %d 个 P/B 帧）\n",
			gate.droppedCount.Swap(0))
	}
	return wasContaminated
}
