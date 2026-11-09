package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/cloudapi"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/proxy"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/sdkver"
	"github.com/moyunteng/myt/internal/taskqueue"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// publicSnapHTTP 截图专用 HTTP 客户端（10 秒超时，不共享连接池）
var publicSnapHTTP = &http.Client{
	Timeout: 10 * time.Second,
}

type SdkApiServiceWrapper struct {
	proxySvc           *proxy.Service
	mirrorSvc          *MirrorServiceWrapper
	iscsiSvc           *IscsiServiceWrapper // iSCSI 无盘服务（任务队列用）
	devicePasswords    map[string]string    // deviceID → password
	ipToDeviceID       map[string]string    // IP → deviceID 反向映射
	passwordsMu        sync.RWMutex
	cm                 *config.Manager
	app                *application.App
	onAndroidsChanged  func(deviceIPs []string)        // 云机变更后回调，触发 androidlist 即时刷新
	onStructureChanged func(deviceIPs []string)        // 结构变更回调（move/rename：无 WS 事件源，local 也须查询）
	onSdkExpired       func(deviceID, deviceIP string) // 版本查询成功且 < MinWSCapableSDK：老 SDK 设备（无 WS 端点）HTTP 可达——setup 注入即时恢复

	mytAgentSvc *MytAgentService // public 模式提供魔云腾账号给网关登录
	cloudClient *cloudapi.Client // public 模式网关客户端（复用 access_token）
	sdkVer      *sdkver.Client   // 本地 SDK 版本（SDK 升级状态判定用，setup 注入）

	// v2 模式设备信息回调（setup 注入：Docker 主机无 8000 SDK API，
	// GetDeviceInfo 转发到 myt_sdk 容器 :81/systeminfo）
	v2DeviceInfoGetter   func(ip string) (*sdkapi.DeviceInfo, error)
	v2AutoInstallTrigger func(ip, trigger string)
	v2AutoInstallReset   func(ip string)

	// v2 模式 SDK 升级回调（setup 注入：超级 SDK 插件更新，需云端元数据）
	v2UpgradeFn func(deviceIP string) (bool, error)

	// 云端最新 SDK 版本缓存（SdkCheckUpgrade 用，10 分钟节流防 UI 打公网）
	cloudVerMu sync.Mutex
	// installMu 安装防重入：同一时间只允许一个 sdkInstall 在跑（V3/v2 共用，
	// 与 SdkUpgradeDevice 的 deviceInstalled 语义互补——安装走队列，用户重复
	// 点击/多设备任务共享同一下载与确认窗口）
	installMu  sync.Mutex
	cloudVer   int
	cloudVerAt time.Time
	cloudDesc  string // 与 cloudVer 同缓存的云端版本描述（升级确认弹窗展示用）

	// 任务队列服务（SubmitSdkInstallTask 提交 sdkInstall 批量任务，setup 注入）
	taskSvc *taskqueue.Service

	// v4Idx V4 实例序号派生器（与 v4ws 适配层共享同一份，保证 REST 与 WS 两条
	// 写入路径的截图 key（IP_indexNum）一致；setup 注入，nil 时按需自建）
	v4Idx *v4Indexer

	// v4Clip V4 剪贴板防回声记录（本机→云机主动设置的最近文本，按主机+实例键控；
	// WS 回灌路径用它忽略自己刚设置又被推送回来的文本，见 v4_clipboard.go）
	v4Clip *v4ClipboardEcho

	cancelOps   map[string]context.CancelFunc // taskID → 取消函数（长任务取消）
	cancelOpsMu sync.Mutex
}

func NewSdkApiServiceWrapper(proxySvc *proxy.Service) *SdkApiServiceWrapper {
	return &SdkApiServiceWrapper{
		proxySvc:        proxySvc,
		devicePasswords: make(map[string]string),
		ipToDeviceID:    make(map[string]string),
		cancelOps:       make(map[string]context.CancelFunc),
		v4Idx:           newV4Indexer(), // 未注入时的兜底（setup 会注入共享实例）
	}
}

