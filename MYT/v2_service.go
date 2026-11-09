package main

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
	"github.com/moyunteng/myt/internal/taskqueue"
	"github.com/moyunteng/myt/internal/util"
	"github.com/moyunteng/myt/internal/v2host"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// errV2DeviceNotFound 主机不在 v2 设备列表中（或 IP 为空）
var errV2DeviceNotFound = errors.New("V2 主机不存在或 IP 为空")

// errV2ScanBusy 扫描防重入错误：ScanV2Hosts / 后台扫描 / 定向状态刷新共用
// scanGuard 互斥锁，用户扫描撞上另一轮用户扫描时明确报错（此前静默返回空结果，
// 前端把"忙"当成"没有设备"）。撞上后台批次时不会走到这里——用户优先，取消
// 对方后接管（见 v2_service_guard.go / v2_service_scan.go）。
var errV2ScanBusy = errors.New("正在扫描或刷新设备状态，请稍后再试")

// V2ServiceWrapper V2 模式服务：设备数据源对接云机主机（宿主机）的 Docker Engine API（2375）。
// 发现复用本地 UDP 7678 广播（V2 主机 = MYT 设备，回复 _v2 后缀），容器数据走 Docker API。
type V2ServiceWrapper struct {
	discoverer    *discover.Discoverer
	client        *v2host.Client
	configManager *config.Manager
	app           *application.App
	// scan 扫描/状态刷新互斥守卫（v2_service_guard.go）：用户扫描优先，
	// 后台批次（模式切换扫描/定向刷新）被抢占时静默让位
	scan scanGuard

	// 心跳循环（v2_service_heartbeat.go）：常驻 UDP 单播测活，对齐壳子
	hbMu      sync.Mutex
	hbStarted bool
	hbStopCh  chan struct{}

	// tokenGetter 当前登录账号 token 来源（setup 注入 paymentSvc.GetToken，
	// 注入模式——云端插件元数据需要登录态）
	tokenGetter func() (string, error)

	// hbFailCount 心跳离线迟滞计数：IP → 连续探测失败轮数。只在心跳循环
	// 单 goroutine（heartbeatLoop → heartbeatTick）内读写，HeartbeatBatch
	// 只返回结果 map 不触碰本 map，无需加锁。设备从列表消失后条目残留无副作用
	// （下一 tick 顺带清理，见 heartbeatTick）。
	hbFailCount map[string]int

	// androidsRefresher 主机上线后触发容器列表重查（setup 注入 androidListSvc.
	// QueryDeviceByIdent）。v2 设备状态由本服务直接写（定向状态刷新/心跳，
	// 绕过 device_state_reporter），而 reporter 的「离线首报清容器缓存」对所有
	// 模式生效——启动时磁盘恢复的容器缓存被清掉后，若无此回调，在线后没人重查
	// （reporter 的 queryOnce 是 8000 通道，v2 无），表现为截图全无 + 容器显示
	// 「开机中」，要手动刷新才恢复。
	androidsRefresher func(ident string)

	// SDK 自动部署（v2_service_sdk.go）：添加设备后检测 8000，无则部署 myt-sdk-go-v2
	sdkDeployMu       sync.Mutex
	sdkDeployInFlight map[string]bool

	// sdkAuto 自动安装状态机（v2_service_sdk.go）：per-IP 记录失败探测计数/
	// 尝试轮数/放弃标记，统一自动安装触发决策（版本查询失败/心跳翻转/启动
	// 对账/添加主机四个触发点汇聚到 shouldAttempt 决策，内存态不持久化——
	// 重启客户端后重新探测，语义自洽）
	sdkAutoMu sync.Mutex
	sdkAuto   map[string]*v2SdkAutoState

	// installMu SDK 安装链路互斥锁（SdkApiServiceWrapper 的 installMu，setup
	// 注入；deployV2SdkAsync 部署期间持有，避免与手动安装同时 SSH 操作同一设备）
	installMu *sync.Mutex

	// taskSvc 任务队列服务（setup 注入）：超级 SDK 安装提交 superSdkInstall
	// 任务（v2_service_supersdk.go），执行/进度/持久化全由任务队列接管
	taskSvc *taskqueue.Service
}

func NewV2ServiceWrapper(cm *config.Manager, d *discover.Discoverer) *V2ServiceWrapper {
	return &V2ServiceWrapper{
		discoverer:        d,
		client:            v2host.NewClient(),
		configManager:     cm,
		sdkDeployInFlight: make(map[string]bool),
		sdkAuto:           make(map[string]*v2SdkAutoState),
		hbFailCount:       make(map[string]int),
	}
}

func (s *V2ServiceWrapper) SetApp(app *application.App) { s.app = app }

