package main

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/sdkver"
	"github.com/moyunteng/myt/internal/util"
)

// upgradeResult 单台设备的升级结果
type upgradeResult int

const (
	resultUpgraded upgradeResult = iota // 已触发升级
	resultSkipped                       // 版本一致，无需升级
	resultFailed                        // 升级失败
)

// ManualUpgradeResult 手动升级单台设备的返回结果
type ManualUpgradeResult struct {
	DeviceID string `json:"deviceId"`
	IP       string `json:"ip"`
	Status   string `json:"status"` // "upgraded" | "skipped" | "failed" | "abandoned"
	Message  string `json:"message"`
}

// failRecord 记录设备连续失败次数
// 连续失败 2 次后永久放弃该设备，直到客户端重启
type failRecord struct {
	mu    sync.Mutex
	count int // 连续失败次数
}

// AutoUpgradeService SDK 自动升级服务
// 首次启动从公网下载最新 SDK，之后每 2 分钟轮询在线设备版本并推送升级
type AutoUpgradeService struct {
	cm         *config.Manager
	sdkSvc     *SdkApiServiceWrapper
	sdkVer     *sdkver.Client
	stopCh     chan struct{}
	manualCh   chan struct{}
	wg         sync.WaitGroup
	localVer   atomic.Int64 // 本地 SDK 版本缓存
	lastRemote atomic.Int64 // 上次公网查询时间 (unix seconds)
	failMap    sync.Map     // deviceID → *failRecord, 2次失败后放弃
	running    atomic.Bool  // 是否正在运行
}

func NewAutoUpgradeService(cm *config.Manager, sdkSvc *SdkApiServiceWrapper, sdkVer *sdkver.Client) *AutoUpgradeService {
	log.Printf("[sdk-upgrade] 服务创建, SDK目录: %s", cm.UpdateSDKDir())
	return &AutoUpgradeService{
		cm:       cm,
		sdkSvc:   sdkSvc,
		sdkVer:   sdkVer,
		stopCh:   make(chan struct{}),
		manualCh: make(chan struct{}, 1),
	}
}

func (s *AutoUpgradeService) Start() {
	if s.running.Load() {
		return
	}
	s.running.Store(true)
	s.stopCh = make(chan struct{})
	log.Printf("[sdk-upgrade] 服务启动")
	s.wg.Add(1)
	util.GoSafe(func() {
		defer s.wg.Done()
		s.run()
	})
}

func (s *AutoUpgradeService) Stop() {
	if !s.running.Load() {
		return
	}
	log.Printf("[sdk-upgrade] 服务停止中...")
	s.running.Store(false)
	close(s.stopCh)
	s.wg.Wait()
}

// SetEnabled 动态启停 SDK 自动升级服务
func (s *AutoUpgradeService) SetEnabled(enabled bool) {
	if enabled {
		s.Start()
	} else {
		s.Stop()
	}
}

func (s *AutoUpgradeService) run() {
	s.initSDK()

	// 30 分钟轮询：latestVersion 是 SDK 转发 newapi 的外网查询（2min=每天720次外网请求，
	// 文档 §11 #7 建议拉长）；已装版本由 WS hello 实时感知，手动升级走 manualCh 即时触发
	util.Debugf("[sdk-upgrade] 开始30分钟轮询")
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	s.pollDevices()

	for {
		select {
		case <-ticker.C:
			s.pollDevices()
		case <-s.manualCh:
			log.Println("[sdk-upgrade] 收到手动升级信号，立即轮询")
			s.pollDevices()
		case <-s.stopCh:
			log.Printf("[sdk-upgrade] 服务已停止")
			return
		}
	}
}

// TriggerManual 通知后台 goroutine 立即执行一次轮询（串行执行，与 ticker 互斥）
func (s *AutoUpgradeService) TriggerManual() {
	select {
	case s.manualCh <- struct{}{}:
	default:
		// 通道已有待处理信号，不重复堆积
	}
}

