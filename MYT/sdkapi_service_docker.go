package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/sdkver"
)

// DockerExec 在安卓容器内执行命令
func (s *SdkApiServiceWrapper) DockerExec(deviceIP string, containerName string, command string) (string, error) {
	client := s.newClient(deviceIP)
	result, err := client.DockerExec(containerName, command)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

// BatchDockerExec 批量在安卓容器内执行命令（并发）
func (s *SdkApiServiceWrapper) BatchDockerExec(deviceIPs []string, containerNames []string, command string) []sdkapi.BatchOpResult {
	results := make([]sdkapi.BatchOpResult, len(deviceIPs))
	var wg sync.WaitGroup
	for i, ip := range deviceIPs {
		if i >= len(containerNames) {
			break
		}
		wg.Add(1)
		go func(idx int, deviceIP string, containerName string) {
			defer wg.Done()
			client := s.newClient(deviceIP)
			result, err := client.DockerExec(containerName, command)
			if err != nil {
				results[idx] = sdkapi.BatchOpResult{
					DeviceID: deviceIP,
					Name:     containerName,
					Success:  false,
					Error:    errToString(err),
				}
				return
			}
			results[idx] = sdkapi.BatchOpResult{
				DeviceID: deviceIP,
				Name:     containerName,
				Success:  true,
				Output:   result.Output,
				Error:    "",
			}
		}(i, ip, containerNames[i])
	}
	wg.Wait()
	return results
}

// SetV2UpgradeFn 注入 v2 模式的「SDK 升级」实现（超级 SDK 插件更新，需云端
// 元数据——由 V2ServiceWrapper 提供，注入模式避免 wrapper 间互持）。
func (s *SdkApiServiceWrapper) SetV2UpgradeFn(fn func(deviceIP string) (bool, error)) {
	s.v2UpgradeFn = fn
}

// SdkUpgradeDevice 触发单台设备 SDK 自升级（设备从云端拉取最新 SDK）。
// 返回是否已触发升级（false 表示已是最新版本）。
// v2 模式：走超级 SDK 插件更新（81 del+install，注入的 v2UpgradeFn）。
func (s *SdkApiServiceWrapper) SdkUpgradeDevice(deviceIP string) (bool, error) {
	if s.cm != nil && s.cm.GetDeployMode() == "v2" {
		if s.v2UpgradeFn == nil {
			return false, fmt.Errorf("v2 升级通道未注入")
		}
		return s.v2UpgradeFn(deviceIP)
	}
	client := s.newClient(deviceIP)
	return client.TriggerDeviceUpgrade()
}

// GetLatestSdkVersion SDK 最新版本（全局单值：本地 SDK 文件版本与公网查询取
// max，公网带 10 分钟缓存）。供批量升级弹窗等场景免逐台探测设备——设备当前
// 版本走 useSdkVersions 缓存（WS hello），最新版只差这一个值。
// 云端与本地都不可知时返回 -1（与 SdkCheckUpgrade 的 LatestVersion 语义一致）。
func (s *SdkApiServiceWrapper) GetLatestSdkVersion() int {
	if s.sdkVer == nil {
		return -1
	}
	if best := max(s.sdkVer.LocalVersion(), s.fetchCloudSDKVersion()); best > 0 {
		return best
	}
	return -1
}

// ListSdkVersions 查询云端 SDK 版本列表（全量、云端已按版本号降序，含 test
// 状态版本——前端标注展示，不静默过滤）。简单透传 internal/sdkver。
func (s *SdkApiServiceWrapper) ListSdkVersions(sdkType string) ([]sdkver.SdkVersion, error) {
	return sdkver.ListVersions(sdkType)
}

// SdkCheckUpgrade 查询单台设备的 API 版本信息（当前版本 + 最新版本），用于判断是否有更新。
// latestVersion 用云端最新版覆盖设备自报值——老 SDK 设备自报的 latest 恒等于
// 自身版本（194 设备自报 194 最新），前端会误显示"已是最新"而不提示升级。
// 云端版本优先级：本地已下载 SDK 的版本文件 → 公网查询（10 分钟内存缓存）。
// 两者都不可用时 latestVersion 置 -1（前端显示"未知"，不冒充"已最新"）。
//
// v2 模式分支：设备 8000 由 myt-sdk-go-v2 提供，/info 版本号是 string——
// 直连 8000 查询，StringCurrentVersion 为准；latest 走 v2_box_sdk 云端查询
// （V3 的 int 版本缓存/过期判定不适用，全部跳过）。
func (s *SdkApiServiceWrapper) SdkCheckUpgrade(deviceIP string) (*sdkapi.APIVersionInfo, error) {
	if s.cm != nil && s.cm.GetDeployMode() == "v2" {
		return s.sdkCheckUpgradeV2(deviceIP)
	}
	client := s.newClient(deviceIP)
	info, err := client.GetAPIVersionInfo()
	if err != nil {
		return nil, err
	}
	s.notifySdkExpired(deviceIP, info.CurrentVersion) // 查到过期版本（<208）= 老 SDK 设备 HTTP 可达：即时恢复（setup 钩子）
	s.cacheSdkVersion(deviceIP, info.CurrentVersion)  // HTTP 查询结果反哺后端缓存：升级轮询下轮免探测
	// 单台查询成功 = 实时版本源：推给前端版本缓存（与 hello 同频道）。覆盖
	// 「降级到无 WS 版本且设备从未离线」的盲区——无 hello（无 WS）也无翻转
	// （State 全程 0），三源都不触发时列表版本停留在旧值（如 vv209 绿点，
	// 实际已 204 应橙点）。用户查看设备的动作本身就是缓存刷新信号。
	if info.CurrentVersion > 0 {
		if deviceID := s.GetDeviceIDByIP(deviceIP); deviceID != "" {
			s.emitEvent("sdkversion:updated", map[string]any{"deviceId": deviceID, "version": info.CurrentVersion})
		}
	}
	if s.sdkVer != nil {
		// max(本地 SDK 文件版本, 公网查询)：本地单独优先可致「假最新」——本地
		// 缓存旧 SDK 205 而云端 210 时误报 205 最新
		local := s.sdkVer.LocalVersion()
		desc, cloud := s.fetchCloudSDKVersionInfo()
		if best := max(local, cloud); best > 0 {
			info.LatestVersion = best
			// 描述只在「最新版确实来自云端」时展示——本地版本更高时云端描述
			// 对应的不是返回的 latest，展示会张冠李戴
			if cloud >= local {
				info.Description = desc
			}
		} else {
			info.LatestVersion = -1 // 云端版本不可知：前端按"未知"渲染，不显示"已最新"
		}
	}
	return info, nil
}

// fetchCloudSDKVersion 公网查询最新 SDK 版本（10 分钟内存缓存——UI 每次选中设备
// 都会查，不打公网）。描述（描述随版本缓存，版本号复用缓存时描述同步复用）。
// 网络调用在锁外：httpClient 超时 30s，持锁调用会让慢网期间（不出网环境首查）
// 所有并发 SdkCheckUpgrade 串行排队。并发未命中缓存的调用各自发起查询——
// 幂等 GET、且 UI 侧并发量小，重复无害。
func (s *SdkApiServiceWrapper) fetchCloudSDKVersion() int {
	_, v := s.fetchCloudSDKVersionInfo()
	return v
}

// fetchCloudSDKVersionInfo 同 fetchCloudSDKVersion，附带云端版本描述。
func (s *SdkApiServiceWrapper) fetchCloudSDKVersionInfo() (string, int) {
	s.cloudVerMu.Lock()
	if time.Since(s.cloudVerAt) < 10*time.Minute {
		v, desc := s.cloudVer, s.cloudDesc
		s.cloudVerMu.Unlock()
		return desc, v
	}
	s.cloudVerMu.Unlock()

	v, err := s.sdkVer.FetchLatestVersion()

	s.cloudVerMu.Lock()
	defer s.cloudVerMu.Unlock()
	if err != nil {
		log.Printf("[sdk-upgrade] SdkCheckUpgrade 公网版本查询失败: %v", err)
		s.cloudVerAt = time.Now() // 失败也节流 10 分钟，避免 UI 反复触发公网请求
		return s.cloudDesc, s.cloudVer // 返回上次已知值（可能为 0=未知）
	}
	s.cloudVer, s.cloudVerAt, s.cloudDesc = v, time.Now(), s.lastFetchedDescription()
	return s.cloudDesc, v
}

// lastFetchedDescription 提取最近一次 FetchLatestVersion 查到的版本描述
// （VersionResponse.Files[0].description；云端响应缓存在 wrapper 上方查不到时兜底）。
// FetchLatestVersion 只返回版本号，描述经 sdkver 包级最近值带出（单例语义，
// box_sdk latest 查询全局只有这一处入口）。
func (s *SdkApiServiceWrapper) lastFetchedDescription() string {
	return sdkver.LastFetchDescription()
}
