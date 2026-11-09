package screenshot

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/ports"
	"github.com/moyunteng/myt/internal/util"
)

// fetchSingle 抓取单个容器截图，失败时保留旧数据。
//
// 三条抓图通道（按优先级）：
//  1. 公网模式：publicSnap 网关透传（DeviceIP 为 device_id，无法直连）
//  2. 自定义抓图：targetFetcher（V4 走 8088 截图接口，由 setup 注入）
//  3. 本地模式：直连容器 9082 /task=snap
func (s *Service) fetchSingle(target ContainerTarget, key string) {
	// 公网模式：走网关透传截图（DeviceIP 为 device_id，无法直连）
	if target.DeviceIP != "" && s.publicSnap != nil && isDeviceID(target.DeviceIP) {
		imgData, err := s.publicSnap(target.DeviceIP, target.IndexNum)
		if err != nil || len(imgData) == 0 {
			log.Printf("[screenshot] public fetchSingle 失败 key=%s deviceID=%s index=%d err=%v len=%d",
				key, target.DeviceIP, target.IndexNum, err, len(imgData))
			s.updateError(key, "error")
			return
		}
		contentType := http.DetectContentType(imgData)
		if !strings.HasPrefix(contentType, "image/") {
			log.Printf("[screenshot] public fetchSingle 非图片 key=%s contentType=%s len=%d preview=%s",
				key, contentType, len(imgData), truncatePreview(imgData))
		}
		s.storeImage(key, imgData, contentType, "公网")
		return
	}

	// 自定义抓图通道（V4：8088 GET /android/:id/screenshot，超时/并发由注入方控制）
	if target.InstanceID != "" {
		fn := s.getTargetFetcher()
		if fn == nil {
			// 未注入却收到 V4 目标：绝不能回落到 9082（V4 主机上没有该端口，必然超时刷日志）
			s.updateError(key, "error")
			return
		}
		imgData, contentType, err := fn(s.ctx, target)
		if err != nil || len(imgData) == 0 {
			s.updateErrorWithReason(key, "error", fmt.Sprintf("v4 抓图失败 instance=%s/%s err=%v",
				target.Backend, target.InstanceID, err))
			return
		}
		s.storeImage(key, imgData, contentType, "V4")
		return
	}

	var addr ports.Addr
	if ports.IsMacvlanDirect(target.NetworkName, target.PortBindings) && target.ContainerIP != "" {
		addr = ports.Addr{Host: target.ContainerIP, Port: ports.PortMYTOS}
	} else {
		addr = ports.MappedPort(target.PortBindings, "9082", target.IndexNum, ports.OffsetMYTOS, target.DeviceIP)
	}
	url := fmt.Sprintf("http://%s:%d/task=snap&level=1", addr.Host, addr.Port)

	if util.DebugEnabled && target.DeviceIP == "10.10.11.46" {
		log.Printf("[screenshot:DEBUG] fetchSingle: key=%s networkName=%s isMacvlan=%v url=%s",
			key, target.NetworkName, ports.IsMacvlanDirect(target.NetworkName, target.PortBindings), url)
	}

	ctx, cancel := context.WithTimeout(s.ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		s.updateError(key, "error")
		return
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		s.updateError(key, "error")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.updateError(key, "error")
		return
	}

	imgData, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		s.updateError(key, "error")
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	s.storeImage(key, imgData, contentType, "")
}

// storeImage 写入截图缓存：内容未变化时只更新比较基线与退避状态，不产生新版本。
//
// 三条抓图通道共用（原先各写一份几乎相同的比较/退避/日志代码，易演化分叉）。
// label 仅用于首次成功日志的前缀（如「公网」「V4」）。
//
// 入口先做重压缩（recompressImage）：三条通道唯一汇聚点，PNG→JPEG q75 /
// 大 JPEG 重编码，把 IPC 与缓存的字节量压到 1/5~1/8（见 screenshot_recompress.go）。
func (s *Service) storeImage(key string, imgData []byte, contentType, label string) {
	if compressed, newType := recompressImage(imgData); newType != "" {
		imgData = compressed
		contentType = newType
	}
	if contentType == "" {
		contentType = http.DetectContentType(imgData)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.cache[key]
	// 用原始字节快速比较，避免 base64 字符串全量对比
	if existing != nil && existing.rawData != nil && bytes.Equal(existing.rawData, imgData) {
		// 数据未变化：保留本次抓取的字节作为下一轮比较基线（内容相同，仅换引用）。
		// 不能置 nil——置空后下一轮比较被跳过、直接按"变化"处理（版本自增+
		// 重新 base64+通知前端重取同图），unchangedCount 在 0/1 振荡，退避永远无法触发。
		existing.rawData = imgData
		existing.unchangedCount++
		existing.UpdatedAt = time.Now() // 未变化也更新时间，防止 cleanupStale 误删
		if existing.unchangedCount >= unchangedBackoffThreshold {
			existing.unchangedSkipUntil = backoffDeadline(unchangedBackoffDuration)
		}
		return
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(imgData))
	isFirst := s.cache[key] == nil
	s.cache[key] = &CacheItem{
		DataURL:     dataURL,
		rawData:     imgData, // 保留给下一轮字节比较
		contentType: contentType,
		Version:     time.Now().UnixMilli(),
		Status:    "ok",
		FailCount: 0,
		UpdatedAt: time.Now(),
	}
	if isFirst {
		log.Printf("[screenshot] ★ %s首次截图成功: key=%s len=%d", label, key, len(imgData))
	}
}

// truncatePreview 截取前 200 字节用于日志预览
func truncatePreview(data []byte) string {
	n := len(data)
	if n > 200 {
		n = 200
	}
	return string(data[:n])
}
