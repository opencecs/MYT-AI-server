package screencast

import (
	"fmt"
	"log"
	"sync"

	"github.com/moyunteng/myt/internal/config"
)

// Service 投屏管理服务，管理所有活跃的投屏窗口
type Service struct {
	mu          sync.Mutex
	configMgr   *config.Manager
	projections map[string]Projection
	onExit      func(key string, exitCode int, failed bool, cfg *ProjectionConfig)

	camPushExe string // WindowsPusher.exe 路径（摄像头推流，空=不启用）
	camPushMu  sync.RWMutex

	// sdkPwdFn 设备 SDK 密码查询（侧边栏上传 APK 安装走 SDK /android/exec 需
	// Basic Auth）。由 setup 注入 sdkApiSvc.GetDevicePassword，空=该设备无鉴权。
	sdkPwdFn func(deviceID string) string
	sdkPwdMu sync.RWMutex
}

func NewService(cm *config.Manager) *Service {
	return &Service{
		configMgr:   cm,
		projections: make(map[string]Projection),
	}
}

// SetSdkPasswordProvider 注入设备 SDK 密码查询函数（setup.go 挂接 sdkApiSvc）。
func (s *Service) SetSdkPasswordProvider(fn func(deviceID string) string) {
	s.sdkPwdMu.Lock()
	s.sdkPwdFn = fn
	s.sdkPwdMu.Unlock()
}

// isV2Mode 当前部署模式是否 v2（configMgr 注入即可读，无需额外接线）。
func (s *Service) isV2Mode() bool {
	return s.configMgr.GetDeployMode() == "v2"
}

func (s *Service) sdkPassword(deviceID string) string {
	s.sdkPwdMu.RLock()
	defer s.sdkPwdMu.RUnlock()
	if s.sdkPwdFn == nil {
		return ""
	}
	return s.sdkPwdFn(deviceID)
}

// SetCamPushExe 设置 WindowsPusher.exe 路径（摄像头推流，全窗口生效）
func (s *Service) SetCamPushExe(path string) {
	s.camPushMu.Lock()
	s.camPushExe = path
	s.camPushMu.Unlock()
}

// getCamPushExe 读取 WindowsPusher.exe 路径
func (s *Service) getCamPushExe() string {
	s.camPushMu.RLock()
	defer s.camPushMu.RUnlock()
	return s.camPushExe
}

// StartProjectionWithConfig 用指定配置启动投屏（公网 WebView2 模式用）
func (s *Service) StartProjectionWithConfig(ref SlotRef, cfg ProjectionConfig) error {
	key := ref.String()

	s.mu.Lock()
	if _, ok := s.projections[key]; ok {
		s.mu.Unlock()
		log.Printf("[screencast] projection already running, skipped: %s", key)
		return nil
	}
	s.mu.Unlock()

	return s.startProjectionInternal(ref, cfg)
}

func (s *Service) startProjectionInternal(ref SlotRef, cfg ProjectionConfig) error {
	key := ref.String()

	// 摄像头推流 exe 路径：调用方未显式传入时用全局设置
	if cfg.CamPushExe == "" {
		cfg.CamPushExe = s.getCamPushExe()
	}

	proj, err := NewProjectionWindow(cfg)
	if err != nil {
		return fmt.Errorf("create projection: %w", err)
	}

	s.mu.Lock()
	s.projections[key] = proj
	s.mu.Unlock()

	// 窗口退出后自动从 map 中移除
	proj.SetOnExit(func(exitCode int, failed bool, cfg *ProjectionConfig) {
		s.mu.Lock()
		delete(s.projections, key)
		s.mu.Unlock()
		log.Printf("[screencast] projection auto-removed: %s", key)
		if s.onExit != nil {
			s.onExit(key, exitCode, failed, cfg)
		}
	})

	log.Printf("[screencast] projection started: key=%s", key)
	return nil
}

