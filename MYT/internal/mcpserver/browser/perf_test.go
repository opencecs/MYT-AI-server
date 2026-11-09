package browser

// perf_test.go — performance trace 与 evaluate_script 单测：
// Tracing 命令序列、trace 事件摘要提取、insight 查询、脚本执行的 objectId 链路。

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/uimap"
)

// feedTraceEvent 注入 Tracing.dataCollected 事件。
func feedTraceEvent(h *Hub, events []map[string]any) {
	b, _ := json.Marshal(map[string]any{"value": events})
	h.dispatch(cdp.Event{Method: "Tracing.dataCollected", Params: b})
}

// TestTraceSummaryExtraction trace 事件 → 核心指标（FCP/LCP/CLS/时长）。
func TestTraceSummaryExtraction(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	feedTraceEvent(h, []map[string]any{
		{"name": "navigationStart", "ts": 1000000, "cat": "devtools.timeline",
			"args": map[string]any{"frame": map[string]any{"url": "http://localhost/"}}},
		{"name": "firstContentfulPaint", "ts": 1500000, "cat": "loading,runtime,devtools.timeline"},
		{"name": "largestContentfulPaint::Candidate", "ts": 2000000, "cat": "loading,runtime,devtools.timeline"},
		{"name": "LayoutShift", "ts": 1600000, "cat": "devtools.timeline",
			"args": map[string]any{"data": map[string]any{"score": 0.05, "had_recent_input": false}}},
		{"name": "LayoutShift", "ts": 1700000, "cat": "devtools.timeline",
			"args": map[string]any{"data": map[string]any{"score": 0.1, "had_recent_input": true}}}, // 有输入，不计入
	})
	s := summarizeTrace(h.traceEvents)
	if s.URL != "http://localhost/" {
		t.Fatalf("主 frame URL = %q", s.URL)
	}
	if s.Metrics["FCP"] != 500.0 {
		t.Fatalf("FCP = %v, want 500（微秒→毫秒）", s.Metrics["FCP"])
	}
	if s.Metrics["LCP"] != 1000.0 {
		t.Fatalf("LCP = %v, want 1000", s.Metrics["LCP"])
	}
	if s.Metrics["CLS"] != 0.05 {
		t.Fatalf("CLS = %v, want 0.05（had_recent_input 不计入）", s.Metrics["CLS"])
	}
	if s.EventCount != 5 {
		t.Fatalf("EventCount = %d, want 5", s.EventCount)
	}
}

// TestStartStopTraceSequence start/stop trace 的 CDP 命令序列与摘要产出。
func TestStartStopTraceSequence(t *testing.T) {
	var h *Hub
	h, rec := newTestHub(t, func(rec *recorder, method string, params json.RawMessage) (any, error) {
		if method == "Tracing.start" {
			// 模拟 CDP：start 后推送数据 + 完成
			feedTraceEvent(h, []map[string]any{
				{"name": "navigationStart", "ts": 1000000, "cat": "devtools.timeline",
					"args": map[string]any{"frame": map[string]any{"url": "http://localhost/"}}},
				{"name": "firstContentfulPaint", "ts": 1500000, "cat": "loading"},
			})
			go func() {
				time.Sleep(50 * time.Millisecond)
				h.onTraceComplete()
			}()
			return map[string]any{}, nil
		}
		return stdHandler(rec, method, params)
	})
	if _, err := h.Session(ctxBg()); err != nil {
		t.Fatalf("Session 失败: %v", err)
	}
	out, err := h.startTrace(ctxBg(), startTraceArgs{Reload: false, AutoStop: false})
	if err != nil {
		t.Fatalf("startTrace 失败: %v", err)
	}
	if !strings.Contains(out.(map[string]any)["result"].(string), "being recorded") {
		t.Fatalf("start 响应异常: %v", out)
	}
	// 重复 start → 官方「已在运行」提示
	out2, err := h.startTrace(ctxBg(), startTraceArgs{Reload: false, AutoStop: false})
	if err != nil {
		t.Fatalf("重复 startTrace 失败: %v", err)
	}
	if !strings.Contains(out2.(map[string]any)["result"].(string), "already running") {
		t.Fatalf("重复 start 应提示已在运行: %v", out2)
	}
	out3, err := h.stopTrace(ctxBg(), "")
	if err != nil {
		t.Fatalf("stopTrace 失败: %v", err)
	}
	if !strings.Contains(out3.(map[string]any)["result"].(string), "has been stopped") {
		t.Fatalf("stop 响应异常: %v", out3)
	}
	foundStart := false
	for _, c := range rec.calls {
		if c.Method == "Tracing.start" {
			foundStart = true
			cfg, _ := c.Params["traceConfig"].(map[string]any)
			if cfg == nil {
				t.Fatal("Tracing.start 应带 traceConfig")
			}
		}
	}
	if !foundStart {
		t.Fatal("缺少 Tracing.start 调用")
	}
	// 摘要已存储，analyze_insight 可查
	out4, err := h.analyzeInsight("set-1", "FCP")
	if err != nil {
		t.Fatalf("analyzeInsight 失败: %v", err)
	}
	m := out4.(map[string]any)
	if m["value"] != 500.0 {
		t.Fatalf("insight FCP = %v, want 500", m["value"])
	}
}

