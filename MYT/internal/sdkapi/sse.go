package sdkapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SSEResult 设备端 SSE 事件（text/event-stream，每行 data: {json}）。
// 字段并集覆盖导出/导入等长任务（各事件只用其中一部分）。
type SSEResult struct {
	Stage      string `json:"stage"`
	Progress   int    `json:"progress"`
	Msg        string `json:"msg"`
	Current    int64  `json:"current"`
	Total      int64  `json:"total"`
	Speed      string `json:"speed"`
	ExportName string `json:"exportName"` // 导出 complete 事件
	Name       string `json:"name"`       // 导入 complete 事件
}

// SSEEventFn SSE 事件回调（每个 data 事件触发一次）
type SSEEventFn func(SSEResult)

// readSSEResponse 流式读取设备端 SSE 长任务响应（用于自定义请求体的接口，
// 如 /androidV2/import 的 octet-stream 上传）：
//   - stage=complete：正常结束，返回 complete 事件（含 exportName/name）
//   - stage=error：返回错误（带设备端 msg）
//   - 流中断/未收到 complete：返回错误
func readSSEResponse(resp *http.Response, onEvent SSEEventFn) (SSEResult, error) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var last SSEResult
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var ev SSEResult
		if json.Unmarshal([]byte(data), &ev) != nil || ev.Stage == "" {
			continue
		}
		last = ev
		if onEvent != nil {
			onEvent(ev)
		}
		switch ev.Stage {
		case "complete":
			return last, nil
		case "error":
			return last, fmt.Errorf("设备端操作失败: %s", ev.Msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return last, fmt.Errorf("SSE 流读取中断: %w", err)
	}
	return last, fmt.Errorf("SSE 流结束但未收到 complete 事件（最后阶段: %s）", last.Stage)
}

// typedStageAdapter 把 client_sse.go 的裸事件回调（func([]byte)）适配为
// 类型化阶段事件回调（SSEEventFn），非阶段事件（无 stage 字段）静默跳过。
func typedStageAdapter(emit SSEEventFn) func(body []byte) {
	return func(body []byte) {
		if emit == nil {
			return
		}
		var ev SSEResult
		if json.Unmarshal(body, &ev) != nil || ev.Stage == "" {
			return
		}
		emit(ev)
	}
}

// stageFromEvent 解析 doPostSSE 返回的最后一个裸事件：
// stage=complete 正常返回；stage=error 返回错误；其他视为未完成。
func stageFromEvent(lastBody []byte) (SSEResult, error) {
	var ev SSEResult
	if err := json.Unmarshal(lastBody, &ev); err != nil {
		return ev, fmt.Errorf("SSE 末事件解析失败: %w", err)
	}
	switch ev.Stage {
	case "complete":
		return ev, nil
	case "error":
		return ev, fmt.Errorf("设备端操作失败: %s", ev.Msg)
	default:
		return ev, fmt.Errorf("SSE 未完成（最后阶段: %s）", ev.Stage)
	}
}