// ManualUpgradeDevice 手动升级单台设备（立即执行，复用已有 SDK 文件）
func (s *AutoUpgradeService) ManualUpgradeDevice(deviceID string) ManualUpgradeResult {
	result := ManualUpgradeResult{DeviceID: deviceID}

	if s.shouldSkip(deviceID) {
		result.Status = "abandoned"
		result.Message = "该设备连续失败次数过多，已被放弃（重启客户端后重置）"
		return result
	}

	s.refreshLocalSDK()

	localVer := int(s.localVer.Load())
	if localVer <= 0 {
		result.Status = "failed"
		result.Message = "云端版本查询失败，请检查网络后重试"
		return result
	}

	devices := s.cm.GetOnlineDevices()
	for _, d := range devices {
		if d.DeviceID == deviceID {
			result.IP = d.IP
			r := s.upgradeIfNeeded(deviceID, d.IP, localVer)
			switch r {
			case resultUpgraded:
				result.Status = "upgraded"
				result.Message = "升级已触发，等待设备确认"
			case resultSkipped:
				result.Status = "skipped"
				result.Message = "设备SDK版本已是最新"
			case resultFailed:
				result.Status = "failed"
				result.Message = s.failReason(deviceID)
			}
			return result
		}
	}

	result.Status = "failed"
	result.Message = "设备不在线或未找到"
	return result
}

// failReason 返回设备升级失败的可读描述
func (s *AutoUpgradeService) failReason(deviceID string) string {
	if val, ok := s.failMap.Load(deviceID); ok {
		if rec, ok := val.(*failRecord); ok {
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.count >= 2 {
				return fmt.Sprintf("升级失败 %d 次，设备已被放弃（重启客户端可重置）", rec.count)
			}
			return fmt.Sprintf("升级失败 %d 次，请检查设备和网络", rec.count)
		}
	}
	return "升级失败，请检查设备和网络"
}

func (s *AutoUpgradeService) initSDK() {
	log.Printf("[sdk-upgrade] 首次初始化: 查询公网版本...")

	version, err := s.sdkVer.FetchLatestVersion()
	if err != nil {
		log.Printf("[sdk-upgrade] 查询公网版本失败: %v，将使用本地缓存", err)
		localVer := s.sdkVer.LocalVersion()
		s.localVer.Store(int64(localVer))
		if localVer > 0 {
			log.Printf("[sdk-upgrade] 使用本地缓存版本: %d", localVer)
		} else {
			log.Printf("[sdk-upgrade] 本地无缓存, 等待下次轮询重试")
		}
		return
	}

	s.localVer.Store(int64(version))
	s.lastRemote.Store(time.Now().Unix())
	localCacheVer := s.sdkVer.LocalVersion()
	log.Printf("[sdk-upgrade] 公网最新版本: %d, 本地缓存版本: %d", version, localCacheVer)
}

func (s *AutoUpgradeService) pollDevices() {
	// public/v2 模式设备无 SDK API（公网无局域网 IP / V2 主机是裸 Docker），直连会刷屏报错，停用自动升级。
	// v4 同理：V4 主机（tobox_player）只监听 8088，没有 V3 SDK 的版本/升级接口，
	// 轮询只会逐台失败刷日志（升级链路属 V3 容器体系，V4 无对应物）。
	if s.cm != nil {
		switch s.cm.GetDeployMode() {
		case "public", "v2", "v4":
			return
		}
	}

	s.refreshLocalSDK()

	localVer := int(s.localVer.Load())
	if localVer <= 0 {
		log.Printf("[sdk-upgrade] 本地版本无效(%d), 跳过本轮轮询", localVer)
		return
	}

	devices := s.cm.GetOnlineDevices()
	if len(devices) == 0 {
		return
	}

	util.Debugf("[sdk-upgrade] 开始轮询 %d 台在线设备, 云端最新版本: %d", len(devices), localVer)

	results := make([]upgradeResult, len(devices))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for i, d := range devices {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, deviceID, ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = s.upgradeIfNeeded(deviceID, ip, localVer)
		}(i, d.DeviceID, d.IP)
	}
	wg.Wait()

	var upgradeCount, skipCount, failCount, abandonedCount int
	for _, r := range results {
		switch r {
		case resultUpgraded:
			upgradeCount++
		case resultSkipped:
			skipCount++
		case resultFailed:
			failCount++
		}
	}

	if upgradeCount > 0 || failCount > 0 {
		// 统计已放弃的设备数
		s.failMap.Range(func(_, _ any) bool { abandonedCount++; return true })
		util.Debugf("[sdk-upgrade] 轮询完成: 升级 %d 台, 无需升级 %d 台, 失败 %d 台, 已放弃 %d 台", upgradeCount, skipCount, failCount, abandonedCount)
	}
}

