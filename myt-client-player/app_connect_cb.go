package main

import (
	"sync/atomic"
	"time"
)

// h264IsKeyFrame 判断一帧 NALU 是否含 IDR（关键帧）。
// H264 nal_unit_type 在低 5 位：5=IDR，7=SPS，8=PPS。一帧可含多个 NALU（SPS+PPS+IDR）。
func h264IsKeyFrame(nalus [][]byte) bool {
	for _, n := range nalus {
		if len(n) == 0 {
			continue
		}
		t := n[0] & 0x1F
		if t == 5 { // IDR
			return true
		}
	}
	return false
}

// connectStart is captured by RTSP callbacks to measure first-frame latency.
type connectStartTime struct{ val time.Time }

// naluJob holds NALU data and PTS for async decoding.
type naluJob struct {
	nalus [][]byte
	pts   time.Duration
	isKey bool // 是否关键帧（IDR），用于智能丢帧（满时优先丢 P/B 保留 IDR）
}

// drainNonKey 从 channel 清空所有非关键帧（P/B），保留 IDR。返回清出的帧数。
// 用于 IDR 到来时清理过时的 P/B（IDR 重新开始解码序列，旧 P/B 无用）。
func drainNonKey(ch chan naluJob) int {
	cleared := 0
	for {
		select {
		case job := <-ch:
			if job.isKey {
				// 遇到 IDR，放回去（不能丢），停止清理
				ch <- job
				return cleared
			}
			cleared++
		default:
			return cleared
		}
	}
}

// setupRTSPCallbacks registers H264/AAC/Close/Kicked callbacks on the RTSP client.
func (a *App) setupRTSPCallbacks(start time.Time) {
	// H264 帧管线（NALU 智能丢帧 → 异步解码）为 RTSP/tebox 共用，见 startDecodePipeline。
	a.RTSP.OnH264Frame = a.startDecodePipeline(start)

	a.RTSP.OnAACFrame = func(data []byte) {
		if a.NoAudio || a.SpeakerOff.Load() {
			return
		}
		aud := a.Audio.Load()
		if aud == nil {
			if a.audioInitFailed.Load() {
				return
			}
			a.audioInitFailed.Store(true)
			sr := a.RTSP.aacSampleRate
			ch := a.RTSP.aacChannels
			if sr == 0 {
				sr = 44100
			}
			if ch == 0 {
				ch = 2
			}
			dataCopy := make([]byte, len(data))
			copy(dataCopy, data)
			go func() {
				newAud := NewAudioDecoder()
				if err := newAud.Initialize(sr, ch); err != nil {
					logf("[app] audio init failed: %v (no audio)\n", err)
					return
				}
				if atomic.LoadInt32(&a.disconnected) != 0 {
					newAud.Close()
					return
				}
				a.Audio.Store(newAud)
				a.audioInitFailed.Store(false)
				newAud.DecodeAndPlay(dataCopy)
			}()
			return
		}
		aud.DecodeAndPlay(data)
	}

	a.RTSP.OnClose = func() {
		a.setConnState(ConnDisconnected, 0)
	}

	// DESCRIBE 完成（SDP 解析出 H264 参数集、SETUP/PLAY 之前）：与上次会话的
	// SPS/PPS 一致时预灌解码器，首个 IDR 无需先等参数集即可立即解码（缩短
	// 重连后首帧延迟）；不一致（切分辨率/换流）则等码流自带参数集自然重建。
	a.RTSP.OnDescribeH264 = a.onDescribeSPSPPS

	a.RTSP.OnLgcmdData = func(data []byte) {
		if len(data) > 4 && data[0] == 'C' && data[1] == 'M' && data[2] == 'D' {
			logf("[lgcmd] device command: % x\n", data[:min(len(data), 32)])
		}
	}

	a.RTSP.OnKicked = func() {
		if a.kicked.CompareAndSwap(false, true) {
			logf("[conn] session kicked by another client (454 in heartbeat)\n")
			a.setConnState(ConnKicked, 0)
		}
	}
	a.RTSP.Playarg = a.Playarg
}

