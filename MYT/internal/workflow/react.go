package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// runReActLoop ReAct 循环：AI 分析 → tool_call → 执行 → 回传 → 继续
func (e *Engine) runReActLoop(ctx context.Context, wf *Workflow, contextStr string) (string, error) {
	// 初始消息
	messages := []ChatMessage{
		{Role: "system", Content: wf.SystemPrompt},
		{Role: "user", Content: contextStr + "\n\n请分析以上数据，找出问题并给出建议。"},
	}

	for i := 0; i < wf.MaxIter; i++ {
		// 调用 AI
		req := &ChatRequest{
			SystemPrompt: wf.SystemPrompt,
			Messages:      messages,
			Tools:         wf.Tools,
		}
		resp, err := e.aiClient.ChatWithTools(ctx, req)
		if err != nil {
			// Function Calling 失败 → 降级为单轮
			log.Printf("[workflow] ChatWithTools 失败(轮次%d): %v，降级为单轮", i, err)
			return e.aiClient.SimpleChat(ctx, wf.SystemPrompt,
				contextStr+"\n\n请分析以上数据，找出问题并给出建议。")
		}

		// 无 tool_calls → 最终回答
		if len(resp.ToolCalls) == 0 {
			return resp.Content, nil
		}

		// 有 tool_calls → 执行并回传
		// 先把 assistant 消息（含 tool_calls）加入历史
		messages = append(messages, ChatMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// 逐个执行 tool_call
		for _, tc := range resp.ToolCalls {
			result := e.executeToolCall(wf, tc)
			messages = append(messages, ChatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return "", fmt.Errorf("ReAct 循环超过最大轮次 %d", wf.MaxIter)
}

// executeToolCall 执行单个工具调用
func (e *Engine) executeToolCall(wf *Workflow, tc ToolCall) string {
	// 查找工具
	var tool *ToolDef
	for i := range wf.Tools {
		if wf.Tools[i].Name == tc.Name {
			tool = &wf.Tools[i]
			break
		}
	}
	if tool == nil {
		return fmt.Sprintf("错误：未知工具 %s", tc.Name)
	}

	// 解析参数
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		log.Printf("[workflow] 工具 %s 参数解析失败: %v", tc.Name, err)
		return fmt.Sprintf("错误：参数解析失败 %s", tc.Arguments)
	}

	// 执行
	result, err := tool.Handler(args)
	if err != nil {
		log.Printf("[workflow] 工具 %s 执行失败: %v", tc.Name, err)
		return fmt.Sprintf("错误：执行失败 %v", err)
	}
	return result
}

// BuildToolsParam 构建 OpenAI tools 参数
func BuildToolsParam(tools []ToolDef) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		var params map[string]any
		if t.Parameters != "" {
			_ = json.Unmarshal([]byte(t.Parameters), &params)
		}
		result = append(result, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		})
	}
	return result
}

// MessagesToRaw 将 ChatMessage 列表转换为 OpenAI messages 格式
func MessagesToRaw(msgs []ChatMessage, _ []ToolDef) []map[string]any {
	result := make([]map[string]any, 0, len(msgs))

	for _, m := range msgs {
		switch m.Role {
		case "system", "user":
			result = append(result, map[string]any{
				"role":    m.Role,
				"content": m.Content,
			})
		case "assistant":
			entry := map[string]any{
				"role":    "assistant",
				"content": m.Content,
			}
			if len(m.ToolCalls) > 0 {
				entry["tool_calls"] = toolCallsToRaw(m.ToolCalls)
			}
			result = append(result, entry)
		case "tool":
			result = append(result, map[string]any{
				"role":       "tool",
				"content":    m.Content,
				"tool_call_id": m.ToolCallID,
			})
		}
	}

	return result
}

// toolCallsToRaw 将 ToolCall 列表转为 OpenAI 格式
func toolCallsToRaw(calls []ToolCall) []map[string]any {
	result := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		var args any
		if err := json.Unmarshal([]byte(c.Arguments), &args); err != nil {
			args = c.Arguments
		}
		result = append(result, map[string]any{
			"id":   c.ID,
			"type": "function",
			"function": map[string]any{
				"name":      c.Name,
				"arguments": args,
			},
		})
	}
	return result
}
