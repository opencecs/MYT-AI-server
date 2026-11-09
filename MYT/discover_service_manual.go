package main

import (
	"context"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
)

// Discover 手动发现（前端"扫描网络"调用）：纯发现，有界 ~2.5s（UDP listenTimeout）。
// 语义（用户澄清 2026-09-04）：扫描只把内网设备扫进「待添加」列表——
//   - 不对已添加设备做任何操作（不更新 IP/型号，正式列表零改动）
//   - 不 emit devicelist:updated（正式列表没变；且该事件会触发授权页全量重查授权，
//     扫描一次 = 93 台外网请求脉冲——之前"扫描特别慢"的根源）
//   - 只有 pendinglist:updated（待添加列表刷新）
//
// 已存在设备的 IP 漂移修正走设备列表「刷新」按钮（RefreshDevices → discoverAndUpdate
// 增量合并路径，2026-09-05 起）。
func (s *DiscoverServiceWrapper) Discover() ([]discover.Device, error) {
	// v4 模式守卫：本方法走 UDP 广播 + LocalDevices(v3) 过滤，在 v4 下跑会把 v3 设备
	// 灌进 v4 待添加列表（跨模式串台）。V4 的扫描入口是 V4ServiceWrapper.ScanV4Hosts
	// （广播 + 8088 /health 验证），这里直接返回当前列表。
	if s.configManager.GetDeployMode() == "v4" {
		return s.configManager.GetDeviceListAsDiscover(), nil
	}
	// 防重入：复用 polling 标志——若自动轮询进行中则直接返回
	if !atomic.CompareAndSwapInt32(&s.polling, 0, 1) {
		return s.configManager.GetDeviceListAsDiscover(), nil
	}
	defer atomic.StoreInt32(&s.polling, 0)

	devs, err := s.discoverer.Discover(context.Background())
	if err != nil {
		return nil, err
	}
	// 本地模式只收 v3 设备：UDP 广播是全量回复，V2 主机（_v2）也监听 7678，
	// 不过滤会混进 local 待添加列表（V2 主机由 v2 模式的 ScanV2Hosts 负责）
	devs = discover.LocalDevices(devs)

	// 只收集不在正式列表的设备 → 待添加；已存在的一律跳过（扫描是纯发现）
	added := 0
	var lastEmit time.Time
	for _, dev := range devs {
		if s.configManager.GetDevice(dev.DeviceID) != nil {
			continue
		}
		s.configManager.UpsertPendingDevice(config.DeviceCache{
			DeviceID:   dev.DeviceID,
			IP:         dev.IP,
			DeviceType: dev.DeviceType,
			DeviceName: dev.DeviceName,
			Version:    dev.Version,
			State:      1,
			Order:      0,
		})
		added++
		// 流式推送（150ms 节流）：前端扫描态的计数/雷达 blip 实时更新。
		// 不节流时几百台设备 = 几百个全量 pendinglist 事件 × 前端全树重渲染，
		// 扫描瞬间把 UI 打满（2026-09-05 几百台卡顿教训）。
		if s.app != nil && s.app.Event != nil && time.Since(lastEmit) >= 150*time.Millisecond {
			lastEmit = time.Now()
			s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
		}
	}

	log.Printf("[discover:scan] 扫描完成: 回复 %d 台, 待添加新增 %d 台（正式列表未动）", len(devs), added)

	if added > 0 {
		if s.app != nil && s.app.Event != nil {
			s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
		}
	}

	return s.configManager.GetDeviceListAsDiscover(), nil
}

// DiscoverDevicesManually 手动输入IP添加设备（前端调用）。
// 按部署模式分流（local/public/v2 行为不变）：
//   - v4：直接探 IP 的 8088 /health（不走 UDP 单播、不做 LocalDevices 版本过滤——
//     V4 设备的版本标识未定，套 v3 过滤会把合法 V4 设备当「无响应」丢掉）。
//     探测通过即写入正式列表（手动添加是明确的用户意图），并清掉待添加残留。
//   - local（含未识别模式）：UDP 单播 + LocalDevices(version=="v3") 过滤，原行为。
func (s *DiscoverServiceWrapper) DiscoverDevicesManually(ips string) ([]discover.Device, []string) {
	ipList := parseIPList(ips)
	if len(ipList) == 0 {
		return nil, nil
	}
	if s.configManager.GetDeployMode() == "v4" {
		return s.discoverDevicesManuallyV4(ipList)
	}

	found, failedIPs := s.discoverer.DiscoverManually(context.Background(), ipList)
	// 本地手动添加只收 v3 设备（_v2 回复是 V2 主机，走 v2 模式的手动添加）
	found = discover.LocalDevices(found)

	for _, dev := range found {
		cacheItem := config.DeviceCache{
			DeviceID:   dev.DeviceID,
			IP:         dev.IP,
			DeviceType: dev.DeviceType,
			DeviceName: dev.DeviceName,
			Version:    dev.Version,
			State:      1,
			Order:      0,
		}
		if s.ipMappingSetter != nil {
			s.ipMappingSetter(dev.IP, dev.DeviceID)
		}
		// 手动添加是明确的用户意图：新设备直接进正式设备列表（不随自动扫描走"待添加"）
		if existing := s.configManager.GetDevice(dev.DeviceID); existing != nil {
			changed := s.configManager.UpsertDeviceCheck(cacheItem)
			if changed {
				log.Printf("[discover:manual] 设备变化: id=%s ip=%s", dev.DeviceID, dev.IP)
				if s.onNewDevice != nil {
					s.onNewDevice(dev.DeviceID, dev.IP)
				}
			}
		} else {
			s.configManager.UpsertDevice(cacheItem)
			// 之前扫描可能已把它留在待添加列表，清掉防两处重复显示
			s.configManager.RemovePendingDevice(dev.DeviceID)
			log.Printf("[discover:manual] 手动添加新设备: id=%s ip=%s", dev.DeviceID, dev.IP)
		}
	}
	s.configManager.SortDeviceListByIP()

	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
		if s.configManager.PendingDevicesCount() > 0 {
			s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
		}
	}
	if s.eventwsSyncer != nil {
		s.eventwsSyncer()
	}

	return found, failedIPs
}

