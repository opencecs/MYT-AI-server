package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/androidlist"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/devicedetail"
	"github.com/moyunteng/myt/internal/device/screenshot"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// DeviceDetailServiceWrapper 设备信息查询服务包装
type DeviceDetailServiceWrapper struct {
	service        *devicedetail.Service
	sdkApiSvc      *SdkApiServiceWrapper
	cm             *config.Manager
	app            *application.App
	onAuthResolved func(deviceID, ip string)

	// statsEmitMu/statsLastEmit：devicestats:updated 的 500ms 节流。
	// 330 台设备 × WS 心跳(20s/台) ≈ 16 次通知/秒，每次都全量序列化 330 台
	// 列表并触发前端全树重渲染——磁盘/内存条视觉上根本不需要亚秒级刷新，
	// 合并到最多 2 次/秒（2026-09-04 心跳风暴的同类收口）。
	statsEmitMu   sync.Mutex
	statsLastEmit time.Time
}

// emitDeviceStatsThrottled 节流版 devicestats:updated：500ms 内多次通知只发一次。
// 最后一次通知被吞时由下一次到期补发（心跳 20s/台持续到来，丢一拍无感）。
func (s *DeviceDetailServiceWrapper) emitDeviceStatsThrottled() {
	if s.app == nil || s.app.Event == nil || s.cm == nil {
		return
	}
	s.statsEmitMu.Lock()
	defer s.statsEmitMu.Unlock()
	if time.Since(s.statsLastEmit) < 500*time.Millisecond {
		return
	}
	s.statsLastEmit = time.Now()
	s.app.Event.Emit("devicestats:updated", s.cm.GetDeviceList())
}

func NewDeviceDetailServiceWrapper(s *devicedetail.Service) *DeviceDetailServiceWrapper {
	return &DeviceDetailServiceWrapper{service: s}
}

func (s *DeviceDetailServiceWrapper) SetSdkApiService(svc *SdkApiServiceWrapper) {
	s.sdkApiSvc = svc
}

func (s *DeviceDetailServiceWrapper) SetConfigManager(cm *config.Manager) {
	s.cm = cm
}

func (s *DeviceDetailServiceWrapper) SetApp(app *application.App) {
	s.app = app
}

// SetOnAuthResolved 设置认证成功后的回调（联动安卓列表、截图等）
func (s *DeviceDetailServiceWrapper) SetOnAuthResolved(fn func(deviceID, ip string)) {
	s.onAuthResolved = fn
}

func (s *DeviceDetailServiceWrapper) Start() {
	// 设备指标更新（WS system/stats 心跳流）推送——专用 devicestats:updated 事件：
	// 前端设备列表订阅它刷新磁盘/内存条；不能复用 devicelist:updated，
	// 否则授权页等订阅者会对每次心跳全量重查授权（20s/台 × N 台 = 心跳风暴）。
	// emitDeviceStatsThrottled：500ms 节流，N 台设备的心跳合并成最多 2 次/秒
	s.service.SetOnUpdated(func() {
		s.emitDeviceStatsThrottled()
	})
	// 单设备 devicedetail:updated 已下线：唯一消费者（设备详情弹窗）改为打开时
	// 主动调 RefreshDeviceInfo（心跳流经门控 emit 的 6 次/秒 IPC 白付问题随之消除）。
	s.service.Start()
}

// RefreshDeviceInfo 设备详情弹窗主动拉取（打开/刷新按钮），返回 *sdkapi.DeviceInfo。
// local 模式：REST /info/device 全量查询 → 磁盘/内存/详情缓存落库，返回权威快照；
// public/v2 降级：SDK 网关查询（newClient 已按模式分发，仅显示不落缓存）；
// v4：SDK 无设备信息端点，返回既有的「暂不支持」明确错误。
// 失败时返回 (nil, err)，前端展示错误（与原 GetDeviceInfo 调用行为一致）。
func (s *DeviceDetailServiceWrapper) RefreshDeviceInfo(deviceIdent string) (*sdkapi.DeviceInfo, error) {
	// 标识解析：local 模式弹窗传设备 IP；也容忍 deviceID（查列表双键匹配）
	deviceID, ip := s.resolveDeviceIdent(deviceIdent)
	if s.cm != nil && s.cm.GetDeployMode() == "local" && ip != "" {
		info, code := s.service.FetchDeviceInfo(deviceID, ip)
		if info != nil {
			return info, nil
		}
		return nil, fmt.Errorf("设备查询失败 code=%d（1=待认证）", code)
	}
	// public/v2 降级：SDK 网关查询（仅显示）
	if s.sdkApiSvc != nil {
		return s.sdkApiSvc.GetDeviceInfo(deviceIdent)
	}
	return nil, fmt.Errorf("设备查询服务不可用")
}

