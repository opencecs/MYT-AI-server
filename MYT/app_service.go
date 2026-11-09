package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkver"
	"github.com/moyunteng/myt/internal/updater"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Version 由构建时 -ldflags 注入，dev 模式为 "dev"
var Version = "dev"

// BuildTime 由构建时 -ldflags 注入
var BuildTime = ""

//go:embed all:frontend/dist
var assets embed.FS

// AppService 暴露应用级信息给前端
type AppService struct {
	cm             *config.Manager
	app            *application.App
	autoUpgradeSvc *AutoUpgradeService
	sdkApiSvc      *SdkApiServiceWrapper
	paymentSvc     *PaymentServiceWrapper
	discoverSvc    *DiscoverServiceWrapper
	screenshotSvc  *ScreenshotServiceWrapper
	// eventwsResync 模式切换对账回调（setup 注入 eventwsSvc.Sync）：
	// 切 public 停掉全部本地连接，切 local 按缓存列表重建
	eventwsResync func()
	// onSwitchToLocal 切回 local 后的选中设备数据刷新回调（setup 注入）：
	// 容器列表已被切换清空，按持久化勾选（order>0）REST 补拉
	onSwitchToLocal func()
	// onSwitchToV2 切到 v2 后的选中设备数据刷新回调（setup 注入）：
	// 容器列表已被切换清空且无 WS 事件，按持久化勾选（order>0）经 Docker API 补拉
	onSwitchToV2 func()
}

func NewAppService(cm *config.Manager) *AppService {
	return &AppService{cm: cm}
}

func (s *AppService) SetApp(app *application.App) {
	s.app = app
}

func (s *AppService) SetSdkApiService(svc *SdkApiServiceWrapper) {
	s.sdkApiSvc = svc
}

func (s *AppService) SetDiscoverService(svc *DiscoverServiceWrapper) {
	s.discoverSvc = svc
}

func (s *AppService) SetScreenshotService(svc *ScreenshotServiceWrapper) {
	s.screenshotSvc = svc
}

// SetEventwsResync 注入模式切换对账回调（eventwsSvc.Sync 的闭包包装，
// 避免本服务直接依赖 eventws 包）。
func (s *AppService) SetEventwsResync(fn func()) {
	s.eventwsResync = fn
}

// SetOnSwitchToLocal 注入切回 local 后的数据刷新回调（setup 注入：
// 按持久化勾选设备补拉容器列表/详情）。public 方向不需要——
// discoverFromCloud 阶段1 已触发 TriggerNow 网关查询。
func (s *AppService) SetOnSwitchToLocal(fn func()) {
	s.onSwitchToLocal = fn
}

// SetOnSwitchToV2 注入切到 v2 后的数据刷新回调（setup 注入：勾选设备经 Docker API 补拉容器列表）
func (s *AppService) SetOnSwitchToV2(fn func()) {
	s.onSwitchToV2 = fn
}

func (s *AppService) GetVersion() string         { return Version }
func (s *AppService) GetBuildTime() string       { return BuildTime }
func (s *AppService) GetDataStoragePath() string { return s.cm.GetDataStoragePath() }
func (s *AppService) SetDataStoragePath(newPath string) error {
	return s.cm.SetDataStoragePath(newPath)
}

func (s *AppService) OpenDataDirectory() error {
	dir := s.cm.GetDataStoragePath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	return s.cm.OpenDirectory(dir)
}

func (s *AppService) GetAutoStart() bool              { return s.cm.GetAutoStart() }
func (s *AppService) SetAutoStart(enabled bool) error { return s.cm.SetAutoStart(enabled) }
func (s *AppService) GetCloseBehavior() string        { return s.cm.GetCloseBehavior() }
func (s *AppService) SetCloseBehavior(b string) error { return s.cm.SetCloseBehavior(b) }
func (s *AppService) GetDeployMode() string           { return s.cm.GetDeployMode() }

