package taskqueue

import (
	"encoding/json"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Service 任务队列 Wails Service 对外接口
type Service struct {
	queue *Queue
}

// NewService 创建 Service
func NewService(q *Queue) *Service {
	return &Service{queue: q}
}

// SetSdk 注入 SDK API 提供者（供 setup.go 注入，不暴露 Queue 本身给前端）
func (s *Service) SetSdk(sdk SdkApiProvider) {
	s.queue.executor.SetSdkApi(sdk)
}

// SetApp 注入 Wails app（用于事件推送）
func (s *Service) SetApp(app *application.App) {
	s.queue.SetApp(app)
}

// Restore 从磁盘恢复任务列表（启动时调用）
func (s *Service) Restore() {
	s.queue.Restore()
}

// CreateBatch 创建批量任务
// taskType: restart/start/shutdown/delete/reset/shake/closeS5 等
// targetsJSON: JSON 编码的 targets 数组，每项含 {deviceIp, name, ...}
// 返回任务 ID
func (s *Service) CreateBatch(taskType, targetsJSON string) (string, error) {
	var targets []map[string]any
	if err := json.Unmarshal([]byte(targetsJSON), &targets); err != nil {
		return "", err
	}
	if len(targets) == 0 {
		return "", nil
	}
	deviceIps := extractDeviceIps(targets)
	return s.queue.Submit(taskType, deviceIps, targets, len(targets), nil, nil, true), nil
}

// CreateSingle 创建单任务（长任务：下载/上传/复制等）
// detailsJSON: JSON 编码的任务详情（镜像信息/源目标等）
func (s *Service) CreateSingle(taskType, detailsJSON string) (string, error) {
	var details map[string]any
	if detailsJSON != "" {
		if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
			return "", err
		}
	}
	deviceIps := extractAnyStrings(details["deviceIps"])
	targets := extractAnyTargets(details["targets"])
	total := 0
	if n, ok := details["total"].(float64); ok {
		total = int(n)
	}
	// 前端可带 deviceProgress 预填（如批量上传文件按 设备×文件数 初始化分母）：
	// 随任务创建进后端，消除 CreateSingle 的 task:updated 快照(dp=null) 晚于首个 updateTask
	// 到达时覆盖掉预填值的竞态（事件处理器 fallback total=1 会致 done 提前判定）。
	// 解析后从 details 删除，避免 flattenTask 的 Details 平铺与真实字段重复。
	deviceProgress := parseDeviceProgressAny(details["deviceProgress"])
	delete(details, "deviceProgress")
	// 有后端 handler 的任务（清理/sdkUpgrade 等）立即执行；纯前端编排的长任务（下载/上传/复制/create）排队等前端驱动。
	execute := hasHandler(taskType)
	return s.queue.Submit(taskType, deviceIps, targets, total, details, deviceProgress, execute), nil
}

// Cancel 取消任务
func (s *Service) Cancel(taskID string) {
	s.queue.Cancel(taskID)
}

// SubmitSdkInstallTask 提交 SDK 指定版本安装批量任务（batch 类型 sdkInstall，
// 单任务内 5 路并发）。version 随每个 target 携带（统一 string 存储——v2_box_sdk
// 版本号可能非纯数字，V3 路径 handler 侧 Atoi），由 handler 闭包捕获。
// 返回任务 ID。
func (s *Service) SubmitSdkInstallTask(deviceIPs []string, version string) string {
	targets := make([]map[string]any, 0, len(deviceIPs))
	for _, ip := range deviceIPs {
		targets = append(targets, map[string]any{"deviceIp": ip, "version": version})
	}
	details := map[string]any{"version": version}
	return s.queue.Submit("sdkInstall", deviceIPs, targets, len(targets), details, nil, true)
}

// CreateTeboxInstallTask 创建 tebox SDK 安装单任务（类型 teboxInstall，
// single handler 阻塞执行：取包 → 上传 → 装脚本 → /health 验证）。
// 返回任务 ID。
func (s *Service) CreateTeboxInstallTask(deviceIP string) string {
	targets := []map[string]any{{"deviceIp": deviceIP}}
	return s.queue.Submit("teboxInstall", []string{deviceIP}, targets, 1, nil, nil, true)
}

// CreateSuperSdkInstallTask 创建超级 SDK 安装单任务（类型 superSdkInstall，
// single handler 阻塞执行：镜像检查/拉取 → myt_sdk 容器创建/启动 → 81 回验，
// 见 handlers_supersdk.go）。返回任务 ID。
func (s *Service) CreateSuperSdkInstallTask(deviceIP string) string {
	targets := []map[string]any{{"deviceIp": deviceIP}}
	return s.queue.Submit("superSdkInstall", []string{deviceIP}, targets, 1, nil, nil, true)
}

// Remove 移除任务（仅终态）
func (s *Service) Remove(taskID string) {
	s.queue.Remove(taskID)
}

// ClearFinished 清除所有终态任务
func (s *Service) ClearFinished() {
	s.queue.ClearFinished()
}

// List 返回所有任务（Details 平铺到顶层，前端顶层字段直接可读）
func (s *Service) List() []map[string]any {
	tasks := s.queue.List()
	result := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, flattenTask(t))
	}
	return result
}

// Get 获取单个任务（Details 平铺）
func (s *Service) Get(taskID string) map[string]any {
	t := s.queue.Get(taskID)
	if t == nil {
		return nil
	}
	return flattenTask(*t)
}

// Update 前端推进度（长任务用）：patchJSON 为 JSON 编码的部分字段
// 允许字段：status/progress/completed/failed/error/deviceProgress/failedTargets/details
// 及顶层平铺的 copyLogs/agentSteps/steps/stage 等类型特有字段
func (s *Service) Update(taskID, patchJSON string) {
	var patch map[string]any
	if err := json.Unmarshal([]byte(patchJSON), &patch); err != nil {
		return
	}
	if s.queue == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[taskqueue] Update panic: taskID=%s patch=%s err=%v", taskID, patchJSON, r)
		}
	}()
	s.queue.Update(taskID, patch)
}

// extractDeviceIps 从 targets 提取设备 IP 列表
func extractDeviceIps(targets []map[string]any) []string {
	seen := map[string]bool{}
	var ips []string
	for _, t := range targets {
		ip := str(t, "deviceIp")
		if ip != "" && !seen[ip] {
			seen[ip] = true
			ips = append(ips, ip)
		}
	}
	return ips
}

// extractAnyStrings 从 any 提取 []string（兼容 JSON 反序列化的 []any）
func extractAnyStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, x := range arr {
		if s, ok := x.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// extractAnyTargets 从 any 提取 []map[string]any（兼容 JSON 反序列化）
func extractAnyTargets(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}
