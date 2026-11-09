package screencast

import (
	"fmt"
	"log"
)

// tebox 投屏模式（V4 / tobox_player 的实例投屏）。
//
// tebox 是 QEMU Android 模拟器，投屏协议与 ARM 设备 RTSP 完全不同：
//   - 视频：TCP 全局端口（默认 9901），TEBOX-CAST/1 握手 + 大端帧记录
//   - 音频：TCP 全局端口（默认 9904），TEBOX-AUDIO/1 握手
//   - 控制：TCP+UDP 全局端口（默认 9905），UDP 无握手，事件自带 id/seq
//
// 关键语义（见 v4host/backend.go 的实测依据）：实例的 cast_port/control_port 是
// **宿主机 loopback 内部端口**，外部播放器不能直连；一律连后端所属的全局端口并在
// 握手行里带实例 id，由服务端按 id 路由。
//
// 本文件只负责拼装 go_legacy 的 tebox 启动参数并复用现有进程管理拉起，
// 不触碰 RTSP/矩阵路径的任何逻辑。

// tebox 协议默认端口（默认后端；其他后端按 gRPC 端口偏移，见 v4host.CastPortsForBackend）
const (
	TeboxDefaultCastPort  = 9901
	TeboxDefaultCtrlPort  = 9905
	TeboxDefaultAudioPort = 9904
)

// TeboxParams tebox 投屏参数（嵌入 ProjectionConfig，buildArgs 按_cast-mode 分支）。
type TeboxParams struct {
	CastMode      string // "tebox"（空/其他 = RTSP 原有路径）
	CastHost      string // tebox-api 主机
	CastID        string // 实例 id（握手行 + UDP 事件 id）
	CastPort      int    // 视频 TCP 端口（0 = 默认 9901）
	CastCtrlPort  int    // 控制 UDP 端口（0 = 默认 9905）
	CastAudioPort int    // 独立音频 TCP 端口（0 = 不连，旧部署 cast 流内 codec=16；
	//                        新部署（上游 0360f5e 拆分后）传 9904/9914… 连 TEBOX-AUDIO/1）
}

// TeboxCastRequest V4 实例投屏请求（Wails 层 → 服务层）。
//
// Key 用于投屏去重与停止：必须是「主机 IP + 后端 + 实例 id」的组合，因为不同后端
// 可以有同名实例 id，同一实例也可能在不同主机上重名。Key 为空时按
// TeboxRef(host, backend, instanceID) 生成。
type TeboxCastRequest struct {
	Host       string
	Backend    string
	InstanceID string
	// 三个端口由调用方按后端推导（v4host.CastPortsForBackend）；0 = 用默认值
	CastPort  int
	CtrlPort  int
	AudioPort int
	Title     string
}

// TeboxRef tebox 投屏的 SlotRef（key = "tebox:<host>/<backend>/<id>_0"）。
//
// 与 RTSP 的 "IP_indexNum" 语义隔离（前缀 tebox:），互不冲突；同一实例重复点击
// 命中同一 key → StartProjectionWithConfig 的「已在投屏则跳过」天然去重。
func TeboxRef(host, backend, instanceID string) SlotRef {
	return SlotRef{DeviceIP: fmt.Sprintf("tebox:%s/%s/%s", host, backend, instanceID), IndexNum: 0}
}

// StartTeboxCast 拉起 V4 实例的投屏窗口（复用现有 ProjectionWindow 进程管理：
// 日志/退出回调/自动移除）。已在投屏时直接返回（不重复开窗）。
func (s *Service) StartTeboxCast(req TeboxCastRequest) error {
	if req.Host == "" || req.InstanceID == "" {
		return fmt.Errorf("tebox cast: host and instanceID are required")
	}
	castPort := req.CastPort
	if castPort <= 0 {
		castPort = TeboxDefaultCastPort
	}
	ctrlPort := req.CtrlPort
	if ctrlPort <= 0 {
		ctrlPort = TeboxDefaultCtrlPort
	}
	title := req.Title
	if title == "" {
		title = "tebox " + req.InstanceID
	}

	cfg := ProjectionConfig{
		Title: title,
		TeboxParams: TeboxParams{
			CastMode:      "tebox",
			CastHost:      req.Host,
			CastID:        req.InstanceID,
			CastPort:      castPort,
			CastCtrlPort:  ctrlPort,
			CastAudioPort: req.AudioPort,
		},
	}
	ref := TeboxRef(req.Host, req.Backend, req.InstanceID)
	// StartProjectionWithConfig 内部已做「key 已存在则跳过」的去重（并记日志）
	if err := s.StartProjectionWithConfig(ref, cfg); err != nil {
		return err
	}
	log.Printf("[screencast] tebox projection started: key=%s host=%s id=%s cast=%d ctrl=%d audio=%d",
		ref.String(), req.Host, req.InstanceID, castPort, ctrlPort, req.AudioPort)
	return nil
}

// IsTeboxCastRunning 该 V4 实例是否已在投屏（前端按钮态/去重用）
func (s *Service) IsTeboxCastRunning(host, backend, instanceID string) bool {
	key := TeboxRef(host, backend, instanceID).String()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.projections[key]
	return ok
}

// BringTeboxCastToFront 把已开的 tebox 投屏窗口置顶（重复点击时调用）
func (s *Service) BringTeboxCastToFront(host, backend, instanceID string) {
	s.BringToFront(TeboxRef(host, backend, instanceID))
}

// StopTeboxCast 停止该实例的投屏窗口（未在投屏时无副作用）
func (s *Service) StopTeboxCast(host, backend, instanceID string) {
	s.StopProjection(TeboxRef(host, backend, instanceID))
}
