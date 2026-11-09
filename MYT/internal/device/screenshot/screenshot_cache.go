package screenshot

import (
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

const (
	cacheExpire       = 60 * time.Second // 延长到60秒，避免切设备时误删
	maxFailCount      = 6
	offlineSkipCycles = 30 // 离线目标跳过30轮(30s)再重试
	// syncFetchTimeout GetScreenshot 缓存未命中时同步抓图的总时限（MCP 单张请求，
	// 正常几百 ms；3s 覆盖 9082 的 fetchTimeout=2s 与公网网关透传，取保守值）
	syncFetchTimeout = 3 * time.Second
)

// CacheItem 单个容器的截图缓存
type CacheItem struct {
	DataURL   string
	rawData   []byte // 原始 JPEG 字节，用于快速比较 + HTTP 二进制通道直出
	contentType string // 图片 MIME（HTTP 通道 Content-Type 用，storeImage 写入）
	Version   int64
	Status    string // "ok" | "error" | "offline"
	FailCount int
	UpdatedAt time.Time
	SkipUntil time.Time // 离线退避：跳过轮询直到此时间

	unchangedCount     int       // 连续未变化次数
	unchangedSkipUntil time.Time // 未变化退避：跳过轮询直到此时间
}

// backoffDeadline 计算退避解禁时刻：在 d 之上叠加 [0, d/2] 的随机抖动。
// 不加抖动时，同一批被抓取的目标会同步进退避、同步出退避——实测 skipUnchanged
// 在 2↔58 之间整批共振，解除的那一轮一次涌出几十张变化，前端被迫在同一帧里
// 接收并解码几十张图（观感就是"一阵一阵地卡"）。抖动把解禁时刻摊平到窗口内。
func backoffDeadline(d time.Duration) time.Time {
	return time.Now().Add(d + time.Duration(rand.Int64N(int64(d/2)+1)))
}

// ResetBackoff 重置全部缓存项的退避状态（离线跳过/未变化跳过/失败计数），
// 保留已有图片数据避免闪烁。刷新按钮用——强制下一轮立即重新抓取，不受退避限制。
func (s *Service) ResetBackoff() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.cache {
		item.FailCount = 0
		item.SkipUntil = time.Time{}
		item.unchangedCount = 0
		item.unchangedSkipUntil = time.Time{}
	}
}

// InvalidateSlot 清除指定坑位的截图缓存（操作后立即调用，避免显示过期截图）
func (s *Service) InvalidateSlot(deviceIP string, indexNum int) {
	key := fmt.Sprintf("%s_%d", deviceIP, indexNum)
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
}

// InvalidateDevice 清除指定设备的全部截图缓存（设备离线时调用，与 InvalidateSlot 同族）。
// key 格式为 "deviceIP_indexNum"，前缀带 "_" 避免设备 IP 前缀误匹配
// （192.0.2.2_ 不会命中 192.0.2.20_1）。public 模式设备传 device_id——
// 其容器列表与截图 key 的前缀同为 device_id（sdkapi_service_droid.go ListAndroids）。
func (s *Service) InvalidateDevice(ipPrefix string) {
	if ipPrefix == "" {
		return
	}
	prefix := ipPrefix + "_"
	s.mu.Lock()
	for key := range s.cache {
		if strings.HasPrefix(key, prefix) {
			delete(s.cache, key)
		}
	}
	s.mu.Unlock()
}

// GetScreenshotVersions 返回所有缓存截图的版本号（极轻量，前端轮询用）
func (s *Service) GetScreenshotVersions() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]int64, len(s.cache))
	for key, item := range s.cache {
		result[key] = item.Version
	}
	return result
}

// GetScreenshots 返回指定 key 的截图数据
func (s *Service) GetScreenshots(keys []string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if item, ok := s.cache[key]; ok && item.DataURL != "" {
			result[key] = item.DataURL
		}
	}
	return result
}

