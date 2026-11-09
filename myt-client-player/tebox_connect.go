package main

import (
	"fmt"
	"time"

	"go_legacy/teboxcast"
)

// connectTebox tebox 模式连接：teboxcast TCP 拉流 → 既有解码渲染管线（复用
// startDecodePipeline/initDecoder，零改动），控制事件经 TeboxInput 翻译走
// tebox UDP（RTSP/ControlChannel 路径不参与）。
//
// 建连是异步的：瞬时断连由 teboxcast 内部每 1s 重连消化（上报 ConnRetrying，
// 不触发全量重连）；持续无帧由 stall 看门狗兜底（ConnDisconnected → 全量重连）。
func (a *App) connectTebox() error {
	connectStart := time.Now()
	gen := a.connectGen.Add(1)

	// 控制出口：tebox UDP JSON 事件（触摸保活/双 up 在包内实现）
	in, err := NewTeboxInput(a.CastHost, a.CtrlPort, a.CastID)
	if err != nil {
		a.setConnState(ConnError, 0)
		return fmt.Errorf("tebox ctrl: %w", err)
	}
	a.teboxIn.Store(in)
	a.storeCtrl(in)

	a.setConnState(ConnConnecting, 0)
	a.kicked.Store(false)

	if _, decErr := a.initDecoder(); decErr != nil {
		logf("[app] decoder init failed: %v (video will not render)\n", decErr)
	}

	// Don't attempt connection if shutdown is already in progress.
	select {
	case <-a.shutdownCh:
		in.Close()
		if d := a.Decoder.Swap(nil); d != nil {
			d.Close()
		}
		return fmt.Errorf("shutting down")
	default:
	}

	src := NewTeboxSource(a.CastHost, a.CastPort, a.CastID)
	src.SetOnH264Frame(a.startDecodePipeline(connectStart))
	src.SetOnState(func(connected bool) {
		if connected {
			// 断线重连：清空音频队列（上游 avcodec_flush_buffers 等价物），
			// 防止重连突发积压（play_aac 250ms 上限兜底之外的及时清理）
			if aud := a.teboxAudio.Load(); aud != nil {
				aud.Reset()
			}
			a.setConnState(ConnConnected, 0)
		} else {
			// 瞬时断连：teboxcast 内部 1s 重连，UI 显示重试中即可
			a.setConnState(ConnRetrying, 0)
		}
	})
	// 音频：-noaudio 时不注册回调（teboxcast 对 nil 回调零开销跳过）。
	// 懒初始化：首个 AAC 帧到达才建解码器与 oto（上游 audio_open 同款）。
	// 独立音频端口（AudioPort>0，上游 0360f5e 拆分后的新部署）：起 AudioClient
	// 连 TEBOX-AUDIO/1 端点，回调同一条 teboxAudio.Play；=0 维持现状（旧部署
	// 靠 cast 流内 codec=16，src.SetOnAudio 路径）。两种模式互不干扰：新部署的
	// cast 流里已无 codec=16，旧部署的音频端点不存在。
	if !a.NoAudio {
		aud := newTeboxAudio()
		if prev := a.teboxAudio.Swap(aud); prev != nil {
			prev.Close() // 全量重连重建：旧实例释放（避免解码器泄漏）
		}
		if a.AudioPort > 0 {
			ac := teboxcast.NewAudioClient(a.CastHost, a.AudioPort, a.CastID)
			ac.Logf = logf
			ac.OnAudio = aud.Play
			// 独立音频断线重连同样清队列：防积压突发（与 cast 流内重连同款）
			ac.OnState = func(connected bool) {
				if connected {
					if cur := a.teboxAudio.Load(); cur != nil {
						cur.Reset()
					}
				}
			}
			a.teboxAudioConn.Store(ac)
			if err := ac.Start(); err != nil {
				logf("[app] tebox audio client start: %v (audio stays off)\n", err)
			}
		} else {
			src.SetOnAudio(aud.Play)
		}
	}
	a.Video = src
	if err := src.Start(); err != nil {
		in.Close()
		if d := a.Decoder.Swap(nil); d != nil {
			d.Close()
		}
		a.setConnState(ConnError, 0)
		return fmt.Errorf("tebox cast start: %w", err)
	}

	a.startStallWatchdog()
	logf("[perf] tebox connect start: %dms (host=%s id=%s cast=%d ctrl=%d)\n",
		time.Since(connectStart).Milliseconds(), a.CastHost, a.CastID, a.CastPort, a.CtrlPort)

	// 建连兜底：teboxcast 异步建连（连不上不报错、内部重试）。12s 仍未连上
	// （实例未运行/编码器重启中）触发全量重连——与 RTSP 首帧 12s 强制重连同节奏。
	// 代际标记防旧代际定时器误触发（全量重连会再次进入 connectTebox）。
	go func() {
		t := time.NewTimer(12 * time.Second)
		defer t.Stop()
		select {
		case <-t.C:
		case <-a.shutdownCh:
			return
		}
		if a.connectGen.Load() == gen && !a.Connected.Load() {
			logf("[conn] tebox not connected after 12s, forcing reconnect\n")
			// ConnError（而非 ConnDisconnected）：Connected 尚为 false，
			// 只有 ConnError 分支会无条件触发 autoReconnect
			a.setConnState(ConnError, 0)
		}
	}()

	return nil
}
