package taskqueue

import (
	"context"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	MaxGlobalConcurrency = 3 // 同时最多运行 3 个任务
	MaxTaskConcurrency   = 5 // 单任务内最多 5 路并发
)

// Status 任务状态
const (
	StatusPending  = "pending"
	StatusQueued   = "queued"
	StatusRunning  = "running"
	StatusDone     = "completed"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
)

func isTerminal(status string) bool {
	return status == StatusDone || status == StatusFailed || status == StatusCanceled
}

// FailedTarget 失败目标详情
type FailedTarget struct {
	DeviceIp    string `json:"deviceIp,omitempty"`
	MachineName string `json:"machineName,omitempty"`
	Slot        *int   `json:"slot,omitempty"`
	Error       string `json:"error"`
}

// Task 任务条目（对齐前端 TaskQueueItem）
type Task struct {
	ID             string                    `json:"id"`
	Type           string                    `json:"type"`
	Status         string                    `json:"status"`
	Total          int                       `json:"total"`
	Completed      int                       `json:"completed"`
	Failed         int                       `json:"failed"`
	Progress       int                       `json:"progress"`
	DeviceIps      []string                  `json:"deviceIps"`
	Targets        []map[string]any          `json:"targets"`
	DeviceProgress map[string]DeviceProgress `json:"deviceProgress"`
	FailedTargets  []FailedTarget            `json:"failedTargets"`
	StartTime      *int64                    `json:"startTime"`
	EndTime        *int64                    `json:"endTime"`
	Error          string                    `json:"error,omitempty"`
	// FailedAck 已确认的失败数（用户点开过任务详情后记录当时的 Failed），
	// 角标 failedCount = Failed > FailedAck 的任务数（未读提醒语义）。
	// 持久化在任务上——重启后任务从磁盘恢复，确认状态不丢（本地 state 方案重启即失效）。
	FailedAck int                       `json:"failedAck"`
	// 各类型特有字段（copyLogs/agentSteps/steps/stage 等）
	Details map[string]any `json:"details,omitempty"`

	mu        sync.Mutex
	cancelCtx context.Context
	cancelFn  context.CancelFunc
}

// DeviceProgress 设备维度进度
type DeviceProgress struct {
	Total           int    `json:"total"`
	Completed       int    `json:"completed"`
	Failed          int    `json:"failed"`
	CurrentProgress int    `json:"currentProgress,omitempty"`
	Stage           string `json:"stage,omitempty"`
	Error           string `json:"error,omitempty"`
}

// Snapshot 返回线程安全的任务快照（用于事件推送/前端展示）
func (t *Task) Snapshot() Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := Task{
		ID:             t.ID,
		Type:           t.Type,
		Status:         t.Status,
		Total:          t.Total,
		Completed:      t.Completed,
		Failed:         t.Failed,
		Progress:       t.Progress,
		DeviceIps:      append([]string(nil), t.DeviceIps...),
		Targets:        t.Targets,
		DeviceProgress: copyDeviceProgress(t.DeviceProgress),
		FailedTargets:  append([]FailedTarget(nil), t.FailedTargets...),
		StartTime:      t.StartTime,
		EndTime:        t.EndTime,
		Error:          t.Error,
		FailedAck:      t.FailedAck,
		Details:        copyDetails(t.Details),
	}
	return cp
}

func copyDeviceProgress(dp map[string]DeviceProgress) map[string]DeviceProgress {
	if dp == nil {
		return nil
	}
	out := make(map[string]DeviceProgress, len(dp))
	for k, v := range dp {
		out[k] = v
	}
	return out
}

