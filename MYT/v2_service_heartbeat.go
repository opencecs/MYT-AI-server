package main

import (
	"context"
	"log"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// --- V2 主机心跳循环（对齐 V2 壳子：常驻 UDP 单播测活）---
// 壳子行为：进程内常驻心跳，状态变化全量推给网页（LG_DeviceFind，不在最新
// 列表即离线）。我们对齐为：常驻循环 + 状态真变化才 emit devicelist:updated
// （事件铁律：真变化才广播，不塞高频数据）。
// 通道选择 UDP 7678 而非 2375：心跳不依赖 Docker 端口，防火墙只放行 7678 也能判活。

const (
	heartbeatInterval = 30 * time.Second // 心跳周期

	// offlineThreshold 连续失败多少轮才判离线（离线迟滞）。
	// 单轮 UDP 探测失败可能只是单包丢失/网络抖动（无重传的单发单收探测，
	// 1.5s 超时窗口内丢包并不罕见），立即判死会造成在线/离线锯齿横跳；
	// 连续 ≥2 轮全失败才认定为真离线。探测成功则立即恢复在线（误报在线无害）。
	offlineThreshold = 2
)

// StartHeartbeat 启动心跳循环（幂等）。循环常驻、模式内自检：
// 非 v2 模式时 tick 直接返回，无需随模式切换启停。
func (s *V2ServiceWrapper) StartHeartbeat() {
	s.hbMu.Lock()
	if s.hbStarted {
		s.hbMu.Unlock()
		return
	}
	s.hbStarted = true
	stopCh := make(chan struct{})
	s.hbStopCh = stopCh
	s.hbMu.Unlock()
	go s.heartbeatLoop(stopCh)
	// 启动对账：已在列表且在线的 v2 主机逐台做 /info 检测（老版本 SDK/未部署
	// 自动升级）——「添加主机」与心跳「离线→在线翻转」两个触发点都覆盖不到
	// 「客户端启动时设备已在线」的场景（上次会话在线的设备重启客户端不会翻转）。
	// 幂等：maybeDeployV2Sdk 内部有模式门卫 + /info 判定 + 部署防重入；
	// 异步逐台执行，CheckV2SdkAlive（2s 超时）不阻塞启动。
	util.GoSafe(func() {
		time.Sleep(5 * time.Second) // 等设备列表加载与服务就绪
		for _, d := range s.configManager.GetDeviceList() {
			if d.State == 0 && d.Version == "v2" && d.IP != "" {
				ip := d.IP
				util.GoSafe(func() { s.maybeDeployV2Sdk(ip) })
			}
		}
	})
}

// StopHeartbeat 停止心跳循环（进程退出时调用）。
func (s *V2ServiceWrapper) StopHeartbeat() {
	s.hbMu.Lock()
	defer s.hbMu.Unlock()
	if !s.hbStarted {
		return
	}
	s.hbStarted = false
	close(s.hbStopCh)
}

func (s *V2ServiceWrapper) heartbeatLoop(stopCh chan struct{}) {
	s.heartbeatTick() // 首轮立即执行：启动即校准在线状态
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			s.heartbeatTick()
		}
	}
}

// heartbeatTick 单轮心跳：全量测活 → 与缓存状态比对 → 有变化才落库+广播。
// 两道防误判闸门：
//  1. scanGuard 忙（用户扫描/后台批次/定向刷新进行中）：跳过本轮，不测不写
//     不 emit——8-worker 重探测正并发打满设备，此刻的探测结果不可信，等下轮；
//  2. 离线迟滞（offlineThreshold）：连续 ≥2 轮失败才写离线，单轮丢包视为抖动。
func (s *V2ServiceWrapper) heartbeatTick() {
	if s.configManager.GetDeployMode() != "v2" {
		return
	}
	if s.scan.busy() {
		log.Printf("[v2] 心跳: 扫描/刷新进行中，本轮跳过")
		return
	}
	devices := s.configManager.GetDeviceList()
	if len(devices) == 0 {
		return
	}
	ips := make([]string, 0, len(devices))
	inList := make(map[string]bool, len(devices))
	idByIP := make(map[string]string, len(devices))
	for _, d := range devices {
		if d.IP != "" {
			ips = append(ips, d.IP)
			inList[d.IP] = true
			idByIP[d.IP] = d.DeviceID
		}
	}
	if len(ips) == 0 {
		return
	}

	online := s.client.HeartbeatBatch(context.Background(), ips)

	changed := 0
	onlineCount := 0
	for ip, hb := range online {
		// 迟滞计数：失败累加，成功清零。连续 ≥offlineThreshold 轮失败才判离线；
		// 未达阈值时本轮不写状态（沿用上一轮结果），也不 emit。
		if hb.Online {
			delete(s.hbFailCount, ip)
		} else {
			s.hbFailCount[ip]++
			if s.hbFailCount[ip] < offlineThreshold {
				log.Printf("[v2] 心跳: ip=%s 连续失败 %d 轮，未达阈值 %d，暂不判离线",
					ip, s.hbFailCount[ip], offlineThreshold)
				continue
			}
		}

		newState := 1
		if hb.Online {
			newState = 0
			onlineCount++
		}
		// 旧数据自愈：条目 DeviceID 为空（早期版本以 IP 为主键）时，
		// 用 UDP 回复里的序列号回填——回填后所有按 ID 的操作恢复正常
		id := idByIP[ip]
		if id == "" && hb.DeviceID != "" {
			if s.configManager.FillDeviceIDByIP(ip, hb.DeviceID) {
				id = hb.DeviceID
				log.Printf("[v2] 心跳回填序列号: ip=%s → %s", ip, id)
			}
		}
		if id == "" {
			// 无序列号可回填（主机不可达且条目无 ID）：按 IP 更新状态
			if s.configManager.UpdateDeviceStateByIP(ip, newState) {
				changed++
			}
			continue
		}
		if s.configManager.GetDeviceState(id) != newState {
			s.configManager.UpdateDeviceState(id, newState)
			changed++
			// 离线→在线翻转：立即重查容器列表（离线期间容器缓存可能已被清理，
			// v2 无事件通道，上线后必须主动拉一次）
			if newState == 0 && s.androidsRefresher != nil {
				refresher := s.androidsRefresher
				util.GoSafe(func() { refresher(ip) })
			}
			// 离线→在线翻转：v2 主机 8000 无可用 /info（老版本 SDK / 未部署）时
			// 自动升级——已在列表的设备不经过「添加主机」触发点，上线是唯一
			// 自愈时机。幂等：maybeDeployV2Sdk 内部有模式门卫 + /info 判定 +
			// 部署防重入；只对 version=="v2" 的主机做（v3 主机 8000 是另一套
			// SDK，不能刷成 v2）
			if newState == 0 {
				if dev := s.configManager.GetDevice(id); dev != nil && dev.Version == "v2" {
					ipSnapshot := ip
					util.GoSafe(func() { s.maybeDeployV2Sdk(ipSnapshot) })
				}
			}
		}
	}
	// 清理已不在设备列表中的 IP 计数条目（防长期残留；map 仅心跳 goroutine 访问）
	for ip := range s.hbFailCount {
		if !inList[ip] {
			delete(s.hbFailCount, ip)
		}
	}
	if changed > 0 {
		log.Printf("[v2] 心跳: %d/%d 在线, 状态变化 %d 台", onlineCount, len(online), changed)
		s.emitDeviceList()
	}
}