func (s *SdkApiServiceWrapper) SetMirrorService(ms *MirrorServiceWrapper) { s.mirrorSvc = ms }

// SetSdkVerClient 注入 SDK 版本客户端（SDK 升级状态判定用，setup.go 创建）
func (s *SdkApiServiceWrapper) SetSdkVerClient(c *sdkver.Client) { s.sdkVer = c }

// MinWSCapableSDK SDK 过期阈值：低于此版本的设备无 /ws/events 端点（WS 404）。
// 必须与前端 useSdkVersions.ts 的 MIN_SDK_VERSION 保持一致（改则同步）。
const MinWSCapableSDK = 208

// SetOnSdkExpired 注入「SDK 过期设备 HTTP 可达」钩子（setup.go 实现：离线老 SDK
// 设备立即恢复在线）。触发条件是**过期判定**而非在线判定：仅版本 < MinWSCapableSDK
// 的设备——老 SDK 没有 WS 端点，在线性只能以 HTTP 可达为准；版本达标设备的
// 在线性由 WS/探测链路自管，前端查询不顺手抢答。
func (s *SdkApiServiceWrapper) SetOnSdkExpired(fn func(deviceID, deviceIP string)) {
	s.onSdkExpired = fn
}

// notifySdkExpired 版本查询成功后做过期判定：cur < MinWSCapableSDK = 老 SDK 设备
// 且 HTTP 刚刚可达（查询本身就是证据）——通知 setup 即时恢复，不等 30s 兜底。
func (s *SdkApiServiceWrapper) notifySdkExpired(deviceIP string, cur int) {
	if s.onSdkExpired == nil || deviceIP == "" || cur <= 0 || cur >= MinWSCapableSDK {
		return
	}
	if deviceID := s.GetDeviceIDByIP(deviceIP); deviceID != "" {
		s.onSdkExpired(deviceID, deviceIP)
	}
}

// SetIscsiService 注入 iSCSI 服务（供任务队列调用挂载/卸载）
func (s *SdkApiServiceWrapper) SetIscsiService(is *IscsiServiceWrapper) { s.iscsiSvc = is }

// SetTaskQueueService 注入任务队列服务（SubmitSdkInstallTask 提交批量安装任务）
func (s *SdkApiServiceWrapper) SetTaskQueueService(ts *taskqueue.Service) { s.taskSvc = ts }

// MountIscsi 无盘挂载（任务队列接口，返回 success/message/error）
func (s *SdkApiServiceWrapper) MountIscsi(deviceIP, portal, iqn string, autoInit bool, localIP string) (bool, string, error) {
	if s.iscsiSvc == nil {
		return false, "iSCSI 服务未初始化", fmt.Errorf("iscsiSvc is nil")
	}
	result, err := s.iscsiSvc.MountIscsi(deviceIP, portal, iqn, autoInit, localIP)
	if err != nil {
		return false, err.Error(), err
	}
	if result == nil || !result.Success {
		// 注意：不能用 result.StorageMode 当错误信息——那是 "local"/"iscsi" 模式值，对用户无意义
		return false, "挂载失败", fmt.Errorf("挂载失败")
	}
	return true, "挂载成功", nil
}

// UmountIscsi 无盘卸载（任务队列接口，返回 success/message/error）
func (s *SdkApiServiceWrapper) UmountIscsi(deviceIP, portal, iqn string, force bool) (bool, string, error) {
	if s.iscsiSvc == nil {
		return false, "iSCSI 服务未初始化", fmt.Errorf("iscsiSvc is nil")
	}
	result, err := s.iscsiSvc.UmountIscsi(deviceIP, portal, iqn, force)
	if err != nil {
		return false, err.Error(), err
	}
	if result == nil || !result.Success {
		return false, "卸载失败", fmt.Errorf("卸载失败")
	}
	return true, "卸载成功", nil
}