// SetDeployMode 切换部署模式。双列表隔离后，local/public 各自独立存储，切换不清空数据。
// public 模式不做本地缓存兑底：不提前 emit 缓存列表，直接拉接口后由 discoverFromCloud emit 真实列表。
// 加载状态由前端遮罩控制：等第一次 devicelist:updated 后取消遮罩。
func (s *AppService) SetDeployMode(mode string) error {
	// 代际必须在 mode 落定之前 bump：进行中的发现轮次（public 网关拉取可耗时数秒）
	// 结束时先校验 generation 再校验 mode。若先 SetDeployMode 后 BumpGeneration，
	// 中间这段窗口（含 eventwsResync + emit，公网慢时可放大到百毫秒级）里完成的
	// 旧模式轮次 generation 校验仍通过，而 SetDeviceList 已按新模式取指针 →
	// 公网列表写进 device_list_local.json 并 5s 落档，本地设备被整表抹掉。
	// 提前 bump 后：切模式一开始就让所有进行中的轮次作废，不存在该窗口。
	if s.discoverSvc != nil {
		s.discoverSvc.BumpGeneration()
	}
	if err := s.cm.SetDeployMode(mode); err != nil {
		return err
	}
	// 先对账 eventws 连接再处理容器缓存：切 public 时本地设备连接全停（新列表全为
	// 空 IP）、公网设备经网关透传建连；切 local 时反向（公网连接全停 + 按缓存列表
	// 重建本地连接，与 discover 完成后的对账幂等）。设备不可达时按退避 10s/台永久空转重拨。
	// 置于容器裁剪之前：停连接后残余帧毫秒级排空，随后的裁剪覆盖其写入。
	if s.eventwsResync != nil {
		s.eventwsResync()
	}
	// 容器列表已按模式隔离（android_list_<mode>.json），切换模式**不再清空**——
	// 各模式容器各自保留，切回来还是原来的数据（旧实现共用一份文件，只能整表清空，
	// 切回 local 得重新查一遍且期间容器列表空白）。
	// 但残留污染仍需处理：旧模式连接的残余 snapshot/容器事件会写进新模式的数据桶
	// （MergeAndroidList 不校验设备是否属于当前列表），故按「当前模式设备列表」裁剪
	// 一次——不属于当前模式设备的容器一律丢弃，当前模式自己的缓存原样保留。
	if pruned := s.cm.PruneAndroidsOutsideDeviceList(); pruned > 0 {
		log.Printf("[app] 切换模式清理跨模式残留容器 %d 个", pruned)
	}
	// 截图缓存必须清：本地模式 key="IP_indexNum"，公网模式 key="device_id_indexNum"，
	// 混用会导致截图/投屏取到别的设备的图。
	if s.screenshotSvc != nil {
		s.screenshotSvc.ClearCache()
	}
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("androidlist:updated", s.cm.GetAndroidList())
	}
	// local / v2 / v4 模式：立即推送当前列表（本地发现前先显示已有设备；v2/v4 列表来自
	// 各自独立数据桶的缓存，随后由 RefreshNow 的单次发现增量刷新）
	if (mode == "local" || mode == "v2" || mode == "v4") && s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.cm.GetDeviceList())
	}
	// 分组按模式过滤（v2 与 local 共桶、public 独立），切换模式后过滤结果变化——
	// 前端 useDeviceGroups 只订 devicegroups:updated，不补发这一条会出现
	// 「切到 v2 分组全消失」（前端缓存还是 local 的过滤结果）的现象
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicegroups:updated", s.cm.GetDeviceGroups())
	}
	// 预登录网关：切到 public 时立即用魔云腾账号登录，token 缓存到 cloudClient。
	// 后台执行（不阻塞切换返回）：token 未缓存时走 moyunteng.com 登录（15s 超时），
	// 同步执行会卡住整个切换——首轮发现（StartAutoPoll 的 discoverFromCloud）
	// 自带 EnsureCloudLogin，此处只是提前预热，放 goroutine 无序也无碍。
	if mode == "public" && s.sdkApiSvc != nil {
		go func() {
			if err := s.sdkApiSvc.EnsureCloudLogin(); err != nil {
				log.Printf("[app] 预登录网关失败（不影响功能，后台会重试）: %v", err)
			}
		}()
	}
	// 重启自动轮询：StopAutoPoll 停旧模式轮询协程（代际已在函数入口 bump，
	// 使进行中的旧轮次丢弃结果），按新模式立即发现一次。
	// public 走 StartAutoPoll（cloud 拉取需要轮询保持在线）；
	// local / v2 / v4 走 RefreshNow（单次发现）：local = UDP 广播 + v3 过滤，
	// v2 = 主机定向探测，v4 = UDP 7678 广播候选 IP + 8088 /health 验证
	// （见 discover_service_core.go 的 v4 分支）。
	if s.discoverSvc != nil {
		s.discoverSvc.StopAutoPoll()
		if mode == "public" {
			s.discoverSvc.StartAutoPoll()
		} else {
			s.discoverSvc.RefreshNow()
		}
	}
	// 切回 local：按持久化勾选（order>0）立即 REST 补拉容器列表/详情——
	// 容器列表刚被清空，WS 快照要等建连（老 SDK 404 无 WS）；且勾选集合与公网
	// 会话相同时前端不会重发 SetSelectedDevices，queryDevicesByID（只查新增）不触发。
	// 用列表 order 派生勾选而非后端 selectedIDs——后者此刻还停留在公网会话的 ID。
	if mode == "local" && s.onSwitchToLocal != nil {
		s.onSwitchToLocal()
	}
	// 切到 v2：容器列表刚被清空且无 WS 事件推送，按持久化勾选立即经 Docker API 补拉。
	// 后台执行（不阻塞切换返回）：RefreshSelected 同步等待整轮查询完成
	// （16 并发，单台 81 通道常态 3s+、超时 20s+），同步会卡住整个切换；
	// 完成后由既有 onUpdated 链路 emit androidlist:updated。
	if mode == "v2" && s.onSwitchToV2 != nil {
		go s.onSwitchToV2()
	}
	return nil
}
func (s *AppService) GetLogLevel() string            { return s.cm.GetLogLevel() }
func (s *AppService) SetLogLevel(level string) error { return s.cm.SetLogLevel(level) }
func (s *AppService) SetPaymentService(svc *PaymentServiceWrapper) {
	s.paymentSvc = svc
}

