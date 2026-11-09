package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/moyunteng/myt/internal/util"
)

// 本文件承载 v2 主机在线状态定向刷新（设备添加联动等场景）。
// 历史问题：原全量 RefreshHostStates 串行逐台探测（单台最坏 20s），长期占用
// 扫描锁把 ScanV2Hosts 挡在门外（共用标志），用户点扫描频繁撞「正在扫描或
// 刷新设备状态」。现只保留按 IP 定向刷新（8-worker 并发池），与扫描共用
// scanGuard——后台批次被用户扫描抢占时静默让位。全量版已删除（零调用方，
// 被 OnPendingAdded 的定向刷新取代）。

// RefreshHostStatesByIPs 定向刷新：只探测给定 IP 的主机（设备添加联动等场景）。
// 走 scanGuard 防重入与并发池；IP 不在设备列表中的会被跳过。
func (s *V2ServiceWrapper) RefreshHostStatesByIPs(ips []string) {
	if s.configManager.GetDeployMode() != "v2" {
		return
	}
	s.refreshHostStatesByIPs(ips)
}

// refreshHostStatesByIPs 核心实现，调用方已做模式判断：
//   - scanGuard 防重入守卫，owner="background"：抢不到锁（用户扫描/另一轮
//     刷新在跑）静默跳过，保持原「忙时跳过」语义
//   - 8-worker 并发探测（单台 20s 超时不变），响应 ctx 取消——被用户扫描
//     打断时在途探测级联中断，已完成批次的探测照常落状态
//   - worker 只探测不写共享状态；状态落库 / 离线→在线翻转触发容器重查 /
//     devicelist:updated 统一 emit，全部收在探测池返回之后的主 goroutine
//     （worker 探测 + 主协程收结果，无共享写竞态）
func (s *V2ServiceWrapper) refreshHostStatesByIPs(ips []string) {
	if len(ips) == 0 {
		return
	}
	// 后台批次：抢不到锁静默跳过（用户扫描优先；前端刷新按钮连点也在此挡住）
	if ok, _ := s.scan.tryLock("background"); !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.scan.attachCancel(cancel)
	defer s.scan.unlock()

	// 去重 + 解析 DeviceID（不在列表中的 IP 记空 ID，UpdateDeviceState 对空 ID 不命中，等价于跳过）
	idByIP := make(map[string]string, len(ips))
	targets := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip == "" {
			continue
		}
		if _, dup := idByIP[ip]; dup {
			continue
		}
		idByIP[ip] = s.deviceIDByIP(ip)
		targets = append(targets, ip)
	}
	if len(targets) == 0 {
		return
	}

	// 并发准入探测（worker pool 8，与扫描路径同款）：在线判定以 2375 /info 为准
	// （放宽语义：81 缺失设备也算在线），顺带刷新超级 SDK 就绪状态
	errs := make([]error, len(targets))
	ready := make([]bool, len(targets))
	runIdxPool(ctx, len(targets), func(idx int) {
		probe := s.client.AdmissionProbe(ctx, targets[idx])
		if !probe.OK {
			errs[idx] = fmt.Errorf("%s", probe.Err)
			return
		}
		ready[idx] = probe.SuperSdkReady
	})

	// 主 goroutine 统一落状态 + 触发联动（UpdateDeviceState/GetDeviceState
	// 内部有锁，但保持单线程写与原行为完全一致）
	online := 0
	for idx, ip := range targets {
		if errors.Is(errs[idx], context.Canceled) {
			continue // 被用户扫描打断的在途探测：不误标离线，等下轮扫描/心跳兜底
		}
		state := 1
		if errs[idx] == nil {
			state = 0
			online++
		}
		id := idByIP[ip]
		prev := s.configManager.GetDeviceState(id)
		s.configManager.UpdateDeviceState(id, state)
		// 准入通过时顺带落超级 SDK 就绪状态（探测已拿全，不额外发一轮 81 请求）
		if state == 0 && id != "" {
			s.configManager.UpdateDeviceSuperSdkReady(id, ready[idx])
		}
		// 离线→在线翻转：立即重查容器列表（reporter 的离线首报可能已清掉缓存，
		// 而 v2 在线后没有 8000 通道的 queryOnce 兜底，见字段注释）
		if state == 0 && prev != 0 && s.androidsRefresher != nil {
			ip := ip
			util.GoSafe(func() { s.androidsRefresher(ip) })
		}
	}
	log.Printf("[v2] 主机状态刷新: %d/%d 在线", online, len(targets))
	s.emitDeviceList()
}
