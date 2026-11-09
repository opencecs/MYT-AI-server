package v2host

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// --- 设备准入探测（v2 模式「UDP+2375 即可添加」的放宽语义）---
//
// 原语义：81 dc_api 探测失败 = 非 V2 可管理，扫描/添加直接丢弃设备。
// 新语义（用户决策）：UDP 7678 应答 + 2375 Docker daemon 可达即通过准入；
// 81 探测降级为「超级 SDK 就绪度」状态探测，失败不再丢弃——只标记未就绪，
// 由用户在 UI 里点「安装超级 SDK」补装（EnsureSuperSdk，见 supersdk.go）。
// 仅 UDP 可达、2375 不可达 → 仍丢弃（Docker daemon 不可达无法管理容器）。

// AdmissionProbe 单台主机准入探测：2375 /info 验证可管理性 + 81 验证超级 SDK 就绪度。
// 返回 ProbeResult（OK=准入通过；SuperSdkReady=false 表示 81 缺失待安装）。
func (c *Client) AdmissionProbe(parent context.Context, ip string) ProbeResult {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	info, err := c.Info(ctx, ip) // 2375 /info：Docker daemon 可达性（hc 2.5s 短超时兜底）
	if err != nil {
		return ProbeResult{IP: ip, OK: false, Err: fmt.Sprintf("2375 不可达: %v", err)}
	}
	result := ProbeResult{
		IP:         ip,
		OK:         true,
		HostID:     info.ID,
		HostName:   info.Name,
		Version:    info.ServerVersion,
		Containers: info.Containers,
	}
	// 81 超级 SDK 就绪度探测（状态探测，失败只降级不拒绝；20s——list 实测常态 3s+）
	sdkCtx, sdkCancel := context.WithTimeout(parent, 20*time.Second)
	defer sdkCancel()
	entries, sdkErr := c.ListContainersViaSDK(sdkCtx, ip)
	if sdkErr != nil {
		result.SdkErr = sdkErr.Error()
		return result
	}
	result.SuperSdkReady = true
	result.Containers = len(entries) // 81 加工视图只含云机容器，口径与 81 探测语义一致
	return result
}

// AdmissionProbeBatch 并发准入探测（复用 ProbeBatch 的并发上限）。
// 返回结果顺序与输入 IP 一一对应。
func (c *Client) AdmissionProbeBatch(parent context.Context, ips []string) []ProbeResult {
	results := make([]ProbeResult, len(ips))
	sem := make(chan struct{}, maxConcurrentProbes)
	var wg sync.WaitGroup
	for i, ip := range ips {
		wg.Add(1)
		go func(idx int, addr string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = c.AdmissionProbe(parent, addr)
		}(i, ip)
	}
	wg.Wait()
	return results
}
