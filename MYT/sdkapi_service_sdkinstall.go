package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// SDK 指定版本安装：客户端下载指定版本 zip → 上传设备 /server/upgrade/upload
// → 轮询确认设备自报版本变化。供任务队列 sdkInstall batch 调用。
//
// 与 SdkUpgradeDevice（设备自拉云端最新）不同：本链路可安装任意云端版本
// （含旧版，用于回退；装旧版会覆盖本地缓存，可接受）。

// installVersionPollInterval 安装后确认轮询间隔
const installVersionPollInterval = 3 * time.Second

// installVersionPollTimeout 确认轮询上限：设备解压安装 35MB SDK 需要时间
const installVersionPollTimeout = 60 * time.Second

// installSdkOverallTimeout V3 安装整体兜底超时（与 v2 同理：任一步挂死会
// 无限期持有 installMu，挡住所有安装任务）
const installSdkOverallTimeout = 10 * time.Minute

// InstallSdkVersion 在单台设备上安装指定版本 SDK，版本确认变化才返回 nil。
// 步骤：EnsureSDK 取本地 zip（版本一致秒回，否则下载）→ UploadSDK 上传 →
// 每 3s 查一次 /info 的 currentVersion，最长 60s；超时/版本不符返回明确错误。
func (s *SdkApiServiceWrapper) InstallSdkVersion(deviceIP string, version int) error {
	if s.sdkVer == nil {
		return fmt.Errorf("SDK 版本服务未初始化")
	}
	if version <= 0 {
		return fmt.Errorf("无效的 SDK 版本号: %d", version)
	}
	// 防重入：与 SubmitSdkInstallTask 共用 installMu——任务队列执行期间
	// 新的提交/直调会被拒绝（下载+轮询确认是分钟级操作，并发互相干扰）
	if !s.installMu.TryLock() {
		return fmt.Errorf("已有 SDK 安装任务在执行中")
	}
	defer s.installMu.Unlock()

	// 整体兜底超时：挂死最多 10 分钟后强制返回，defer 释放 installMu
	done := make(chan error, 1)
	go func() {
		done <- s.installSdkVersionInner(deviceIP, version)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(installSdkOverallTimeout):
		return fmt.Errorf("SDK 安装整体超时（%d 分钟），已中止", int(installSdkOverallTimeout.Minutes()))
	}
}

// installSdkVersionInner 安装实际流程（供整体超时守护包裹）。
func (s *SdkApiServiceWrapper) installSdkVersionInner(deviceIP string, version int) error {

	// 1. 确保本地有该版本 zip（缓存命中秒回；装旧版覆盖缓存可接受）
	zipPath, err := s.sdkVer.EnsureSDK(version)
	if err != nil {
		return fmt.Errorf("下载 SDK v%d 失败: %w", version, err)
	}

	// 2. 上传到设备
	client := s.newClient(deviceIP)
	if err := client.UploadSDK(zipPath); err != nil {
		return fmt.Errorf("上传 SDK v%d 到设备失败: %w", version, err)
	}
	log.Printf("[sdk-install] 设备 %s 上传完成，开始轮询确认版本 v%d", deviceIP, version)

	// 3. 轮询确认设备版本变化（复用 SdkCheckUpgrade 的设备查询链路）
	if err := s.waitForDeviceVersion(deviceIP, version); err != nil {
		return err
	}
	log.Printf("[sdk-install] 设备 %s 已确认升级到 v%d", deviceIP, version)
	return nil
}

// waitForDeviceVersion 轮询设备自报版本，与目标一致才成功。
// 间隔 3s、上限 60s；查询本身失败不立即终止（设备安装期间 API 可能抖动），
// 累计超时仍未确认才报错。
func (s *SdkApiServiceWrapper) waitForDeviceVersion(deviceIP string, target int) error {
	deadline := time.Now().Add(installVersionPollTimeout)
	var lastSeen int
	for {
		client := s.newClient(deviceIP)
		info, err := client.GetAPIVersionInfo()
		if err == nil && info != nil && info.CurrentVersion > 0 {
			if info.CurrentVersion == target {
				return nil
			}
			lastSeen = info.CurrentVersion
		}
		if time.Now().After(deadline) {
			if lastSeen > 0 {
				return fmt.Errorf("确认超时：设备版本仍为 %d，期望 %d", lastSeen, target)
			}
			return fmt.Errorf("确认超时：%ds 内无法读取设备版本（期望 %d）", int(installVersionPollTimeout.Seconds()), target)
		}
		time.Sleep(installVersionPollInterval)
	}
}

// InstallSdkVersionAny 任务队列统一入口：按部署模式分流安装实现。
// v2 模式版本号是 string（可能非纯数字）直传 InstallV2SdkVersion；
// 其他模式走 V3 链路（Atoi 转 int，非数字报错）。
func (s *SdkApiServiceWrapper) InstallSdkVersionAny(deviceIP, version string) error {
	if s.cm != nil && s.cm.GetDeployMode() == "v2" {
		return s.InstallV2SdkVersion(deviceIP, version)
	}
	v, err := strconv.Atoi(strings.TrimSpace(version))
	if err != nil || v <= 0 {
		return fmt.Errorf("无效的 SDK 版本号: %q", version)
	}
	return s.InstallSdkVersion(deviceIP, v)
}

// SubmitSdkInstallTask 提交 SDK 指定版本安装批量任务（任务队列执行）。
// deviceIPs 为目标设备标识列表（local 为 IP，public 为 device_id）；
// version 为云端 /sdk/list 返回的版本号（统一按 string 存储——v2_box_sdk
// 版本号可能非纯数字，V3 路径在 handler 侧 Atoi 转换）。
// 安装防重入：已有安装任务在执行中时拒绝重复提交（下载+确认是分钟级操作，
// 重复提交会并发下载同一 zip 且互相干扰版本确认）。
// 返回任务 ID；同一任务内 5 路并发逐台上传+确认。
func (s *SdkApiServiceWrapper) SubmitSdkInstallTask(deviceIPs []string, version string) (string, error) {
	if len(deviceIPs) == 0 {
		return "", fmt.Errorf("未选择目标设备")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("无效的 SDK 版本号: %q", version)
	}
	if !s.installMu.TryLock() {
		return "", fmt.Errorf("已有 SDK 安装任务在执行中，请等待完成后再提交")
	}
	s.installMu.Unlock()
	targets := make([]map[string]any, 0, len(deviceIPs))
	for _, ip := range deviceIPs {
		targets = append(targets, map[string]any{"deviceIp": ip, "version": version})
	}
	// 直接走队列 Submit：details 携带 version 供任务详情展示；
	// sdkInstall 已注册 batch handler，execute=true 立即排队执行
	return s.taskSvc.SubmitSdkInstallTask(deviceIPs, version), nil
}