// GetScreenshot 返回单个截图（兼容旧接口）。
//
// 缓存未命中时同步抓一次（fetchSingleOnce）：后台轮询只覆盖前端勾选的设备，
// MCP get_screenshot 对未勾选设备的槽位调用时缓存里没有图，此前直接返回空串
// （表现为"空图"）。同步抓到后照常写入缓存，后台轮询体系不受影响。
func (s *Service) GetScreenshot(deviceIP string, indexNum int) (string, error) {
	key := fmt.Sprintf("%s_%d", deviceIP, indexNum)
	s.mu.RLock()
	if item, ok := s.cache[key]; ok && item.DataURL != "" {
		defer s.mu.RUnlock()
		return item.DataURL, nil
	}
	s.mu.RUnlock()

	if data, err := s.fetchSingleOnce(deviceIP, indexNum, key); err == nil {
		return data, nil
	}
	// 同步抓失败（目标不存在/抓图失败）仍返回空串 + nil：
	// 旧语义是"未命中返回空"，调用方（MCP 工具层）已对空串做参数化报错，这里不叠加
	// fetchSingleOnce 的 error 以免覆盖掉更明确的提示。
	return "", nil
}

// fetchSingleOnce 缓存未命中时的单目标同步抓取。
//
// 与后台轮询共用 fetchSingle 三条抓图通道（公网网关 / V4 8088 / 直连 9082）；
// 目标从 getContainers 取（含校验：容器存在且 running，否则报「容器不存在」）。
// 单张 snap 正常几百 ms，超时给 3s（fetchSingle 内部 9082 路径为 2s、V4 为 15s，
// 取最长通道的保守下限）。并发未命中同一 key 时用 per-key mutex 去重，等待方
// 直接读缓存（先到者已写入）。
func (s *Service) fetchSingleOnce(deviceIP string, indexNum int, key string) (string, error) {
	// per-key 去重：多个 MCP 调用同 key 同时未命中时只抓一次，其余等结果后读缓存。
	s.onceMu.Lock()
	if s.onceKeys == nil {
		s.onceKeys = make(map[string]*sync.Mutex)
	}
	km, ok := s.onceKeys[key]
	if !ok {
		km = &sync.Mutex{}
		s.onceKeys[key] = km
	}
	s.onceMu.Unlock()

	km.Lock()
	defer func() {
		s.onceMu.Lock()
		delete(s.onceKeys, key)
		s.onceMu.Unlock()
		km.Unlock()
	}()

	// double-check：等锁期间可能已被别的调用方抓到
	s.mu.RLock()
	if item, ok := s.cache[key]; ok && item.DataURL != "" {
		defer s.mu.RUnlock()
		return item.DataURL, nil
	}
	s.mu.RUnlock()

	// 从目标源找匹配容器：校验存在且 running（后台轮询同样只抓 running）
	s.mu.RLock()
	getFn := s.getContainers
	s.mu.RUnlock()
	if getFn == nil {
		return "", fmt.Errorf("截图目标源未就绪")
	}
	var target *ContainerTarget
	for _, t := range getFn() {
		if t.DeviceIP == deviceIP && t.IndexNum == indexNum {
			tt := t
			target = &tt
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("容器不存在或未运行: key=%s（请先确认设备 IP 与坑位号，可用 list_androids 查询）", key)
	}

	done := make(chan struct{})
	var onceErr error
	util.GoSafe(func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] screenshot fetchSingleOnce recovered: %v", r)
				onceErr = fmt.Errorf("抓图协程 panic: %v", r)
			}
			close(done)
		}()
		s.fetchSingle(*target, key)
	})
	select {
	case <-done:
	case <-time.After(syncFetchTimeout):
		onceErr = fmt.Errorf("同步抓图超时(%v): key=%s", syncFetchTimeout, key)
	}

	s.mu.RLock()
	if item, ok := s.cache[key]; ok && item.DataURL != "" {
		s.mu.RUnlock()
		return item.DataURL, nil
	}
	s.mu.RUnlock()
	if onceErr != nil {
		return "", onceErr
	}
	return "", fmt.Errorf("抓图失败: key=%s（设备可能离线或容器未运行）", key)
}

