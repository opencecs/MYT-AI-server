package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/screencast"
	"github.com/moyunteng/myt/internal/util"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// BatchProjectionItem 批量投屏参数
type BatchProjectionItem struct {
	DeviceIP    string `json:"deviceIP"`
	IndexNum    int    `json:"indexNum"`
	Orientation string `json:"orientation"` // "portrait" | "landscape" | ""
}

// ScreencastServiceWrapper Wails 服务包装器，暴露投屏功能给前端
type ScreencastServiceWrapper struct {
	svc       *screencast.Service
	cm        *config.Manager
	app       *application.App
	publicMgr *screencast.PublicScreencastMgr

	mirrorSvc *MirrorServiceWrapper // 用于按镜像特质版判断（特质版=真机镜像，D3D11VA 解出黑帧，强制 CPU 软解）

	accessibilityChecked bool
	accessibilityMu      sync.Mutex

	// 公网投屏会话注册表（端口过期双保险刷新，详见 screencast_service_public_session.go）
	publicMu       sync.Mutex
	publicSessions map[string]*publicSession
	refresherStop  chan struct{}

	// v2/local 异常退出自动重启限流（60s 内每 key 最多 2 次，防重启风暴）
	localRestartMu  sync.Mutex
	localRestartLog map[string][]time.Time
}

func NewScreencastServiceWrapper(cm *config.Manager) *ScreencastServiceWrapper {
	svc := screencast.NewService(cm)
	// 摄像头推流：ffmpeg.exe 可用时全局启用
	if camFFmpeg, err := ensureFFmpegExe(); err == nil && camFFmpeg != "" {
		svc.SetCamPushExe(camFFmpeg)
		log.Printf("[screencast] camera push enabled globally: %s", camFFmpeg)
	}
	return &ScreencastServiceWrapper{
		svc:             svc,
		cm:              cm,
		publicSessions:  make(map[string]*publicSession),
		localRestartLog: make(map[string][]time.Time),
	}
}

// SetMirrorService 注入镜像服务（用于按镜像特质版判断真机镜像）。
func (s *ScreencastServiceWrapper) SetMirrorService(ms *MirrorServiceWrapper) {
	s.mirrorSvc = ms
}

// SetSdkPasswordProvider 注入设备 SDK 密码查询（侧边栏上传 APK 安装走 SDK
// /android/exec 需 Basic Auth；由 setup.go 挂接 sdkApiSvc.GetDevicePassword）。
func (s *ScreencastServiceWrapper) SetSdkPasswordProvider(fn func(deviceID string) string) {
	s.svc.SetSdkPasswordProvider(fn)
}

// isSpecialMirror 判断 image 是否为特质版镜像（真机镜像）。
// 特质版镜像经 D3D11VA 硬解会解出黑帧（Y≈0）导致 GPU 闪屏，CPU 软解正常，
// 故特质版强制走 CPU 软解。镜像元数据 sys_ver_des=="特质版" 即特质版（与前端 isSpecialMirror 对齐）。
// 查不到元数据（客户端未缓存该镜像）则按非特质版处理（保持硬解，靠 player 黑帧回退兜底）。
func (s *ScreencastServiceWrapper) isSpecialMirror(image string) bool {
	if s.mirrorSvc == nil || image == "" {
		return false
	}
	for _, m := range s.mirrorSvc.GetCachedMirrors() {
		if m.URL == image {
			return m.SysVerDes == "特质版"
		}
	}
	return false
}

// gpuAccelFor 计算指定镜像的 GPU 硬解开关：全局开关开启且非特质版镜像时才用硬解。
// 特质版（真机镜像）D3D11VA 硬解会闪黑屏，强制 CPU 软解。
func (s *ScreencastServiceWrapper) gpuAccelFor(image string) bool {
	return s.cm.GetGpuAccel() && !s.isSpecialMirror(image)
}

// SetPublicCredentialsFn 注入魔云腾账号密码（保留兼容，实际鉴权改用 token）
func (s *ScreencastServiceWrapper) SetPublicCredentialsFn(fn func() (string, string)) {
	s.publicMgr = screencast.NewPublicScreencastMgr(fn, nil)
}