// TestAnalyzeInsightNoTrace 无 trace 时官方提示文案。
func TestAnalyzeInsightNoTrace(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	out, err := h.analyzeInsight("set-1", "LCPBreakdown")
	if err != nil {
		t.Fatalf("analyzeInsight 失败: %v", err)
	}
	if !strings.Contains(out.(map[string]any)["result"].(string), "No recorded traces found") {
		t.Fatalf("无 trace 提示文案异常: %v", out)
	}
}

// TestEvaluateScriptObjectIDs evaluate_script(function)：fn 与 uid 参数经
// RemoteObject objectId 传入 callFunctionOn（官方 ElementHandle 参数等价）。
func TestEvaluateScriptObjectIDs(t *testing.T) {
	h, rec := newTestHub(t, func(rec *recorder, method string, params json.RawMessage) (any, error) {
		switch method {
		case "Runtime.evaluate":
			var p struct {
				Expression string `json:"expression"`
			}
			_ = json.Unmarshal(params, &p)
			if strings.Contains(p.Expression, "window") {
				return map[string]any{"result": map[string]any{"objectId": "win-1"}}, nil
			}
			if strings.Contains(p.Expression, "findEl") {
				// uid 参数的身份定位（act.go locateObjectID）
				return map[string]any{"result": map[string]any{"objectId": "obj-1"}}, nil
			}
			// fn 表达式（function 形态）与 script 形态的值
			return map[string]any{"result": map[string]any{"objectId": "fn-1"}}, nil
		case "Runtime.callFunctionOn":
			return map[string]any{"result": map[string]any{"value": `{"title":"ok"}`}}, nil
		}
		return stdHandler(rec, method, params)
	})
	withRefs(h, uimap.RefTable{"1_0": refOf("button", "性能", 10, 20)})
	out, err := h.evaluateScript(ctxBg(), evaluateScriptArgs{
		Function: "() => document.title",
		Args:     []string{"1_0"},
	})
	if err != nil {
		t.Fatalf("evaluateScript 失败: %v", err)
	}
	var call *recordedCall
	for i := range rec.calls {
		if rec.calls[i].Method == "Runtime.callFunctionOn" {
			call = &rec.calls[i]
		}
	}
	if call == nil {
		t.Fatal("缺少 Runtime.callFunctionOn")
	}
	args, _ := call.Params["arguments"].([]any)
	if len(args) != 2 {
		t.Fatalf("应有 fn + 1 个 uid 参数，实际 %v", call.Params["arguments"])
	}
	if args[0].(map[string]any)["objectId"] != "fn-1" || args[1].(map[string]any)["objectId"] != "obj-1" {
		t.Fatalf("objectId 参数映射异常: %v", args)
	}
	if m := out.(map[string]any); m["value"] != `{"title":"ok"}` {
		t.Fatalf("返回值异常: %v", m)
	}
}

// TestEvaluateScriptValidation evaluate_script 参数校验（官方语义）。
func TestEvaluateScriptValidation(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	// function 与 sourcePath 二选一
	if _, err := h.evaluateScript(ctxBg(), evaluateScriptArgs{}); err == nil ||
		!strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("双空应报错, got %v", err)
	}
	if _, err := h.evaluateScript(ctxBg(), evaluateScriptArgs{
		Function: "() => 1", SourcePath: "x.js"}); err == nil ||
		!strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("双填应报错, got %v", err)
	}
	// script 形态不支持 args
	if _, err := h.evaluateScript(ctxBg(), evaluateScriptArgs{
		Function: "1+1", Format: "script", Args: []string{"1_0"}}); err == nil ||
		!strings.Contains(err.Error(), "args cannot be used") {
		t.Fatalf("script+args 应报官方错, got %v", err)
	}
}
