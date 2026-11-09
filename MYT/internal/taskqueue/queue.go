package taskqueue

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sync/atomic"
	"time"
)

// Submit 提交任务，返回任务 ID。execute=true 启动执行器（批量任务），false 只存状态（长任务由前端编排）
// deviceProgress 为可选的设备进度预填（前端编排任务在创建时即可确定分母，如批量上传按设备×文件数）
func (q *Queue) Submit(taskType string, deviceIps []string, targets []map[string]any, total int, details map[string]any, deviceProgress map[string]DeviceProgress, execute bool) string {
	id := genID()
	now := time.Now().UnixMilli()
	t := &Task{
		ID:             id,
		Type:           taskType,
		Status:         StatusQueued,
		Total:          total,
		DeviceIps:      deviceIps,
		Targets:        targets,
		DeviceProgress: deviceProgress,
		Details:        details,
		StartTime:      &now,
	}
	if total == 0 {
		t.Total = len(targets)
	}
	q.mu.Lock()
	q.tasks = append(q.tasks, t)
	if execute && len(q.running) < MaxGlobalConcurrency {
		q.startTaskLocked(t)
	} else {
		t.Status = StatusQueued
	}
	q.mu.Unlock()
	q.emitTask(t.Snapshot())
	q.persist()
	return id
}

// startTaskLocked 启动任务（调用方持锁）
func (q *Queue) startTaskLocked(t *Task) {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancelCtx = ctx
	t.cancelFn = cancel
	t.Status = StatusRunning
	q.running[t.ID] = t
	go q.executor.Run(ctx, t)
}

// onTaskDone 任务执行完成回调（由 Executor 调用）
func (q *Queue) onTaskDone(t *Task) {
	q.mu.Lock()
	delete(q.running, t.ID)
	// 调度下一个排队任务
	if len(q.running) < MaxGlobalConcurrency {
		for _, nt := range q.tasks {
			if nt.Status == StatusQueued {
				q.startTaskLocked(nt)
				break
			}
		}
	}
	q.mu.Unlock()
	q.emitTask(t.Snapshot())
	q.persist()
}

// Cancel 取消任务。运行中任务通过 context 中断，排队任务直接标记取消。
func (q *Queue) Cancel(taskID string) {
	q.mu.Lock()
	t, ok := q.findTaskLocked(taskID)
	if !ok {
		q.mu.Unlock()
		return
	}
	if t.Status == StatusQueued {
		t.setStatus(StatusCanceled)
		delete(q.running, t.ID) // 排队任务不在 running，但防御性处理
		q.mu.Unlock()
		q.emitTask(t.Snapshot())
		q.persist()
		return
	}
	if t.cancelFn != nil {
		t.cancelFn()
	}
	q.mu.Unlock()
}

// Remove 从列表移除任务（仅终态可移除）
func (q *Queue) Remove(taskID string) {
	q.mu.Lock()
	t, ok := q.findTaskLocked(taskID)
	if !ok || !isTerminal(t.Status) {
		q.mu.Unlock()
		return
	}
	for i, tt := range q.tasks {
		if tt.ID == taskID {
			q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)
			break
		}
	}
	q.mu.Unlock()
	q.persist()
}

// ClearFinished 清除所有终态任务
func (q *Queue) ClearFinished() {
	q.mu.Lock()
	remaining := q.tasks[:0]
	for _, t := range q.tasks {
		if !isTerminal(t.Status) {
			remaining = append(remaining, t)
		}
	}
	q.tasks = remaining
	q.mu.Unlock()
	q.persist()
}

// List 返回所有任务快照（按状态排序：running/queued → failed → completed）
func (q *Queue) List() []Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	result := make([]Task, 0, len(q.tasks))
	for _, t := range q.tasks {
		result = append(result, t.Snapshot())
	}
	sortTasks(result)
	return result
}

// Get 获取单个任务快照
func (q *Queue) Get(taskID string) *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	t, ok := q.findTaskLocked(taskID)
	if !ok {
		return nil
	}
	cp := t.Snapshot()
	return &cp
}

// Update 前端推进度：合并 patch 到任务，emit task:updated + 持久化
func (q *Queue) Update(taskID string, patch map[string]any) {
	q.mu.Lock()
	t, ok := q.findTaskLocked(taskID)
	if !ok {
		q.mu.Unlock()
		return
	}
	t.applyPatch(patch)
	q.mu.Unlock()
	q.emitTask(t.Snapshot())
	q.persist()
}

func (q *Queue) findTaskLocked(taskID string) (*Task, bool) {
	for _, t := range q.tasks {
		if t.ID == taskID {
			return t, true
		}
	}
	return nil, false
}

