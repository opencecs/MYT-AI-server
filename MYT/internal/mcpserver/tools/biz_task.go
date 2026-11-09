package tools

// biz_task.go — 任务队列域工具：批量设备操作任务的创建/查询/取消，
// 以及设备端异步任务（创建/迁移等长操作）状态查询。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

// --- 工具参数结构 ---

type createBatchTaskArgs struct {
	TaskType string   `json:"taskType" jsonschema_desc:"任务类型：restart/start/shutdown/delete/reset/shake/closeS5/sdkUpgrade（delete/reset 破坏性）"`
	Targets  []target `json:"targets" jsonschema_desc:"目标列表（deviceIp + name）"`
}

type target struct {
	DeviceIP string `json:"deviceIp"`
	Name     string `json:"name"`
}

type taskIDArgs struct {
	TaskID string `json:"taskId" jsonschema_desc:"任务 ID（来自 create_batch_task/list_tasks）"`
}

type taskStatusArgs struct {
	DeviceIP string `json:"deviceIp" jsonschema_desc:"设备 IP"`
	TaskID   string `json:"taskId" jsonschema_desc:"设备端任务 ID"`
}

// registerTaskTools 注册任务队列域工具。
func registerTaskTools(srv *mcp.Server, sink reg.Sink, d *BizDeps) {
	if d.Tasks == nil {
		return
	}
	reg.Reg(srv, sink, "create_batch_task",
		"创建批量设备操作任务（进任务队列后台执行）。taskType：restart/start/shutdown/delete/reset 等，"+
			"其中 delete/reset【破坏性】。返回任务 ID，可用 get_task 跟踪进度。",
		func(_ context.Context, a createBatchTaskArgs) (any, error) {
			if len(a.Targets) == 0 {
				return nil, fmt.Errorf("targets 不能为空")
			}
			targetsJSON, err := json.Marshal(a.Targets)
			if err != nil {
				return nil, err
			}
			id, err := d.Tasks.CreateBatch(a.TaskType, string(targetsJSON))
			if err != nil {
				return nil, err
			}
			return map[string]any{"taskId": id, "total": len(a.Targets)}, nil
		}, reg.Destructive())

	reg.Reg(srv, sink, "list_tasks", "列出任务队列中所有任务（含状态/进度/失败原因）。无破坏性。",
		func(context.Context, struct{}) (any, error) { return d.Tasks.List(), nil })

	reg.Reg(srv, sink, "get_task", "查询单个任务的详细状态。无破坏性。",
		func(_ context.Context, a taskIDArgs) (any, error) {
			t := d.Tasks.Get(a.TaskID)
			if t == nil {
				return nil, fmt.Errorf("任务 %q 不存在", a.TaskID)
			}
			return t, nil
		})

	reg.Reg(srv, sink, "cancel_task", "取消一个未完成的任务（已执行到一半的操作不会回滚）。",
		func(_ context.Context, a taskIDArgs) (any, error) {
			d.Tasks.Cancel(a.TaskID)
			return map[string]any{"result": "已请求取消", "taskId": a.TaskID}, nil
		}, reg.Destructive())

	reg.Reg(srv, sink, "get_device_task_status", "查询设备端异步任务（创建/迁移等长操作）的执行状态。无破坏性。",
		func(_ context.Context, a taskStatusArgs) (any, error) {
			status, err := d.SDK.GetTaskStatus(a.DeviceIP, a.TaskID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"status": strings.TrimSpace(status)}, nil
		})
}
