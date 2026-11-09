package main

import (
	"time"

	"go_legacy/teboxcast"
)

// CastModeTebox 启动参数 -cast-mode 的 tebox 取值（QEMU Android 模拟器投屏）。
const CastModeTebox = "tebox"

// TeboxSource VideoSource 的 tebox 适配：包装 teboxcast.CastClient（TCP 9901
// 自定义帧流），把 Annex-B payload 拆成 NALU 后喂给既有解码渲染管线。
type TeboxSource struct {
	host string
	port int
	id   string

	client  *teboxcast.CastClient
	onFrame func([][]byte, time.Duration)
	onState func(connected bool)
	// onAudio AAC 记录回调（nil=不收音频，-noaudio 时保持 nil 零开销跳过）
	onAudio func(payload []byte)
}

// NewTeboxSource 创建 tebox 视频源适配器。
func NewTeboxSource(host string, port int, instanceID string) *TeboxSource {
	return &TeboxSource{host: host, port: port, id: instanceID}
}

// SetOnH264Frame 注册帧回调（NALU 不带 start code，与 RTSP 输出同格式）。
func (s *TeboxSource) SetOnH264Frame(cb func([][]byte, time.Duration)) { s.onFrame = cb }

// SetOnState 注册连接状态回调。
func (s *TeboxSource) SetOnState(cb func(connected bool)) { s.onState = cb }

// SetOnAudio 注册 AAC 音频记录回调（codec=16 的记录逐帧吐出，nil=不收音频）。
func (s *TeboxSource) SetOnAudio(cb func(payload []byte)) { s.onAudio = cb }

// Start 启动 teboxcast 客户端（断线由其内部每 1s 重连并重发握手行）。
func (s *TeboxSource) Start() error {
	s.client = teboxcast.NewCastClient(s.host, s.port, s.id)
	s.client.Logf = logf
	s.client.OnFrame = func(f teboxcast.Frame) {
		if s.onFrame == nil {
			return
		}
		nalus := splitAnnexB(f.Payload)
		if len(nalus) == 0 {
			return
		}
		s.onFrame(nalus, time.Duration(f.PTSUs)*time.Microsecond)
	}
	s.client.OnState = s.onState
	s.client.OnAudio = s.onAudio
	return s.client.Start()
}

// Stop 停止连接循环（幂等）。
func (s *TeboxSource) Stop() {
	if s.client != nil {
		s.client.Close()
	}
}

// splitAnnexB 把 H.264 Annex-B 流拆成 NALU 列表（去 start code，每个 NALU 独立拷贝，
// 满足解码管线对数据生命周期的要求）。兼容 3/4 字节 start code 与起始零填充。
func splitAnnexB(payload []byte) [][]byte {
	// 先扫描出所有 start code 位置（4 字节优先；3 字节检查需保证 i+3 不越界）
	type scPos struct{ off, size int }
	var positions []scPos
	for i := 0; i < len(payload); i++ {
		if i+4 <= len(payload) &&
			payload[i] == 0 && payload[i+1] == 0 && payload[i+2] == 0 && payload[i+3] == 1 {
			positions = append(positions, scPos{off: i, size: 4})
			i += 3
			continue
		}
		if i+3 <= len(payload) && payload[i] == 0 && payload[i+1] == 0 && payload[i+2] == 1 {
			positions = append(positions, scPos{off: i, size: 3})
			i += 2
		}
	}
	if len(positions) == 0 {
		return nil
	}
	nalus := make([][]byte, 0, len(positions))
	for i, p := range positions {
		start := p.off + p.size
		end := len(payload)
		if i+1 < len(positions) {
			end = positions[i+1].off
		}
		// 去除 NALU 末尾的零填充（下一 start code 前的 trailing zeros 不属于本 NALU）
		for end > start && payload[end-1] == 0 {
			end--
		}
		if end <= start {
			continue
		}
		nalu := make([]byte, end-start)
		copy(nalu, payload[start:end])
		nalus = append(nalus, nalu)
	}
	return nalus
}