// IscsiDeviceState 返回设备 iSCSI 原始状态（有无 session / 是否已挂载），供任务队列进度推断。
// 用 5s 超时 client 避免轮询堆积；查询失败按"无 session 未挂载"处理（不阻塞进度推进）。
func (s *SdkApiServiceWrapper) IscsiDeviceState(deviceIP string) (hasSession, isMounted bool) {
	client := s.newClientWithTimeout(deviceIP, 5*time.Second)
	sessions, _ := client.ListIscsi()
	hasSession = len(sessions) > 0
	status, _ := s.iscsiSvc.GetIscsiStatus(deviceIP)
	if status != nil {
		isMounted = status.IsMounted
	}
	return
}

// SetMytAgentService 注入 MytAgentService（public 模式提供 session_token）
func (s *SdkApiServiceWrapper) SetMytAgentService(svc *MytAgentService) { s.mytAgentSvc = svc }

// CloudClient 返回共享的网关客户端（供 discover 等服务复用，避免重复登录）。
func (s *SdkApiServiceWrapper) CloudClient() *cloudapi.Client {
	if s.cloudClient == nil {
		s.cloudClient = cloudapi.NewClient()
	}
	return s.cloudClient
}

// EnsureCloudLogin 确保网关客户端已配置 token 来源（供 discover 等服务复用）。
// 网关接受授权 token，无需主动登录；token 由注入的 tokenFn（payment.GetToken）提供。
func (s *SdkApiServiceWrapper) EnsureCloudLogin() error {
	if s.cloudClient == nil {
		s.cloudClient = cloudapi.NewClient()
	}
	if s.cloudClient.IsLoggedIn() {
		return nil
	}
	return fmt.Errorf("未获取到授权 token，请先登录魔云腾账号")
}

func (s *SdkApiServiceWrapper) SetOnAndroidsChanged(fn func(deviceIPs []string)) {
	s.onAndroidsChanged = fn
}

// SetOnStructureChanged 设置结构变更回调（move/rename 等）。
// 与 onAndroidsChanged 的区别：结构变更（坑位号/容器名）不产生 docker lifecycle
// 事件，WS 事件通道无从感知——回调方必须在 local 模式下也查询一次。
func (s *SdkApiServiceWrapper) SetOnStructureChanged(fn func(deviceIPs []string)) {
	s.onStructureChanged = fn
}

func (s *SdkApiServiceWrapper) SetConfigManager(cm *config.Manager) {
	s.cm = cm
}

// SetApp 设置应用实例（用于事件推送）
func (s *SdkApiServiceWrapper) SetApp(app *application.App) {
	s.app = app
}

// emitEvent 向前端发送事件
func (s *SdkApiServiceWrapper) emitEvent(name string, data any) {
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit(name, data)
	}
}

// SetV4Indexer 注入 V4 实例序号派生器（setup 把 v4ws 适配层用的同一份实例注入进来，
// 保证 REST 与 WS 两条写入路径派生的截图 key 一致）。必须在服务启动前注入。
func (s *SdkApiServiceWrapper) SetV4Indexer(idx *v4Indexer) { s.v4Idx = idx }

// isV4Mode 当前是否 V4 部署模式（V4 走 8088 REST 分支，见 sdkapi_service_droid_v4.go）
func (s *SdkApiServiceWrapper) isV4Mode() bool {
	return s.cm != nil && s.cm.GetDeployMode() == "v4"
}

// resolveDeviceIP 解析前端设备标识为真实主机 IP：标识可能是序列号/deviceId
// （v2 主机序列号、v4 设备 /health 的 deviceId）也可能是 IP 兜底。查不到时原样返回。
func (s *SdkApiServiceWrapper) resolveDeviceIP(ident string) string {
	if s.cm != nil {
		if dev := s.cm.GetDevice(ident); dev != nil && dev.IP != "" {
			return dev.IP
		}
		if dev := s.cm.GetDeviceByIP(ident); dev != nil && dev.IP != "" {
			return dev.IP
		}
	}
	return ident
}

