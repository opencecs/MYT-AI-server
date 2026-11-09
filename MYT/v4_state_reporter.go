// v4_state_reporter.go — V4 主机在线状态上报（v4ws 连接状态 → config.State）。
//
// 为什么需要（2026-10 用户实测问题「设备显示离线，但投屏正常」）：
//   - V4 之前没有任何东西把 v4ws 的连接状态写回 config.State：v4ws 的
//     SetStateChangeHandler 只打日志（注释写「在线性由发现扫描 /health 负责」），
//     而发现扫描只在模式切换时跑一次。
//   - 反过来 V3 的 eventws 会对 V4 设备（IP）拨 ws://ip:8000/ws/events——V4 设备
//     只监听 8088，拨号必然失败 → device_state_reporter 把 State 写成 1（离线）。
//     于是「首次添加时 /health 通过写在线 → 随后被 V3 拨号失败写离线 → 再没有
//     机制翻回在线」，设备永远显示离线，而投屏走 8088 REST 一切正常。
//
// 修复后的在线性定义（用户口径）：**v4ws 连上并收到 snapshot 即在线**；
// 断线/重连中/失败即离线。可辅以 /health 探测兜底——WS 通道单独故障（503 连接数
// 已满、服务端重启中的短窗口）时 /health 仍可达，此时设备实际在线（能投屏），
// 不应显示离线。
package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/util"
	"github.com/moyunteng/myt/internal/v4host"
	"github.com/moyunteng/myt/internal/v4ws"
)

const (
	// v4StateNotifyDebounce devicelist:updated 合并窗口：批量上线/下线只推一条
	v4StateNotifyDebounce = 100 * time.Millisecond
	// v4ProbeMemoryInterval /health 兜底探测记忆：WS 退避重连期间每轮都报离线，
	// 不记忆会变成「每秒一次 /health 探测」的轰炸（与 V3 的 probeMemoryInterval 同款）
	v4ProbeMemoryInterval = 60 * time.Second
	// v4ProbeTimeout 兜底探测超时（/health 是毫秒级响应，局域网 4s 足够）
	v4ProbeTimeout = 4 * time.Second
)

// v4StateReporter 把 v4ws 的连接状态落到 config 设备列表（V4 的 DeviceID 恒等于 IP）。
type v4StateReporter struct {
	cm   *config.Manager
	emit func(name string, data any)

	probeMu  sync.Mutex
	probeMem map[string]time.Time // 主机 IP → 上次 /health 兜底探测成功时间

	emitMu  sync.Mutex
	pending bool
}

func newV4StateReporter(cm *config.Manager, emit func(name string, data any)) *v4StateReporter {
	return &v4StateReporter{
		cm:       cm,
		emit:     emit,
		probeMem: make(map[string]time.Time),
	}
}

// Handle v4ws 状态变化回调（v4ws.Service.SetStateChangeHandler 注入）。
//
// state 语义与 v4ws 常量一致：0=在线（已收到 snapshot）1=离线 2=认证失败。
// 离线不是终态：WS 退避重连会持续重试，重连成功收到 snapshot 后自动翻回在线。
func (r *v4StateReporter) Handle(host string, state int) {
	ip := v4HostIP(host)
	if ip == "" || r.cm == nil {
		return
	}
	// 模式守卫：切走 v4 后旧连接的在途上报不能写进别的模式的数据桶
	// （deviceListPtrLocked 按当前 mode 取指针，写进去就是跨模式污染）
	if r.cm.GetDeployMode() != "v4" {
		return
	}
	if r.cm.GetDevice(ip) == nil {
		return // 设备已被移除：迟到的上报忽略
	}

	switch state {
	case v4ws.StateOnline:
		r.probeMu.Lock()
		delete(r.probeMem, ip) // 连上了：清兜底记忆
		r.probeMu.Unlock()
		r.write(ip, 0)
	case v4ws.StateAuthFail:
		r.write(ip, 2)
	default:
		// 离线：先按事实写离线（WS 断开要秒级反映），再异步 /health 兜底复核
		r.write(ip, 1)
		util.GoSafe(func() { r.probeRecover(ip) })
	}
}

// write 状态变化才写库 + 广播（事件铁律：状态没变不推全量列表）。
func (r *v4StateReporter) write(ip string, state int) {
	if r.cm.GetDeployMode() != "v4" {
		return
	}
	dev := r.cm.GetDevice(ip)
	if dev == nil || dev.State == state {
		return
	}
	// 认证失败由 v4ws 的 401 分支负责；其余路径不覆盖认证态（沿用 V3 语义：
	// 认证态只有认证链路能改回）
	if dev.State == 2 && state != 2 {
		return
	}
	r.cm.UpdateDeviceState(ip, state)
	log.Printf("[v4] 主机 %s 在线状态 → %d（0=在线 1=离线 2=认证失败）", ip, state)
	r.notifySoon()
}

// probeRecover 离线上报的 /health 兜底复核（异步）。
//
// 场景：WS 通道单独不可用但设备本身在线（事件连接数上限 32、服务端重启窗口、
// 协议层被中间设备拦截），此时 /health 仍可达 → 设备实际在线（投屏可用），
// 写回在线避免误判。真不可达则维持离线（等 WS 重连自愈）。
//
// 记忆期内的重复离线直接跳过：WS 退避重连每轮都会报一次离线，逐轮探测等于
// 每秒打一次 /health。
func (r *v4StateReporter) probeRecover(ip string) {
	r.probeMu.Lock()
	if t, ok := r.probeMem[ip]; ok && time.Since(t) < v4ProbeMemoryInterval {
		r.probeMu.Unlock()
		return
	}
	r.probeMem[ip] = time.Now() // 先占位：探测进行中的并发离线上报不重复探测
	r.probeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), v4ProbeTimeout)
	defer cancel()
	if _, err := v4host.Health(ctx, ip); err != nil {
		return // 真不可达：维持离线，等 WS 重连
	}
	// 探测期间用户可能已切走模式 / 移除设备：写回前复查
	if r.cm.GetDeployMode() != "v4" {
		return
	}
	if dev := r.cm.GetDevice(ip); dev == nil {
		return
	}
	log.Printf("[v4] 主机 %s WS 未连上但 /health 可达 → 判在线（投屏可用）", ip)
	r.write(ip, 0)
}

// notifySoon 合并窗口内只推一条 devicelist:updated（批量上线/下线场景）。
func (r *v4StateReporter) notifySoon() {
	r.emitMu.Lock()
	if r.pending {
		r.emitMu.Unlock()
		return
	}
	r.pending = true
	r.emitMu.Unlock()

	time.AfterFunc(v4StateNotifyDebounce, func() {
		r.emitMu.Lock()
		r.pending = false
		r.emitMu.Unlock()
		if r.emit == nil || r.cm == nil {
			return
		}
		r.emit("devicelist:updated", r.cm.GetDeviceList())
	})
}