// ClearCache 清除所有截图缓存
func (s *Service) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]*CacheItem)
}

// updateError 更新错误状态，不清空旧截图数据
func (s *Service) updateError(key, status string) {
	s.updateErrorWithReason(key, status, "")
}

// updateErrorWithReason 同 updateError，额外记录失败原因（V4 抓图的 503/429/超时等）。
// 原因只在连续失败次数 <= 3 时打印，避免 1s 一轮的失败刷屏。
//
// 版本号语义（2026-11 v2 稳定性修复）：失败**不翻新 Version**——版本号只在成功
// 抓图（storeImage）时前进。此前失败也写 Version=now，前端 poll 发现「版本变了但
// GetScreenshots 无数据」→ 清图闪断；v2 主机 mytsdk 忙时间歇失败时表现为截图成片
// 丢失，用户需手动刷新。现在有旧图的条目版本号静止，前端 poll 判「未变」→ 旧图
// 天然保留（ScreenshotView.tsx 的 poll 对版本未变的 key 不做任何写入，已核实）。
// 从未成功过的条目（无 DataURL、Version 恒 0）：前端 versionRef 无记录，poll 仍会
// 每轮判 changed → GetScreenshots 拉空 → 卡片标 loading，与旧行为等价，无图可清。
func (s *Service) updateErrorWithReason(key, status, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.cache[key]
	failCount := 1
	if existing != nil {
		failCount = existing.FailCount + 1
	}
	if reason != "" && failCount <= 3 {
		log.Printf("[screenshot] %s key=%s（连续失败 %d 次）", reason, key, failCount)
	}

	finalStatus := status
	if failCount >= maxFailCount {
		finalStatus = "offline"
		if existing != nil && existing.Status != "offline" {
			log.Printf("[screenshot] ★ 截图离线: key=%s (连续%d次失败)", key, failCount)
		}
	}

	if failCount <= 3 || finalStatus == "offline" {
		log.Printf("[screenshot] updateError key=%s status=%s failCount=%d finalStatus=%s versionUnchanged=true", key, status, failCount, finalStatus)
	}

	skipUntil := time.Time{}
	if finalStatus == "offline" {
		skipUntil = backoffDeadline(offlineSkipCycles * pollInterval)
	}

	if existing != nil {
		existing.FailCount = failCount
		existing.Status = finalStatus
		// 不翻新 Version（见函数注释）：旧图保留，前端无感
		existing.UpdatedAt = time.Now()
		existing.SkipUntil = skipUntil
	} else {
		// 首次失败建条目：Version 零值（0）——没有图就没有版本，前端按无图处理
		s.cache[key] = &CacheItem{
			Status:    finalStatus,
			FailCount: failCount,
			UpdatedAt: time.Now(),
			SkipUntil: skipUntil,
		}
	}
}

func (s *Service) cleanupStale() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	// 选中设备非空时，清理阈值加倍（避免切设备时误删正在显示的截图）
	threshold := cacheExpire
	if len(s.selectedDeviceIDs) > 0 {
		threshold = cacheExpire * 2
	}

	deleted := 0
	for key, item := range s.cache {
		if now.Sub(item.UpdatedAt) > threshold {
			log.Printf("[screenshot] cleanupStale 删除缓存: key=%s status=%s age=%v threshold=%v",
				key, item.Status, now.Sub(item.UpdatedAt).Round(time.Second), threshold)
			delete(s.cache, key)
			deleted++
		}
	}
	if deleted > 0 {
		log.Printf("[screenshot] cleanupStale 共删除 %d 条缓存（剩余 %d 条）", deleted, len(s.cache))
	}
}
