package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/maintenance"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/sdkver"
)

// V2 设备 SDK（myt-sdk-go-v2）指定版本安装：客户端从 newapi 下载原始 zip
// → 上传设备 8000 /server/upgrade/upload（zip 内 myt-sdk-v2 + hash + version
// 三文件即设备端要求的原始包结构，无需重新打包）→ 轮询 8000 /info 确认
// currentVersion（string）与目标一致。
//
// 与 V3 InstallSdkVersion 的差异：版本号是 string（可能非纯数字）、zip 来自
// newapi 的 v2_box_sdk 类型（无 EnsureSDK 缓存链路）。

// v2InstallVersionPollTimeout v2 安装确认轮询上限（与 V3 一致：设备解压替换
// 二进制 + 重启服务需要时间）
const v2InstallVersionPollTimeout = 60 * time.Second

// v2SdkType newapi 上 V2 盒子 SDK 的类型标识（与 internal/maintenance 的
// v2SdkType 同值；main 包无法引用未导出常量，独立声明并注释同步来源）
const v2SdkType = "v2_box_sdk"

// v2InstallOverallTimeout 安装任务整体兜底超时：内层各步都有超时（下载 5min、
// 上传 120s、轮询确认 60s），但组合路径无整体上限——任一步骤挂死（如 SFTP
// TCP 半开）会无限期持有 installMu，其他安装任务全部被拒。超时后强制返回，
// defer 保证锁释放。
const v2InstallOverallTimeout = 10 * time.Minute

// InstallV2SdkVersion 在单台 V2 设备上安装指定版本 SDK（版本确认变化才返回 nil）。
// deviceIP 为设备标识（v2 模式下与 V3 一致经 newClient 解析到真实主机 IP，
// 8000 由 myt-sdk-go-v2 提供）。
func (s *SdkApiServiceWrapper) InstallV2SdkVersion(deviceIP string, version string) error {
	if version == "" {
		return fmt.Errorf("无效的 SDK 版本号: %q", version)
	}
	// 防重入：与 V3 InstallSdkVersion / SubmitSdkInstallTask 共用 installMu
	if !s.installMu.TryLock() {
		return fmt.Errorf("已有 SDK 安装任务在执行中")
	}
	defer s.installMu.Unlock()

	// 整体兜底超时：任何一步挂死（SFTP 半开/SSH 会话僵死）最多 10 分钟后
	// 强制返回错误，defer 释放 installMu——不会永久占住锁挡住后续任务
	done := make(chan error, 1)
	go func() {
		done <- s.installV2SdkVersionInner(deviceIP, version)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(v2InstallOverallTimeout):
		return fmt.Errorf("SDK 安装整体超时（%d 分钟），已中止", int(v2InstallOverallTimeout.Minutes()))
	}
}

// installV2SdkVersionInner 安装实际流程（供整体超时守护包裹）。
func (s *SdkApiServiceWrapper) installV2SdkVersionInner(deviceIdent, version string) error {
	// 入参可能是 deviceId（v2 列表序列号）也可能是 IP（旧数据兜底）——统一解析为
	// 真实主机 IP：上传/轮询走 newV2DeviceClient（内部也会解析），但 SSH 降级路径
	// 的 DeployV2SdkVersion 直接拿参数当主机拨号，传 deviceId 会 DNS lookup 失败
	// （"lookup pb9efb...: no such host"）。在此解析一次，全流程用 IP。
	deviceIP := s.resolveDeviceIP(deviceIdent)

	// 0. 记录安装前版本（确认逻辑的兜底条件 cur != pre 需要）
	var preVersion string
	if client := s.newV2DeviceClient(deviceIP); client != nil {
		if info, err := client.GetAPIVersionInfo(); err == nil && info != nil {
			preVersion = info.StringCurrentVersion
		}
	}

	// 1. 从 newapi 下载指定版本原始 zip（下载后即校验 hash/version，防错装）
	zipPath, err := maintenance.FetchV2SdkZip(version, nil)
	if err != nil {
		return fmt.Errorf("下载 SDK v%s 失败: %w", version, err)
	}
	defer os.Remove(zipPath)

	// 2. 上传到设备 8000（v2 设备 SDK 直连 8000，与 SdkCheckUpgrade v2 分支同形态）
	client := s.newV2DeviceClient(deviceIP)
	uploadErr := client.UploadSDK(zipPath)
	if uploadErr == nil {
		log.Printf("[v2sdk-install] 设备 %s 上传完成，开始轮询确认版本 v%s", deviceIP, version)
		// 3. 轮询确认设备版本变化（cur == target 或 cur != preVersion）
		if err := s.waitForV2DeviceVersion(deviceIP, version, preVersion); err != nil {
			return err
		}
	} else {
		// 上传失败：老版本 SDK 无 /server/upgrade/upload 接口（404）→ 降级 SSH
		// 部署（探测真实二进制路径/杀旧进程/拉起，见 maintenance.DeployV2SdkVersion）；
		// 其他错误（认证失败/超时）不降级，直接报。
		if !isUploadUnsupportedErr(uploadErr) {
			return fmt.Errorf("上传 SDK v%s 到设备失败: %w", version, uploadErr)
		}
		log.Printf("[v2sdk-install] %s 设备无上传接口，降级 SSH 部署: %v", deviceIP, uploadErr)
		if err := maintenance.DeployV2SdkVersion(deviceIP, nil); err != nil {
			return fmt.Errorf("上传失败（%v）且 SSH 部署失败: %w", uploadErr, err)
		}
		// SSH 部署成功后同样轮询确认（部署拉起的新二进制版本应为目标）
		if err := s.waitForV2DeviceVersion(deviceIP, version, preVersion); err != nil {
			return err
		}
	}
	log.Printf("[v2sdk-install] 设备 %s 已确认安装到 v%s", deviceIP, version)
	// 手动安装成功：重置该设备自动安装状态机（清失败计数/放弃标记，自动链路重新可用）
	if s.v2AutoInstallReset != nil {
		hostIP := s.resolveDeviceIP(deviceIP)
		s.v2AutoInstallReset(hostIP)
	}
	return nil
}

