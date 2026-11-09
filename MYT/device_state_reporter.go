// device_state_reporter.go — 事件推送（eventws）的在线性判定与 SDK 过期即时恢复。
//
// 从 setup.go 的内嵌闭包拆出（2026-09-08，CLAUDE.md 行数指标 + 可测试性）：
// setup.go 只留装配接线（NewDeviceStateReporter + 三个 Set* 注入），实现收敛于此，
// 原闭包局部状态（复核记忆 wsProbeOK / emit 防抖）字段化，测试经函数注入解耦
// devicedetail/screenshot 等具体服务。
package main

import (
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/devicedetail"
	"github.com/moyunteng/myt/internal/eventws"
	"github.com/moyunteng/myt/internal/util"
)

// probeMemoryInterval 复核成功记忆时长：老 SDK 设备（WS 404 但 HTTP 可达）的
// 在线确认节流——404 每轮退避（1~10s）都上报离线，探测分支若不记忆会秒级
// HTTP 轰炸设备；记忆期内重复上报直接忽略（也不重复写状态）。
const probeMemoryInterval = 60 * time.Second

// deviceStateReporter eventws 状态上报的消费端（setup 装配后注入 eventwsSvc）。
type deviceStateReporter struct {
	cm            *config.Manager
	dd            *devicedetail.Service
	sdkApiSvc     *SdkApiServiceWrapper // emitEvent（广播前端）
	screenshotSvc *ScreenshotServiceWrapper

	// 函数注入面（默认真实现，测试替换）——serviceRegistry 装配后由 setup 注入。
	querySync  func(deviceID, ip string) int // 探测（devicedetail.QueryDeviceSync：0=在线 1=认证 2=不可达）
	queryOnce  func(deviceID, ip string)     // 上线详情拉取（devicedetail.QueryDevice）
	invalidate func(keyPrefix string)        // 截图缓存失效（screenshot.InvalidateDevice）
	trigger    func()                        // 截图立即刷新（screenshot.TriggerNow）

	wsProbeOK sync.Map // deviceID → time.Time（上次 HTTP 复核成功时间）
	emitMu    struct { // devicelist:updated 广播防抖：批量翻在线时合并成一条
		sync.Mutex
		pending bool
	}
}

// NewDeviceStateReporter 构造（依赖注入后由 setup 接线）。
func NewDeviceStateReporter(cm *config.Manager, dd *devicedetail.Service,
	sdkApiSvc *SdkApiServiceWrapper, screenshotSvc *ScreenshotServiceWrapper) *deviceStateReporter {
	r := &deviceStateReporter{
		cm:            cm,
		dd:            dd,
		sdkApiSvc:     sdkApiSvc,
		screenshotSvc: screenshotSvc,
	}
	r.querySync = dd.QueryDeviceSync
	r.queryOnce = dd.QueryDevice
	r.invalidate = screenshotSvc.InvalidateDevice
	r.trigger = screenshotSvc.TriggerNow
	return r
}