// SetInstallMutex 注入 SDK 安装链路的互斥锁（setup.go 注入 sdkApiSvc.installMu）
func (s *V2ServiceWrapper) SetInstallMutex(mu *sync.Mutex) { s.installMu = mu }

// OnPendingAdded 待添加主机移入正式列表后的 v2 联动（发现服务注入回调）：
// 逐台拉取容器列表（androidlist:updated 驱动前端）+ 探测刷新在线状态。
func (s *V2ServiceWrapper) OnPendingAdded(deviceIDs []string) {
	for _, id := range deviceIDs {
		deviceID := id
		util.GoSafe(func() {
			if _, err := s.RefreshV2Containers(deviceID); err != nil {
				log.Printf("[v2] 新添加主机容器拉取失败: id=%s err=%v", shortID(deviceID), err)
			}
		})
	}
	// 只对本轮新增主机做定向状态刷新（原全量刷新串行持扫描锁 N×20s，会挡住
	// 用户的扫描/手动刷新；并发核心见 v2_service_refresh.go）
	util.GoSafe(func() {
		ips := make([]string, 0, len(deviceIDs))
		for _, id := range deviceIDs {
			if dev := s.configManager.GetDevice(id); dev != nil && dev.IP != "" {
				ips = append(ips, dev.IP)
			}
		}
		s.RefreshHostStatesByIPs(ips)
	})
	// SDK 自动部署（与 AddV2Hosts 同款，v2_service_add.go）：扫描→待添加→确认
	// 入列的设备此前不触发部署，导致 8000 上永远没有 myt-sdk-go-v2。逐台按 ID
	// 取 IP 异步检测；maybeDeployV2Sdk 内部有模式门卫 + 8000 存活检查，幂等
	for _, id := range deviceIDs {
		deviceID := id
		util.GoSafe(func() {
			if dev := s.configManager.GetDevice(deviceID); dev != nil && dev.IP != "" {
				s.maybeDeployV2Sdk(dev.IP)
			}
		})
	}
}

// RemoveV2Hosts 批量移除主机（含容器缓存清理），返回移除台数。
func (s *V2ServiceWrapper) RemoveV2Hosts(deviceIDs string) int {
	if s.configManager.GetDeployMode() != "v2" {
		return 0
	}
	removed := 0
	for _, id := range splitIDs(deviceIDs) {
		s.configManager.RemoveDevice(id)
		s.configManager.CleanAndroidsByDevice(id)
		removed++
		log.Printf("[v2] 移除主机: id=%s", shortID(id))
	}
	if removed > 0 {
		s.emitDeviceList()
		s.emitAndroidList()
	}
	return removed
}

// RefreshV2Containers 拉取指定主机的容器列表 → 合并进 androidlist → 推送事件。
// 返回容器数。主机不在线/查询失败返回 error（前端提示，保留旧缓存）。
func (s *V2ServiceWrapper) RefreshV2Containers(deviceID string) (int, error) {
	dev := s.configManager.GetDevice(deviceID)
	if dev == nil || dev.IP == "" {
		return 0, errV2DeviceNotFound
	}
	return s.refreshContainersFor(dev.DeviceID, dev.IP)
}

// RefreshV2ContainersByIP 按 IP 拉容器列表（添加主机时设备 ID 可能刚入列，
// 直接以 IP 为锚，容器归属的 DeviceID 按列表中该 IP 的设备解析）。
func (s *V2ServiceWrapper) RefreshV2ContainersByIP(ip string) (int, error) {
	deviceID := s.deviceIDByIP(ip)
	if deviceID == "" {
		return 0, errV2DeviceNotFound
	}
	return s.refreshContainersFor(deviceID, ip)
}

// refreshContainersFor 拉取并合并容器列表（DeviceID + IP 已确定）。
// 走 81 /dc_api/v1/list（壳子同款通道，不依赖 2375）。
// 81 拉取成功本身即证明超级 SDK 就绪，顺带落就绪状态（防添加路径误标未就绪
// 后一直残留）。
func (s *V2ServiceWrapper) refreshContainersFor(deviceID, ip string) (int, error) {
	androids, err := s.listV2Androids(deviceID, ip)
	if err != nil {
		return 0, err
	}
	if deviceID != "" {
		s.configManager.UpdateDeviceSuperSdkReady(deviceID, true)
	}
	s.configManager.MergeAndroidList([]config.DeviceAndroidResult{
		{DeviceID: deviceID, Androids: androids},
	}, time.Now(), map[string]string{deviceID: ip})
	s.emitAndroidList()
	log.Printf("[v2] 容器列表刷新: host=%s ip=%s count=%d", shortID(deviceID), ip, len(androids))
	return len(androids), nil
}

