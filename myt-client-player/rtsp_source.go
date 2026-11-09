package main

import "time"

// RTSPSource VideoSource 的 RTSP 适配：薄包装现有 RTSPClient（不重写 rtsp.go）。
//
// 接线说明：RTSP 模式的回调注册仍走 setupRTSPCallbacks（AAC/lgcmd/454 等专属
// 回调超出 VideoSource 抽象面，直接挂 RTSPClient），本适配器在 App.Connect 里
// 只承担 Start（连接触发）职责；Stop 交由 App.Disconnect 既有的 a.RTSP.Close()
// 承担（避免双重关闭），故 Stop 为空实现。
type RTSPSource struct {
	client  *RTSPClient
	url     string
	user    string
	pass    string
	overTCP bool
}

// NewRTSPSource 创建 RTSP 视频源适配器。
func NewRTSPSource(client *RTSPClient, rawURL, user, pass string, overTCP bool) *RTSPSource {
	return &RTSPSource{client: client, url: rawURL, user: user, pass: pass, overTCP: overTCP}
}

// Start 建立 RTSP 连接（错误语义与 RTSPClient.Connect 一致，含 ErrSessionKicked）。
func (s *RTSPSource) Start() error {
	return s.client.Connect(s.url, s.user, s.pass, s.overTCP)
}

// Stop 空实现：连接关闭由 App.Disconnect 的 a.RTSP.Close() 统一承担（见类型注释）。
func (s *RTSPSource) Stop() {}

// SetOnH264Frame 委托给 RTSPClient.OnH264Frame。
func (s *RTSPSource) SetOnH264Frame(cb func([][]byte, time.Duration)) {
	s.client.OnH264Frame = cb
}

// SetOnState 委托给 RTSPClient.OnClose（RTSP 无「已连接」推送，断开即状态变化）。
func (s *RTSPSource) SetOnState(cb func(connected bool)) {
	s.client.OnClose = func() { cb(false) }
}
