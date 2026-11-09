//go:build linux

package main

// stubs_linux.go — Linux 平台桩：对齐 darwin 桩模式（d3d11_renderer_darwin.go），
// 为平台无关文件里引用的 Windows 专属符号提供 no-op/零值兜底，保证 Linux 可编译。
// 功能语义：Linux 投屏 MVP1（单窗口 RTSP：画面/声音/鼠标键盘），摄像头推流、
// tebox、剪贴板同步均不在 MVP 范围。

// ─── tebox 音频（tebox_audio.go 是 windows tag，app.go/tebox_connect.go 引用）───

// teboxAudio 是 windows 平台的 AAC 回放器类型；Linux 上 tebox 模式不在 MVP 范围，
// 仅提供类型与 no-op 方法使 app.go/tebox_connect.go 可编译（tebox 连接在 Linux
// 上不会真正使用音频路径——未初始化的 Play 直接返回）。
type teboxAudio struct{}

func (a *teboxAudio) Play(adts []byte) {}
func (a *teboxAudio) Reset()           {}
func (a *teboxAudio) Close()           {}

// ─── 摄像头推流（camera_push*.go 是 windows tag，app.go 引用）───────────────

// CameraPusher Linux 桩：MVP1 不含摄像头推流，统一 no-op。
type CameraPusher struct{}

func NewCameraPusher(ffmpegPath, deviceIP string, camTCPPort, vport int) *CameraPusher {
	return &CameraPusher{}
}

func (c *CameraPusher) Stop()                             {}
func (c *CameraPusher) Available() bool                   { return false }
func (c *CameraPusher) IsRunning() bool                   { return false }
func (c *CameraPusher) SharedState() (bool, bool, string) { return false, false, "" }

// newTeboxAudio Linux 桩：MVP1 tebox 模式音频不接线（tebox_audio.go 是 windows tag）。
func newTeboxAudio() *teboxAudio { return &teboxAudio{} }