// SetTokenFn 注入授权 token 获取函数（由 setup.go 注入 paymentSvc.GetToken）。
// 网关侧已统一接受 moyunteng.com 授权 token，公网投屏不再调用 /api/ai/login。
func (s *ScreencastServiceWrapper) SetTokenFn(fn func() (string, error)) {
	if s.publicMgr == nil {
		s.publicMgr = screencast.NewPublicScreencastMgr(func() (string, string) { return "", "" }, fn)
		return
	}
	s.publicMgr.SetTokenFn(fn)
}

// SetApp 设置应用实例（用于投屏退出事件推送）
func (s *ScreencastServiceWrapper) SetApp(app *application.App) {
	s.app = app
	// 投屏窗口退出时，通知前端
	// 公网窗口退出：
	// - 正常关闭（用户关窗）→ 注销会话
	// - 异常退出（崩溃等）→ 被动保险：重拉端口自动重启
	// - 刷新后 15s 内的退出是"预期退出"（进程替换），忽略
	s.svc.SetOnProjectionExit(func(key string, exitCode int, failed bool, cfg *screencast.ProjectionConfig) {
		// 正常关窗（exitCode==0）：elapsed<5s 的干净退出是用户主动关闭而非崩溃，
		// 不能触发公网自动重启（否则快速关窗会立即重开僵尸窗口），且必须注销会话
		// （否则端口刷新器到期时会把窗口拉起来）。真正异常退出（非 0 退出码）才重启。
		cleanExit := exitCode == 0
		s.publicMu.Lock()
		sess, isPublic := s.publicSessions[key]
		suppress := sess != nil && time.Since(sess.refreshAt) < 15*time.Second
		if isPublic && !suppress && (!failed || cleanExit) {
			delete(s.publicSessions, key)
		}
		s.publicMu.Unlock()

		if isPublic && failed && !cleanExit && !suppress {
			log.Printf("[screencast] 公网投屏 %s 异常退出 (code=%d)，尝试自动重启", key, exitCode)
			go s.refreshPublicSessionByKey(key)
		}

		// v2/local 模式：异常退出（failed = 启动后 5s 内非正常退出，播放器崩溃/异常）
		// 按原配置自动重启。公网无次数限制（会话注销后端口刷新器兜底），本地直接
		// 重启进程，加次数限制防「端口不通/设备不可达」时无限重启风暴：
		// 60s 内最多 2 次，超限则放弃并推送退出事件（前端标记失败）。
		// 排除 WebURL（公网专有，会话注册前的竞态窗口）与 tebox（V4 投屏，
		// failed 语义=后端端口不通/实例关闭，属预期错误不该重开窗口）。
		if !isPublic && failed && !cleanExit && cfg != nil &&
			cfg.WebURL == "" && cfg.CastMode != "tebox" {
			if s.allowLocalRestart(key) {
				log.Printf("[screencast] 本地投屏 %s 异常退出 (code=%d)，自动重启", key, exitCode)
				go func(c screencast.ProjectionConfig, k string) {
					time.Sleep(500 * time.Millisecond) // 让旧进程资源（端口/GPU）释放完
					ref, ok := slotRefFromKey(k)
					if !ok {
						log.Printf("[screencast] 本地投屏 %s 自动重启失败: 无法解析投屏标识", k)
						return
					}
					if err := s.svc.StartProjectionWithConfig(ref, c); err != nil {
						log.Printf("[screencast] 本地投屏 %s 自动重启失败: %v", k, err)
					}
				}(*cfg, key)
				return
			}
			log.Printf("[screencast] 本地投屏 %s 异常退出 (code=%d)，60s 内重启次数已达上限，放弃重启", key, exitCode)
		}

		if s.app != nil && s.app.Event != nil {
			s.app.Event.Emit("screencast:exited", map[string]any{
				"key":    key,
				"failed": failed,
			})
		}
	})
}

// slotRefFromKey 从投屏 key（"ip_indexNum"，SlotRef.String 格式）解析回 SlotRef。
// 本地模式 DeviceIP 为 IPv4/主机名（不含下划线），LastIndex('_') 分割安全。
func slotRefFromKey(key string) (screencast.SlotRef, bool) {
	i := strings.LastIndex(key, "_")
	if i <= 0 || i == len(key)-1 {
		return screencast.SlotRef{}, false
	}
	n, err := strconv.Atoi(key[i+1:])
	if err != nil {
		return screencast.SlotRef{}, false
	}
	return screencast.SlotRef{DeviceIP: key[:i], IndexNum: n}, true
}

