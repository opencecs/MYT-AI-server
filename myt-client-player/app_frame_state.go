package main

import (
	"sync"
	"sync/atomic"
)

// frameState 帧管线状态（解码/丢帧/降级标志）。
// 通过匿名嵌入 App，字段自动提升为 a.firstFrameSent 等。
type frameState struct {
	firstFrameSent      atomic.Bool
	firstFramePresented atomic.Bool  // 首帧已 Present 上屏（UI 线程设，用于 overlay 隐藏时机）
	droppedFrames       atomic.Int32 // 智能丢帧丢弃的帧数（日志用）
	droppedKeys         atomic.Int32 // 智能丢帧丢弃的 IDR 数（日志用，正常应为 0）
	framesDecoded       atomic.Int32 // 跳过解码器输出的第 1 帧（UV/解码缓冲未就绪可能产生绿色闪屏）
	gpuFrameLogCount    atomic.Int32
	naluCh              chan naluJob // async NALU decode channel, closed on disconnect
	// badStreak 连续 UV=0 损坏帧计数（单帧丢弃保上一帧，连续 ≥2 帧才灰度降级）。
	// 实际计数由渲染器维护（D3D11Renderer.badStreakCb，d3d11_frame.go 探针路径）；
	// 灰度输出由渲染端 shader/C 侧分级逻辑完成（不依赖 app 层动作）。
	badStreak    atomic.Int32
	texFallbackDone     atomic.Bool  // 纹理管线损坏→降级 BGRA 只执行一次
	swFallbackDone      atomic.Bool  // 硬解卡死→回退软解只执行一次（mpv force_fallback）
	audioInitFailed     atomic.Bool

	// lastSPS/lastPPS 最近一次码流的参数集（预灌用）：重连时若新会话 SDP 的
	// SPS/PPS 与上次一致，先灌一遍让解码器提前建好 codec 上下文，首个 IDR
	// 立即可解（省一次「等 IDR → 建 codec → 解码」的往返）。
	lastSPS []byte
	lastPPS []byte
	spsMu   sync.Mutex
}