// newClient 创建设备 SDK 客户端。
// local 模式：deviceIP 为设备局域网 IP，走直连。
// public 模式：deviceIP 实际为 device_id（前端公网模式以 device_id 为标识），走网关透传。
// v2 模式：标识经 resolveDeviceIP 解析为真实主机 IP，81 端口 + 翻译层。
// v4 模式：标识经 resolveDeviceIP 解析为真实 IP，8088 端口（V4 SDK，鉴权待确认）。
// 失败时不返回 nil（避免调用方 panic），返回一个会产生连接错误的 client。
func (s *SdkApiServiceWrapper) newClient(deviceIP string) *sdkapi.Client {
	// 空标识符防护：避免 http://:8000 无意义连接刷屏
	if deviceIP == "" {
		return sdkapi.NewErrorClient("设备标识符为空")
	}
	// v2 模式：设备语义由主机 81 端口（超级 SDK）承接，请求经 sdkapi v2 翻译层转 dc_api
	if s.cm != nil && s.cm.GetDeployMode() == "v2" {
		return sdkapi.NewV2Client(s.resolveDeviceIP(deviceIP), 0)
	}
	// v4 模式：V4（tobox_player）只提供 8088 的实例/镜像管理接口，没有 V3 的 8000
	// SDK 接口（容器 exec / 标签 / 机型 / 设备信息 / 维护类等）。这里返回一个立即报错的
	// 客户端——调用方（终端、批量 ADB、切换机型、维护类操作…）拿到的是「V4 模式暂不支持
	// 该操作」而不是等 15s 超时或一个看不懂的 404，UI 层据此 toast 明确提示。
	// V4 真正可用的操作全部走 v4host 专用客户端（见 v4_instance_ops.go），不经此函数。
	if s.cm != nil && s.cm.GetDeployMode() == "v4" {
		return sdkapi.NewErrorClient("tebox 模式暂不支持该操作（tebox 设备只提供 8088 的实例管理接口）")
	}
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		deviceID := deviceIP // public 模式前端传入的是 device_id
		token, err := s.getPublicToken()
		if err != nil {
			log.Printf("[sdkapi] public newClient deviceID=%q tokenErr=%v，降级直连", deviceID, err)
			return sdkapi.NewClient(deviceID) // 降级直连，HTTP 请求会失败但不 panic
		}
		pwd := s.GetDevicePassword(deviceID) // 设备 SDK 密码（X-Device-Password）
		return sdkapi.NewPublicClient(deviceID, token, pwd)
	}
	client := sdkapi.NewClient(deviceIP)
	client.SetPassword(s.GetDevicePassword(deviceIP))
	return client
}

// newClientWithTimeout 创建带超时的设备 SDK 客户端（用于长耗时操作如格式化、大文件上传）。
// public 模式走网关透传（NewPublicClientWithTimeout），local 走 8000 直连，v4 走 8088。
func (s *SdkApiServiceWrapper) newClientWithTimeout(deviceIP string, timeout time.Duration) *sdkapi.Client {
	// 空标识符防护：避免 http://:8000 无意义连接刷屏
	if deviceIP == "" {
		return sdkapi.NewErrorClient("设备标识符为空")
	}
	// v2 模式：与 newClient 同款（标识解析 + 81 端口 + 翻译层），尊重调用方超时
	if s.cm != nil && s.cm.GetDeployMode() == "v2" {
		return sdkapi.NewV2Client(s.resolveDeviceIP(deviceIP), timeout)
	}
	// v4 模式：与 newClient 同款——V3 的 8000 SDK 接口在 V4 不存在，立即报错。
	if s.cm != nil && s.cm.GetDeployMode() == "v4" {
		return sdkapi.NewErrorClient("tebox 模式暂不支持该操作（tebox 设备只提供 8088 的实例管理接口）")
	}
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		deviceID := deviceIP
		token, err := s.getPublicToken()
		if err != nil {
			log.Printf("[sdkapi] public newClientWithTimeout deviceID=%q tokenErr=%v，降级直连", deviceID, err)
			return sdkapi.NewClientWithTimeout(deviceID, timeout)
		}
		pwd := s.GetDevicePassword(deviceID)
		return sdkapi.NewPublicClientWithTimeout(deviceID, token, pwd, timeout)
	}
	client := sdkapi.NewClientWithTimeout(deviceIP, timeout)
	client.SetPassword(s.GetDevicePassword(deviceIP))
	return client
}