func (s *AutoUpgradeService) refreshLocalSDK() {
	last := s.lastRemote.Load()
	if time.Since(time.Unix(last, 0)) < 10*time.Minute {
		return
	}

	version, err := s.sdkVer.FetchLatestVersion()
	if err != nil {
		log.Printf("[sdk-upgrade] 刷新版本查询失败: %v", err)
		return
	}
	s.lastRemote.Store(time.Now().Unix())

	currentLocal := int(s.localVer.Load())
	if version != currentLocal {
		log.Printf("[sdk-upgrade] 发现新版本 %d (当前 %d)", version, currentLocal)
		s.localVer.Store(int64(version))
	}
}

// shouldSkip 检查设备是否已达到失败上限（2次），达到后永久跳过直到客户端重启
func (s *AutoUpgradeService) shouldSkip(deviceID string) bool {
	val, ok := s.failMap.Load(deviceID)
	if !ok {
		return false
	}
	rec := val.(*failRecord)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.count >= 2
}

// recordSuccess 记录设备升级成功，清除失败记录
func (s *AutoUpgradeService) recordSuccess(deviceID string) {
	s.failMap.Delete(deviceID)
}

// recordFail 记录设备升级失败，连续 2 次失败后永久放弃该设备
func (s *AutoUpgradeService) recordFail(deviceID string) {
	val, ok := s.failMap.Load(deviceID)
	if !ok {
		s.failMap.Store(deviceID, &failRecord{count: 1})
		log.Printf("[sdk-upgrade] %s 失败 1 次", clipID(deviceID))
		return
	}
	rec := val.(*failRecord)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.count++
	if rec.count >= 2 {
		log.Printf("[sdk-upgrade] %s 连续失败 %d 次, 已放弃该设备(重启客户端后重置)", clipID(deviceID), rec.count)
	} else {
		log.Printf("[sdk-upgrade] %s 失败 %d 次", clipID(deviceID), rec.count)
	}
}

func (s *AutoUpgradeService) upgradeIfNeeded(deviceID, ip string, localVer int) upgradeResult {
	// 检查是否已达到失败上限
	if s.shouldSkip(deviceID) {
		return resultSkipped
	}

	shortID := clipID(deviceID)
	client := sdkapi.NewClient(ip)
	client.SetPassword(s.sdkSvc.GetDevicePassword(ip))

	// 已装版本优先读 WS hello 缓存（设备重启必经 WS 重连 hello，版本最权威，
	// 且免去逐台 HTTP 探测）；无缓存（老 SDK <208 无 WS）才回退 HTTP 查询。
	// 前端版本查询（sdkapi GetAPIVersionInfo）也会反哺这份缓存。
	deviceVer := 0
	if d := s.cm.GetDeviceDetail(deviceID); d != nil {
		deviceVer = d.SDKVersion
	}
	if deviceVer <= 0 {
		info, err := client.GetAPIVersionInfo()
		if err != nil {
			log.Printf("[sdk-upgrade] %s(%s) 查询版本失败: %v", shortID, ip, err)
			s.recordFail(deviceID)
			return resultFailed
		}
		deviceVer = info.CurrentVersion
	}

	if deviceVer == localVer {
		s.recordSuccess(deviceID)
		return resultSkipped
	}

	log.Printf("[sdk-upgrade] %s(%s) 版本不一致: 设备=%d, 云端最新=%d, 触发设备自升级", shortID, ip, deviceVer, localVer)

	// 设备自己从云端拉取最新 SDK，客户端不再上传文件
	upgraded, err := client.TriggerDeviceUpgrade()
	if err != nil {
		log.Printf("[sdk-upgrade] %s(%s) 触发设备升级失败: %v", shortID, ip, err)
		s.recordFail(deviceID)
		return resultFailed
	}
	if !upgraded {
		// 设备认为已是最新（可能版本号比对异常，或刚升完还没刷新）
		s.recordSuccess(deviceID)
		return resultSkipped
	}

	log.Printf("[sdk-upgrade] %s(%s) 设备自升级完成", shortID, ip)
	return resultUpgraded
}

func clipID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
