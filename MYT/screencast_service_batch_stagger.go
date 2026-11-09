package main

import (
	"log"
	"net"
	"time"
)

// firstFrameStaggerTimeout 批量投屏错峰等待前一台首帧信号的超时兜底。
//
// 根因背景：N 路播放器进程几乎同时向容器 RTSP 端口发起连接时，约 1/3 的 SYN
// 被静默丢弃（backlog 溢出），Windows 初始 RTO 3s 导致对应窗口首帧延迟 3s+。
// 旧实现固定等 800ms 就放行下一台——播放器进程启动 ≠ RTSP 握手完成（快 200ms
// 慢 7s+），握手慢的批次里 N 路仍会重叠冲击 backlog，恶性循环。
//
// 现改为「按握手完成」错峰：go_legacy 首帧解码成功后向父进程 notifyport 发
// UDP "firstframe"（myt-client-player/app.go notifyReady，此时 RTSP
// DESCRIBE/SETUP/PLAY 必然已完成，语义严格于「握手完成」）。收到信号立即启动
// 下一台；超时（15s，覆盖 3s 重传×2 + 解码余量）则放行，避免单台卡死阻塞整批。
const firstFrameStaggerTimeout = 15 * time.Second

// waitFirstFrameOrTimeout 错峰等待：阻塞直到收到前一台的首帧就绪信号或超时。
//
// notifyConn 为 nil（UDP 监听创建失败/单台）时退回固定 800ms 兜底——此时没有
// 信号源，只能靠间隔错峰（与旧行为一致）。
//
// 已知边界：超时放行后，迟到的那条 "firstframe" 会滞留在 socket 缓冲区，被
// 下一台的等待消费掉（相当于下一台提前放行）。仅在上一台首帧 >15s（硬解故障
// 回退软解等极端场景）时发生，且下一台仍有自身首帧串行约束，可接受。
func waitFirstFrameOrTimeout(notifyConn *net.UDPConn, key string) {
	if notifyConn == nil {
		time.Sleep(800 * time.Millisecond)
		return
	}
	start := time.Now()
	notifyConn.SetReadDeadline(time.Now().Add(firstFrameStaggerTimeout))
	buf := make([]byte, 32)
	if _, _, err := notifyConn.ReadFromUDP(buf); err != nil {
		log.Printf("[screencast] 首帧错峰等待超时(%v) key=%s，继续启动下一台: %v",
			firstFrameStaggerTimeout, key, err)
		return
	}
	log.Printf("[screencast] 前一台首帧就绪 key=%s 等待 %v → 启动下一台",
		key, time.Since(start).Round(time.Millisecond))
}