func (s *AppService) ClearAllCache() error {
	if err := s.cm.ClearAllCache(); err != nil {
		return err
	}
	// 清理缓存会清除登录凭证，需同步登出状态
	if s.paymentSvc != nil {
		s.paymentSvc.OnCacheCleared()
	}
	return nil
}
func (s *AppService) GetAutoCheckUpdate() bool           { return s.cm.GetAutoCheckUpdate() }
func (s *AppService) SetAutoCheckUpdate(e bool) error    { return s.cm.SetAutoCheckUpdate(e) }
func (s *AppService) GetAutoDownloadUpdate() bool        { return s.cm.GetAutoDownloadUpdate() }
func (s *AppService) SetAutoDownloadUpdate(e bool) error { return s.cm.SetAutoDownloadUpdate(e) }
func (s *AppService) GetAutoUpgradeSDK() bool            { return s.cm.GetAutoUpgradeSDK() }
func (s *AppService) GetPlatform() string                { return updater.GetPlatform() }

func (s *AppService) SetAutoUpgradeSDK(enabled bool) error {
	if err := s.cm.SetAutoUpgradeSDK(enabled); err != nil {
		return err
	}
	if s.autoUpgradeSvc == nil {
		s.autoUpgradeSvc = NewAutoUpgradeService(s.cm, s.sdkApiSvc, sdkver.NewClient(s.cm.UpdateSDKDir()))
	}
	s.autoUpgradeSvc.SetEnabled(enabled)
	return nil
}

// ManualUpgradeSDK 手动触发一次 SDK 升级轮询（通知后台 goroutine 立即执行，复用已下载的 SDK 文件）
func (s *AppService) ManualUpgradeSDK() error {
	if s.autoUpgradeSvc == nil {
		return fmt.Errorf("SDK升级服务未初始化")
	}
	log.Println("[app-service] 手动触发SDK升级轮询")
	s.autoUpgradeSvc.TriggerManual()
	return nil
}

// ManualUpgradeDevice 手动升级单台设备（立即执行，不依赖轮询周期）
func (s *AppService) ManualUpgradeDevice(deviceID string) ManualUpgradeResult {
	if s.autoUpgradeSvc == nil {
		return ManualUpgradeResult{DeviceID: deviceID, Status: "failed", Message: "SDK升级服务未初始化"}
	}
	log.Printf("[app-service] 手动升级设备: %s", deviceID)
	return s.autoUpgradeSvc.ManualUpgradeDevice(deviceID)
}