// WithDeviceIdentifier 投屏窗口标题追加设备标识后缀：「名称 (标识)」。
// V3/V2（local/v2 模式）传设备 IP，公网模式传 device_id——调用方统一传
// android.IP 即可（public 下该字段存的就是 device_id，见 sdkapi_service_droid.go）。
// 标题或标识为空时原样返回。
func WithDeviceIdentifier(title, ip string) string {
	if title == "" || ip == "" {
		return title
	}
	return fmt.Sprintf("%s (%s)", title, ip)
}

// projectionBaseTitle 计算投屏窗口基础标题：备注优先，其次云机名称短名
//（ContainerDisplayName：`1793348153001_6_1` → `1`，规则对齐前端 containerDisplayName）。
func (s *Service) projectionBaseTitle(android config.AndroidInfo) string {
	if note := s.configMgr.GetAndroidNote(android.ID); note != "" {
		return note
	}
	return config.ContainerDisplayName(android.Name, android.DeviceID, android.IndexNum)
}

// StartMatrixProjectionWindow 启动矩阵投屏窗口（无边框，用于群控紧密贴合排列）
func (s *Service) StartMatrixProjectionWindow(ref SlotRef, android config.AndroidInfo, extra ProjectionConfig) error {
	key := ref.String()

	s.mu.Lock()
	if _, ok := s.projections[key]; ok {
		s.mu.Unlock()
		log.Printf("[screencast] projection already running, skipped: %s", key)
		return nil
	}

	cfg := CalcProjectionParams(android, ref.DeviceIP)
	// SDK 安装上下文补全：设备密码（CalcProjectionParams 只有 android+IP，密码经此查询）
	if cfg.SdkHost != "" && android.DeviceID != "" {
		cfg.SdkPassword = s.sdkPassword(android.DeviceID)
	}
	// V2 主机无 8000 SDK：安装命令走 81 and_api/v1/shell（播放器 sdkExec 按 V2Mode 分支）
	cfg.V2Mode = s.isV2Mode()
	// 合并额外配置
	cfg.CtrlList = extra.CtrlList
	cfg.AllCtrlList = extra.AllCtrlList
	cfg.SelfAddr = extra.SelfAddr
	cfg.Controlled = extra.Controlled
	cfg.PrimaryAddr = extra.PrimaryAddr
	cfg.Scale = extra.Scale
	cfg.Landscape = extra.Landscape
	cfg.NoSidebar = extra.NoSidebar // 批量投屏由调用方指定（侧边栏开关），单台投屏走 StartProjection* 用全局 GetNoSidebar
	cfg.GpuAccel = extra.GpuAccel   // GPU 硬件加速同上
	cfg.WinX = extra.WinX           // 批量投屏预计算网格位置，窗口一创建就在目标位置
	cfg.WinY = extra.WinY
	cfg.WinW = extra.WinW
	cfg.WinH = extra.WinH
	cfg.Fill = extra.Fill             // 画面拉伸填满（单台竖屏对齐壳子画面比例 480:798）
	cfg.NotifyPort = extra.NotifyPort // 首帧就绪通知端口：批量错峰依赖此参数（漏传 → 播放器不发信号 → 每台等满超时）
	// 摄像头推流 exe 路径：优先用调用方显式传入，否则用全局设置
	if extra.CamPushExe != "" {
		cfg.CamPushExe = extra.CamPushExe
	} else {
		cfg.CamPushExe = s.getCamPushExe()
	}
	cfg.Title = WithDeviceIdentifier(s.projectionBaseTitle(android), android.IP)
	// 矩阵投屏：主控窗口标题加前缀，便于区分角色
	if extra.Controlled && extra.SelfAddr != "" {
		isPrimary := extra.PrimaryAddr == "" || extra.PrimaryAddr == extra.SelfAddr
		if isPrimary {
			cfg.Title = "[主控] " + cfg.Title
		}
	}

	proj, err := NewProjectionWindow(cfg)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("create projection: %w", err)
	}

	s.projections[key] = proj
	s.mu.Unlock()

	proj.SetOnExit(func(exitCode int, failed bool, cfg *ProjectionConfig) {
		s.mu.Lock()
		delete(s.projections, key)
		s.mu.Unlock()
		log.Printf("[screencast] matrix projection auto-removed: %s", key)
		if s.onExit != nil {
			s.onExit(key, exitCode, failed, cfg)
		}
	})

	log.Printf("[screencast] matrix projection started: key=%s vport=%d cport=%d controlled=%v primaryAddr=%s",
		key, cfg.VideoPort, cfg.ControlPort, cfg.Controlled, cfg.PrimaryAddr)
	return nil
}

