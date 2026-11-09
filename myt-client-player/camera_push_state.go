//go:build windows

package main

import (
	"io"
	"os/exec"
	"time"
)

// 视频源类型：cam=PC 摄像头（dshow，现状）；screenDevice=窗口画面（播放器解码帧）；
// screenDesktop=电脑桌面；screenWindow=被遮挡窗口（WGC，camera_push_wgc.go）。
// 伪源走 ffmpeg rawvideo 管线（见 camera_push_screen.go）。
const (
	camSrcCam        = ""                 // 现状：真实摄像头
	camSrcDevice     = "screenDevice"     // 窗口画面（播放器解码帧）
	camSrcDesktop    = "screenDesktop"
	camSrcWindow     = "screenWindow"     // 被遮挡窗口（WGC，见 camera_push_wgc.go）
	camSrcWindowRect = "screenWindowRect" // 窗口矩形桌面画面（WDA+WGC monitor，camera_push_winrect.go）
)

// camTarget 描述一次推流的期望组合：cam/mic 任一为空表示不推该路。
// src 为视频源类型；cam 仅在 src=cam 时有意义（dshow 设备名），
// 伪源时 cam 恒空（needsVideo 由 src 判定）。
type camTarget struct {
	cam string
	mic string
	src string
}

// isPseudoSrc 伪源判定（rawvideo 管线，非真实摄像头）。
func isPseudoSrc(src string) bool {
	return src == camSrcDevice || src == camSrcDesktop ||
		src == camSrcWindow || src == camSrcWindowRect
}

// empty 表示完全停止（无视频且无音频）。
func (t camTarget) empty() bool { return t.cam == "" && t.mic == "" && t.src == "" }

// needsVideo / needsAudio 判定各路是否需要运行。
// 伪源（isPseudoSrc）只占 src，cam 为空，needsVideo 由 src 判定。
func (t camTarget) needsVideo() bool { return t.cam != "" || t.src != "" }
func (t camTarget) needsAudio() bool { return t.mic != "" }

// audioViaVideo 表示音频是否应搭在视频连接上注入（有视频会话时优先共用），
// 否则走独立 TCP 的 audio-only 路。
func (t camTarget) audioViaVideo() bool { return t.cam != "" && t.mic != "" }

// runHandle 管理一路独立可启停的子循环（视频路或 audio-only 路）。
// stop 关闭即请求该路退出；done 在该路 goroutine 收尾后关闭，供上层等待串行化。
type runHandle struct {
	stop   chan struct{}
	done   chan struct{}
	cmd    *exec.Cmd       // ffmpeg（视频路）或 WindowsPusher（audio-only 路），可为 nil
	stdout io.ReadCloser   // 视频路：ffmpeg Annex-B 输出（videoLoopEncoder 消费）
	stdin  io.WriteCloser  // 伪源视频路：rawvideo BGRA 输入（screenFramePump 写）
}

func newRunHandle() *runHandle {
	return &runHandle{stop: make(chan struct{}), done: make(chan struct{})}
}

// requestStop 幂等地请求该路停止（重复调用不会 panic）。
func (h *runHandle) requestStop() {
	select {
	case <-h.stop:
	default:
		close(h.stop)
	}
}

// waitDone 等待某路 done，最多 dur；超时返回 false（子进程疑似卡死，调用方继续推进）。
func waitDone(done chan struct{}, dur time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(dur):
		return false
	}
}

// transitionPlan 描述从 active 到 desired 需要执行的动作集合。
// 采用方案 A：mic 变化若影响视频路构成，则整条视频路重启（画面短暂重握手 <1s）。
type transitionPlan struct {
	restartVideo   bool // 停旧视频路并起新视频路（含可能的音频注入）
	startVideo     bool // 仅起视频路（active 无视频、desired 有视频）
	stopVideo      bool // 仅停视频路（active 有视频、desired 无视频）
	startAudioOnly bool // 起独立音频路（desired 需音频且不走视频）
	stopAudioOnly  bool // 停独立音频路
}

// computeTransitions 是纯函数：给定已生效态与期望态，算出如何调整。
// 判定原则——只要「视频是否存在」或「音频是否随视频」发生变化，就需要重建视频路；
// audio-only 路仅在「需要音频且无视频会话」时存在。
func computeTransitions(active, desired camTarget) transitionPlan {
	var p transitionPlan

	activeHasVideo := active.needsVideo()
	desiredHasVideo := desired.needsVideo()

	// 视频路是否需要（重新）建立：desired 要有视频，且其构成相对 active 发生变化
	// （摄像头名变了、源类型变了，或音频随视频的状态变了）。
	videoShapeChanged := active.cam != desired.cam ||
		active.src != desired.src ||
		active.audioViaVideo() != desired.audioViaVideo()

	switch {
	case desiredHasVideo && !activeHasVideo:
		p.startVideo = true
	case desiredHasVideo && activeHasVideo && videoShapeChanged:
		p.restartVideo = true
	case !desiredHasVideo && activeHasVideo:
		p.stopVideo = true
	}

	// audio-only 路：desired 需要音频且没有视频会话承载它时才存在。
	wantAudioOnly := desired.needsAudio() && !desiredHasVideo
	haveAudioOnly := active.needsAudio() && !activeHasVideo
	if wantAudioOnly && !haveAudioOnly {
		p.startAudioOnly = true
	}
	if haveAudioOnly && !wantAudioOnly {
		p.stopAudioOnly = true
	}
	return p
}