// allowLocalRestart v2/local 异常退出自动重启的次数限流：60s 滑动窗口内
// 每 key 最多 2 次。公网路径无此限制（会话注销 + 端口刷新器兜底，重启由
// refreshPublicSessionByKey 幂等管理），本地直接拉进程，必须自己防风暴：
// 设备不可达/端口不通时无限重启会打爆设备与 UI。
func (s *ScreencastServiceWrapper) allowLocalRestart(key string) bool {
	s.localRestartMu.Lock()
	defer s.localRestartMu.Unlock()
	now := time.Now()
	logs := s.localRestartLog[key]
	kept := logs[:0]
	for _, t := range logs {
		if now.Sub(t) < 60*time.Second {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 2 {
		s.localRestartLog[key] = kept
		return false
	}
	s.localRestartLog[key] = append(kept, now)
	return true
}

// StopProjection 停止指定投屏
func (s *ScreencastServiceWrapper) StopProjection(deviceIP string, indexNum int) {
	deviceIP = s.resolveSlotDeviceIP(deviceIP)
	if util.DebugEnabled {
		log.Printf("[screencast:DEBUG] StopProjection: deviceIP=%s indexNum=%d", deviceIP, indexNum)
	}
	// 公网模式：先注销会话（防退出事件触发自动重启）。
	// RTSP 端口组在网关自动过期（600s），无需主动释放
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		s.publicMu.Lock()
		delete(s.publicSessions, fmt.Sprintf("%s_%d", deviceIP, indexNum))
		s.publicMu.Unlock()
	}
	s.svc.StopProjection(screencast.SlotRef{DeviceIP: deviceIP, IndexNum: indexNum})
}

// StopAllProjections 停止所有投屏
func (s *ScreencastServiceWrapper) StopAllProjections() {
	// 公网模式：先清空会话注册表（防停止过程中触发自动重启）。
	// RTSP 端口组在网关自动过期（600s），无需主动释放
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		s.clearPublicSessions()
	}
	s.svc.StopAll()
}

// LaunchTeboxCast tebox 投屏手动测试入口（联调用，需自带全部参数）：
// 给定 tebox-api 主机、实例 id、视频 TCP 端口与控制 UDP 端口，拉起 go_legacy tebox 模式。
// 端口传 0 时用默认值（视频 9901 / 控制 9905）。
// audioPort 独立音频 TCP 端口：0=不连（旧部署，音频走 cast 流内 codec=16）；
// 新部署（上游 0360f5e 拆分后）传 9904（默认后端）/9914（第二套栈）等。
//
// 正常 UI 路径请用 LaunchV4Cast（自动按后端推导端口 + 去重/置顶）。
func (s *ScreencastServiceWrapper) LaunchTeboxCast(host string, instanceID string, castPort, ctrlPort, audioPort int) error {
	return s.svc.StartTeboxCast(screencast.TeboxCastRequest{
		Host:       host,
		InstanceID: instanceID,
		CastPort:   castPort,
		CtrlPort:   ctrlPort,
		AudioPort:  audioPort,
	})
}

// LaunchV4Cast 拉起 V4（tobox_player）实例的投屏窗口——「云机」TAB 的投屏按钮走这里。
//
// host 为主机 IP（可为 ip:8088）；backend 为实例所属 tebox 后端名（GET /android 的
// backend 字段，可空=默认后端）；instanceID 为实例 id（GET /android 的 id）。
//
// 端口不再由前端传：实例的 cast_port/control_port 是宿主机 loopback 内部端口，
// 外部播放器必须连该后端所属的**全局**媒体端口（9901/9904/9905 及其偏移，依据见
// internal/v4host/backend.go 的实测注释），由 Go 侧按 backend 查 /backend/list 推导。
//
// 去重：同一实例重复点击不会开第二个窗口——已在投屏则置顶并返回。
func (s *ScreencastServiceWrapper) LaunchV4Cast(host string, backend string, instanceID string, title string) error {
	ip := v4HostIP(host)
	if ip == "" || instanceID == "" {
		return fmt.Errorf("投屏失败：主机或实例标识为空")
	}
	ports := newV4InstanceClient(ip).ResolveCastPorts(context.Background(), backend)
	if err := s.svc.StartTeboxCast(screencast.TeboxCastRequest{
		Host:       ip,
		Backend:    backend,
		InstanceID: instanceID,
		CastPort:   ports.Video,
		CtrlPort:   ports.Ctrl,
		AudioPort:  ports.Audio,
		Title:      title,
	}); err != nil {
		return err
	}
	// 已在投屏（去重命中）时把窗口提到前台，避免用户以为「点了没反应」
	s.svc.BringTeboxCastToFront(ip, backend, instanceID)
	return nil
}