// listV2Androids 81 通道容器列表 → AndroidInfo（mytsdk 加工视图：
// index/state 现成）。81 不返回 NAT 端口明细表，用 dc 字段填充关键映射——
// 实测规律（153 容器 _4_2-222: 9082→10014、10000→10012、10001→10013；
// 2.46 dc list _1: ctr=10004、api=10005；_2: ctr=10007、api=10008）：
//   - 9082 (MYTOS API) = api_port
//   - 10001 (控制 UDP) = ctr_port
//   - 10000 (RTSP 视频) = api_port - 2（= ctr_port - 1，每容器占 3 连号）
//   - 5555 (ADB)       = adb
//
// 截图/投屏/控制据此直连；字段缺失时公式兜底（V2 NAT 下公式不准，仅应急）。
func (s *V2ServiceWrapper) listV2Androids(deviceID, ip string) ([]config.AndroidInfo, error) {
	entries, err := s.client.ListContainersViaSDK(context.Background(), ip)
	if err != nil {
		return nil, err
	}
	androids := make([]config.AndroidInfo, 0, len(entries))
	for _, e := range entries {
		if e.Index <= 0 {
			continue
		}
		a := config.AndroidInfo{
			ID:       e.ID,
			Name:     e.Name,
			Status:   e.State,
			Image:    e.Image,
			IndexNum: e.Index,
			DeviceID: deviceID,
			IP:       ip,
		}
		if e.APIPort > 0 || e.CtrPort > 0 || e.Adb > 0 {
			a.PortBindings = map[string][]config.PortBinding{}
			put := func(key string, port int) {
				a.PortBindings[key] = []config.PortBinding{{HostIP: "0.0.0.0", HostPort: strconv.Itoa(port)}}
			}
			if e.APIPort > 0 {
				put("9082/tcp", e.APIPort)
				if e.APIPort > 2 {
					put("10000/tcp", e.APIPort-2) // RTSP 视频（投屏）
				}
			}
			if e.CtrPort > 0 {
				put("10001/udp", e.CtrPort) // 控制
			}
			if e.Adb > 0 {
				put("5555/tcp", e.Adb)
			}
		}
		androids = append(androids, a)
	}
	return androids, nil
}

// deviceIDByIP 在 v2 设备列表中按 IP 找设备 ID
func (s *V2ServiceWrapper) deviceIDByIP(ip string) string {
	for _, d := range s.configManager.GetDeviceList() {
		if d.IP == ip {
			return d.DeviceID
		}
	}
	return ""
}

// SetAndroidsRefresher 注入主机上线后的容器列表刷新回调（setup 装配，见字段注释）。
func (s *V2ServiceWrapper) SetAndroidsRefresher(fn func(ident string)) {
	s.androidsRefresher = fn
}

// RefreshHostStatesByIPs（在线状态定向刷新）移至 v2_service_refresh.go：
// 8-worker 并发探测，接入 scanGuard（用户扫描优先，后台批次静默让位）。
// 原全量 RefreshHostStates 已删除（零调用方，被 OnPendingAdded 的定向刷新取代）。

// ListAndroidsForDevice 供 androidlist.Service 注入的 v2 查询回调：
// 查容器列表并转为 AndroidInfo（不含事件推送，由 androidlist 统一通知）。
// 走 81 /dc_api/v1/list（壳子同款通道，不依赖 2375——主机防火墙对 Docker 端口
// 间歇拦截时列表照常工作；mytsdk 加工视图：index/state 现成，端口公式兜底）。
// ip 参数兼容序列号（androidlist 内部标识可能为序列号——resolveDevice 兜底解析）。
func (s *V2ServiceWrapper) ListAndroidsForDevice(deviceID, ip string) ([]config.AndroidInfo, error) {
	if dev := s.configManager.GetDeviceByIP(ip); dev == nil {
		// ip 不是已知主机的 IP——可能传的是序列号，解析为真实 IP
		if dev := s.configManager.GetDevice(deviceID); dev != nil && dev.IP != "" {
			ip = dev.IP
		} else if dev := s.configManager.GetDevice(ip); dev != nil && dev.IP != "" {
			ip = dev.IP
		}
	}
	androids, err := s.listV2Androids(deviceID, ip)
	if err != nil {
		return nil, err
	}
	// 伴随动作：顺带拉主机系统信息刷新设备行磁盘/内存条（失败静默）
	s.attachSystemInfo(ip)
	return androids, nil
}

// --- 内部工具 ---

func (s *V2ServiceWrapper) emitDeviceList() {
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
	}
}

func (s *V2ServiceWrapper) emitAndroidList() {
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("androidlist:updated", s.configManager.GetAndroidList())
	}
}

// splitIDs 解析逗号分隔的 ID 列表（去空白去重）
func splitIDs(input string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, part := range strings.Split(input, ",") {
		id := strings.TrimSpace(part)
		if id != "" && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

// shortID 日志用短 ID
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