// UpdateCheckResult 检查更新结果，前端使用
type UpdateCheckResult struct {
	HasUpdate    bool   `json:"hasUpdate"`
	Version      string `json:"version,omitempty"`
	DownloadURL  string `json:"downloadUrl,omitempty"`
	Checksum     string `json:"checksum,omitempty"`
	FileSize     int64  `json:"fileSize,omitempty"`
	ReleaseNotes string `json:"releaseNotes,omitempty"`
	Mandatory    bool   `json:"mandatory,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (s *AppService) CheckUpdate() UpdateCheckResult {
	client := updater.NewClient()
	info, err := client.Check(Version, updater.GetPlatform(), "")
	if err != nil {
		log.Printf("[updater] 检查更新失败: %v", err)
		return UpdateCheckResult{Error: err.Error()}
	}
	if info == nil {
		return UpdateCheckResult{HasUpdate: false}
	}
	return UpdateCheckResult{
		HasUpdate: true, Version: info.Version, DownloadURL: info.DownloadURL,
		Checksum: info.Checksum, FileSize: info.FileSize,
		ReleaseNotes: info.ReleaseNotes, Mandatory: info.Mandatory,
	}
}

// DownloadUpdateProgress 下载更新进度
type DownloadUpdateProgress struct {
	Downloading bool   `json:"downloading"`
	Progress    int    `json:"progress"`
	Step        string `json:"step,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (s *AppService) DownloadUpdate(downloadUrl, checksum string) error {
	if downloadUrl == "" {
		return fmt.Errorf("下载地址为空")
	}
	if !updater.AcquireUpdateLock() {
		return fmt.Errorf("更新正在进行中，请勿重复操作")
	}
	go s.doDownloadUpdate(downloadUrl, checksum)
	return nil
}

func (s *AppService) doDownloadUpdate(downloadUrl, checksum string) {
	defer updater.ReleaseUpdateLock()

	emit := func(step string, progress int, err string) {
		if s.app != nil {
			s.app.Event.Emit("update:download-progress", DownloadUpdateProgress{
				Downloading: err == "" && step != "", Progress: progress, Step: step, Error: err,
			})
		}
	}

	// 1. 下载到临时目录
	tmpDir, err := os.MkdirTemp("", "myt-update-*")
	if err != nil {
		emit("", 0, "创建临时目录失败")
		return
	}
	zipPath := filepath.Join(tmpDir, "update.zip")

	dl := updater.NewDownloader()
	if err := dl.Download(downloadUrl, zipPath, func(percent int, _, _ int64) { emit("正在下载...", percent, "") }); err != nil {
		os.RemoveAll(tmpDir)
		emit("", 0, "下载失败: "+err.Error())
		return
	}

	// 2. SHA256 校验
	emit("正在校验...", 100, "")
	if err := updater.VerifyChecksum(zipPath, checksum); err != nil {
		os.RemoveAll(tmpDir)
		emit("", 0, "校验失败: "+err.Error())
		return
	}

	// 3. 启动 --update 进程替换文件
	emit("正在替换，即将重启...", 100, "")
	pid := os.Getpid()
	var elevateErr error
	if updater.NeedsElevation() {
		elevateErr = updater.StartElevatedUpdate(zipPath, pid)
	} else {
		elevateErr = updater.StartNormalUpdate(zipPath, pid)
	}
	if elevateErr != nil {
		os.RemoveAll(tmpDir)
		emit("", 0, "启动更新进程失败: "+elevateErr.Error())
		return
	}

	// 更新进程接管，当前应用退出
	if s.cm != nil {
		s.cm.StopFlushLoop()
	}
	os.Exit(0)
}

func (s *AppService) RestartApp() error {
	return updater.RestartApp()
}

// SDKUpgradeStatus 单台设备的 SDK 升级状态（前端判断显示升级按钮还是已最新）。
type SDKUpgradeStatus struct {
	DeviceID       string `json:"deviceId"`
	CurrentVersion int    `json:"currentVersion"`
	LatestVersion  int    `json:"latestVersion"`
	NeedsUpgrade   bool   `json:"needsUpgrade"`
	Online         bool   `json:"online"`
}

// GetSDKUpgradeStatus 查询多台设备的 SDK 升级状态（并查询版本）。
// deviceIdentifiers 为设备 IP 或 device_id（public 模式）。
func (s *AppService) GetSDKUpgradeStatus(deviceIdentifiers []string) []SDKUpgradeStatus {
	results := make([]SDKUpgradeStatus, 0, len(deviceIdentifiers))
	if s.sdkApiSvc == nil {
		return results
	}
	for _, id := range deviceIdentifiers {
		status := SDKUpgradeStatus{DeviceID: id, Online: true}
		info, err := s.sdkApiSvc.SdkCheckUpgrade(id)
		if err != nil {
			status.Online = false
			results = append(results, status)
			continue
		}
		status.CurrentVersion = info.CurrentVersion
		status.LatestVersion = info.LatestVersion
		status.NeedsUpgrade = info.LatestVersion > info.CurrentVersion
		results = append(results, status)
	}
	return results
}
