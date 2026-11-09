package main

import (
	"context"
	"log"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
	"github.com/moyunteng/myt/internal/util"
	"github.com/moyunteng/myt/internal/v2host"
)

// ProbeV2Hosts 批量探测主机（添加设备第一步：输入 IP → 展示探测结果）。
// 入参为逗号/空格/换行分隔的 IP 列表，与本地手动添加的 parseIPList 同口径。
// 探测走准入语义（2375 可达即通过，81 降级为超级 SDK 就绪度状态探测，
// 见 v2host.AdmissionProbe）。
func (s *V2ServiceWrapper) ProbeV2Hosts(ips string) []v2host.ProbeResult {
	ipList := parseIPList(ips)
	if len(ipList) == 0 {
		return nil
	}
	return s.client.AdmissionProbeBatch(context.Background(), ipList)
}

// V2AddHostsResult 添加主机结果：
//   - added/exists=准入通过并已入列（含 81 未就绪的，前端按 SuperSdkReady 标注）
//   - failed=2375 不可达（Docker daemon 不可达无法管理，拒绝添加）
type V2AddHostsResult struct {
	Added  []v2host.ProbeResult `json:"added"`
	Exists []v2host.ProbeResult `json:"exists"`
	Failed []v2host.ProbeResult `json:"failed"`
}

// AddV2Hosts 探测并添加主机进 v2 设备列表。仅 v2 模式生效。
// 探测双通道：UDP 7678 单播（lgcloud 魔术字节）拿设备序列号 + 2375 /info 验证
// Docker daemon 可达性（准入语义：仅 UDP 可达、2375 不可达仍拒绝添加）。
// 81 缺失不再拒绝：正常入列并标记 SuperSdkReady=false，用户在设备详情点
// 「安装超级 SDK」补装（InstallSuperSdk）。
// DeviceID 统一用 UDP 协议的设备序列号——与扫描路径（ScanV2Hosts）一致，同一台主机
// 两条路径添加不会产生重复条目；UDP 无回复的纯 Docker 主机退回 Docker ID。
// 添加成功的主机异步拉取容器列表，前端经 androidlist:updated 自动刷新。
func (s *V2ServiceWrapper) AddV2Hosts(ips string) V2AddHostsResult {
	result := V2AddHostsResult{}
	if s.configManager.GetDeployMode() != "v2" {
		log.Printf("[v2] AddV2Hosts 非 v2 模式调用，忽略")
		return result
	}
	ipList := parseIPList(ips)
	if len(ipList) == 0 {
		return result
	}

	// UDP 单播批量探测：拿设备序列号（一次会话收全部回复，~1s；不过滤后缀——
	// v3 设备开启 2375 后同样可被 V2 模式管理，序列号对所有代数设备通用）
	udpByIP := make(map[string]discover.Device, len(ipList))
	if found, _ := s.discoverer.DiscoverManually(context.Background(), ipList); len(found) > 0 {
		for _, dev := range found {
			if _, dup := udpByIP[dev.IP]; !dup {
				udpByIP[dev.IP] = dev
			}
		}
	}

	// 准入探测（2375 /info + 81 就绪度）：2375 不可达才进 failed
	for _, probe := range s.client.AdmissionProbeBatch(context.Background(), ipList) {
		if !probe.OK {
			result.Failed = append(result.Failed, probe)
			continue
		}
		ip := probe.IP
		// DeviceID/名称/型号：UDP 回复优先（与扫描路径同一标识），退回 Docker ID。
		// DeviceType 存 UDP 的真实型号（a1/q1/p1_v3...）——创建表单按它过滤适配镜像
		// （壳子同款 ttype==dType 分类），不能硬编码 "v2host"
		deviceID := probe.HostID
		deviceName := probe.HostName
		deviceType := ""
		if udpDev, ok := udpByIP[ip]; ok {
			deviceID = udpDev.DeviceID
			deviceName = udpDev.DeviceName
			deviceType = udpDev.DeviceType
		}
		probe.HostID = deviceID
		probe.HostName = deviceName
		cacheItem := config.DeviceCache{
			DeviceID:      deviceID,
			IP:            ip,
			DeviceType:    deviceType,
			DeviceName:    deviceName,
			State:         0, // 探测通过即在线
			Order:         0,
			SuperSdkReady: probe.SuperSdkReady,
		}
		isNew := s.configManager.UpsertDeviceCheck(cacheItem)
		if isNew {
			result.Added = append(result.Added, probe)
			log.Printf("[v2] 添加主机: id=%s ip=%s name=%s sdkReady=%v", shortID(deviceID), probe.IP, deviceName, probe.SuperSdkReady)
		} else {
			// 已在列表：同步刷新就绪状态（设备可能被用户手动补装了超级 SDK）
			result.Exists = append(result.Exists, probe)
			s.configManager.UpdateDeviceSuperSdkReady(deviceID, probe.SuperSdkReady)
		}
		// 手动添加成功即从待添加列表移除（Added/Exists 都清，与 local 模式
		// discover_service_manual.go 的做法对齐），否则残留条目继续出现在弹窗
		s.configManager.RemovePendingDevice(deviceID)
	}
	s.configManager.SortDeviceListByIP()
	s.emitDeviceList()
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
	}

	// 新主机异步拉容器列表（androidlist:updated 事件驱动前端刷新）
	for _, probe := range result.Added {
		probe := probe
		util.GoSafe(func() {
			if _, err := s.RefreshV2ContainersByIP(probe.IP); err != nil {
				log.Printf("[v2] 新主机容器拉取失败: ip=%s err=%v", probe.IP, err)
			}
		})
	}
	// SDK 自动部署：8000 有服务不管，无则部署 myt-sdk-go-v2（Added+Exists 都检测——
	// 用户「添加进来的时候」含已在列表的主机；幂等，CheckV2SdkAlive 直接跳过）
	for _, probe := range result.Added {
		probe := probe
		util.GoSafe(func() { s.maybeDeployV2Sdk(probe.IP) })
	}
	for _, probe := range result.Exists {
		probe := probe
		util.GoSafe(func() { s.maybeDeployV2Sdk(probe.IP) })
	}
	return result
}