// emitTask 推送任务更新事件给前端。Details 平铺到顶层，前端可直接读 imageProgress/copyLogs 等
func (q *Queue) emitTask(t Task) {
	if q.app == nil || q.app.Event == nil {
		return
	}
	q.app.Event.Emit("task:updated", flattenTask(t))
}

// flattenTask 把 Task + Details 平铺成 map[string]any，前端顶层字段直接可读
func flattenTask(t Task) map[string]any {
	out := map[string]any{
		"id":             t.ID,
		"type":           t.Type,
		"status":         t.Status,
		"total":          t.Total,
		"completed":      t.Completed,
		"failed":         t.Failed,
		"progress":       t.Progress,
		"deviceIps":      t.DeviceIps,
		"targets":        t.Targets,
		"deviceProgress": t.DeviceProgress,
		"failedTargets":  t.FailedTargets,
		"startTime":      t.StartTime,
		"endTime":        t.EndTime,
		"error":          t.Error,
		"failedAck":      t.FailedAck,
	}
	// 合并 Details 到顶层（imageProgress/currentStep/copyLogs/agentSteps/stage 等）
	for k, v := range t.Details {
		out[k] = v
	}
	return out
}

// persist 持久化任务列表（落档）
func (q *Queue) persist() {
	if q.cm == nil {
		return
	}
	q.mu.Lock()
	snapshot := make([]Task, 0, len(q.tasks))
	for _, t := range q.tasks {
		snapshot = append(snapshot, t.Snapshot())
	}
	q.mu.Unlock()
	snapshot = sanitizeSnapshot(snapshot)
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(q.cm.TaskQueuePath(), data, 0644)
}

// secretKeys 落档时需要抹掉的 payload 字段：任务可能携带第三方服务器 SSH 密码
// （公网穿透安装/卸载）。内存中的任务保留原值供 handler 执行，只在此处抹成空串——
// 重启后未完成任务一律标记 canceled，不再需要凭据。
var secretKeys = map[string]bool{"serverSSHPassword": true, "sshPassword": true, "password": true}

// sanitizeSnapshot 返回落档用的副本：Targets/Details 里的凭据字段抹为空串
func sanitizeSnapshot(tasks []Task) []Task {
	out := make([]Task, len(tasks))
	copy(out, tasks)
	for i := range out {
		if len(out[i].Targets) > 0 {
			targets := make([]map[string]any, len(out[i].Targets))
			for j, tg := range out[i].Targets {
				targets[j] = redactSecrets(tg)
			}
			out[i].Targets = targets
		}
		out[i].Details = redactSecrets(out[i].Details)
	}
	return out
}

// redactSecrets 抹掉 map 里的凭据字段（无凭据字段时原样返回，避免多余分配）
func redactSecrets(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	hit := false
	for k := range m {
		if secretKeys[k] {
			hit = true
			break
		}
	}
	if !hit {
		return m
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if secretKeys[k] {
			out[k] = ""
			continue
		}
		out[k] = v
	}
	return out
}

// Restore 从磁盘恢复任务列表（启动时调用）
// 运行中的任务标记为 canceled（无法恢复执行）
func (q *Queue) Restore() {
	if q.cm == nil {
		return
	}
	data, err := os.ReadFile(q.cm.TaskQueuePath())
	if err != nil || len(data) == 0 {
		return
	}
	var saved []Task
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	q.mu.Lock()
	for i := range saved {
		t := saved[i]
		if !isTerminal(t.Status) {
			t.Status = StatusCanceled
			now := time.Now().UnixMilli()
			t.EndTime = &now
			t.Error = "应用重启，任务中断"
		}
		q.tasks = append(q.tasks, &t)
	}
	q.mu.Unlock()
	log.Printf("[taskqueue] 恢复 %d 个任务", len(saved))
}

func sortTasks(tasks []Task) {
	rank := func(s string) int {
		if s == StatusRunning || s == StatusQueued || s == StatusPending {
			return 0
		}
		if s == StatusFailed {
			return 1
		}
		return 2
	}
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			ri, rj := rank(tasks[i].Status), rank(tasks[j].Status)
			if ri > rj || (ri == rj && (tasks[i].StartTime != nil && tasks[j].StartTime != nil && *tasks[i].StartTime < *tasks[j].StartTime)) {
				tasks[i], tasks[j] = tasks[j], tasks[i]
			}
		}
	}
}

var idCounter atomic.Int64

func genID() string {
	return time.Now().Format("0102150405") + "_" + itoa(int(idCounter.Add(1)))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