// decodeLoop runs in a dedicated goroutine, consuming NALU jobs from the channel
// and decoding + rendering them. This decouples the RTSP read loop from decoding.
func (a *App) decodeLoop(naluCh <-chan naluJob, start time.Time) {
	for job := range naluCh {
		dec := a.Decoder.Load()
		if dec == nil {
			continue
		}
		res, ok := dec.Decode(job.nalus)
		if !ok {
			continue
		}
		// 空帧过滤：无参考帧的 P 帧在 FFmpeg 层被拒（receive_frame 报错返回 0）→
		// 空帧（w=0 h=0 BGRA=0）。渲染空帧会上传空纹理/空 DIB，瞬间绿屏。
		// 直接丢弃，窗口保持 D3D11/GDI 初始化黑色，直到首个有效帧（IDR）到达。
		if res.Width <= 0 || res.Height <= 0 || res.BGRA == 0 {
			continue
		}
		// 损坏帧丢弃（硬解 UV=0 半帧）：FF_DecodeTexture 返回 3，不渲染保留最后一帧。
		// 跟软解一致——软解遇损坏帧内部丢弃停上一帧。硬解吐半帧不报错，C 侧检测后丢弃。
		if res.Width > 0 && res.Height > 0 {
			if c := a.gpuFrameLogCount.Add(1); c <= 5 {
				logf("[app] frame: w=%d h=%d gpu=%v\n", res.Width, res.Height, res.IsTexture)
			}
		}
		frame := &VideoFrame{
			Width:  res.Width,
			Height: res.Height,
			Format: PixelFormatBGRA,
			BGRA:   res,
			PTS:    job.pts,
		}
		if r := a.activeRenderer(); r != nil {
			// 纹理帧释放：槽位路径在 SendFrame 内 CopyTexture 提交后立即 unref（mpv 时序）
			if _, ok := r.(*D3D11Renderer); ok && res.IsTexture && res.BGRA != 0 {
				frame.releaseRef = func() { dec.UnrefFrame(res.FrameHandle) }
			}

			// 纹理拷贝管线损坏（设备不一致/SRV 创建失败）：降级 BGRA 输出（只执行一次）
			if d3d, ok := r.(*D3D11Renderer); ok && d3d.TexBroken() && a.texFallbackDone.CompareAndSwap(false, true) {
				dec.SetTextureOutput(false)
			}

			// tebox 方向自适应：按解码分辨率更新 Rotated（必须先于 SendFrame，
			// 帧渲染/lbGeo/鼠标映射三者一致，无过渡帧）。横竖切换时返回 true，
			// 顺带重置一次性初始 fit 让窗口 refit 到新方向（tebox_orientation.go）。
			if a.ApplyTeboxOrientation(res.Width, res.Height) {
				a.onTeboxOrientationChanged()
			}

			// 首帧已解码标记（含过渡期被丢弃的首帧）：触发 OnFirstFrame → fitWindowToVideo + UI。
			// 放在过渡期丢弃之前，确保过渡期首帧也触发 fit（Bug1 修复依赖此）。
			if !a.firstFrameSent.Load() && res.BGRA != 0 && res.Width > 0 && res.Height > 0 {
				a.firstFrameSent.Store(true)
				logf("[perf] first frame decoded: %dms (%dx%d, gpu=%v)\n", time.Since(start).Milliseconds(), res.Width, res.Height, res.IsTexture)
				a.notifyReady() // 通知父进程首帧就绪（错开批量启动 GPU 峰值）
				if a.OnFirstFrame != nil {
					a.OnFirstFrame()
				}
			}

			// 有帧就渲染（业界做法：GPU 硬解零拷贝直渲染，绿帧由 shader 逐像素
			// UV<0.01→中性128 灰度修复，零 CPU Map 回读）。

			r.SendFrame(frame)
		}
		// For BGRA CPU path (non-texture), release immediately
		if res.BGRA != 0 && !res.IsTexture {
			// BGRA data is owned by the decoder's buffer; no explicit release needed
		}
	}
}