// StopProjection 停止指定投屏
func (s *Service) StopProjection(ref SlotRef) {
	key := ref.String()
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.projections[key]; ok {
		p.Stop()
		delete(s.projections, key)
	}
}

// StopAll 停止所有投屏（并行优雅退出：12 窗口同时发 WM_CLOSE+等释放，
// 最多 gracefulTimeout 全部完成，不串行等 N×timeout）。
// 保证 GPU 资源释放完，马上重开新批量不黑闪。
func (s *Service) StopAll() {
	s.mu.Lock()
	projs := s.projections
	s.projections = make(map[string]Projection)
	s.mu.Unlock()

	var wg sync.WaitGroup
	for key, p := range projs {
		wg.Add(1)
		go func(k string, proj Projection) {
			defer wg.Done()
			proj.Stop() // 优雅退出+等释放+强杀兑底
			log.Printf("[screencast] projection stopped: %s", k)
		}(key, p)
	}
	wg.Wait()
	log.Printf("[screencast] all projections stopped (%d)", len(projs))
}

// GetRunningCount 获取投屏窗口数量
func (s *Service) GetRunningCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.projections)
}

// UpdateProjectionTitle 实时更新投屏窗口标题
func (s *Service) UpdateProjectionTitle(ref SlotRef, title string) {
	key := ref.String()
	s.mu.Lock()
	p, ok := s.projections[key]
	s.mu.Unlock()

	if ok {
		p.UpdateTitle(title)
	}
}

// IsRunning 检查指定投屏是否正在运行
func (s *Service) IsRunning(ref SlotRef) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.projections[ref.String()]
	return ok
}

// ProjectionInfo 运行中的投屏（MCP screenshot_window 的容器→进程解析用）
type ProjectionInfo struct {
	Key         string // 投屏 key，格式 "ip_indexNum"
	PID         int    // go_legacy 播放器进程 PID（0 = 平台未跟踪）
	ControlPort int    // 播放器本地控制 HTTP 端口（0 = 未启用）
}

// RunningProjections 列出当前投屏（key + PID）
func (s *Service) RunningProjections() []ProjectionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ProjectionInfo, 0, len(s.projections))
	for k, p := range s.projections {
		out = append(out, ProjectionInfo{Key: k, PID: p.PID(), ControlPort: p.ControlPort()})
	}
	return out
}

// ProjectionPID 按投屏 key 查播放器进程 PID（不在投屏返回 0）
func (s *Service) ProjectionPID(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.projections[key]; ok {
		return p.PID()
	}
	return 0
}

// ProjectionControlPort 按投屏 key 查播放器本地控制端口（不在投屏返回 0）
func (s *Service) ProjectionControlPort(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.projections[key]; ok {
		return p.ControlPort()
	}
	return 0
}

// GetRunningKeys 获取所有正在运行的投屏 key（格式：ip_indexNum）
func (s *Service) GetRunningKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.projections))
	for k := range s.projections {
		keys = append(keys, k)
	}
	return keys
}

// BringToFront 将指定投屏窗口置顶
func (s *Service) BringToFront(ref SlotRef) {
	key := ref.String()
	s.mu.Lock()
	p, ok := s.projections[key]
	s.mu.Unlock()

	if ok {
		p.BringToFront()
	}
}

// SetOnProjectionExit 设置投屏窗口退出回调。
// cfg 为该投屏的启动配置（StartProjectionWithConfig/StartMatrixProjectionWindow
// 捕获），供调用方按原配置自动重启；tebox 等无 cfg 的退出传 nil。
func (s *Service) SetOnProjectionExit(fn func(key string, exitCode int, failed bool, cfg *ProjectionConfig)) {
	s.onExit = fn
}