// resolveDeviceIdent 弹窗标识 → (deviceID, ip)：先按 deviceID 查，再按 IP 查，
// 都查不到时原样作为 IP 处理（未入库设备直连场景）。
func (s *DeviceDetailServiceWrapper) resolveDeviceIdent(ident string) (deviceID, ip string) {
	if s.cm == nil {
		return "", ident
	}
	if dev := s.cm.GetDevice(ident); dev != nil {
		return dev.DeviceID, dev.IP
	}
	if dev := s.cm.GetDeviceByIP(ident); dev != nil {
		return dev.DeviceID, dev.IP
	}
	return "", ident
}

func (s *DeviceDetailServiceWrapper) Stop() {
	s.service.Stop()
}

// VerifyDeviceAuth 验证设备密码：保存密码 → 触发单次查询 → 认证通过则恢复 State=0
// 返回 true 表示认证成功（设备已恢复在线），false 表示仍然认证失败
func (s *DeviceDetailServiceWrapper) VerifyDeviceAuth(deviceID, deviceIP, password string) bool {
	// 保存密码到内存和配置
	if s.sdkApiSvc != nil {
		_ = s.sdkApiSvc.UpdateDevicePasswordForIP(deviceIP, password)
	}

	// 触发单次查询
	result := s.service.QueryDeviceSync(deviceID, deviceIP)

	if result == 0 {
		// 认证通过，恢复 State=0
		if s.cm != nil {
			s.cm.UpdateDeviceState(deviceID, 0)
		}
		log.Printf("[detailSvc] ★ 设备认证成功，恢复在线: id=%s ip=%s", safeID(deviceID), deviceIP)
		// 触发上线回调（联动安卓列表、截图等）
		if s.onAuthResolved != nil {
			s.onAuthResolved(deviceID, deviceIP)
		}
		return true
	}

	log.Printf("[detailSvc] 设备认证仍然失败: id=%s ip=%s", safeID(deviceID), deviceIP)
	return false
}

// MarkAuthFailed 标记设备认证失败（State=2），供前端在调用设备接口遇到 401 时调用。
// 标记后设备列表与设置页均显示“待认证”，后续 fetchDetailsFor 会按 state!==0 跳过该设备。
func (s *DeviceDetailServiceWrapper) MarkAuthFailed(deviceID string) {
	if s.cm == nil {
		return
	}
	s.cm.UpdateDeviceState(deviceID, 2)
	log.Printf("[detailSvc] 设备标记待认证: id=%s", safeID(deviceID))
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.cm.GetDeviceList())
	}
}

func safeID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// ScreenshotServiceWrapper 截图服务包装。
// 注册为 Wails Service 时带 Route="/screenshots"：Service 自身实现 http.Handler
// （internal/device/screenshot/screenshot_http.go），Wails 把它挂到内部 asset
// server，前端 img src 直连 http://wails.localhost/screenshots/{key}?v={version}
// 取二进制图（绕开 base64 dataURL 过 IPC 的 1.33x 膨胀与字符串拷贝）。
type ScreenshotServiceWrapper struct {
	service       *screenshot.Service
	app           *application.App
	configManager *config.Manager // 标识解析（v2 序列号 → IP），InvalidateSlot 用
}

func NewScreenshotServiceWrapper(s *screenshot.Service) *ScreenshotServiceWrapper {
	return &ScreenshotServiceWrapper{service: s}
}

// SetConfigManager 注入配置管理器（标识解析：v2 序列号 → 真实 IP）
func (s *ScreenshotServiceWrapper) SetConfigManager(cm *config.Manager) {
	s.configManager = cm
}

func (s *ScreenshotServiceWrapper) SetApp(app *application.App) {
	s.app = app
}

func (s *ScreenshotServiceWrapper) Start() {
	s.service.SetOnUpdated(func(changedKeys []string) {
		if s.app != nil && s.app.Event != nil {
			s.app.Event.Emit("screenshot:updated", changedKeys)
		}
	})
	s.service.Start()
}

