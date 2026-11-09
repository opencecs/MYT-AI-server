package devicedetail

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/util"
)

const (
	httpTimeout = 8 * time.Second
	mb          = 1048576 // MB 转 bytes
)

type Service struct {
	mu      sync.Mutex
	stopCh  chan struct{}
	wg      sync.WaitGroup
	running bool

	configManager     *config.Manager
	getDevicePassword func(deviceIP string) string
	getIscsiDiskUsage func(deviceIP string) (usedBytes, totalBytes int64, ok bool)
	onUpdated         func()                                        // 数据更新后回调，用于通知前端刷新
	onDeviceUpdated   func(deviceID string, d *config.DeviceDetail) // 单设备更新回调（弹窗实时刷新用）

	// 事件通知节流（devicedetail_events.go）：100ms 合并窗口
	notifyMu    sync.Mutex
	notifyTimer *time.Timer

	// diskFrameSeen 标记设备已发过 system/stats disk 帧（新 SDK）。
	// panel 帧据此跳过磁盘更新（新 SDK 的 panel mmctotal/mmcuse 是设备端冻结值，
	// 磁盘权威源已切换为 disk 帧）。进程生命周期状态，无需持久化。
	diskMu        sync.Mutex
	diskFrameSeen map[string]bool

	ctx    context.Context
	cancel context.CancelFunc
}

func NewService(cm *config.Manager) *Service {
	return &Service{
		configManager: cm,
		stopCh:        make(chan struct{}),
		diskFrameSeen: make(map[string]bool),
	}
}

func (s *Service) SetPasswordGetter(fn func(deviceIP string) string) {
	s.mu.Lock()
	s.getDevicePassword = fn
	s.mu.Unlock()
}

// SetIscsiDiskUsageGetter 注入 iSCSI 磁盘用量查询回调（避免循环依赖）
func (s *Service) SetIscsiDiskUsageGetter(fn func(deviceIP string) (usedBytes, totalBytes int64, ok bool)) {
	s.mu.Lock()
	s.getIscsiDiskUsage = fn
	s.mu.Unlock()
}

// SetOnUpdated 设置轮询完成后的回调（用于推送前端事件）
func (s *Service) SetOnUpdated(fn func()) {
	s.mu.Lock()
	s.onUpdated = fn
	s.mu.Unlock()
}

// SetOnDeviceUpdated 设置单设备更新回调（设备详情弹窗实时刷新：system/stats 到达即推送该设备）
func (s *Service) SetOnDeviceUpdated(fn func(deviceID string, d *config.DeviceDetail)) {
	s.mu.Lock()
	s.onDeviceUpdated = fn
	s.mu.Unlock()
}

func (s *Service) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.mu.Unlock()
	// 无周期轮询：设备指标由 eventws system/stats 事件推送（20s，见 devicedetail_events.go）。
	// REST 查询仅剩两个触发点：设备上线一次性 QueryDevice（静态字段+memtotal 基准）、
	// VerifyDeviceAuth 认证验证。
}

func (s *Service) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.cancel()
	close(s.stopCh)
	s.running = false
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) QueryDevice(deviceID, ip string) {
	s.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		defer s.wg.Done()
		s.fetchSingle(deviceID, ip)
	}()
}

// QueryDeviceSync 同步查询单个设备，返回 fetchResult 值（0=成功, 1=认证失败, 2=其他失败）
func (s *Service) QueryDeviceSync(deviceID, ip string) int {
	result := s.fetchSingle(deviceID, ip)
	switch result {
	case fetchSuccess:
		return 0
	case fetchAuth:
		return 1
	default:
		return 2
	}
}

// FetchDeviceInfo 同步拉取单台设备全量信息并落缓存（设备详情弹窗「打开/刷新」用）。
// 成功返回原始 *sdkapi.DeviceInfo（含 hwaddr 等展示字段）并完成与 fetchSingle
// 一致的落库（磁盘/内存更新 + mergeDeviceDetail 覆盖缓存，静态字段非空才写）。
// 仅 local 模式可用——public/v2 由调用方降级到 SDK 网关查询（仅显示，不落缓存）。
// 返回 code：0=成功 1=认证失败 2=其他失败（fetchResult 语义）。
func (s *Service) FetchDeviceInfo(deviceID, ip string) (*sdkapi.DeviceInfo, int) {
	info, result := s.doFetch(ip)
	switch result {
	case fetchSuccess:
		if info != nil {
			s.updateDeviceDisk(deviceID, ip, info.MMCUse*mb, info.MMCTotal*mb)
			s.configManager.UpdateDeviceMem(deviceID, info.MemUse*mb, info.MemTotal*mb)
			s.mergeDeviceDetail(deviceID, ip, info)
		}
	case fetchAuth:
		s.configManager.UpdateDeviceFields(deviceID, "", "", "", "", 2)
	case fetchFail:
	}
	if result != fetchSuccess {
		return nil, int(result)
	}
	return info, int(fetchSuccess)
}

