package main

import (
	"log"
	"path/filepath"
	"time"

	"github.com/moyunteng/myt/internal/account/payment"
	"github.com/moyunteng/myt/internal/account/slot"
	"github.com/moyunteng/myt/internal/androidlist"
	"github.com/moyunteng/myt/internal/auth"
	"github.com/moyunteng/myt/internal/clientreport"
	"github.com/moyunteng/myt/internal/cloudapi"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/devicedetail"
	"github.com/moyunteng/myt/internal/device/discover"
	"github.com/moyunteng/myt/internal/device/screenshot"
	"github.com/moyunteng/myt/internal/eventws"
	"github.com/moyunteng/myt/internal/imagemgr"
	"github.com/moyunteng/myt/internal/maintenance"
	"github.com/moyunteng/myt/internal/mcpserver"
	"github.com/moyunteng/myt/internal/mcpserver/winshot"
	"github.com/moyunteng/myt/internal/mirror"
	"github.com/moyunteng/myt/internal/proxy"
	"github.com/moyunteng/myt/internal/screencast"
	"github.com/moyunteng/myt/internal/sdkver"
	"github.com/moyunteng/myt/internal/taskqueue"
	"github.com/moyunteng/myt/internal/util"
	"github.com/moyunteng/myt/internal/v4ws"
	"github.com/moyunteng/myt/internal/workflow"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// serviceRegistry 组装所有服务、绑定依赖、注册到 Wails
type serviceRegistry struct {
	appSvc          *AppService
	discoverSvc     *DiscoverServiceWrapper
	detailSvc       *DeviceDetailServiceWrapper
	authSvc         *AuthServiceWrapper
	mirrorSvc       *MirrorServiceWrapper
	screenshotSvc   *ScreenshotServiceWrapper
	slotSvc         *SlotServiceWrapper
	androidListSvc  *AndroidListServiceWrapper
	paymentSvc      *PaymentServiceWrapper
	sdkApiSvc       *SdkApiServiceWrapper
	imageSvc        *ImageServiceWrapper
	vpcSvc          *VpcServiceWrapper
	phoneModelSvc   *PhoneModelService
	aiSvc           *AIServiceWrapper
	screencastSvc   *ScreencastServiceWrapper
	agentSvc        *AgentServiceWrapper
	mytAgentSvc     *MytAgentService
	uploadSvc       *UploadService
	fileSvc         *FileService
	maintenanceSvc  *MaintenanceServiceWrapper
	autoUpgradeSvc  *AutoUpgradeService
	iscsiSvc        *IscsiServiceWrapper
	iscsiTargetSvc  *IscsiTargetServiceWrapper
	groupCtrlSvc    *GroupControlServiceWrapper
	streamSvc       *StreamServiceWrapper
	cameraMicSvc    *CameraMicServiceWrapper
	taskSvc         *taskqueue.Service
	eventwsSvc      *eventws.Service
	v4wsSvc         *v4ws.Service
	v4Adapter       *v4EventAdapter
	v4Idx           *v4Indexer
	v4StateRep      *v4StateReporter
	clientReportSvc *clientreport.Reporter
	v2Svc           *V2ServiceWrapper
	v4Svc           *V4ServiceWrapper
	mcpSvc          *MCPService
}

func setupServices(cm *config.Manager, rootDir string, enableDebug bool) *serviceRegistry {
	r := &serviceRegistry{appSvc: NewAppService(cm)}

	// 核心服务创建
	d := discover.NewDiscoverer()
	dd := devicedetail.NewService(cm)
	a := auth.NewAuthService()
	ms := mirror.NewService()
	imgMgr := imagemgr.NewService(rootDir)
	if err := imgMgr.Load(); err != nil {
		log.Printf("初始化镜像目录失败: %v", err)
	}
	imgMgr.CleanupStaleDownloads()
	ss := screenshot.NewService()
	al := androidlist.NewService(cm)
	sl := slot.NewService()
	sl.SetConfigManager(cm)
	ps := payment.NewService()
	ps.SetSlotService(sl)

	// Wrapper 层
	r.detailSvc = NewDeviceDetailServiceWrapper(dd)
	r.discoverSvc = NewDiscoverServiceWrapper(d, cm)
	r.authSvc = NewAuthServiceWrapper(a)
	r.authSvc.SetDeviceIPsGetter(func() []string { return cm.GetOnlineDeviceIPs() })
	r.mirrorSvc = NewMirrorServiceWrapper(ms, cm)
	r.mirrorSvc.LoadCache()
	// V1 镜像接口需要登录 token（壳子同款分类字段 type/ttype/sort 依赖它）；
	// paymentSvc 在下方创建，这里用闭包延迟取值
	r.mirrorSvc.SetTokenGetter(func() (string, error) { return r.paymentSvc.GetToken() })
	r.screenshotSvc = NewScreenshotServiceWrapper(ss)
	r.screenshotSvc.SetConfigManager(cm)
	ss.SetContainerGetter(func() []screenshot.ContainerTarget {
		// v4 模式：tobox_player 8088 提供 GET /android/:id/screenshot（返回 PNG），
		// 目标集 = 该主机下所有 running 实例（未 running 服务端返回 503，抓不到）。
		// 抓图走下方注入的 SetTargetFetcher，不再走 9082。
		if cm.GetDeployMode() == "v4" {
			return v4ScreenshotTargets(cm)
		}
		androids := cm.GetRunningAndroids()
		targets := make([]screenshot.ContainerTarget, 0, len(androids))
		for _, a := range androids {
			// v2 模式：容器列表来自 81 mytsdk（listV2Androids），PortBindings 由 dc 字段
			// 填充；缺失时 9082 直连地址只能靠 30000+(index-1)*100+1 公式兜底——那是
			// V3 NAT 语义，v2 下必错，白白积累抓图失败进退避。跳过该容器抓图（记 debug
			// 日志），等下次列表刷新拿到端口映射后自然恢复。v3/local 的公式兜底不动。
			if cm.GetDeployMode() == "v2" && len(a.PortBindings) == 0 {
				util.Debugf("[screenshot] v2 跳过无端口映射容器: %s_%d（PortBindings 缺失，公式兜底在 v2 下必错）", a.IP, a.IndexNum)
				continue
			}
			targets = append(targets, screenshot.ContainerTarget{
				DeviceID: a.DeviceID, DeviceIP: a.IP, IndexNum: a.IndexNum,
				ContainerIP: a.ContainerIP, NetworkName: a.NetworkName, PortBindings: a.PortBindings,
			})
		}
		screenshot.SortTargets(targets, cm.GetDeployMode())
		return targets
	})
	// V4 抓图通道：8088 GET /android/:id/screenshot?backend=（按主机限并发）
	ss.SetTargetFetcher(newV4ScreenshotFetcher(cm))
	// 公网模式截图走网关透传（避免直连设备 IP）
	ss.SetPublicSnap(func(deviceID string, indexNum int) ([]byte, error) {
		return r.sdkApiSvc.PublicSnap(deviceID, indexNum)
	})
	r.slotSvc = NewSlotServiceWrapper(sl, cm)
	r.androidListSvc = NewAndroidListServiceWrapper(al, cm)
	// 截图事件联动：容器状态转换事件 → 清坑位截图缓存 + 立即重抓（事件驱动，1s 轮询保留兜底）
	al.SetScreenshotEventCallback(func(deviceIP string, indexNum int) {
		ss.InvalidateSlot(deviceIP, indexNum)
		ss.TriggerNow()
	})
	// 事件推送：WS 连接所有在线 local 设备，实时维护容器状态（androidlist 实现 Handlers）
	r.eventwsSvc = eventws.NewService()
	r.paymentSvc = NewPaymentServiceWrapper(ps, cm)
	// 客户端启动上报：dev 构建同样上报（dev 也算日活）。
	// uid 从 paymentSvc 取（登录后才有值），登录成功后 wrapper 会触发补报。
	r.clientReportSvc = clientreport.New(clientreport.Options{
		Store:   cm,
		Version: Version,
		UID:     r.paymentSvc.currentUID,
		Enabled: true,
	})
	r.paymentSvc.setClientReport(r.clientReportSvc)
	// 授权同步（手动）：推的是「当前登录账号」的 token，故从 paymentSvc 注入来源——
	// auth 包不 import payment（反向已依赖），靠 wrapper 注入，同 screencast/cloudapi 的 SetTokenFn
	r.authSvc.SetTokenGetter(func() (string, error) { return r.paymentSvc.GetToken() })

	// 代理 & SDK API
	proxySvc := proxy.NewService()
	if err := proxySvc.Start(); err != nil {
		log.Fatalf("启动代理服务失败: %v", err)
	}
	r.sdkApiSvc = NewSdkApiServiceWrapper(proxySvc)
	r.sdkApiSvc.SetConfigManager(cm)
	r.sdkApiSvc.SetMirrorService(r.mirrorSvc)
	r.sdkApiSvc.UpdateDevicePasswords(cm.GetDevicePasswordsMap())

	// 任务队列服务
	taskQueue := taskqueue.NewQueue(cm)
	r.taskSvc = taskqueue.NewService(taskQueue)
	r.taskSvc.SetSdk(r.sdkApiSvc)
	r.sdkApiSvc.SetTaskQueueService(r.taskSvc)
	// 容器列表即时刷新回调（sdkApiSvc 和 iscsiSvc 共用）
	refreshAndroids := func(deviceIPs []string) {
		// v4 模式：deviceIPs 是主机 IP。V4 的 WS 事件通道会推状态，但「刚创建的实例」
		// 事件可能滞后（甚至先于快照到达），操作后的 REST 全量补拉是权威事实。
		if cm.GetDeployMode() == "v4" {
			for _, ip := range deviceIPs {
				list, err := r.sdkApiSvc.listAndroidsV4(ip)
				if err != nil {
					log.Printf("[refreshAndroids] v4 查询失败 ip=%s err=%v", ip, err)
					continue
				}
				// 用 Replace（而非 Merge）：REST 全量返回即该主机的完整实例集合，
				// Merge 在「合并后为空」时保留旧数据，会让删除后的幽灵条目留在列表里
				cm.ReplaceAndroidListForDevice(v4HostIP(ip), list)
			}
			r.sdkApiSvc.emitEvent("androidlist:updated", cm.GetAndroidList())
			return
		}
		// v2 模式：设备无 WS 事件通道，操作后的即时刷新走注入的 v2 容器查询
		// （deviceIPs 实为设备标识——序列号或 IP，androidlist 内部按设备表解析）
		if cm.GetDeployMode() == "v2" {
			for _, ident := range deviceIPs {
				r.androidListSvc.QueryDeviceByIdent(ident)
			}
			return
		}
		// public 模式：deviceIPs 是 device_id，走网关查询（public 无 WS 事件，查询是操作后唯一反馈）
		if cm.GetDeployMode() == "public" {
			for _, deviceID := range deviceIPs {
				list, err := r.sdkApiSvc.ListAndroids(deviceID)
				if err != nil {
					log.Printf("[refreshAndroids] public 查询失败 deviceID=%s err=%v", deviceID, err)
					continue
				}
				cm.MergeAndroidList([]config.DeviceAndroidResult{
					{DeviceID: deviceID, Androids: list},
				}, time.Time{}, nil)
			}
			// 触发前端 androidlist:updated 事件
			r.sdkApiSvc.emitEvent("androidlist:updated", cm.GetAndroidList())
			return
		}
		// local 模式：SDK 产生的 container/boot 事件经 WS 实时推送（开机全程
		// queued→booting→completed→ready 逐步反映，比补查更细更权威），WS 健康时
		// 事件链秒级自愈。但创建云机（前端直调设备 API POST /android）后新容器可能
		// 无任何事件到达：老 SDK 无 /ws/events、WS 在退避窗口、未知容器补拉失败——
		// 此时列表只能手动刷新。保底查一次 + 2s 后补查一次（覆盖 mytsdk list 视图
		// 同步延迟，对齐 v2 两段式语义）。与 WS 事件补丁的竞态由 MergeAndroidList
		// 的 queryStartedAt 防护：晚到的旧 REST 结果不会覆盖 WS 已补丁的新状态。
		for _, ip := range deviceIPs {
			deviceID := r.sdkApiSvc.GetDeviceIDByIP(ip)
			if deviceID == "" {
				if dev := cm.GetDeviceByIP(ip); dev != nil {
					deviceID = dev.DeviceID
				}
			}
			if deviceID == "" {
				log.Printf("[refreshAndroids] local 未解析到 deviceID ip=%s，跳过保底查询", ip)
				continue
			}
			// QueryDevice 内部同步 HTTP（8s 超时）：GoSafe 异步执行，避免阻塞
			// NotifyAndroidsChanged 的前端调用（该回调是同步分发的）
			util.GoSafe(func() { al.QueryDevice(deviceID, ip) })
			time.AfterFunc(2*time.Second, func() { al.QueryDevice(deviceID, ip) })
		}
	}
	r.sdkApiSvc.SetOnAndroidsChanged(refreshAndroids)
	// 结构变更刷新（move/rename）：坑位号/容器名变更不产生 docker lifecycle 事件，
	// WS 事件通道无从感知——查询是唯一感知途径（refreshAndroids 现在各模式都查询）。
	r.sdkApiSvc.SetOnStructureChanged(func(deviceIPs []string) {
		if cm.GetDeployMode() == "public" {
			refreshAndroids(deviceIPs) // public 无 WS：网关查询（deviceIPs 实为 device_id）
			return
		}
		for _, ip := range deviceIPs {
			deviceID := r.sdkApiSvc.GetDeviceIDByIP(ip)
			if deviceID == "" {
				continue
			}
			util.GoSafe(func() { al.QueryDevice(deviceID, ip) })
		}
	})
	// 前端直调设备 API 的操作（批量创建等）完成后经此触发容器列表刷新
	r.androidListSvc.SetOnChanged(refreshAndroids)
	// 刷新按钮联动：列表刷新完成后同步重抓选中设备的截图
	r.androidListSvc.SetScreenshotService(r.screenshotSvc)

	// 公网模式 androidlist 走网关查询（注入回调，避免 internal 包直接依赖 main 包）
	al.SetPublicListAndroids(func(deviceID string) ([]config.AndroidInfo, error) {
		return r.sdkApiSvc.ListAndroids(deviceID)
	})

	// V2 模式服务：主机/容器数据源 = Docker 2375（androidlist 查询回调 + 模式判定注入）
	// 发现复用同一个 UDP Discoverer（V2 主机回复 _v2 后缀，ScanV2Hosts 内按版本过滤）
	r.v2Svc = NewV2ServiceWrapper(cm, d)
	al.SetV2ListAndroids(r.v2Svc.ListAndroidsForDevice)
	al.SetDeployModeGetter(cm.GetDeployMode)
	// v2 的"发现" = 网段扫描（新主机进待添加，已添加的刷新状态），对齐本地模式语义。
	// 后台扫描走静默入口：抢不到扫描锁（用户扫描/刷新在跑）直接放弃，用户扫描优先
	r.discoverSvc.SetV2Refresher(func() { r.v2Svc.ScanV2HostsQuiet() })
	r.discoverSvc.SetOnPendingAdded(r.v2Svc.OnPendingAdded)

	// V4 模式服务：UDP 7678 广播发现候选 IP + 8088 /health 验证（方案 A）。
	// 发现主流程（discoverAndUpdate 的 v4 分支）走静默扫描入口（抢不到锁即跳过），
	// 手动添加走定向探测（只探 IP，不做 UDP 单播/版本过滤）。
	r.v4Svc = NewV4ServiceWrapper(cm, d)
	r.discoverSvc.SetV4Scanner(r.v4Svc.ScanForDiscover)
	r.discoverSvc.SetV4Prober(r.v4Svc.ProbeV4IPs)
	// 创建云机后的 REST 补拉（复用 refreshAndroids 的 v4 分支：全量覆盖 + 推前端）
	r.v4Svc.SetRefreshInstances(func(ip string) {
		util.GoSafe(func() { refreshAndroids([]string{ip}) })
	})
	// v2 模式设备信息弹窗：GetDeviceInfo 转发到 myt_sdk :81/systeminfo
	// v2 主机面板的云端插件元数据需要登录 token（注入模式，同 authSvc）
	r.v2Svc.SetTokenGetter(func() (string, error) {
		return r.paymentSvc.GetToken()
	})
	// v2 主机上线后立即重查容器列表（reporter 离线首报会清缓存，v2 无 8000 通道
	// 的 queryOnce 兜底——启动/心跳恢复在线后必须主动拉，否则截图全无 + 容器
	// 显示「开机中」要手动刷新）
	r.v2Svc.SetAndroidsRefresher(r.androidListSvc.QueryDeviceByIdent)
	// v2 模式「SDK 升级」= 超级 SDK 插件更新（SdkApiServiceWrapper 的 v2 回调，
	// taskqueue 批量升级传 deviceIP）
	r.sdkApiSvc.SetV2UpgradeFn(func(deviceIP string) (bool, error) {
		return r.v2Svc.UpgradeSuperSdkByIP(deviceIP)
	})
	// v2 SDK 自动部署与安装链路互斥：复用 sdkApiSvc.installMu（避免同时两路
	// SSH/上传操作同一设备）
	r.v2Svc.SetInstallMutex(&r.sdkApiSvc.installMu)
	r.sdkApiSvc.SetV2DeviceInfoGetter(r.v2Svc.DeviceInfoByIP)
	// v2 SDK 自动安装触发：版本查询失败（/info 不可用）时经状态机决策后台安装
	r.sdkApiSvc.SetV2AutoInstallTrigger(r.v2Svc.maybeAutoInstallV2Sdk)
	// 手动安装成功重置自动安装状态机（清失败计数/放弃标记）
	r.sdkApiSvc.SetV2AutoInstallReset(r.v2Svc.ResetV2SdkAutoState)

	// 公网模式 SSH 诊断走网关透传（注入回调，workflow 包通过此回调获取公网配置）
	workflow.SetPublicSSHGetter(func(host string) *workflow.PublicSSHConfig {
		if cm.GetDeployMode() != "public" {
			return nil
		}
		token, err := r.sdkApiSvc.getPublicToken()
		if err != nil {
			return nil
		}
		return &workflow.PublicSSHConfig{
			DeviceID: host,
			Token:    token,
		}
	})

	// 设备列表更新后立即刷新容器列表（加速公网模式容器加载）
	r.discoverSvc.SetOnDeviceListUpdated(func() { al.TriggerNow() })

	r.detailSvc.SetSdkApiService(r.sdkApiSvc)
	r.detailSvc.SetConfigManager(cm)

	// 密码回调：devicedetail / androidlist / eventws 共用
	passwordGetter := func(deviceIP string) string { return r.sdkApiSvc.GetDevicePassword(deviceIP) }
	dd.SetPasswordGetter(passwordGetter)
	al.SetPasswordGetter(passwordGetter)
	r.eventwsSvc.SetPasswordGetter(passwordGetter)
	// 公网 WS 透传（网关实测 101，帧协议与本地同构）：智能体网关 session JWT 作
	// Bearer（每次拨号现取，15min 过期自动换新）+ cloudapi 网关地址。
	// 闭包延迟求值——拨号发生在 startBackgroundServices 之后，mytAgentSvc 必已就绪。
	r.eventwsSvc.SetPublicDialer(func() (string, error) {
		if r.mytAgentSvc == nil {
			return "", nil
		}
		return r.mytAgentSvc.GetSessionToken()
	}, cloudapi.GatewayURL)
	r.eventwsSvc.SetHandlers(al)
	// 设备面板指标：system/stats 事件（20s 推送，与 /info/device 动态字段同构）+ hello 身份字段
	// 替代 devicedetail 30s 轮询；静态字段（memtotal 基准等）由上线一次性 QueryDevice 建立
	r.eventwsSvc.SetSystemStatsHandler(dd.HandleSystemStats)
	// 在线性判定 + SDK 过期即时恢复 + hello 版本推送：实现拆至
	// device_state_reporter.go（CLAUDE.md 行数指标 + 可测试性——闭包状态字段化）。
	rep := NewDeviceStateReporter(cm, dd, r.sdkApiSvc, r.screenshotSvc)
	r.eventwsSvc.SetHelloHandler(rep.OnHello)
	r.eventwsSvc.SetStateReporter(rep.Handle)
	r.sdkApiSvc.SetOnSdkExpired(rep.OnSdkExpired)
	// WS 事件日志 → 前端 console（调试观察用）
	r.eventwsSvc.SetLogHandler(func(deviceID, line string) {
		r.sdkApiSvc.emitEvent("eventws:log", map[string]string{"deviceId": deviceID, "line": line})
	})
	// 在线性对账查询：manager 去重时读 config 实际状态——
	// 状态被外部（认证链路/迁移）写歪后，下一个心跳帧（stats 20s）自动纠正
	r.eventwsSvc.SetStateQueryer(cm.GetDeviceState)
	// 发现完成 → eventws 对账（新设备建连；替代 httpbeat TriggerNow）。
	// 对账范围用全部设备（非 GetOnlineDevices）：WS 拨号本身就是探测——
	// 拨通+snapshot=在线，拨号失败=离线。缓存中 State=1 的设备也参与拨号，避免启动死锁
	// （旧模型 State=1 等 httpbeat 确认，httpbeat 已撤）。
	// public 设备（IP 空）经网关透传建连（SetPublicDialer 已配置），local 设备直连。
	// v2 模式跳过：V2 主机是裸 Docker（无 eventws 端点），拨号只会空转报错。
	r.discoverSvc.SetEventwsSyncer(func() {
		// v4 模式：走独立的 V4 WS 连接池（协议不同，见 internal/v4ws）
		if cm.GetDeployMode() == "v4" {
			if r.v4wsSvc != nil {
				r.v4wsSvc.Sync(v4HostsOf(cm.GetDeviceList()))
			}
			return
		}
		if cm.GetDeployMode() == "v2" {
			return
		}
		r.eventwsSvc.Sync(cm.GetDeviceList())
	})
	// 设备删除 → 断开对应 WS 连接
	r.discoverSvc.SetEventwsDisconnect(func(deviceID string) {
		if r.v4wsSvc != nil && cm.GetDeployMode() == "v4" {
			r.v4wsSvc.Disconnect(deviceID) // V4 的 key 是 IP，deviceID 字段值即 IP
			return
		}
		r.eventwsSvc.Disconnect(deviceID)
	})

	// --- V4 WS 事件通道（tobox_player /ws/events，8088）---
	// 与 V3 的 eventws 完全独立：协议不同（snake_case 字段、instance/clipboard/system
	// 事件、快照基准 seq），V4 以 IP 为设备 key。看门狗按「数据帧 + 协议层 ping」
	// 双通道判活（tobox 空闲无数据帧，V3 只看数据帧的做法会误杀）。
	v4User, v4Pass := v4Creds()
	v4Idx := newV4Indexer()
	r.v4Idx = v4Idx
	r.sdkApiSvc.SetV4Indexer(v4Idx)
	r.v4wsSvc = v4ws.NewService(v4User, v4Pass)
	r.v4Adapter = newV4EventAdapter(cm, v4Idx, func(name string, data any) {
		if r.appSvc != nil && r.appSvc.app != nil && r.appSvc.app.Event != nil {
			r.appSvc.app.Event.Emit(name, data)
		}
	})
	// 剪贴板回灌：WS clipboard/push → 防回声 → 写系统剪贴板（Wails Clipboard）
	r.sdkApiSvc.v4Clip = newV4ClipboardEcho()
	r.v4Adapter.SetClipboardSink(r.sdkApiSvc)
	r.v4wsSvc.SetHandlers(r.v4Adapter)
	// V4 实例状态事件 → 清对应实例截图缓存 + 立即重抓（V3 同款语义，见 v4_screenshot.go）。
	// 状态转换意味着画面变化（开机完成/重启/关机），不清缓存会残留旧画面。
	r.v4Adapter.SetScreenshotInvalidator(v4ScreenshotInvalidator(cm, ss))
	r.v4wsSvc.SetLogHandler(func(host, line string) {
		r.sdkApiSvc.emitEvent("v4ws:log", map[string]string{"deviceId": v4HostIP(host), "line": line})
	})
	// 在线性判据（用户口径 2026-10）：v4ws 连上并收到 snapshot = 在线，断开/重连中
	// = 离线（离线上报后有一次 /health 兜底复核，见 v4_state_reporter.go）。
	// 修复前这里只打日志、没有任何东西写 config.State，而 V3 的 eventws 会去拨
	// V4 设备的 8000 端口（不存在）把状态刷成离线 → 设备永远显示离线但投屏正常。
	r.v4StateRep = newV4StateReporter(cm, func(name string, data any) {
		if r.appSvc != nil && r.appSvc.app != nil && r.appSvc.app.Event != nil {
			r.appSvc.app.Event.Emit(name, data)
		}
	})
	r.v4wsSvc.SetStateChangeHandler(r.v4StateRep.Handle)
	// V4 容器列表的 REST 兜底查询（WS 未连上/事件缺口时）
	al.SetV4ListAndroids(func(deviceID, ip string) ([]config.AndroidInfo, error) {
		return r.sdkApiSvc.listAndroidsV4(ip)
	})

	// iSCSI 磁盘用量回调：devicedetail 轮询时为已挂载设备获取正确的磁盘数据
	dd.SetIscsiDiskUsageGetter(func(deviceIP string) (int64, int64, bool) {
		status, err := r.iscsiSvc.GetIscsiStatus(deviceIP)
		if err != nil || !status.IsMounted {
			return 0, 0, false
		}
		return status.UsedMB * 1024 * 1024, status.TotalMB * 1024 * 1024, true
	})

	r.discoverSvc.SetIPMappingSetter(func(ip, deviceID string) {
		r.sdkApiSvc.UpdateIPMapping(ip, deviceID)
	})

	// 事件联动
	// refreshDeviceData 单设备数据级联（新发现设备 / 切回 local 的勾选设备共用）：
	// 设备详情 + 容器列表 + 容器落库后重抓截图（截图 TriggerNow 在容器查询完成
	// 之后串行触发，保证截图轮询取到最新容器列表）。
	refreshDeviceData := func(deviceID, ip string) {
		util.GoSafe(func() { dd.QueryDevice(deviceID, ip) })
		util.GoSafe(func() { al.QueryDevice(deviceID, ip); r.screenshotSvc.TriggerNow() })
	}
	r.discoverSvc.SetOnNewDevice(func(deviceID, ip string) {
		// v4 模式：不建 V3 eventws 连接（V4 设备只监听 8088，拨 8000 必然失败并
		// 经 device_state_reporter 把状态刷成离线——这正是「投屏正常却显示离线」
		// 的根因）。V4 的连接由 v4ws 负责，状态由 v4StateReporter 落库。
		// 实例列表仍要拉（al.QueryDevice 内部按模式路由到 8088 REST），拉到后触发一轮截图
		// （V4 截图走 8088 /android/:id/screenshot，见 v4_screenshot.go）；
		// 设备详情不适用 V4（8088 无 /info 设备详情）。
		if cm.GetDeployMode() == "v4" {
			util.GoSafe(func() { al.QueryDevice(deviceID, ip); r.screenshotSvc.TriggerNow() })
			return
		}
		refreshDeviceData(deviceID, ip)
		r.eventwsSvc.Connect(deviceID, ip)
	})
	r.discoverSvc.SetPaymentService(r.paymentSvc)
	// 坑位到期查询：V4 没有「坑位」概念（无订阅/无坑位授权），只在其余模式挂接
	r.discoverSvc.SetSlotQueryFn(func(deviceIDs []string) {
		if cm.GetDeployMode() == "v4" {
			return
		}
		r.slotSvc.QuerySlotInfo(deviceIDs)
	})
	r.paymentSvc.SetDiscoverService(r.discoverSvc)
	r.detailSvc.SetOnAuthResolved(func(deviceID, ip string) {
		util.GoSafe(func() { al.QueryDevice(deviceID, ip) })
		r.screenshotSvc.TriggerNow()
	})

	// 其余服务
	r.imageSvc = NewImageServiceWrapper(imgMgr)
	r.imageSvc.SetGetDevicePassword(passwordGetter)
	r.vpcSvc = NewVpcServiceWrapper(r.sdkApiSvc)
	r.phoneModelSvc = NewPhoneModelService(cm, r.sdkApiSvc)
	r.aiSvc = NewAIServiceWrapper(cm, rootDir)
	r.screencastSvc = NewScreencastServiceWrapper(cm)
	screencast.SetDataDir(rootDir)                // macOS: 让 findPlayer 优先搜数据目录（ExtractGoLegacy 提取到此）
	r.screencastSvc.SetMirrorService(r.mirrorSvc) // 注入镜像服务：特质版（真机）镜像强制 CPU 软解
	// 侧边栏上传 APK 安装走 SDK /android/exec（与客户端 UploadToCloud 同路径），需要设备 SDK 密码
	r.screencastSvc.SetSdkPasswordProvider(func(deviceID string) string {
		return r.sdkApiSvc.GetDevicePassword(deviceID)
	})
	// 公网投屏：注入授权 token（网关统一接受 moyunteng.com 授权 token，不再登录网关）
	r.screencastSvc.SetPublicCredentialsFn(func() (string, string) {
		return cm.GetAuthCredentials()
	})
	// 公网投屏走智能体网关（119.96.73.226），复用其 session JWT 鉴权
	r.screencastSvc.SetTokenFn(func() (string, error) { return r.mytAgentSvc.GetSessionToken() })
	r.agentSvc = NewAgentServiceWrapper()
	r.mytAgentSvc = NewMytAgentService(cm)
	r.mytAgentSvc.SetPaymentService(r.paymentSvc)
	// public 模式设备列表拉取依赖 MytAgentService 的授权 token
	r.discoverSvc.SetMytAgentService(r.mytAgentSvc)
	// public 模式 SDK 调用走线上服务透传，依赖 MytAgentService 的授权 token
	r.sdkApiSvc.SetMytAgentService(r.mytAgentSvc)
	// 网关客户端注入 token 来源（cloudapi 复用智能体网关 session JWT）
	r.sdkApiSvc.CloudClient().SetTokenFn(func() (string, error) { return r.mytAgentSvc.GetSessionToken() })
	// discover 复用 sdkApiSvc 的共享网关客户端，避免重复登录
	r.discoverSvc.SetSdkApiService(r.sdkApiSvc)

	// 从内存恢复 IP 映射
	if cached := cm.GetDeviceList(); len(cached) > 0 {
		r.sdkApiSvc.RebuildIPMappings(cached)
	}

	r.uploadSvc = NewUploadService(cm)
	r.uploadSvc.SetMirrorService(r.mirrorSvc)
	r.uploadSvc.SetSdkApiService(r.sdkApiSvc)
	r.fileSvc = NewFileService(cm)
	r.fileSvc.SetMirrorService(r.mirrorSvc)
	r.fileSvc.SetSdkApiService(r.sdkApiSvc)
	r.maintenanceSvc = NewMaintenanceServiceWrapper(cm)
	r.maintenanceSvc.SetSdkApiService(r.sdkApiSvc)
	// v2 模式魔云互联：前端传来的设备标识是序列号，SSH 链路先解析真实主机 IP。
	// 解析在 internal/maintenance 包入口（resolveHostIP，非 IP 即解析）——任务
	// 队列 handler 直调 maintenance 不经 wrapper，注入必须落在包级回调上
	// （设备列表 → ARP MAC 反查 → 81 systeminfo 兜底，见 v2_service_resolve.go）
	maintenance.SetV2HostIPResolver(r.v2Svc.resolveV2HostIP)
	// v2 魔云互联已并入 SSH 链路（2026-09-22）：不再注入 81 插件分流
	// （initV2PanelOps 摘除，v2PanelOps 为 nil → Install/Uninstall/Check 三入口
	// 统一走 SSH 推二进制；v2_service_panel.go 暂留作回滚通道）。
	// 更新 check 接口按部署模式选型号：v2 → r1s_v2，其余 → r1s
	if cm.GetDeployMode() == "v2" {
		maintenance.SetPanelDeviceType("r1s_v2")
	}
	// 魔云互联面板形态（myt-panel-v2/-host-ip/-sdk-port/-port 8181 vs
	// myt-panel/-port 8081）按部署模式判定——模式可运行时切换，注入动态判定
	// 而非一次性布尔
	maintenance.SetPanelV2Mode(func() bool { return cm.GetDeployMode() == "v2" })
	// tebox 安装：注入任务队列（右键「tebox 安装」提交任务）+ 安装锁释放钩子
	// （锁在提交侧获取、handler 终态释放——taskqueue 不能依赖 main，经钩子注入）
	r.maintenanceSvc.SetTaskQueueService(r.taskSvc)
	// v2 超级 SDK 安装：注入任务队列（InstallSuperSdk 提交 superSdkInstall 任务）
	r.v2Svc.SetTaskQueueService(r.taskSvc)
	taskqueue.SetTeboxInstallUnlock(UnlockTeboxInstall)
	// SDK 版本客户端：升级判定基准（本地缓存的云端最新版）共享给 sdkApiSvc
	// （SdkCheckUpgrade 用它覆盖设备自报 latest，防老 SDK 自称最新）
	sdkVerClient := sdkver.NewClient(cm.UpdateSDKDir())
	r.sdkApiSvc.SetSdkVerClient(sdkVerClient)
	r.autoUpgradeSvc = NewAutoUpgradeService(cm, r.sdkApiSvc, sdkVerClient)
	r.iscsiSvc = NewIscsiServiceWrapper(r.sdkApiSvc)
	r.iscsiSvc.SetConfigManager(cm)
	r.iscsiSvc.SetOnAndroidsChanged(refreshAndroids)
	// 注入 iscsiSvc 到 sdkApiSvc，使任务队列可调用挂载/卸载
	r.sdkApiSvc.SetIscsiService(r.iscsiSvc)
	r.iscsiTargetSvc = NewIscsiTargetServiceWrapper(rootDir)
	r.iscsiSvc.SetIscsiTargetManager(r.iscsiTargetSvc.GetIscsiTargetManager(), r.iscsiTargetSvc.GetIscsiTargetIQN())
	r.iscsiSvc.SetIscsiTargetService(r.iscsiTargetSvc)
	r.iscsiTargetSvc.SetIscsiService(r.iscsiSvc)

	r.groupCtrlSvc = NewGroupControlServiceWrapper(cm)

	// 流媒体服务
	r.streamSvc = NewStreamServiceWrapper()
	r.streamSvc.SetConfigManager(cm)

	// 摄像头+麦克风推流服务
	r.cameraMicSvc = NewCameraMicServiceWrapper()
	r.cameraMicSvc.SetConfigManager(cm)

	// AppService 依赖注入（必须在 sdkApiSvc 和 autoUpgradeSvc 创建之后）
	r.appSvc.SetSdkApiService(r.sdkApiSvc)
	r.appSvc.SetDiscoverService(r.discoverSvc)
	r.appSvc.SetScreenshotService(r.screenshotSvc)
	r.appSvc.autoUpgradeSvc = r.autoUpgradeSvc
	r.appSvc.SetPaymentService(r.paymentSvc)
	// 模式切换 → eventws 对账：切 public 时新列表全为空 IP → Sync 停掉全部
	// 本地连接（防事件回灌/空转重拨）；切 local 时按缓存列表重建；切 v2 时
	// 全停（StopAll）——V2 主机是裸 Docker（无 8000 eventws 端点），不能拿
	// v2 列表 Sync 拨号（会按退避标离线，与 v2 UDP 心跳的状态写入打架，
	// 造成在线/离线横跳），只需停掉 local/public 旧连接。
	r.appSvc.SetEventwsResync(func() {
		mode := cm.GetDeployMode()
		// V4：先停掉 V3 连接池（协议不同、key 不同，绝不能让 V3 连接拨 V4 设备），
		// 再按 V4 设备列表对账（切到 v4 时列表刚切换，Sync 会停掉旧连接并按新列表建连）
		if mode == "v4" {
			r.eventwsSvc.StopAll()
			if r.v4wsSvc != nil {
				r.v4wsSvc.Sync(v4HostsOf(cm.GetDeviceList()))
			}
			return
		}
		// 从 v4 切走：停掉全部 V4 连接（V4 的 /ws/events 只在 v4 模式下使用）
		if r.v4wsSvc != nil {
			r.v4wsSvc.StopAll()
			r.v4Idx.DropAll()
		}
		if mode == "v2" {
			r.eventwsSvc.StopAll()
			return
		}
		r.eventwsSvc.Sync(cm.GetDeviceList())
	})
	// 切回 local → 按持久化勾选（order>0）REST 补拉选中设备数据：容器列表刚被
	// 切换清空，WS 快照要等建连（老 SDK 404 无 WS），且勾选集合与公网会话相同
	// 时前端不重发 SetSelectedDevices（queryDevicesByID 只查新增 ID）
	r.appSvc.SetOnSwitchToLocal(func() {
		for _, d := range selectedDevicesWithIP(cm.GetDeviceList()) {
			refreshDeviceData(d.DeviceID, d.IP)
		}
	})
	// 切到 v2 → 按持久化勾选立即经 Docker API 补拉容器列表（v2 无 WS 事件推送）
	r.appSvc.SetOnSwitchToV2(func() { r.androidListSvc.RefreshSelected() })
	r.paymentSvc.SetAppService(r.appSvc)

	// 自建 MCP server：业务工具依赖各 Wrapper（窄接口注入，mcpserver 不反向依赖 main）
	r.mcpSvc = NewMCPService(cm, rootDir, &mcpserver.BizDeps{
		AndroidList: r.androidListSvc,
		Discover:    r.discoverSvc,
		Screenshot:  r.screenshotSvc,
		SDK:         r.sdkApiSvc,
		Tasks:       r.taskSvc,
		Mirror:      r.mirrorSvc,
		Maintenance: r.maintenanceSvc,
		Iscsi:       r.iscsiSvc,
		StreamGet:   func() any { return r.streamSvc.GetActiveStreams() },
		StreamStop:  func(ips []string) any { return r.streamSvc.StopDevicePlay(ips) },
		Screencast:  newWinshotDeps(r),
		CastKeys:    r.screencastSvc.ScreencastRunningKeys,
	})

	return r
}

// selectedDevicesWithIP 提取持久化勾选（order>0）且有局域网 IP 的设备。
// 切回 local 的数据补拉用：order 是前后端共同的勾选持久化字段（全选/单选都写
// order），IP 为空的是 public 残留条目（REST 直连不可达，跳过）。
func selectedDevicesWithIP(list []config.DeviceCache) []config.DeviceCache {
	var selected []config.DeviceCache
	for _, d := range list {
		if d.Order > 0 && d.IP != "" {
			selected = append(selected, d)
		}
	}
	return selected
}

func (r *serviceRegistry) toWailsServices() []application.Service {
	services := []application.Service{
		application.NewService(r.appSvc),
		application.NewService(r.discoverSvc),
		application.NewService(r.detailSvc),
		application.NewService(r.authSvc),
		application.NewService(r.mirrorSvc),
		// 带 Route：内部 asset server 挂 /screenshots 二进制图片通道（img src 直连，见
		// device_service.go 的 ScreenshotServiceWrapper 注释与 screenshot_http.go）
		application.NewServiceWithOptions(r.screenshotSvc, application.ServiceOptions{Route: "/screenshots"}),
		application.NewService(r.slotSvc),
		application.NewService(r.androidListSvc),
		application.NewService(r.paymentSvc),
		application.NewService(r.sdkApiSvc),
		application.NewService(r.imageSvc),
		application.NewService(r.vpcSvc),
		application.NewService(r.phoneModelSvc),
		application.NewService(r.aiSvc),
		application.NewService(r.screencastSvc),
		application.NewService(r.uploadSvc),
		application.NewService(r.fileSvc),
		application.NewService(r.maintenanceSvc),
		application.NewService(r.agentSvc),
		application.NewService(r.mytAgentSvc),
		application.NewService(r.iscsiSvc),
		application.NewService(r.iscsiTargetSvc),
		application.NewService(r.groupCtrlSvc),
		application.NewService(r.v2Svc),
		application.NewService(r.v4Svc),
	}
	services = append(services, application.NewService(r.streamSvc))
	services = append(services, application.NewService(r.cameraMicSvc))
	services = append(services, application.NewService(r.taskSvc))
	services = append(services, application.NewService(r.mcpSvc))
	return services
}

func (r *serviceRegistry) setApp(app *application.App) {
	r.imageSvc.SetApp(app)
	r.appSvc.SetApp(app)
	r.paymentSvc.SetApp(app)
	r.screenshotSvc.SetApp(app)
	r.aiSvc.SetApp(app)
	r.androidListSvc.SetApp(app)
	r.sdkApiSvc.SetApp(app)
	r.discoverSvc.SetApp(app)
	r.v2Svc.SetApp(app)
	r.v4Svc.SetApp(app)
	// 共享 ADB 终端会话中心（terminal_hub.go）：V2/V4 终端事件推送同用
	termHub.SetApp(app)
	r.groupCtrlSvc.SetApp(app)
	r.detailSvc.SetApp(app)
	r.agentSvc.SetApp(app)
	r.mytAgentSvc.SetApp(app)
	r.streamSvc.SetApp(app)
	r.screencastSvc.SetApp(app)
	r.fileSvc.SetApp(app)
	r.taskSvc.SetApp(app)
	r.taskSvc.Restore()
}

func newWailsApp(r *serviceRegistry, rootDir string, enableDebug bool, enableMCPCDP bool) *application.App {
	winOpts := application.WindowsOptions{
		WebviewUserDataPath: filepath.Join(rootDir, "cache", "webview2"),
	}
	// WebView2 CDP 远程调试端口（仅绑 127.0.0.1，截图/自动化取证用）：
	// dev 与生产构建统一固定 9333，与 dev.sh 的 WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS 一致；
	// 原 enableDebug 分支（--debug/APP_DEBUG）行为不变，与默认合并。
	// MCP UI 级工具场景例外：端口 0 = 随机，实际端口由 WebView2 写入
	// WebviewUserDataPath 下的 DevToolsActivePort 文件（mcpserver 轮询读取）。
	// 仅窗口创建时生效——运行中才打开 MCP 开关的场景 CDP 不可用。
	switch {
	case enableMCPCDP:
		winOpts.AdditionalBrowserArgs = []string{"--remote-debugging-port=0"}
	default:
		winOpts.AdditionalBrowserArgs = []string{"--remote-debugging-port=9333"}
	}
	return application.New(application.Options{
		Name:        "MYT",
		Description: "魔云腾ARM算力服务器管理客户端",
		// Linux：g_set_prgname。Frameless 窗口下 vendor 的 setTitle 会跳过
		// gtk_window_set_title（见 linux_cgo.go:1297 / linux_cgo_gtk3.go:1698），
		// GTK 回退标题为 prgname，未设置时显示二进制文件名（如 myt-linux）。
		Linux: application.LinuxOptions{
			ProgramName: "魔云腾",
		},
		Services: r.toWailsServices(),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: winOpts,
	})
}

// 主窗口尺寸基准。
//
// 默认放大到 1720x1000：云机 tab 的工具条按钮较多（模式切换 + 投屏/批量控制/主控选择/
// 全选/刷新 + 批量操作两行），多选时还会多出「主控选择」下拉，1600 宽下容易换行成三行。
// 实际取值再按主屏工作区收窄（见 mainWindowSize），避免小屏上窗口超出屏幕。
const (
	mainWindowPreferredW = 1720
	mainWindowPreferredH = 1000
	mainWindowMinW       = 1400
	mainWindowMinH       = 920
)

// mainWindowSize 计算主窗口默认尺寸：取基准值，但不超过主屏工作区的 92%。
// 屏幕信息在 application.New 时已缓存（platformNew → processAndCacheScreens），
// 故此处（窗口创建前）可安全读取；取不到时退回基准值。
func mainWindowSize(app *application.App) (int, int) {
	width, height := mainWindowPreferredW, mainWindowPreferredH
	if app == nil || app.Screen == nil {
		return width, height
	}
	screen := app.Screen.GetPrimary()
	if screen == nil {
		return width, height
	}
	work := screen.WorkArea
	if work.Width <= 0 || work.Height <= 0 {
		return width, height
	}
	if maxW := work.Width * 92 / 100; width > maxW {
		width = maxW
	}
	if maxH := work.Height * 92 / 100; height > maxH {
		height = maxH
	}
	// 兜底不低于最小尺寸：极小屏上宁可超出一点，也不要出现比最小尺寸还小的窗口
	if width < mainWindowMinW {
		width = mainWindowMinW
	}
	if height < mainWindowMinH {
		height = mainWindowMinH
	}
	return width, height
}

func newMainWindow(app *application.App, enableDebug bool) *application.WebviewWindow {
	width, height := mainWindowSize(app)
	return app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:               "魔云腾",
		Width:               width,
		Height:              height,
		MinWidth:            mainWindowMinW,
		MinHeight:           mainWindowMinH,
		InitialPosition:     application.WindowCentered,
		CloseButtonState:    application.ButtonEnabled,
		MinimiseButtonState: application.ButtonEnabled,
		MaximiseButtonState: application.ButtonHidden,
		Frameless:           true,
		Windows: application.WindowsWindow{
			Permissions: map[application.CoreWebView2PermissionKind]application.CoreWebView2PermissionState{
				application.CoreWebView2PermissionKindMicrophone: application.CoreWebView2PermissionStateAllow,
			},
		},
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 0,
			TitleBar:                application.MacTitleBarHidden,
		},
		BackgroundColour:           application.NewRGB(255, 255, 255),
		URL:                        "/",
		DevToolsEnabled:            enableDebug,
		DefaultContextMenuDisabled: !enableDebug,
		EnableFileDrop:             true,
	})
}

// newWinshotDeps 组装投屏业务工具依赖：容器标识归一化（winshot.resolveAndroid，
// 数据源 = 运行中容器清单）、投屏 key → 控制端口、发起投屏透传（UI
// 「单击卡片→投屏」同款 StartBatchIndividualProjections 单台路径）。
func newWinshotDeps(r *serviceRegistry) *winshot.Deps {
	return &winshot.Deps{
		RunningAndroids: r.androidListSvc.GetRunningAndroids,
		RunningKeys:     r.screencastSvc.ScreencastRunningKeys,
		ControlPortOf:   r.screencastSvc.ScreencastProjectionControlPort,
		StartCast: func(deviceIP string, indexNum int) error {
			return r.screencastSvc.StartBatchIndividualProjections(
				[]BatchProjectionItem{{DeviceIP: deviceIP, IndexNum: indexNum, Orientation: "portrait"}}, 1.0, true)
		},
	}
}