func (s *ScreenshotServiceWrapper) Stop() {
	s.service.Stop()
}

func (s *ScreenshotServiceWrapper) GetScreenshot(deviceIP string, indexNum int) (string, error) {
	return s.service.GetScreenshot(deviceIP, indexNum)
}

func (s *ScreenshotServiceWrapper) GetScreenshotVersions() map[string]int64 {
	result := s.service.GetScreenshotVersions()
	if util.DebugEnabled {
		// DEBUG: 记录特定设备的版本号
		for k, v := range result {
			if strings.Contains(k, "10.10.11.46") {
				log.Printf("[screenshot:wrapper:DEBUG] GetScreenshotVersions: key=%s version=%d", k, v)
			}
		}
	}
	return result
}

func (s *ScreenshotServiceWrapper) GetScreenshots(keys []string) map[string]string {
	result := s.service.GetScreenshots(keys)
	if util.DebugEnabled {
		// DEBUG: 记录特定设备的截图数据
		for k, v := range result {
			if strings.Contains(k, "10.10.11.46") {
				log.Printf("[screenshot:wrapper:DEBUG] GetScreenshots: key=%s dataLen=%d", k, len(v))
			}
		}
	}
	return result
}

func (s *ScreenshotServiceWrapper) TriggerNow() {
	s.service.TriggerNow()
}

// ClearCache 清除所有截图缓存（模式切换时调用，避免旧模式缓存残留）
func (s *ScreenshotServiceWrapper) ClearCache() {
	s.service.ClearCache()
}

// InvalidateSlot 清除指定坑位的截图缓存，并立即通知前端刷新。
// deviceIP 参数兼容设备标识：v2 模式前端传的是序列号（getDeviceId 语义），
// 而截图缓存 key 用真实 IP（ContainerTarget.DeviceIP）——不解析的话失效指令落空，
// 表现为操作后卡片不显示「加载中」过渡、旧截图残留到下一轮版本对比。
func (s *ScreenshotServiceWrapper) InvalidateSlot(deviceIP string, indexNum int) {
	ident := deviceIP
	if s.configManager != nil && s.configManager.GetDeviceByIP(deviceIP) == nil {
		// 不是已知主机的 IP——可能传的是序列号，解析为真实 IP
		if dev := s.configManager.GetDevice(deviceIP); dev != nil && dev.IP != "" {
			ident = dev.IP
		}
	}
	key := fmt.Sprintf("%s_%d", ident, indexNum)
	s.service.InvalidateSlot(ident, indexNum)
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("screenshot:invalidated", key)
	}
}

// InvalidateDevice 清除指定设备的全部截图缓存（设备离线清数据用，setup 状态翻转回调调用）。
// 不发 screenshot:invalidated：整设备清数据由 androidlist:updated 全量刷新驱动前端，
// 单坑位失效事件是操作后即时刷新专用的。
func (s *ScreenshotServiceWrapper) InvalidateDevice(ipPrefix string) {
	s.service.InvalidateDevice(ipPrefix)
}

// ResetBackoffAndRefresh 重置截图退避（离线/未变化跳过）并同步重抓一轮。
// 刷新按钮用：强制截图新鲜，不受退避限制；保留已有图片避免闪烁。
func (s *ScreenshotServiceWrapper) ResetBackoffAndRefresh() {
	s.service.ResetBackoff()
	s.service.RefreshNow()
}

// SetSelectedDevices 设置前端勾选的设备ID列表，并立即触发截图
func (s *ScreenshotServiceWrapper) SetSelectedDevices(deviceIDs []string) {
	s.service.SetSelectedDevices(deviceIDs)
	s.service.TriggerNow()
}

// AndroidListServiceWrapper 安卓列表服务包装
type AndroidListServiceWrapper struct {
	service       *androidlist.Service
	configManager *config.Manager
	app           *application.App
	onChanged     func(deviceIPs []string)  // 容器列表即时刷新回调（setup 注入 refreshAndroids）
	screenshotSvc *ScreenshotServiceWrapper // 刷新按钮联动刷新截图（setup 注入）
}

func NewAndroidListServiceWrapper(s *androidlist.Service, cm *config.Manager) *AndroidListServiceWrapper {
	return &AndroidListServiceWrapper{service: s, configManager: cm}
}

func (s *AndroidListServiceWrapper) SetApp(app *application.App) {
	s.app = app
}

