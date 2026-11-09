package workflow

import (
	"context"
	"time"
)

// Engine 工作流引擎
type Engine struct {
	aiClient  AIChatClient
	emitEvent func(event string, data any)
}

// NewEngine 创建引擎
func NewEngine(aiClient AIChatClient, emitFn func(string, any)) *Engine {
	return &Engine{aiClient: aiClient, emitEvent: emitFn}
}

// Run 执行工作流：采集 → 拼上下文 → AI 分析
func (e *Engine) Run(ctx context.Context, wf *Workflow) (*WorkflowResult, error) {
	// 阶段1：串行采集
	results, stepErrors := e.collectSteps(ctx, wf)

	// 阶段2：拼装上下文
	var contextStr string
	if wf.BuildContext != nil {
		contextStr = wf.BuildContext(results)
	}

	// 阶段3：AI 分析
	if len(wf.Tools) == 0 {
		e.emitProgress(wf.ID, "ai_analysis", "AI 分析中", "running", false)
		content, err := e.aiClient.SimpleChat(ctx, wf.SystemPrompt,
			contextStr+"\n\n请分析以上数据，找出问题并给出建议。")
		if err != nil {
			return nil, err
		}
		e.emitProgress(wf.ID, "ai_analysis", "AI 分析完成", "done", false)
		return &WorkflowResult{Content: content, StepResults: results, StepErrors: stepErrors}, nil
	}

	// ReAct 循环
	e.emitProgress(wf.ID, "ai_analysis", "AI 分析中", "running", false)
	content, err := e.runReActLoop(ctx, wf, contextStr)
	if err != nil {
		return nil, err
	}
	e.emitProgress(wf.ID, "ai_analysis", "AI 分析完成", "done", false)

	return &WorkflowResult{Content: content, StepResults: results, StepErrors: stepErrors}, nil
}

// collectSteps 串行采集步骤（设备端并发能力有限，并行会超时）
func (e *Engine) collectSteps(ctx context.Context, wf *Workflow) (map[string]string, map[string]string) {
	results := make(map[string]string)
	stepErrors := make(map[string]string)

	collectCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	for _, s := range wf.Steps {
		e.emitProgress(wf.ID, s.ID, s.Label, "running", false)

		stepCtx, stepCancel := context.WithTimeout(collectCtx, 15*time.Second)
		out, err := s.Run(stepCtx)
		stepCancel()

		if err != nil {
			stepErrors[s.ID] = err.Error()
			e.emitProgress(wf.ID, s.ID, s.Label, "failed", true)
			continue
		}

		results[s.ID] = out
		e.emitProgress(wf.ID, s.ID, s.Label, "done", false)
	}
	return results, stepErrors
}

// emitProgress 推送进度事件
func (e *Engine) emitProgress(workflow, step, label, status string, failed bool) {
	if e.emitEvent != nil {
		e.emitEvent("workflow:progress", map[string]any{
			"workflow": workflow,
			"step":     step,
			"label":    label,
			"status":   status,
			"failed":   failed,
			"time":     time.Now().Format(time.RFC3339),
		})
	}
}