// Handle StateReporter 主体（eventwsSvc.SetStateReporter 注入）。
// 在线性判定（替代 httpbeat）：WS 心跳即存活信号——snapshot 在流=在线、
// 401=认证失败；判离线（拨号失败/90s 无帧/断连/404）前先 HTTP 探测复核：
// 老 SDK（<208，WS 404）设备 WS 连不上但 HTTP API 仍可达，在线性以 API
// 可达为准（2026-09-08 用户决策），不误标离线。
func (r *deviceStateReporter) Handle(deviceID string, state int) {
	cm, dev := r.cm, r.cm.GetDevice(deviceID)
	// v4 模式守卫：V4 的在线性只由 v4ws 连接状态决定（见 v4_state_reporter.go）。
	// V3 eventws 对 V4 设备（只监听 8088）拨 8000 必然失败，若让它把状态刷成离线，
	// 就会出现「设备离线但投屏正常」——这里直接丢弃 V3 通道的 V4 上报。
	if cm.GetDeployMode() == "v4" {
		return
	}
	if dev == nil {
		// 设备不在当前模式设备列表：跨模式残余上报（切模式后旧连接的迟到
		// 离线上报）。不写不 emit——UpdateDeviceFields 对不存在设备本就
		// no-op，但 emit 会把（可能为空的）当前列表推给前端：切 public 后
		// 这条空列表事件会提前解除前端加载态并把已渲染列表清空（列表为空
		// 时用户看到「加载很久还是空的」）。真实数据由新模式首轮发现负责。
		return
	}
	if dev.State == 2 && state != 2 {
		return // 认证失败状态由认证链路管理，探活不覆盖（沿用 httpbeat 语义）
	}
	if state == 1 && dev != nil && dev.IP != "" && cm.GetDeployMode() == "local" {
		ip := dev.IP
		startMode := cm.GetDeployMode()
		util.GoSafe(func() { r.probeOffline(deviceID, ip, startMode) })
		return
	}
	if state == 0 {
		r.wsProbeOK.Delete(deviceID) // WS 连上（设备升级了）：清复核记忆
	}
	// 状态变化守卫（公网离线风暴）：public 设备（IP 空）走不到 HTTP 复核分支，
	// 30s 兜底 + Sync 重置会让离线上报每轮强制转发——无条件 emit 会让 K 台离线
	// 公网设备随自动轮询每轮 K 条全量 devicelist:updated（网关故障 300 台 ≈ 10/s，
	// 授权风暴同形态）。状态真没变（闪断重连期间的重复上报）就不写不广播。
	if dev.State != state {
		r.cm.UpdateDeviceFields(deviceID, "", "", "", "", state)
		r.sdkApiSvc.emitEvent("devicelist:updated", r.cm.GetDeviceList())
	}
	if state == 0 {
		// 设备（重新）在线：拉一次设备详情（30s 轮询之外加速）；容器列表由 snapshot 覆盖
		if dev := r.cm.GetDevice(deviceID); dev != nil && dev.IP != "" {
			util.GoSafe(func() { r.queryOnce(deviceID, dev.IP) })
		}
		r.trigger()
		return
	}
	// 离线（state=1）：清该设备容器缓存 + 截图缓存（2026-09-05 用户决策，方案B）。
	// 不做 wasOnline（翻转前在线）判定——启动加载的缓存设备默认 State=1，但
	// reporter 由 manager 两级去重保护，每次触发都是「确认离线」信号；启动时对
	// 已离线设备的首次上报同样触发，磁盘加载的残留容器缓存必须清掉（否则
	// 离线设备显示僵尸云机）。恢复在线由 snapshot 整体覆盖重建。
	// 截图 key 前缀：local 用设备 IP，public（IP 空）用 device_id。
	if state == 1 {
		if r.cm.RemoveAndroidsByDevice(deviceID) > 0 {
			r.sdkApiSvc.emitEvent("androidlist:updated", r.cm.GetAndroidList())
		}
		if dev != nil {
			keyPrefix := dev.IP
			if keyPrefix == "" {
				keyPrefix = deviceID // public：容器列表与截图 key 前缀同为 device_id
			}
			r.invalidate(keyPrefix)
		}
	}
}