type fetchResult int

const (
	fetchSuccess fetchResult = iota
	fetchAuth
	fetchFail
)

func (s *Service) fetchSingle(deviceID, ip string) fetchResult {
	info, result := s.doFetch(ip)
	switch result {
	case fetchSuccess:
		if info != nil {
			s.updateDeviceDisk(deviceID, ip, info.MMCUse*mb, info.MMCTotal*mb)
			s.configManager.UpdateDeviceMem(deviceID, info.MemUse*mb, info.MemTotal*mb)
			s.mergeDeviceDetail(deviceID, ip, info)
		}
	case fetchAuth:
		s.configManager.UpdateDeviceFields(deviceID, "", "", "", "", 2)
	case fetchFail:
	}
	return result
}

// mergeDeviceDetail REST 全量结果落 DeviceDetail 缓存（静态字段唯一可靠来源）。
// hello 只写 model/version（可缺省）、system/stats 只带动态字段——此前 REST 拿到的
// model/version/mmcmodel/speed 被直接丢弃，缓存里这些字段恒空，心跳推送经
// devicedetail:updated 合并后把弹窗从 REST 拿到的全量值洗成 '-'。
// 非空才写：不把已有值（hello 写入的 model 等）洗成空。
func (s *Service) mergeDeviceDetail(deviceID, ip string, info *sdkapi.DeviceInfo) {
	existing := s.configManager.GetDeviceDetail(deviceID)
	if existing == nil {
		existing = &config.DeviceDetail{DeviceID: deviceID}
	}
	if info.DeviceID != "" {
		existing.DeviceID = info.DeviceID
	}
	if ip != "" {
		existing.IP = ip
	}
	if info.Model != "" {
		existing.Model = info.Model
	}
	if info.Version != "" {
		existing.Version = info.Version
	}
	if info.MMCModel != "" {
		existing.MMCModel = info.MMCModel
	}
	if info.Speed != "" {
		existing.Speed = info.Speed
	}
	existing.CPUTemp = config.FlexInt(info.CPUTemp)
	if info.CPULoad != "" {
		existing.CPULoad = info.CPULoad
	}
	existing.MMCTotal = config.FlexInt(info.MMCTotal)
	existing.MMCUse = config.FlexInt(info.MMCUse)
	if info.MemTotal > 0 {
		existing.MemTotal = strconv.FormatInt(info.MemTotal, 10)
	}
	if info.MemUse > 0 {
		existing.MemUse = strconv.FormatInt(info.MemUse, 10)
	}
	s.configManager.SetDeviceDetail(deviceID, existing)
}

// updateDeviceDisk 从设备实时状态获取磁盘数据：iSCSI 模式用 iSCSI 数据，否则用 eMMC 数据。
// public 设备（ip 为空——system/stats 事件不带 ip 字段）跳过 iSCSI 查询直接用 eMMC：
// 空标识只会在 newClient 里得到错误客户端，查询必然失败，徒增每 20s/台的无效调用。
func (s *Service) updateDeviceDisk(deviceID, ip string, mmcUse, mmcTotal int64) {
	s.mu.Lock()
	fn := s.getIscsiDiskUsage
	s.mu.Unlock()
	if ip != "" && fn != nil {
		if used, total, ok := fn(ip); ok {
			s.configManager.UpdateDeviceDisk(deviceID, used, total)
			return
		}
	}
	// 非 iSCSI 模式或查询失败，用 eMMC 数据
	s.configManager.UpdateDeviceDisk(deviceID, mmcUse, mmcTotal)
}

func (s *Service) doFetch(ip string) (*sdkapi.DeviceInfo, fetchResult) {
	// public 模式设备无局域网 IP，直连会失败，跳过（设备详情后续走线上获取）
	if ip == "" {
		return nil, fetchFail
	}
	util.AcquireDeviceHTTP()
	defer util.ReleaseDeviceHTTP()
	client := sdkapi.NewClientWithTimeout(ip, httpTimeout)
	s.mu.Lock()
	pwFn := s.getDevicePassword
	s.mu.Unlock()
	if pwFn != nil {
		pwd := pwFn(ip)
		if pwd != "" {
			client.SetPassword(pwd)
		}
	}
	client.SetContext(s.ctx)

	info, err := client.GetDeviceInfo()
	if err != nil {
		if _, ok := err.(*sdkapi.AuthError); ok {
			log.Printf("[devicedetail] 设备 %s 认证失败", ip)
			return nil, fetchAuth
		}
		log.Printf("[devicedetail] 查询设备 %s 失败: %v", ip, err)
		return nil, fetchFail
	}

	return info, fetchSuccess
}
