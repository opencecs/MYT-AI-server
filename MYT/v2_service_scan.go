package main

import (
	"context"
	"log"
	"strings"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
	"github.com/moyunteng/myt/internal/v2host"
)

// ---------- 扫描入口（用户优先 + 后台静默） ----------

// ScanV2Hosts 用户显式扫描：UDP 广播发现 + 准入探测（放宽语义）：
//   - 发现层全量收（UDP 7678 lgcloud 广播，v1/v2/v3 设备都回复，不做后缀过滤）
//   - 对每台回复设备补一发探测：**2375 /info 可达 = V2 可管理**；81 探测降级为
//     超级 SDK 就绪度状态探测（失败不再丢弃设备，只标记待安装，见 AdmissionProbe）
//   - 通过验证的主机：新发现的进「待添加」列表，已在列表的刷新信息并置在线
//   - 不对任何数据做删除（扫描是纯发现）
//
// 返回本轮准入通过的 V2 主机（前端雷达展示用）。
//
// 锁语义（scanGuard，用户扫描优先）：
//   - 后台批次在跑 → 取消对方、短暂等待退出后接管扫描
//   - 另一轮用户扫描在跑 → 报 errV2ScanBusy（前端 toast，不能静默——
//     否则"忙"会被当成"没有设备"）
func (s *V2ServiceWrapper) ScanV2Hosts() ([]v2host.ProbeResult, error) {
	if s.configManager.GetDeployMode() != "v2" {
		return nil, nil
	}
	ok, cur := s.scan.tryLock("user")
	if !ok {
		if cur != "background" {
			log.Printf("[v2] 扫描防重入：另一轮用户扫描进行中，拒绝本次扫描")
			return nil, errV2ScanBusy
		}
		// 用户优先：打断后台批次，短暂等待其退出后接管
		log.Printf("[v2] 用户扫描打断后台扫描/刷新，等待其退出")
		s.scan.cancelOwner()
		s.scan.waitDone(v2ScanPreemptWait)
		if ok, cur = s.scan.tryLock("user"); !ok {
			log.Printf("[v2] 后台批次未在 %s 内退出，本次扫描报忙", v2ScanPreemptWait)
			return nil, errV2ScanBusy
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.scan.attachCancel(cancel)
	defer s.scan.unlock()
	return s.scanV2Hosts(ctx)
}

// ScanV2HostsQuiet 后台扫描入口（模式切换后的 v2「发现」等价物，setup 注入
// discoverSvc.v2Refresher）：抢不到锁（用户扫描/刷新/另一轮后台在跑）静默返回，
// 无错误无提示；扫描失败也只落日志。
func (s *V2ServiceWrapper) ScanV2HostsQuiet() {
	if s.configManager.GetDeployMode() != "v2" {
		return
	}
	if ok, _ := s.scan.tryLock("background"); !ok {
		return // 静默：不与用户扫描/刷新争抢
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.scan.attachCancel(cancel)
	defer s.scan.unlock()
	if _, err := s.scanV2Hosts(ctx); err != nil {
		log.Printf("[v2] 后台扫描失败（静默）: %v", err)
	}
}

// scanV2Hosts 扫描主体，调用方已持 scanGuard 锁并注入可取消 ctx：
// UDP 发现（~2s）→ 白名单过滤 → 8-worker 81 探测 → 落库 + 事件推送。
// 响应 ctx 取消：发现层立即中断；探测池已完成的批次照常落库。
func (s *V2ServiceWrapper) scanV2Hosts(ctx context.Context) ([]v2host.ProbeResult, error) {
	found, err := s.discoverer.Discover(ctx)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}

	// 准入探测目标收集：同 IP 多回复取首个；v2 扫描型号白名单——不在白名单
	// 且不在正式列表的设备直接跳过（不做 81 探测、不进待添加）；已在正式列表的
	// 不受白名单限制，照常刷新/置在线，在线性由 30s 心跳兜底。
	ips := make([]string, 0, len(found))
	devByIP := make(map[string]discover.Device, len(found))
	for _, dev := range found {
		if dev.IP == "" || devByIP[dev.IP].DeviceID != "" {
			continue
		}
		if !v2ModelAllowed(dev.DeviceName) &&
			s.configManager.GetDevice(dev.DeviceID) == nil &&
			s.configManager.GetDeviceByIP(dev.IP) == nil {
			continue
		}
		ips = append(ips, dev.IP)
		devByIP[dev.IP] = dev
	}

	// 并发准入探测（worker pool 8，AdmissionProbeBatch）：2375 /info 可达即通过
	// 准入；81 探测降级为超级 SDK 就绪度状态探测，失败不丢弃（仅 UDP+2375 也
	// 可添加，Docker daemon 不可达才拒）。ctx 取消时在途探测被级联中断，已完成
	// 批次的结果照常落。
	probeResults := s.client.AdmissionProbeBatch(ctx, ips)

	byIP := make(map[string]v2host.ProbeResult, len(ips))
	for _, r := range probeResults {
		if r.OK {
			byIP[r.IP] = r
		}
	}

	newCount := 0
	listChanged := false
	for ip, probe := range byIP {
		dev := devByIP[ip]
		// 旧数据自愈：同 IP 条目已存在但 DeviceID 为空（早期版本以 IP 为主键）→
		// 回填本轮 UDP 序列号，按已存在处理（否则会重复进待添加）
		existing := s.configManager.GetDevice(dev.DeviceID)
		if existing == nil {
			if legacy := s.configManager.GetDeviceByIP(dev.IP); legacy != nil && legacy.DeviceID == "" {
				if s.configManager.FillDeviceIDByIP(dev.IP, dev.DeviceID) {
					log.Printf("[v2] 扫描回填序列号: ip=%s → %s", dev.IP, dev.DeviceID)
					existing = s.configManager.GetDevice(dev.DeviceID)
				}
			}
		}
		cacheItem := config.DeviceCache{
			DeviceID:      dev.DeviceID,
			IP:            dev.IP,
			DeviceType:    dev.DeviceType,
			DeviceName:    dev.DeviceName,
			Version:       dev.Version,
			State:         0,
			Order:         0,
			SuperSdkReady: probe.SuperSdkReady, // 待添加列表标注「未装超级 SDK」用
		}
		if existing != nil {
			// 已在正式列表：刷新信息 + 置在线
			if s.configManager.UpsertDeviceCheck(cacheItem) {
				listChanged = true
			}
			s.configManager.UpdateDeviceState(dev.DeviceID, 0)
		} else {
			// 不在正式列表：进待添加（含已存在待添加的刷新，UpsertPendingDevice 覆盖同 ID）
			s.configManager.UpsertPendingDevice(cacheItem)
			newCount++
		}
		probe.HostID = dev.DeviceID
		probe.HostName = dev.DeviceName
		probe.Version = dev.Version
		byIP[ip] = probe
		// 超级 SDK 就绪状态落库（正式列表条目）：随扫描刷新维护，变化计入
		// listChanged 由统一 emit 广播
		if existing != nil && s.configManager.UpdateDeviceSuperSdkReadyChecked(dev.DeviceID, probe.SuperSdkReady) {
			listChanged = true
		}
	}
	results := make([]v2host.ProbeResult, 0, len(byIP))
	for _, p := range byIP {
		results = append(results, p)
	}
	if newCount > 0 {
		listChanged = true
	}

	log.Printf("[v2] UDP 扫描完成: 发现 %d 台, 准入通过 %d 台(81就绪 %d 台), 新进待添加 %d 台",
		len(found), len(results), countReady(results), newCount)
	if s.app != nil && s.app.Event != nil {
		if newCount > 0 {
			s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
		}
		if listChanged {
			s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
		}
	}
	return results, nil
}

// ---------- 扫描型号白名单 ----------

// v2ScanModelWhitelist：v2 扫描放行的设备型号（UDP 7678 回复的设备名，忽略大小写）。
// 带 _v2 后缀的按白名单过滤；无后缀裸名只有白名单内的才放行，其余裸名一律挡掉。
// 已在正式列表的设备不受白名单限制（见 scanV2Hosts 的过滤落点）。
var v2ScanModelWhitelist = map[string]struct{}{
	"c1": {}, "q1": {}, "q1_10": {}, "c1_10": {},
	"q1_v2": {}, "r1s_v2": {}, "c1_v2": {}, "r1q_v2": {},
	"q1l_v2": {}, "p1": {}, "p1_10": {}, "p1_v2": {}, "m1_v2": {},
}

// v2ModelAllowed 判断设备名是否在 v2 扫描白名单内（忽略大小写与首尾空白）。
func v2ModelAllowed(name string) bool {
	_, ok := v2ScanModelWhitelist[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// countReady 统计准入通过结果中 81 就绪的台数（扫描日志用）
func countReady(results []v2host.ProbeResult) int {
	n := 0
	for _, r := range results {
		if r.SuperSdkReady {
			n++
		}
	}
	return n
}