// IsV4CastRunning 该 V4 实例当前是否在投屏（前端按钮态用）
func (s *ScreencastServiceWrapper) IsV4CastRunning(host string, backend string, instanceID string) bool {
	return s.svc.IsTeboxCastRunning(v4HostIP(host), backend, instanceID)
}

// StopV4Cast 停止该 V4 实例的投屏窗口（未在投屏时无副作用）
func (s *ScreencastServiceWrapper) StopV4Cast(host string, backend string, instanceID string) {
	s.svc.StopTeboxCast(v4HostIP(host), backend, instanceID)
}

// ArrangeMatrixWindows 按指定缩放比例重新排列当前矩阵投屏窗口。
func (s *ScreencastServiceWrapper) ArrangeMatrixWindows(scale float64) {
	if scale < 0.3 {
		scale = 0.3
	}
	if scale > 1.0 {
		scale = 1.0
	}
	screencast.ArrangePlayerWindows(scale, true)
}

// GetProjectionCount 获取当前投屏窗口数量
// ScreencastRunningKeys 当前投屏 key 列表（MCP screenshot_window 报错引导用）
func (s *ScreencastServiceWrapper) ScreencastRunningKeys() []string {
	return s.svc.GetRunningKeys()
}

// ScreencastProjectionPID 投屏 key → go_legacy 播放器进程 PID（不在投屏返回 0）
func (s *ScreencastServiceWrapper) ScreencastProjectionPID(key string) int {
	return s.svc.ProjectionPID(key)
}

// ScreencastProjectionControlPort 投屏 key → 播放器本地控制 HTTP 端口
// （不在投屏/未启用返回 0）
func (s *ScreencastServiceWrapper) ScreencastProjectionControlPort(key string) int {
	return s.svc.ProjectionControlPort(key)
}

func (s *ScreencastServiceWrapper) GetProjectionCount() int {
	return s.svc.GetRunningCount()
}

// UpdateProjectionTitle 实时更新投屏窗口标题（前端更改/清空备注时调用）。
// 旧签名把裸标题挂标识后缀；V4 路径换传实例 ID + 备注，由 Go 端统一取 android、
// 套「备注优先 → 短名」规则（清备注时回退 ContainerDisplayName，与启动标题一致）。
func (s *ScreencastServiceWrapper) UpdateProjectionTitle(deviceIP string, indexNum int, title string) {
	ref := screencast.SlotRef{DeviceIP: s.resolveSlotDeviceIP(deviceIP), IndexNum: indexNum}
	if a := s.findRunningAndroid(ref); a != nil {
		base := title
		if note := s.cm.GetAndroidNote(a.ID); note != "" {
			base = note
		}
		title = screencast.WithDeviceIdentifier(config.ContainerDisplayName(base, a.DeviceID, a.IndexNum), a.IP)
	}
	s.svc.UpdateProjectionTitle(ref, title)
}

// IsProjectionRunning 检查指定投屏是否正在运行
func (s *ScreencastServiceWrapper) IsProjectionRunning(deviceIP string, indexNum int) bool {
	return s.svc.IsRunning(screencast.SlotRef{DeviceIP: s.resolveSlotDeviceIP(deviceIP), IndexNum: indexNum})
}

// BringProjectionToFront 将指定投屏窗口置顶
func (s *ScreencastServiceWrapper) BringProjectionToFront(deviceIP string, indexNum int) {
	s.svc.BringToFront(screencast.SlotRef{DeviceIP: s.resolveSlotDeviceIP(deviceIP), IndexNum: indexNum})
}

// GetRunningProjectionKeys 获取所有正在运行的投屏 key
func (s *ScreencastServiceWrapper) GetRunningProjectionKeys() []string {
	return s.svc.GetRunningKeys()
}

// GetNoSidebar 获取投屏侧边栏开关状态
func (s *ScreencastServiceWrapper) GetNoSidebar() bool {
	return s.cm.GetNoSidebar()
}

// SetNoSidebar 设置投屏侧边栏开关（关闭后新开的投屏窗口不启动 WebView2，省 ~80MB/窗口）
func (s *ScreencastServiceWrapper) SetNoSidebar(enabled bool) error {
	return s.cm.SetNoSidebar(enabled)
}

