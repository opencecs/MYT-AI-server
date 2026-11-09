//go:build windows

package main

import (
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/oto/v3"
)

// tebox_audio.go — tebox 模式 AAC 音频回放（对照上游 cast_view.c 行为）。
//
// 上游协议：codec=16 记录 = AAC-LC ADTS 帧（7 字节头 + raw AAC），48kHz stereo，
// 1024 样本/帧 ≈21.3ms。上游播放行为（cast_view.c audio_open/play_aac）：
//   - FFmpeg AV_CODEC_ID_AAC 解码（ADTS 直接喂）→ 交错 s16 stereo 48kHz；
//   - 队列积压超 250ms 整体清空（latency beats continuity：设备停滞/重连突发时
//     丢积压保实时，不做 pts 对齐）；
//   - 断线重连 flush（teboxcast 断线重连时经 Reset 调用）。
//
// 实现选型：AAC 解码走 ffdecode.dll（FF_CreateAACDecoder/FF_DecodeAAC，与上游
// 同为 FFmpeg AAC 解码，播放器已有 purego 加载先例，零 CGO、不进 vendor）；
// 播放走 oto v3（pull 模型），audioBuffer 环形缓冲上实现积压清空
// （等价上游 SDL_GetQueuedAudioSize + SDL_ClearQueuedAudio）。
//
// 采样率（「滋滋杂音」根因）：上游协议文档写 48kHz，但实例实际按 44.1kHz 编码
// （测试夹具 ADTS 头 srIdx=4=44100 可证；RTSP 模式日志同为 44100Hz）。而
// ffdecode.c 的 FF_DecodeAAC 内部 swr 以解码帧自身采样率为准（不重采样到
// FF_CreateAACDecoder 的请求值），ADTS 头里的 srIdx 才决定输出采样率——若按
// 48kHz 建 oto 而流是 44.1k，oto 消耗速率恒高于生产速率 → 周期性欠载 → 持续
// 滋滋杂音。修复：从首个音频帧的 ADTS 头解析真实采样率/声道，用真实值建
// 解码器与 oto（与 RTSP 路径从 SDP 取 aacSampleRate 同语义）。
//
// 2026-10-15 晚间复测（抓帧 100 帧实测）：服务端已改发完整 ADTS，帧头 FF F1 4C 80 = 48kHz
// LC stereo，100/100 帧一致、帧长字段自洽、FF_DecodeAAC 100/100 解码成功。即用户日志
// "audio on: 48000Hz 2ch s16" 是真实解析结果（非兜底），采样率链路一致，残余滋滋若仍存在则
// 与采样率失配无关（候选方向：网络欠载/源端混音线程参数）。

const (
	// teboxAudioMaxQueued 积压上限：60ms 的 s16 stereo @48kHz。上游 AUDIO_MAX_QUEUED
	// 为 250ms，但那是 SDL 队列直写；oto v3 mux 另有 0.5s 内部缓冲，250ms 阈值下
	// 突发时端到端延迟可累积到 ~0.75s 且不回落（pull 模型积压只增不减直到清空），
	// 收紧到 60ms（~3 帧）保实时，超出部分丢弃的听感代价远小于持续延迟。
	teboxAudioMaxQueued = teboxAudioRate * 2 * 2 * 60 / 1000
	// teboxAudioPreroll 首播预缓冲：攒够 100ms 再让 oto 开始消费（常规防欠载
	// 做法，牺牲 100ms 延迟换稳定）。仅起播时触发一次，稳态不重复攒。
	teboxAudioPreroll = teboxAudioRate * 2 * 2 * 100 / 1000
	// teboxDecodeOutMax 单帧解码输出上限（2048 样本 × 2ch × 2B）
	teboxDecodeOutMax = 2048 * 2 * 2
	// teboxADTSHeaderLen ADTS 头长度（protection_absent=1 时 7 字节）
	teboxADTSHeaderLen = 7
	// teboxAudioRate 文档约定采样率（48kHz）；仅作 ADTS 解析失败时的兜底默认值，
	// 实际以首个音频帧 ADTS 头解析结果为准（根因：实例实际 44.1kHz）。
	teboxAudioRate = 48000
)

// teboxAudio tebox 音频回放器：解码（ffdecode.dll AAC）→ 环形缓冲（积压超
// 250ms 清空）→ oto 播放。线程安全：Play 在 teboxcast 读循环 goroutine 串行
// 调用；Reset/Close 可从其他 goroutine 调用。
type teboxAudio struct {
	mu      sync.Mutex
	decoder uintptr // FF_CreateAACDecoder 句柄
	buf     *audioBuffer
	player  *oto.Player
	outBuf  []byte
	started bool
	failed  bool // 初始化失败（记一次日志，之后静默，与上游 audio stays off 一致）
	closed  bool
}