// copyDetails 深拷贝 Details map，避免 emitTask 迭代时被 applyPatch 并发写入。
func copyDetails(d map[string]any) map[string]any {
	if d == nil {
		return nil
	}
	out := make(map[string]any, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

func (t *Task) setStatus(status string) {
	t.mu.Lock()
	t.Status = status
	if isTerminal(status) && t.EndTime == nil {
		now := time.Now().UnixMilli()
		t.EndTime = &now
	}
	t.mu.Unlock()
}

func (t *Task) addCompleted() {
	t.mu.Lock()
	t.Completed++
	t.Progress = calcProgress(t.Completed, t.Failed, t.Total)
	t.mu.Unlock()
}

func (t *Task) addFailed(deviceIp, name string, slot *int, err error) {
	t.mu.Lock()
	t.Failed++
	t.FailedTargets = append(t.FailedTargets, FailedTarget{
		DeviceIp: deviceIp, MachineName: name, Slot: slot, Error: err.Error(),
	})
	t.Progress = calcProgress(t.Completed, t.Failed, t.Total)
	t.mu.Unlock()
}

func (t *Task) setDeviceProgress(ip string, dp DeviceProgress) {
	t.mu.Lock()
	if t.DeviceProgress == nil {
		t.DeviceProgress = make(map[string]DeviceProgress)
	}
	t.DeviceProgress[ip] = dp
	t.mu.Unlock()
}

func (t *Task) setDetail(key string, val any) {
	t.mu.Lock()
	if t.Details == nil {
		t.Details = make(map[string]any)
	}
	t.Details[key] = val
	t.mu.Unlock()
}

func calcProgress(completed, failed, total int) int {
	if total <= 0 {
		return 0
	}
	p := (completed + failed) * 100 / total
	if p > 100 {
		p = 100
	}
	return p
}

// setProgress 直接设置任务总进度（供 single handler 按 category 等细分推进度）。
func (t *Task) setProgress(p int) {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	t.mu.Lock()
	t.Progress = p
	t.mu.Unlock()
}

// applyPatch 合并前端推进的 patch（仅允许安全字段：状态/进度/详情/失败目标/设备进度）
// 不允许改 ID/Type/Targets/DeviceIps/StartTime 等关键字段
func (t *Task) applyPatch(patch map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if v, ok := patch["status"].(string); ok && v != "" {
		t.Status = v
		if isTerminal(v) && t.EndTime == nil {
			now := millisNow()
			t.EndTime = &now
		}
	}
	if v, ok := patch["progress"].(float64); ok {
		t.Progress = int(v)
	}
	if v, ok := patch["completed"].(float64); ok {
		t.Completed = int(v)
	}
	if v, ok := patch["failed"].(float64); ok {
		t.Failed = int(v)
	}
	// 已确认失败数（角标未读提醒语义）：前端点开任务详情时推进
	if v, ok := patch["failedAck"].(float64); ok {
		t.FailedAck = int(v)
	}
	if v, ok := patch["error"].(string); ok {
		t.Error = v
	}
	// 设备进度
	if dp, ok := patch["deviceProgress"].(map[string]any); ok {
		if t.DeviceProgress == nil {
			t.DeviceProgress = make(map[string]DeviceProgress)
		}
		for ip, raw := range dp {
			if m, ok := raw.(map[string]any); ok {
				t.DeviceProgress[ip] = parseDeviceProgress(m)
			}
		}
	}
	// 失败目标
	if fts, ok := patch["failedTargets"].([]any); ok {
		t.FailedTargets = parseFailedTargets(fts)
	}
	// Details：合并子字段（各类型特有字段，如 copyLogs/agentSteps/stage 等）
	if details, ok := patch["details"].(map[string]any); ok {
		if t.Details == nil {
			t.Details = make(map[string]any)
		}
		for k, v := range details {
			t.Details[k] = v
		}
	}
	// 允许顶层直接写类型特有字段（前端习惯平铺，如 copyLogs/agentSteps 直接在 patch 顶层）
	for _, key := range []string{"copyLogs", "agentSteps", "agentAction", "steps", "totalSteps", "currentStepIndex", "currentStep", "imageProgress", "exportStage", "exportProgress", "moveStage", "moveMessage", "savedPath"} {
		if v, ok := patch[key]; ok {
			if t.Details == nil {
				t.Details = make(map[string]any)
			}
			t.Details[key] = v
		}
	}
}

// Queue 任务调度器
type Queue struct {
	mu       sync.Mutex
	tasks    []*Task
	running  map[string]*Task
	app      *application.App
	cm       *config.Manager
	executor *Executor
}

// NewQueue 创建调度器
func NewQueue(cm *config.Manager) *Queue {
	q := &Queue{
		cm:      cm,
		running: make(map[string]*Task),
	}
	q.executor = NewExecutor(q)
	return q
}

// SetApp 注入 Wails app（用于事件推送）
func (q *Queue) SetApp(app *application.App) {
	q.app = app
}

// Start 启动调度器
func (q *Queue) Start() {}

// Stop 停止调度器，取消所有运行中任务
func (q *Queue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, t := range q.running {
		if t.cancelFn != nil {
			t.cancelFn()
		}
	}
}

// parseDeviceProgress 从 map[string]any 解析设备进度
func parseDeviceProgress(m map[string]any) DeviceProgress {
	dp := DeviceProgress{}
	if v, ok := m["total"].(float64); ok {
		dp.Total = int(v)
	}
	if v, ok := m["completed"].(float64); ok {
		dp.Completed = int(v)
	}
	if v, ok := m["failed"].(float64); ok {
		dp.Failed = int(v)
	}
	if v, ok := m["currentProgress"].(float64); ok {
		dp.CurrentProgress = int(v)
	}
	if v, ok := m["stage"].(string); ok {
		dp.Stage = v
	}
	if v, ok := m["error"].(string); ok {
		dp.Error = v
	}
	return dp
}

// parseDeviceProgressAny 从 any（前端 details.deviceProgress）解析整个设备进度表
func parseDeviceProgressAny(v any) map[string]DeviceProgress {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]DeviceProgress, len(m))
	for ip, raw := range m {
		if mm, ok := raw.(map[string]any); ok {
			out[ip] = parseDeviceProgress(mm)
		}
	}
	return out
}

// parseFailedTargets 解析失败目标列表
func parseFailedTargets(list []any) []FailedTarget {
	result := make([]FailedTarget, 0, len(list))
	for _, raw := range list {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ft := FailedTarget{
			Error: getStr(m, "error"),
		}
		ft.DeviceIp = getStr(m, "deviceIp")
		ft.MachineName = getStr(m, "machineName")
		if v, ok := m["slot"].(float64); ok {
			s := int(v)
			ft.Slot = &s
		}
		result = append(result, ft)
	}
	return result
}

func getStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