// GetGpuAccel 获取投屏 GPU 硬件加速开关状态
func (s *ScreencastServiceWrapper) GetGpuAccel() bool {
	return s.cm.GetGpuAccel()
}

// SetGpuAccel 设置投屏 GPU 硬件加速开关（关闭后新开投屏走 CPU 软解，兼容性更好但 CPU 占用高）
func (s *ScreencastServiceWrapper) SetGpuAccel(enabled bool) error {
	return s.cm.SetGpuAccel(enabled)
}

// GetSingleCastScale 获取单台投屏窗口大小（倍率，1.0=原生布局，前端滑条 50%~100%）
func (s *ScreencastServiceWrapper) GetSingleCastScale() float64 {
	return s.cm.GetSingleCastScale()
}

// SetSingleCastScale 设置单台投屏窗口大小并持久化（下次单台投屏生效）
func (s *ScreencastServiceWrapper) SetSingleCastScale(scale float64) error {
	return s.cm.SetSingleCastScale(scale)
}

// resolveSlotDeviceIP 把前端传的设备标识规范化为真实主机 IP。
// v2 模式前端 getDeviceId 传序列号（device_id），本地模式传 IP；投屏全链路
// （running 索引/SlotRef/窗口管理）都以 IP 为键，入口必须先解析，否则
// v2 下「未找到容器/未开机」恒现。
func (s *ScreencastServiceWrapper) resolveSlotDeviceIP(deviceIP string) string {
	if dev := s.cm.GetDevice(deviceIP); dev != nil && dev.IP != "" {
		return dev.IP
	}
	if dev := s.cm.GetDeviceByIP(deviceIP); dev != nil && dev.IP != "" {
		return dev.IP
	}
	return deviceIP
}

// normalizeItems 规范化批量条目的 DeviceIP（批量投屏/矩阵入口共用）
func (s *ScreencastServiceWrapper) normalizeItems(items []BatchProjectionItem) []BatchProjectionItem {
	out := make([]BatchProjectionItem, len(items))
	for i, it := range items {
		it.DeviceIP = s.resolveSlotDeviceIP(it.DeviceIP)
		out[i] = it
	}
	return out
}

// findRunningAndroid 从运行列表中查找安卓实例。
// GetRunningAndroids 只取一次快照（单次 O(n)），避免循环内每轮重取造成 O(n²)
// 及两次快照内容不一致的风险。
func (s *ScreencastServiceWrapper) findRunningAndroid(ref screencast.SlotRef) *config.AndroidInfo {
	androids := s.cm.GetRunningAndroids()
	for i := range androids {
		a := androids[i]
		if a.IP == ref.DeviceIP && a.IndexNum == ref.IndexNum {
			return &a
		}
	}
	return nil
}

// buildRunningAndroidIndex 一次性构建运行安卓索引 map，避免批量查询时重复遍历
func (s *ScreencastServiceWrapper) buildRunningAndroidIndex() map[string]config.AndroidInfo {
	idx := make(map[string]config.AndroidInfo)
	for _, a := range s.cm.GetRunningAndroids() {
		key := fmt.Sprintf("%s_%d", a.IP, a.IndexNum)
		idx[key] = a
	}
	return idx
}

// existsInAllList 检查安卓是否存在于全部列表中（未开机）
func (s *ScreencastServiceWrapper) existsInAllList(ref screencast.SlotRef) bool {
	for i := range s.cm.GetAndroidList() {
		a := s.cm.GetAndroidList()[i]
		if a.IP == ref.DeviceIP && a.IndexNum == ref.IndexNum {
			return true
		}
	}
	return false
}

// checkAccessibilityOnce 首次投屏时检查辅助功能权限，未授权则提示并打开设置
func (s *ScreencastServiceWrapper) checkAccessibilityOnce() {
	s.accessibilityMu.Lock()
	if s.accessibilityChecked {
		s.accessibilityMu.Unlock()
		return
	}
	s.accessibilityChecked = true
	s.accessibilityMu.Unlock()

	if !screencast.CheckAccessibilityPermission() {
		log.Printf("[screencast] 辅助功能权限未授权，投屏窗口标题更新将不可用")
		screencast.OpenAccessibilitySettings()
	}
}

// orientationToLandscape 将前端 orientation 字符串转为 Landscape bool
func orientationToLandscape(orientation string) bool {
	return orientation == "landscape"
}