// newTeboxAudio 创建未初始化的回放器（首次音频帧到达时才懒初始化，
// 对应上游 audio_open 懒打开）。
func newTeboxAudio() *teboxAudio {
	return &teboxAudio{}
}

// ensureInit 懒初始化：AAC 解码器 + oto。sampleRate/channels 取首个音频帧
// ADTS 头解析出的真实值（解析失败用 48k/2 兜底）。任何失败置 failed 不再重试。
// 调用方须持锁。
func (a *teboxAudio) ensureInit(sampleRate, channels int) bool {
	if a.closed {
		return false
	}
	if a.failed {
		return false
	}
	if a.decoder != 0 {
		return true
	}
	a.failed = true // 先置位：任何失败分支退出后不再重试

	if err := loadFFDecode(); err != nil {
		logf("[tebox-audio] ffdecode load failed: %v (audio off)\n", err)
		return false
	}
	if ffProcCreateAACDecoder == 0 || ffProcDecodeAAC == 0 || ffProcDestroyAACDecoder == 0 {
		logf("[tebox-audio] ffdecode.dll missing AAC exports (audio off)\n")
		return false
	}
	handle, _, _ := purego.SyscallN(ffProcCreateAACDecoder,
		uintptr(sampleRate), uintptr(channels))
	if handle == 0 {
		logf("[tebox-audio] FF_CreateAACDecoder failed (audio off)\n")
		return false
	}
	a.decoder = handle
	a.outBuf = make([]byte, teboxDecodeOutMax)

	buf := newAudioBuffer()
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		logf("[tebox-audio] oto init: %v (audio off)\n", err)
		return false
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		logf("[tebox-audio] oto context ready timeout (audio off)\n")
		return false
	}
	a.buf = buf
	player := ctx.NewPlayer(buf)
	// 拉取水位（残余「滋滋」修复）：oto v3 mux 拉取线程以 bufferSize 为每次拉取
	// 的目标水位，4096B 在 44.1kHz s16 stereo 下仅 ~23ms，且一帧解码输出恰为
	// 4096B——稳态水位贴地运行，TCP 到达抖动/调度抖动直接传导为渲染线程欠载
	// （mux 渲染时 player.buf 为空 → 该 player 输出静音段 → 波形不连续 → 咔哒/滋滋）。
	// 提到 16384B（~93ms）给 mux 内部缓冲垫底，配合 teboxAudioPreroll（起播前
	// ring 先攒 100ms）吸收起播阶段抖动。上游对照：SDL 队列模式天然有 250ms
	// 高水位，pull 模型必须显式补足等价缓冲。
	player.SetBufferSize(16384)
	a.player = player

	a.failed = false
	logf("[tebox-audio] audio on: %dHz %dch s16 (ffdecode AAC + oto)\n", sampleRate, channels)
	return true
}