// discoverDevicesManuallyV4 v4 手动添加：逐台 GET http://{ip}:8088/health，通过即入正式列表。
// 与 local 版的差异：不做 UDP 单播探测（V4 /health 自身就能拿到 deviceId），
// 不做版本过滤；写入目标 UpsertDevice 按当前 mode 取指针，天然进 v4 数据桶。
// 不触发 eventws 对账：V4 的 WS 通道尚未确认，盲连只会反复失败刷日志。
func (s *DiscoverServiceWrapper) discoverDevicesManuallyV4(ipList []string) ([]discover.Device, []string) {
	if s.v4Prober == nil {
		log.Printf("[discover:manual] v4 模式但 V4 探测通道未注入，跳过本轮")
		return nil, ipList
	}

	infos, failedIPs := s.v4Prober(context.Background(), ipList)
	found := make([]discover.Device, 0, len(infos))
	for _, info := range infos {
		dev := discover.Device{
			IP:         info.IP,
			DeviceID:   info.DeviceID,
			DeviceName: info.DeviceName,
			DeviceType: info.Model,
			// Version 留空：V4 版本标识待确认（既不套 v3 过滤，也不伪造 "v4"）
		}
		if dev.DeviceID == "" {
			dev.DeviceID = dev.IP
			info.DeviceIDFromIP = true
		}
		if info.DeviceIDFromIP {
			log.Printf("[discover:manual] V4 设备 /health 无 deviceId，用 IP 兜底: ip=%s", dev.IP)
		}
		cacheItem := config.DeviceCache{
			DeviceID:   dev.DeviceID,
			IP:         dev.IP,
			DeviceType: dev.DeviceType,
			DeviceName: dev.DeviceName,
			Version:    dev.Version,
			State:      0, // /health 通过 = 在线
			Order:      0,
		}
		if s.ipMappingSetter != nil {
			s.ipMappingSetter(dev.IP, dev.DeviceID)
		}
		// 手动添加是明确的用户意图：新设备直接进正式设备列表（不随自动扫描走「待添加」）
		if existing := s.configManager.GetDevice(dev.DeviceID); existing != nil {
			changed := s.configManager.UpsertDeviceCheck(cacheItem)
			if changed {
				log.Printf("[discover:manual] V4 设备变化: id=%s ip=%s", dev.DeviceID, dev.IP)
				if s.onNewDevice != nil {
					s.onNewDevice(dev.DeviceID, dev.IP)
				}
			}
		} else {
			s.configManager.UpsertDevice(cacheItem)
			// 之前扫描可能已把它留在待添加列表，清掉防两处重复显示
			s.configManager.RemovePendingDevice(dev.DeviceID)
			log.Printf("[discover:manual] 手动添加 V4 设备: id=%s ip=%s", dev.DeviceID, dev.IP)
		}
		found = append(found, dev)
	}
	s.configManager.SortDeviceListByIP()

	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
		// 无条件下发：即使本轮没新增，也可能刚清掉了待添加残留条目
		s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
	}
	log.Printf("[discover:manual] V4 手动添加完成: 成功 %d 台, 无响应 %d 台", len(found), len(failedIPs))
	return found, failedIPs
}

// parseIPList 解析用户输入的IP列表（支持逗号、空格、换行分隔）
func parseIPList(input string) []string {
	var result []string
	for _, part := range strings.Split(input, ",") {
		for _, sub := range strings.Fields(part) {
			for _, line := range strings.Split(sub, "\n") {
				ip := strings.TrimSpace(line)
				if ip != "" && net.ParseIP(ip) != nil {
					result = append(result, ip)
				}
			}
		}
	}
	return dedupStrings(result)
}

func dedupStrings(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
