//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// ListCameras 列出可用摄像头设备
func (ui *WindowsUI) ListCameras() []string {
	if ui.app.CamPusher == nil || !ui.app.CamPusher.Available() {
		return nil
	}
	cameras, err := ui.app.CamPusher.ListCameras()
	if err != nil {
		logf("[cam] list cameras error: %v", err)
		return nil
	}
	return cameras
}

// StartCameraPush 启动推流（camName 摄像头名，空=不推视频；micName 麦克风名，空=不推音频；
// src 视频源："cam"=真实摄像头 / "screenDevice"=窗口画面 / "screenDesktop"=电脑桌面）
func (ui *WindowsUI) StartCameraPush(camName, micName, src string) bool {
	if ui.app.CamPusher == nil || !ui.app.CamPusher.Available() {
		return false
	}
	if err := ui.app.CamPusher.Start(camName, micName, src); err != nil {
		logf("[cam] start push error: %v", err)
		return false
	}
	logf("[cam] push started: src=%s cam=%s mic=%s", srcLabel(src), camName, micName)
	return true
}

// StopCameraPush 停止推流
func (ui *WindowsUI) StopCameraPush() {
	if ui.app.CamPusher == nil {
		return
	}
	ui.app.CamPusher.Stop()
	logf("[cam] push stopped")
}

// CameraPushRunning 推流是否正在运行
func (ui *WindowsUI) CameraPushRunning() bool {
	if ui.app.CamPusher == nil {
		return false
	}
	return ui.app.CamPusher.IsRunning()
}

// SetSpeakerOff 开关听筒（false=播放设备声音，true=静音）
func (ui *WindowsUI) SetSpeakerOff(off bool) {
	ui.app.SpeakerOff.Store(off)
	logf("[speaker] off=%v", off)
}

// IsSpeakerOff 听筒是否关闭
func (ui *WindowsUI) IsSpeakerOff() bool {
	return ui.app.SpeakerOff.Load()
}

// EvalCameraList 将摄像头列表发送到侧边栏
func (ui *WindowsUI) EvalCameraList(cameras []string) {
	if ui.chromium == nil { return }
	b, _ := json.Marshal(cameras)
	js := fmt.Sprintf("if(window.updateCameraList)updateCameraList(%s)", string(b))
	ui.chromium.Eval(js)
}

// ListMics 列出可用麦克风设备
func (ui *WindowsUI) ListMics() []string {
	if ui.app.CamPusher == nil || !ui.app.CamPusher.Available() { return nil }
	mics, err := ui.app.CamPusher.ListMics()
	if err != nil {
		logf("[cam] list mics error: %v", err)
		return nil
	}
	return mics
}

// EvalMicList 将麦克风列表发送到侧边栏
func (ui *WindowsUI) EvalMicList(mics []string) {
	if ui.chromium == nil { return }
	b, _ := json.Marshal(mics)
	js := fmt.Sprintf("if(window.updateMicList)updateMicList(%s)", string(b))
	ui.chromium.Eval(js)
}

// pollCameraPushState 定时轮询共享状态并同步到侧边栏
func (ui *WindowsUI) pollCameraPushState() {
	if ui.app.CamPusher == nil || !ui.app.CamPusher.Available() { return }
	go func() {
		for {
			time.Sleep(1000 * time.Millisecond)
			if ui.app.CamPusher == nil { return }
			ui.doSyncStateToPanel()
		}
	}()
}