// startDecodePipeline 创建异步解码管线（缓冲 channel + decodeLoop），返回 H264 帧入口回调。
// RTSP 与 tebox 两种视频源共用，解码渲染管线零改动。
func (a *App) startDecodePipeline(start time.Time) func(nalus [][]byte, pts time.Duration) {
	// Create a buffered channel for async NALU decoding.
	// Buffer size 8: allows RTSP reader to stay ahead of the decoder
	// without blocking (at 30fps, ~260ms of buffered frames).
	a.naluCh = make(chan naluJob, 8)

	// Start the async decode goroutine.
	go a.decodeLoop(a.naluCh, start)

	return func(nalus [][]byte, pts time.Duration) {
		if a.StallTimeout > 0 {
			a.lastFrame.Store(time.Now().UnixNano())
		}
		a.captureSpsPps(nalus)
		// Deep-copy NALU data: the RTSP callback reuses the underlying buffer,
		// and we now decode asynchronously — the data must outlive the callback.
		copied := make([][]byte, len(nalus))
		for i, n := range nalus {
			b := make([]byte, len(n))
			copy(b, n)
			copied[i] = b
		}
		// 丢包感知帧门控：IDR 一律放行（gateOnIDR 清污染/停看门狗/打日志）；
		// 参考链污染期间的 P/B 帧拦截丢弃——画面保留最后一帧，等 IDR 重同步，
		// 消除「解码成功但内容损坏」花屏帧上屏（见 frame_gate.go 顶部说明）。
		isKey := h264IsKeyFrame(copied)
		if gateOnIDR() {
			drainNonKey(a.naluCh) // 清空污染期间可能积压的旧 P/B（IDR 重同步）
		}
		if gatePoll(isKey) {
			n := gate.droppedCount.Add(1)
			if n <= 3 || n%50 == 0 {
				logf("[gate] 冻结中，丢弃 P/B 帧 #%d\n", n)
			}
			return
		}
		// Submit NALU for async decoding.
		// 智能丢帧策略：解码跟不上（channel 满）时，
		// - P/B 帧：直接丢弃新帧（带日志），不阻塞 RTSP 读循环
		// - IDR 关键帧：不能丢（丢了后续 P/B 无参考帧→马赛克，等下个 GOP~5秒才恢复）。
		//   清空 channel 中过时的 P/B 帧后投 IDR（IDR 重新开始解码序列，旧帧全无用）。
		job := naluJob{nalus: copied, pts: pts, isKey: isKey}
		for {
			select {
			case a.naluCh <- job:
				return
			default:
			}
			// channel 满
			if !isKey {
				// P/B 帧：丢弃新帧，记日志
				n := a.droppedFrames.Add(1)
				if n <= 5 || n%100 == 0 {
					logf("[rtsp] 解码跟不上，丢弃 P/B 帧 #%d（channel 满）\n", n)
				}
				return
			}
			// IDR：清空 channel 中过时的 P/B 帧（IDR 重新开始解码序列，旧帧无用）
			cleared := drainNonKey(a.naluCh)
			if cleared == 0 {
				// 清空后仍投不进（极端：全是 IDR），阻塞投保证不丢
				a.naluCh <- job
				return
			}
			a.droppedKeys.Add(int32(cleared)) // 记账：为 IDR 让位清理了 P/B
			logf("[rtsp] IDR 到来 channel 满，清理 %d 个过时 P/B 帧后投递\n", cleared)
		}
	}
}

// h264NaluType 取 NALU 类型（低 5 位）。
func h264NaluType(nalu []byte) int {
	if len(nalu) == 0 {
		return 0
	}
	return int(nalu[0] & 0x1F)
}

// onDescribeSPSPPS RTSP DESCRIBE 回调：SDP 带参数集且与上次会话一致 → 预灌
// 解码器（解码器已建好 codec 上下文，首个 IDR 立即可解）。SDP 不带参数集
// （sps/pps 为 nil）或参数集变化时不动作——码流里的 SPS/PPS 会正常触发建上下文。
func (a *App) onDescribeSPSPPS(sps, pps []byte) {
	if len(sps) == 0 || len(pps) == 0 {
		return
	}
	a.spsMu.Lock()
	lastSPS, lastPPS := a.lastSPS, a.lastPPS
	a.spsMu.Unlock()
	if len(lastSPS) == 0 || len(lastPPS) == 0 {
		logf("[rtsp] SDP sprop-parameter-sets present, no cached params (first session)\n")
		return
	}
	if string(sps) != string(lastSPS) || string(pps) != string(lastPPS) {
		logf("[rtsp] SDP SPS/PPS differ from last session, skip prefill (stream params will rebuild)\n")
		return
	}
	a.prefillDecoder()
}

// captureSpsPps 从码流里捕获最近的 SPS/PPS（重连预灌用，见 prefillDecoder）。
func (a *App) captureSpsPps(nalus [][]byte) {
	for _, n := range nalus {
		switch h264NaluType(n) {
		case 7: // SPS
			cp := make([]byte, len(n))
			copy(cp, n)
			a.spsMu.Lock()
			a.lastSPS = cp
			a.spsMu.Unlock()
		case 8: // PPS
			cp := make([]byte, len(n))
			copy(cp, n)
			a.spsMu.Lock()
			a.lastPPS = cp
			a.spsMu.Unlock()
		}
	}
}

// prefillDecoder 若缓存了上次的 SPS/PPS 且解码器已就绪，灌一遍参数集让解码器
// 提前建好 codec 上下文：正常链路首个 IDR 前必带 SPS/PPS（N=1 无预灌收益），
// 但 SDP/码流不带参数集的链路上，这一步让首个 IDR 无需先等参数集即可立即解码，
// 缩短首帧延迟。Decode 非阻塞（内部 2s 超时兜底），失败无副作用——首个 IDR 自带
// 参数集时会正常重建上下文。
func (a *App) prefillDecoder() {
	a.spsMu.Lock()
	sps, pps := a.lastSPS, a.lastPPS
	a.spsMu.Unlock()
	if len(sps) == 0 || len(pps) == 0 {
		return
	}
	dec := a.Decoder.Load()
	if dec == nil {
		return
	}
	if res, ok := dec.Decode([][]byte{sps, pps}); ok {
		// 预灌无 IDR 不会产出可渲染帧，正常返回空结果；只记日志便于诊断
		logf("[app] decoder prefilled with cached SPS/PPS (w=%d h=%d)\n", res.Width, res.Height)
	} else {
		logf("[app] decoder prefill skipped (decoder busy/empty result)\n")
	}
}