// probeOffline 离线上报的 HTTP 复核（异步执行）。
// 可达=老 SDK 设备：写在线（State=0）——HTTP 可达即在线（8000 端口正常，
// 只是没有 WS 协议；启动加载的缓存 State=1 若不显式写回在线，会永远显示
// 离线）+ 记忆复核结果；不可达=真离线，走原离线清理（容器缓存/截图失效）。
//
// startMode 为发起时的部署模式：探测期间用户可能已切换模式（local→public
// 后设备列表已整体替换），写入前必须复查，模式已变则丢弃本轮结果——
// 否则旧模式的探测结论会写进新模式的设备列表（跨模式数据污染）。
func (r *deviceStateReporter) probeOffline(deviceID, ip string, startMode string) {
	if t, ok := r.wsProbeOK.Load(deviceID); ok && time.Since(t.(time.Time)) < probeMemoryInterval {
		return // 记忆期内已确认在线（老 SDK）：不重复探测，也不重复写状态
	}
	// 写入/广播前的模式守卫：探测（8s 超时 × 网络）期间模式已切换则丢弃
	stale := func() bool { return r.cm.GetDeployMode() != startMode }
	switch r.querySync(deviceID, ip) {
	case 0: // HTTP 可达：设备在线（老 SDK 无 WS），写在线 + 记忆
		r.wsProbeOK.Store(deviceID, time.Now())
		if stale() {
			return
		}
		if d := r.cm.GetDevice(deviceID); d != nil && d.State != 0 {
			r.cm.UpdateDeviceFields(deviceID, "", "", "", "", 0)
			r.sdkApiSvc.emitEvent("devicelist:updated", r.cm.GetDeviceList())
		}
	case 1: // 认证失败：fetchSingle 已写 state=2，补推前端
		if stale() {
			return
		}
		r.sdkApiSvc.emitEvent("devicelist:updated", r.cm.GetDeviceList())
	default: // 真不可达：离线 + 清理
		r.wsProbeOK.Delete(deviceID)
		// 清理（幂等便宜，>0 才 emit）不受状态守卫限制——启动加载的设备默认
		// State=1，守卫若把清理想进去，真离线设备的残留容器缓存永不清理。
		// 容器/截图清理按键前缀（IP/device_id）隔离于模式，跨模式执行无害，
		// 不参与 stale 复查；状态写入必须复查（见函数注释）。
		if r.cm.RemoveAndroidsByDevice(deviceID) > 0 {
			r.sdkApiSvc.emitEvent("androidlist:updated", r.cm.GetAndroidList())
		}
		r.invalidate(ip)
		// 状态写+广播需守卫（30s 兜底强制转发每轮都到这里，无守卫 = 每设备
		// 每 30 秒一条全量广播；复核期间翻成认证态(2)不覆盖——认证链路管理）
		if stale() {
			return
		}
		if d := r.cm.GetDevice(deviceID); d != nil && d.State != 1 && d.State != 2 {
			r.cm.UpdateDeviceFields(deviceID, "", "", "", "", 1)
			r.sdkApiSvc.emitEvent("devicelist:updated", r.cm.GetDeviceList())
		}
	}
}

// OnSdkExpired 「SDK 过期设备 HTTP 可达」即时恢复钩子（sdkApiSvc.SetOnSdkExpired 注入）。
// 版本查询成功且 < 208（老 SDK，无 WS 端点）——离线中的这类设备立即写回在线，
// 不等离线兜底转发（30s）。场景：SDK 回退/降级重启期间首次探测失败写离线，
// 设备起来后用户进列表查到版本 204，过期判定即证据——立刻翻橙。
// 版本达标（≥208）设备不触发（notifySdkExpired 侧已过滤）：其在线性由
// WS/探测链路自管。写状态幂等（State 守卫），认证态(2)不碰。
// emit 经 100ms 防抖合并：进列表页会批量查询多台离线老 SDK 设备，每台一条
// 全量广播会连发（事件风暴）——窗口内合并成一条（全量快照语义，安全）。
func (r *deviceStateReporter) OnSdkExpired(deviceID, deviceIP string) {
	if r.cm.GetDeployMode() != "local" {
		return
	}
	if d := r.cm.GetDevice(deviceID); d != nil && d.State == 1 {
		r.wsProbeOK.Store(deviceID, time.Now()) // 复核成功记忆：兜底转发不再重复探测
		r.cm.UpdateDeviceFields(deviceID, "", "", "", "", 0)
		r.emitMu.Lock()
		if !r.emitMu.pending {
			r.emitMu.pending = true
			time.AfterFunc(100*time.Millisecond, func() {
				r.emitMu.Lock()
				r.emitMu.pending = false
				r.emitMu.Unlock()
				r.sdkApiSvc.emitEvent("devicelist:updated", r.cm.GetDeviceList())
			})
		}
		r.emitMu.Unlock()
	}
}

// OnHello hello 帧：身份字段落详情缓存 + SDK 版本实时推送前端。
// 设备重启（升级/回退）必经 WS 重连 hello——「版本变了」最权威的失效信号
// （升级完成后前端版本缓存若停留旧值，橙点/徽章不会自动消失；老 SDK 设备
// 无 hello，靠 HTTP 版本查询兜底，两源互补）。专用频道（不进
// devicelist:updated——列表事件铁律），仅 useSdkVersions 订阅，幂等合并。
func (r *deviceStateReporter) OnHello(deviceID string, h *eventws.HelloFrame) {
	r.dd.HandleHello(deviceID, h)
	if v := h.SDKVersionNum(); v > 0 {
		r.sdkApiSvc.emitEvent("sdkversion:updated", map[string]any{"deviceId": deviceID, "version": v})
	}
}
