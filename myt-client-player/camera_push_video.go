//go:build windows

package main

import (
	"os"
	"time"
)

// Start 幂等地把推流调整到目标组合 (camName, micName, src)。
// src 为视频源类型（""=真实摄像头 camName；screenDevice/screenDesktop=伪源，
// 此时 camName 必须为空）；空字符串表示该路不推；可在运行中增删视频/音频而无需先 Stop。
func (cp *CameraPusher) Start(camName, micName, src string) error {
	cp.apply(camTarget{cam: camName, mic: micName, src: src})
	return nil
}

// apply 是状态机核心：计算 active→desired 的过渡计划并串行执行。
// 全程持锁做决策与登记 handle，真正的阻塞式启停在锁外进行，避免死锁。
func (cp *CameraPusher) apply(desired camTarget) {
	cp.transitionMu.Lock()
	defer cp.transitionMu.Unlock()

	cp.mu.Lock()
	if desired == cp.desired && cp.running {
		cp.mu.Unlock()
		return
	}
	plan := computeTransitions(cp.active, desired)
	coldStartVideo := (plan.startVideo || plan.restartVideo) && cp.active.empty()
	cp.desired = desired
	cp.running = !desired.empty()

	// 先把要停的旧路的 stop 关闭并从结构里摘除，防止新命令读到半死 handle。
	var oldVideo, oldAudio *runHandle
	if plan.stopVideo || plan.restartVideo {
		oldVideo = cp.video
		cp.video = nil
	}
	if plan.stopAudioOnly {
		oldAudio = cp.audio
		cp.audio = nil
	}

	// 需要起的新路：先建 handle 占位，goroutine 在锁外启动。
	var newVideo, newAudio *runHandle
	if plan.startVideo || plan.restartVideo {
		newVideo = newRunHandle()
		cp.video = newVideo
	}
	if plan.startAudioOnly {
		newAudio = newRunHandle()
		cp.audio = newAudio
	}
	target := desired // 快照，供子循环读取，避免后续 desired 变更竞态
	cp.mu.Unlock()

	// 冷启动视频路：清理上次异常退出可能残留的旧推流进程（按状态文件 PID）。
	if coldStartVideo {
		if st := cp.readState(); st != nil && st.PID > 0 && st.VPort != cp.vport {
			_ = killByPID(st.PID)
			time.Sleep(300 * time.Millisecond)
		}
	}

	// 写共享状态文件（多窗口互斥用），按最终 desired 反映。
	cp.syncStateFile(target)

	// 等待被停的旧路彻底退出，保证同资源不被两路同时占用；加超时兜底防子进程卡死拖垮状态机。
	if oldVideo != nil {
		oldVideo.requestStop()
		waitDone(oldVideo.done, 3*time.Second)
	}
	if oldAudio != nil {
		oldAudio.requestStop()
		waitDone(oldAudio.done, 3*time.Second)
	}

	if newVideo != nil {
		go cp.videoLoop(newVideo, target)
	}

	// screenWindowRect 会话随视频路一起收（WDA 恢复 + WGC 关闭）：
	// 源切换/停止都会走 stopVideo/restartVideo；resize 重启路径自行先停，
	// 这里重复调用安全（winRectStop 幂等）。winRectStart 在 videoLoopEncoder 入口执行。
	if (plan.stopVideo || plan.restartVideo) && cp.active.src == camSrcWindowRect {
		winRectStop()
	}
	if newAudio != nil {
		go cp.audioOnlyLoop(newAudio, target.mic)
	}

	// 过渡登记完成：active 收敛到 desired，供下次 apply 做 diff。
	cp.mu.Lock()
	cp.active = desired
	cp.mu.Unlock()
}

// syncStateFile 依据 desired 更新跨窗口共享状态文件（保持原语义：非空写、空清）。
func (cp *CameraPusher) syncStateFile(t camTarget) {
	if t.empty() {
		if st := cp.readState(); st != nil && st.VPort == cp.vport {
			cp.clearState()
		}
		return
	}
	cp.writeState(&camPushState{
		DeviceIP: cp.deviceIP, TCPPort: cp.tcpPort,
		CamName: t.cam, MicName: t.mic, VPort: cp.vport,
		PID: os.Getpid(), StartedAt: time.Now().Unix(),
	})
}

// videoLoop 视频路子循环：自研 ffmpeg 推流（唯一路径）。
// 结束时清理自己的 handle，并在「非主动停止 + desired 仍需音频」时降级起 audio-only。
func (cp *CameraPusher) videoLoop(h *runHandle, target camTarget) {
	intentional := false
	defer func() {
		h.requestStop()
		h.cmd = nil
		close(h.done)
		cp.reapVideo(h, intentional)
	}()
	cp.videoLoopEncoder(h, target, &intentional)
}

// reapVideo 在视频路结束后清理登记；若非主动停止且期望态仍要求音频、当前又无活着的
// audio-only 路（视频崩溃但用户还想听声音），自动降级起 audio-only。
func (cp *CameraPusher) reapVideo(h *runHandle, intentional bool) {
	cp.mu.Lock()
	if cp.video == h {
		cp.video = nil
	}
	needFallback := !intentional && cp.desired.needsAudio() && cp.audio == nil
	var nh *runHandle
	if needFallback {
		nh = newRunHandle()
		cp.audio = nh
		// 实际生效组合已降级为「无视频 + 音频」，同步 active 使后续 apply diff 正确。
		cp.active = camTarget{mic: cp.desired.mic}
	}
	mic := cp.desired.mic
	cp.mu.Unlock()
	if nh != nil {
		logf("[cam] falling back to audio-only (mic=%s)", mic)
		go cp.audioOnlyLoop(nh, mic)
	}
}
