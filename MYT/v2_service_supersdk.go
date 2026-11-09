package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/moyunteng/myt/internal/taskqueue"
	"github.com/moyunteng/myt/internal/v2host"
)

// --- 超级 SDK 一键安装（任务队列接入）---
//
// v2 准入放宽后（仅 UDP+2375 可达即可添加），81 缺失的主机进列表时标记
// SuperSdkReady=false；用户在设备详情/添加结果处点「安装超级 SDK」触发本文件
// 的 InstallSuperSdk——它只负责校验 + 提交 superSdkInstall 任务（执行体在
// internal/v2host/supersdk.go 的 EnsureSuperSdk，handler 见
// internal/taskqueue/handlers_supersdk.go）：持久化/恢复/进度推送/超时兜底
// 全由任务队列接管，前端在任务面板看阶段进度（dp.stage），完成后经
// devicelist:updated（就绪状态翻转广播）感知。

// SetTaskQueueService 注入任务队列服务（setup.go 装配，InstallSuperSdk 提交用）。
func (s *V2ServiceWrapper) SetTaskQueueService(svc *taskqueue.Service) {
	s.taskSvc = svc
}

// InstallSuperSdk 提交超级 SDK 安装任务（幂等：81 已就绪时 EnsureSuperSdk
// 直接短路返回已安装）。deviceID 兼容序列号与 IP（resolveDevice 兜底）。
// 返回任务 ID，立即返回不阻塞（镜像拉取分钟级由任务队列异步执行）。
//
// 前置就绪校验：提交任务前先读落库的就绪状态，ready=true 直接报错
// 「已安装，无需重复安装」——读的是缓存而非现场 81 探测（避免提交按钮
// 卡 20s 探测超时；缓存 false 但设备实际已装的场景由 EnsureSuperSdk 的
// 幂等短路兜底，白跑一次但不产生副作用）。
func (s *V2ServiceWrapper) InstallSuperSdk(deviceID string) (string, error) {
	if s.configManager.GetDeployMode() != "v2" {
		return "", fmt.Errorf("仅 v2 模式支持安装超级 SDK")
	}
	if s.taskSvc == nil {
		return "", fmt.Errorf("任务队列服务未就绪，请稍后再试")
	}
	dev, err := s.resolveDevice(deviceID)
	if err != nil {
		return "", err
	}
	if dev.IP == "" {
		return "", fmt.Errorf("设备 IP 未知，无法安装超级 SDK")
	}
	if dev.SuperSdkReady {
		return "", fmt.Errorf("超级 SDK 已安装，无需重复安装")
	}
	taskID := s.taskSvc.CreateSuperSdkInstallTask(dev.IP)
	log.Printf("[v2supersdk] 已提交安装任务: ip=%s task=%s image=%s", dev.IP, taskID, v2host.SuperSdkImage)
	return taskID, nil
}

// CheckSuperSdkReady 查询超级 SDK 就绪状态并落库（前端设备详情刷新用）。
// 81 探测成功 = true；探测失败时区分处理：若已有 true 状态，视为瞬时失败
// （超时/网络抖动/81 短暂无响应）不翻转，保持现值防 UI 抖动；仅当前为 false
// 或无记录时才写 false。emit 语义：写入后无条件 emit（false→false 也会广播），
// devicelist:updated 事件本身轻量，前端按快照渲染无副作用。
func (s *V2ServiceWrapper) CheckSuperSdkReady(deviceID string) (bool, error) {
	dev, err := s.resolveDevice(deviceID)
	if err != nil {
		return false, err
	}
	if dev.IP == "" {
		return false, fmt.Errorf("设备 IP 未知")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err = s.client.ListContainersViaSDK(ctx, dev.IP)
	ready := err == nil
	// 瞬时失败防抖：探测失败且缓存已是 true → 保持 true 不写不广播。
	// 注意 81 探测失败不区分「未安装」与「不可达」（接口无该信号），只能按
	// 「失败不降级已就绪状态」的保守策略处理；真正被卸载的容器靠用户主动
	// 卸载操作或后续 false→确认路径刷新。
	if !ready && dev.SuperSdkReady {
		return dev.SuperSdkReady, nil
	}
	s.configManager.UpdateDeviceSuperSdkReady(dev.DeviceID, ready)
	s.emitDeviceList()
	return ready, nil
}