// Play 解码一帧 ADTS 并入队播放（teboxcast OnAudio 回调入口）。
// 队列策略照上游 play_aac：写入前查积压，超阈值整体清空。
//
// 采样率处理（「滋滋杂音」根因修复）：首帧先解析 ADTS 头取真实采样率/声道建链
// （ensureInit），此后 ADTS 原样喂 FF_DecodeAAC——ffdecode.c 的 swr 以解码帧
// 自身采样率为准（不重采样到 FF_CreateAACDecoder 的请求值），头里的 srIdx 才
// 决定输出采样率，与 oto 的真实采样率严格一致，杜绝速率失配型周期性欠载。
func (a *teboxAudio) Play(adts []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	patched := false
	// ADTS 前缀缺失修复（「滋滋杂音」最终根因，2026-10-15 真实音频探针实锤）：
	// tebox 9914 独立音频端点下发的 payload 不是完整 ADTS——服务端把头 3 字节
	// 剥掉了（syncword FF F1 + profile/srIdx/chCfg_hi 字节；44.1kHz LC stereo
	// 时为 FF F1 50），payload = ADTS[3:]（其内嵌帧长字段 = payloadLen+3 可验）。
	// 直接当 ADTS 喂 FF_DecodeAAC：探针实测 900 帧 898 帧返回 AVERROR_INVALIDDATA，
	// 仅个别帧从 AAC 数据里撞出假 syncword 解出乱音——即用户听到的滋滋杂音。
	// 修复：检测 syncword 缺失时原样前置 FF F1 50 重建完整帧（payload[0:4] 是
	// 未被修改的 ADTS[3:7]，帧长/fullness 字段自洽，无需改写）。服务端将来若
	// 改发完整 ADTS（FF Fx 开头），走原路径零改动。0x50 = profile 01(LC) +
	// srIdx 0100(44100) + chCfg_hi 0——tebox 实例固定 44.1kHz LC stereo，探针
	// ffmpeg 交叉验证 900 帧全部解码成功、44100Hz 2ch、无削波无欠载。
	if len(adts) >= 1 && adts[0] != 0xFF {
		rebuilt := make([]byte, 0, len(adts)+3)
		rebuilt = append(rebuilt, 0xFF, 0xF1, 0x50)
		rebuilt = append(rebuilt, adts...)
		adts = rebuilt
		patched = true
	}
	if a.decoder == 0 {
		sr, ch := parseADTSParams(adts)
		// 首帧诊断日志（正式保留）：head=建链所用帧头前 4 字节，patched=是否触发剥头补头，parsed=据此建链的采样率/声道。
		// 复测日志可自解释：head=ff f1 4c .. 且 parsed=48000 → 流真是 48k（当前部署实测）；
		// parsed=48000 但 head 异常 → parseADTSParams 兜底（对照 head 定位原因）；
		// patched=true → 走了剥头补头（此时 parsed 恒 44100；若听感音调/速率不对，即服务端剥头流实际参数与 0x50 约定不符）。
		head := adts
		if len(head) > 4 {
			head = head[:4]
		}
		logf("[tebox-audio] first frame: head=% x patched=%v parsed=%dHz %dch\n", head, patched, sr, ch)
		if !a.ensureInit(sr, ch) {
			return
		}
	}
	// FF_DecodeAAC 输入为 ADTS 帧（上游同款：ADTS 直接喂）
	ret, _, _ := purego.SyscallN(ffProcDecodeAAC,
		a.decoder,
		uintptr(unsafe.Pointer(unsafe.SliceData(adts))),
		uintptr(len(adts)),
		uintptr(unsafe.Pointer(unsafe.SliceData(a.outBuf))),
		uintptr(len(a.outBuf)),
	)
	pcmSize := int(int32(ret))
	if pcmSize <= 0 {
		return
	}
	// 上游：SDL_GetQueuedAudioSize > AUDIO_MAX_QUEUED → SDL_ClearQueuedAudio。
	// 仅起播后执行：预缓冲攒水阶段 oto 未消费，Buffered 天然会超过 60ms 阈值，
	// 若此时清空 teboxAudioPreroll 永远攒不齐（阈值 5760B < 预缓冲 19200B）。
	if a.started && a.buf.Buffered() > teboxAudioMaxQueued {
		a.buf.Clear() // Clear 自取 ab.mu：oto 拉流 goroutine 会并发 Read，不可跨锁
	}
	a.buf.Write(a.outBuf[:pcmSize])
	// 预缓冲起播（残余「滋滋」修复）：攒够 teboxAudioPreroll（100ms）才让 oto
	// 开始消费，避免起播阶段 ring 水位贴地 → mux 拉不满 → 渲染欠载咔哒。此后
	// mux 内部 bufferSize（16384B ≈93ms）+ WASAPI 50ms 设备缓冲维持连续性，
	// 稳态不再重复攒（started 已置位）。
	if !a.started && a.buf.Buffered() >= teboxAudioPreroll {
		a.started = true
		go a.player.Play()
	}
}

// parseADTSParams 从 ADTS 帧头解析采样率与声道数（与 audio_buffer.go 的
// sampleRateToIndex 同表）。帧不可解析时回退文档约定值 48kHz/2ch。
func parseADTSParams(p []byte) (int, int) {
	const fallbackSR, fallbackCh = 48000, 2
	if len(p) < 4 || p[0] != 0xFF || p[1]&0xF6 != 0xF0 {
		return fallbackSR, fallbackCh
	}
	srIdx := int(p[2]>>2) & 0x0F
	chCfg := int(p[2]&0x01)<<2 | int(p[3]>>6)
	var sr int
	switch srIdx {
	case 0:
		sr = 96000
	case 1:
		sr = 88200
	case 2:
		sr = 64000
	case 3:
		sr = 48000
	case 4:
		sr = 44100
	case 5:
		sr = 32000
	case 6:
		sr = 24000
	case 7:
		sr = 22050
	case 8:
		sr = 16000
	case 9:
		sr = 12000
	case 10:
		sr = 11025
	case 11:
		sr = 8000
	default:
		return fallbackSR, fallbackCh
	}
	if chCfg < 1 || chCfg > 8 {
		return fallbackSR, fallbackCh
	}
	return sr, chCfg
}

// Reset 断线重连清空：清空音频队列并丢弃 oto 内部缓冲（上游
// avcodec_flush_buffers 等价物；解码器上下文按 ADTS 自同步，无需重建）。
// 已关闭则忽略。
func (a *teboxAudio) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.decoder == 0 {
		return
	}
	a.buf.Clear()
	logf("[tebox-audio] reconnect: queue flushed\n")
}

// Close 停止播放并释放解码器（幂等）。
func (a *teboxAudio) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	if a.buf != nil {
		a.buf.Close()
	}
	if a.player != nil {
		a.player.Pause()
	}
	a.player = nil
	dec := a.decoder
	a.decoder = 0
	a.mu.Unlock()

	if dec != 0 {
		purego.SyscallN(ffProcDestroyAACDecoder, dec)
	}
}
