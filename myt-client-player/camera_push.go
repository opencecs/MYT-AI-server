//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

// camPushState 共享状态文件结构
type camPushState struct {
	DeviceIP  string `json:"deviceIP"`
	TCPPort   int    `json:"tcpPort"`
	CamName   string `json:"camName"`
	MicName   string `json:"micName"`
	VPort     int    `json:"vport"`
	PID       int    `json:"pid"`
	StartedAt int64  `json:"startedAt"`
}

// CameraPusher 管理摄像头+麦克风推流。
// Start(cam,mic) 为幂等目标态调整：把运行中的各路子循环收敛到 desired，
// 支持运行中增删视频/音频，而非一次性开关。视频路与 audio-only 路各持独立
// runHandle，可单独启停互不牵连。
type CameraPusher struct {
	mu           sync.Mutex
	transitionMu sync.Mutex // 串行化 apply 全过程，避免快速连点时新旧两路并存争抢采集设备
	ffmpegPath   string     // ffmpeg.exe 路径（视频采集编码 + 音频采集）
	deviceIP     string
	tcpPort      int
	vport        int
	statePath    string
	app          *App // 伪源 screenDevice 抓帧用（captureRendererFrame）

	desired camTarget  // 期望组合（唯一事实源）
	active  camTarget  // 已生效组合（用于 diff）
	video   *runHandle // 视频会话（自研 ffmpeg 推流），nil=未跑
	audio   *runHandle // audio-only 会话（独立 TCP + injectAudio），nil=未跑
	running bool       // desired 非空即 true，供 IsRunning()
}

// NewCameraPusher 创建推流管理器（camFFmpeg = 摄像头推流用 ffmpeg.exe 路径）
func NewCameraPusher(camFFmpeg, deviceIP string, tcpPort, vport int) *CameraPusher {
	return &CameraPusher{
		ffmpegPath: camFFmpeg,
		deviceIP:   deviceIP,
		tcpPort:    tcpPort,
		vport:      vport,
		statePath:  filepath.Join(os.TempDir(), "myt-cam-push-state.json"),
	}
}

// SetApp 注入宿主 App（伪源 screenDevice 抓帧用；构造后调用一次）。
func (cp *CameraPusher) SetApp(app *App) { cp.app = app }

// Available 检查推流功能是否可用（ffmpeg 就绪 + 端口有效）
func (cp *CameraPusher) Available() bool {
	if cp == nil || cp.tcpPort <= 0 {
		return false
	}
	ff := cp.ffmpegExe()
	if ff == "" {
		return false
	}
	_, err := os.Stat(ff)
	return err == nil
}