// SetOnChanged 注入容器列表刷新回调（setup.go 的 refreshAndroids 闭包：
// public 模式网关查询 + androidlist:updated 事件；local 模式操作后零查询，事件推送接管）。
func (s *AndroidListServiceWrapper) SetOnChanged(fn func(deviceIPs []string)) {
	s.onChanged = fn
}

// SetScreenshotService 注入截图服务（刷新按钮联动刷新截图）
func (s *AndroidListServiceWrapper) SetScreenshotService(svc *ScreenshotServiceWrapper) {
	s.screenshotSvc = svc
}

// NotifyAndroidsChanged 通知容器列表已变化，触发即时刷新（立即查询+补查+追踪）。
// 供前端直调设备 API 的操作（如批量创建 POST /android）完成后调用——
// 这类操作绕过了 Go service 的 notifyAndroidsChanged，需要显式触发，否则 UI 只能手动刷新。
func (s *AndroidListServiceWrapper) NotifyAndroidsChanged(deviceIPs []string) {
	if s.onChanged != nil && len(deviceIPs) > 0 {
		s.onChanged(deviceIPs)
	}
}

// QueryDeviceByIdent 按设备标识（IP 或序列号）触发一次容器列表查询。
// v2 模式操作后即时刷新用：调用方持有的标识可能是序列号（getDeviceId 语义），
// 先解析为设备表的 (deviceID, ip) 再走 QueryDevice（v2 分支 → 81 dc list）。
func (s *AndroidListServiceWrapper) QueryDeviceByIdent(ident string) {
	for _, d := range s.configManager.GetDeviceList() {
		if d.IP == ident || d.DeviceID == ident {
			go s.service.QueryDevice(d.DeviceID, d.IP)
			return
		}
	}
	log.Printf("[androidlist] QueryDeviceByIdent 未匹配到设备: %s", ident)
}

func (s *AndroidListServiceWrapper) Start() {
	s.service.SetOnUpdated(func() {
		s.NotifyUpdated()
	})
	s.service.Start()
}

func (s *AndroidListServiceWrapper) Stop() {
	s.service.Stop()
}

func (s *AndroidListServiceWrapper) GetAndroidList() []config.AndroidInfo {
	return s.configManager.GetAndroidList()
}

func (s *AndroidListServiceWrapper) GetRunningAndroids() []config.AndroidInfo {
	return s.configManager.GetRunningAndroids()
}

func (s *AndroidListServiceWrapper) GetAndroidsByDevice(deviceID string) []config.AndroidInfo {
	return s.configManager.GetAndroidsByDevice(deviceID)
}

// SetSelectedDevices 设置前端勾选的设备ID，新选中的设备立即查询一次。
func (s *AndroidListServiceWrapper) SetSelectedDevices(deviceIDs []string) {
	s.service.SetSelectedDevices(deviceIDs)
}

// RefreshSelected 刷新当前选中的设备（刷新按钮调用），返回失败设备 IP 列表（空=全部成功）。
// 手动刷新是优先级最高且强制的：跳过离线退避与在线状态过滤（设备可能实际在线只是
// 状态表滞后）、CAS 冲突等待抢占、查询前清失败退避。列表直连设备查询（不走缓存）；
// 完成后同步重抓选中设备的截图——重置截图的离线/未变化退避并立即轮询一轮，
// 图片数据保留避免闪烁，新图按版本号替换。
func (s *AndroidListServiceWrapper) RefreshSelected() []string {
	failedIPs := s.service.RefreshSelected()
	if s.screenshotSvc != nil {
		s.screenshotSvc.ResetBackoffAndRefresh()
	}
	return failedIPs
}

// NotifyUpdated 安卓列表数据变化后由 configManager 调用，推事件+数据给前端
func (s *AndroidListServiceWrapper) NotifyUpdated() {
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("androidlist:updated", s.configManager.GetAndroidList())
	}
}

// PatchAndroidTags 乐观回填容器标签：客户端写入 /android/label 成功后立即调用，
// 设备端 WS 推回新 tags 前的过渡窗口内，前端以补丁值渲染（写后即读的 UI 即时性）。
// 返回命中的容器名列表；未命中（容器刚重建/已删除）为空，由权威推送最终收敛。
func (s *AndroidListServiceWrapper) PatchAndroidTags(deviceIP string, names []string, tags []string) []string {
	return s.configManager.PatchAndroidTags(deviceIP, names, tags)
}