// isUploadUnsupportedErr 判定上传错误是否为「设备不支持上传接口」（老版本 SDK
// 404/路由不存在类）——只有这类错误才降级 SSH 部署，认证失败/超时不降级。
func isUploadUnsupportedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "不支持上传接口") ||
		strings.Contains(msg, "404") ||
		strings.Contains(msg, "405") ||
		strings.Contains(msg, "501")
}

// newV2DeviceClient 创建 v2 设备 8000 SDK 客户端：v2 模式下设备标识经
// resolveDeviceIP 解析为真实主机 IP（targetIP 与 hostIP 同机时结果一致），
// 8000 由 myt-sdk-go-v2 提供；非 v2 模式不应走到这里（调用方已分流）。
// 密码注入与 local 路径 newClient 对齐：列表里混管的 v3 主机 8000 /info 开了
// Basic 认证，缺密码会 401（code 61）导致版本查询失败（侧栏不显示版本）。
func (s *SdkApiServiceWrapper) newV2DeviceClient(deviceIP string) *sdkapi.Client {
	hostIP := s.resolveDeviceIP(deviceIP)
	client := sdkapi.NewClientWithTimeout(hostIP, 5*time.Minute)
	client.SetPassword(s.GetDevicePassword(hostIP))
	return client
}

// waitForV2DeviceVersion 轮询设备自报版本确认安装结果。
//
// 主条件：cur == target（设备端 version 是编译期 consts.Version，云端包
// inject-version 与包内 version 同源，装上后必然等于目标）。
// 兜底条件：cur != pre（版本确实变了但没变成目标——版本体系异常的设备，
// 视为已更新并记日志；至少比「版本纹丝不动却报成功」诚实）。
// 版本完全没变 → 60s 超时报错（上传失败或设备重启失败）。
//
// 历史教训：曾有「/info 可达即成功」的宽松确认——老设备 /info 本来就可达，
// 秒过、实际什么都没装（用户日志实证）。
func (s *SdkApiServiceWrapper) waitForV2DeviceVersion(deviceIP string, target string, preVersion string) error {
	deadline := time.Now().Add(v2InstallVersionPollTimeout)
	var lastSeen string
	for {
		client := s.newV2DeviceClient(deviceIP)
		info, err := client.GetAPIVersionInfo()
		if err == nil && info != nil && info.StringCurrentVersion != "" {
			cur := info.StringCurrentVersion
			if cur == target {
				return nil
			}
			if preVersion != "" && cur != preVersion {
				log.Printf("[v2sdk-install] %s 版本 %q→%q（目标 v%s，版本体系异常但已更新）",
					deviceIP, preVersion, cur, target)
				return nil
			}
			lastSeen = cur
		}
		if time.Now().After(deadline) {
			if lastSeen != "" {
				return fmt.Errorf("确认超时：设备版本仍为 v%s（安装前 v%s），期望 v%s",
					lastSeen, preVersion, target)
			}
			return fmt.Errorf("确认超时：%ds 内无法读取设备版本（期望 v%s，安装前 v%s）",
				int(v2InstallVersionPollTimeout.Seconds()), target, preVersion)
		}
		time.Sleep(installVersionPollInterval)
	}
}

// v2LatestSdkVersion 查询 v2_box_sdk 云端最新版本（string 原样返回）。
func (s *SdkApiServiceWrapper) v2LatestSdkVersion() string {
	v, _ := s.v2LatestSdkVersionInfo()
	return v
}

// v2LatestSdkVersionInfo 同 v2LatestSdkVersion，附带云端版本描述（升级确认
// 弹窗展示用；查询失败版本号为空串）。
func (s *SdkApiServiceWrapper) v2LatestSdkVersionInfo() (string, string) {
	info, err := sdkver.FetchLatestVersionOfTypeInfo(v2SdkType)
	if err != nil {
		log.Printf("[v2sdk-install] 查询 v2_box_sdk 最新版本失败: %v", err)
		return "", ""
	}
	return strings.TrimSpace(info.LatestVersion), strings.TrimSpace(info.Description)
}

// sdkCheckUpgradeV2 v2 模式版本查询：直连设备 8000 /info（myt-sdk-go-v2），
// 版本号为 string。走 V3 的 notifySdkExpired/cacheSdkVersion（int 语义）会
// 把 string 版本折成 0/误判过期——这里不做，前端版本显示直接用 string 字段。
// LatestVersion 语义与 V3 对齐：云端不可知置 -1。
func (s *SdkApiServiceWrapper) sdkCheckUpgradeV2(deviceIP string) (*sdkapi.APIVersionInfo, error) {
	client := s.newV2DeviceClient(deviceIP)
	info, err := client.GetAPIVersionInfo()
	if err != nil {
		return nil, err
	}
	// 云端最新版（string）：查询失败置空 → LatestVersion -1，前端按"未知"渲染。
	// 描述随 latest 一并带出（升级确认弹窗展示用；失败时为空）
	if latest, desc := s.v2LatestSdkVersionInfo(); latest != "" {
		info.StringLatestVersion = latest
		info.Description = desc
	} else {
		info.StringLatestVersion = ""
		info.LatestVersion = -1
	}
	// deviceId 供前端事件缓存对齐（与 V3 emit 同款字段；v2 无 WS 版本频道，
	// 前端直接读本次返回值，不发事件）
	return info, nil
}