// ListCameras 列出可用摄像头（ffmpeg dshow 枚举）
func (cp *CameraPusher) ListCameras() ([]string, error) {
	if !cp.Available() {
		return nil, nil
	}
	ff := cp.ffmpegExe()
	cmd := exec.Command(ff, "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	cmd.Dir = filepath.Dir(ff)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	var cameras []string
	for _, line := range strings.Split(stderr.String(), "\n") {
		idx := strings.Index(line, "\"")
		if idx < 0 {
			continue
		}
		rest := line[idx+1:]
		end := strings.Index(rest, "\"")
		if end < 0 {
			continue
		}
		name := rest[:end]
		if strings.Contains(line, "(video)") && name != "" {
			cameras = append(cameras, name)
		}
	}
	return cameras, nil
}

// ListMics 列出可用麦克风（通过 ffmpeg dshow 枚举音频设备）
func (cp *CameraPusher) ListMics() ([]string, error) {
	if !cp.Available() {
		return nil, nil
	}
	ffmpegPath := cp.ffmpegPath
	if !strings.HasSuffix(strings.ToLower(ffmpegPath), "ffmpeg.exe") {
		ffmpegPath = filepath.Join(os.TempDir(), "myt-ffmpeg", "ffmpeg.exe")
		if _, err := os.Stat(ffmpegPath); err != nil {
			return nil, nil
		}
	}
	cmd := exec.Command(ffmpegPath, "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	cmd.Dir = filepath.Dir(ffmpegPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()
	var mics []string
	for _, line := range strings.Split(stderr.String(), "\n") {
		idx := strings.Index(line, "\"")
		if idx < 0 {
			continue
		}
		rest := line[idx+1:]
		end := strings.Index(rest, "\"")
		if end < 0 {
			continue
		}
		name := rest[:end]
		if strings.Contains(line, "(audio)") && name != "" {
			mics = append(mics, name)
		}
	}
	return mics, nil
}

// IsRunning 推流是否正在运行
func (cp *CameraPusher) IsRunning() bool {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return cp.running
}

// Source 当前推流视频源（"cam"/"screenDevice"/"screenDesktop"，未推流为 "cam"）。
func (cp *CameraPusher) Source() string {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if isPseudoSrc(cp.desired.src) {
		return cp.desired.src
	}
	return camSrcCam
}

// SharedState 返回全局共享推流状态
func (cp *CameraPusher) SharedState() (isMine bool, isOther bool, info string) {
	if !cp.Available() {
		return false, false, ""
	}
	st := cp.readState()
	if st == nil {
		return false, false, ""
	}
	if st.VPort == cp.vport {
		return true, false, ""
	}
	return false, true, fmt.Sprintf("%s:%d", st.DeviceIP, st.TCPPort)
}

// ffmpegExe 获取 ffmpeg.exe 路径
func (cp *CameraPusher) ffmpegExe() string {
	p := cp.ffmpegPath
	if strings.HasSuffix(strings.ToLower(p), "ffmpeg.exe") {
		return p
	}
	return filepath.Join(os.TempDir(), "myt-ffmpeg", "ffmpeg.exe")
}

// ─── 状态文件读写 ───

func (cp *CameraPusher) readState() *camPushState {
	data, err := os.ReadFile(cp.statePath)
	if err != nil {
		return nil
	}
	var st camPushState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	if st.PID > 0 && !processAlive(st.PID) {
		return nil
	}
	return &st
}

func (cp *CameraPusher) writeState(st *camPushState) {
	data, _ := json.Marshal(st)
	tmp := cp.statePath + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		os.Rename(tmp, cp.statePath)
	}
}

func (cp *CameraPusher) clearState() {
	os.Remove(cp.statePath)
}

// ─── 进程工具 ───

func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}

func killByPID(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// ─── TLV 工具（视频和音频推流共用） ───

var camMagic = []byte{0x89, 0x87, 0x67, 0x65}

func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

func appendTLVInt(buf []byte, v uint32) []byte {
	buf = append(buf, 4, 0, 0, 0)
	return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendTLVData(buf []byte, data []byte) []byte {
	n := len(data)
	buf = append(buf, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
	return append(buf, data...)
}

// sendAudioFrame 发送音频帧到设备
func (cp *CameraPusher) sendAudioFrame(conn net.Conn, data []byte, seq *uint32) {
	inner := make([]byte, 0, 64+len(data))
	inner = append(inner, 0x89, 0x87, 0x67, 0x65) // magic
	inner = appendTLVInt(inner, 2)                // streamType=2 (AUDIO)
	inner = appendTLVInt(inner, 0)                // flag=0
	inner = appendTLVData(inner, data)

	outer := make([]byte, 0, 80+len(inner))
	outer = append(outer, 0x89, 0x87, 0x67, 0x65) // magic
	outer = appendTLVInt(outer, 1)                // msgtype=1
	outer = appendTLVInt(outer, 0)                // 0
	outer = appendTLVInt(outer, *seq)             // seq
	outer = appendTLVInt(outer, 1)                // 1
	outer = appendTLVData(outer, inner)
	*seq++

	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(outer)))
	conn.Write(append(hdr[:], outer...))
}

// Stop 停止推流（关闭窗口时调用）
func (cp *CameraPusher) Stop() {
	cp.apply(camTarget{})
	winRectStop() // screenWindowRect 会话（WDA 恢复 + WGC 关闭），其他源为 no-op
}
