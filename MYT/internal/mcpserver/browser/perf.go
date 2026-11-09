package browser

// perf.go — performance 类工具（复刻 chrome-devtools-mcp src/tools/performance.ts）：
// performance_start_trace / performance_stop_trace / performance_analyze_insight。
// 采集：事件会话订阅 Tracing.dataCollected / tracingComplete；
// 官方分析层为 DevTools PerformanceTrace 处理器（TS 大件），此处按 CDP trace
// 事件精简提取核心指标（FCP/LCP/CLS/DCL/Load + 主 frame URL），analyze_insight
// 返回已提取指标——官方标注为「适配移植（精简分析）」。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

// traceCategories 官方 TracingDefaultCategories + 可选类别（JsSampling/Screenshot），
// 与官方 performance.ts 的 categories 列表一致。
var traceCategories = []string{
	"-*",
	"devtools.timeline", "v8.execute", "blink.console", "blink.user_timing",
	"loading", "v8", "disabled-by-default-devtools.timeline",
	"disabled-by-default-devtools.screenshot",
	"disabled-by-default-devtools.timeline.stack",
	"disabled-by-default-devtools.timeline.frame",
	"latencyInfo",
	"disabled-by-default-v8.cpu_profiler",
}

// TraceSummary trace 停止后提取的核心指标（官方 TraceResult 的精简等价）。
type TraceSummary struct {
	URL        string             `json:"url,omitempty"`
	StartTime  float64            `json:"startTime,omitempty"`
	EndTime    float64            `json:"endTime,omitempty"`
	DurationMS float64            `json:"durationMs,omitempty"`
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	EventCount int                `json:"eventCount"`
	Insights   []string           `json:"insights,omitempty"`
}

type startTraceArgs struct {
	Reload   bool   `json:"reload" jsonschema_desc:"true（默认）时开始采集前先导航到 about:blank 清状态再回跳原 URL。需先用 navigate_page 导航到目标 URL"`
	AutoStop bool   `json:"autoStop" jsonschema_desc:"true（默认）时 5 秒后自动停止采集"`
	FilePath string `json:"filePath,omitempty" jsonschema_desc:"可选：原始 trace 保存路径（.json；.gz 压缩不启用，保存为未压缩 JSON）"`
}

type stopTraceArgs struct {
	FilePath string `json:"filePath,omitempty" jsonschema_desc:"可选：原始 trace 保存路径（.json）"`
}

type analyzeInsightArgs struct {
	InsightSetID string `json:"insightSetId" jsonschema_desc:"insight 集合 id（来自 trace 结果的 Available insight sets）"`
	InsightName  string `json:"insightName" jsonschema_desc:"要分析的 Insight 名称，如 DocumentLatency / LCPBreakdown（本实现提供 FCP/LCP/CLS/Load 等核心指标）"`
}

// registerPerfTools 注册 performance 类工具。
func registerPerfTools(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "performance_start_trace",
		"开始页面性能采集（这是复刻 chrome-devtools-mcp 的 performance_start_trace 工具："+
			"经 CDP Tracing 域采集，reload=true 时先清状态回跳原 URL，autoStop=true 时 5 秒后自动停止）。"+
			"分析层为精简实现：提取 FCP/LCP/CLS/DCL/Load 核心指标（官方完整 insights 处理器不移植）。",
		func(ctx context.Context, a startTraceArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			return h.startTrace(ctx, a)
		})

	reg.Reg(srv, sink, "performance_stop_trace",
		"停止性能采集并产出摘要（这是复刻 chrome-devtools-mcp 的 performance_stop_trace 工具："+
			"Tracing.end + dataCollected 汇总，可选保存原始 trace 到 filePath）。",
		func(ctx context.Context, a stopTraceArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			return h.stopTrace(ctx, a.FilePath)
		})

	reg.Reg(srv, sink, "performance_analyze_insight",
		"分析最近一次 trace 的指定洞察（这是复刻 chrome-devtools-mcp 的 performance_analyze_insight 工具；"+
			"精简实现：返回采集摘要中的核心指标，insightName 支持 FCP/LCP/CLS/DCL/Load/EventCount）。",
		func(ctx context.Context, a analyzeInsightArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			return h.analyzeInsight(a.InsightSetID, a.InsightName)
		}, reg.ReadOnly())
}

// startTrace 开始采集（官方 startTrace：reload 清状态 → tracing.start → autoStop）。
func (h *Hub) startTrace(ctx context.Context, a startTraceArgs) (any, error) {
	h.traceMu.Lock()
	if h.traceRunning {
		h.traceMu.Unlock()
		return map[string]any{"result": "Error: a performance trace is already running. Use performance_stop_trace to stop it. Only one trace can be running at any given time."}, nil
	}
	h.traceRunning = true
	h.traceEvents = nil
	h.traceDone = make(chan struct{})
	h.traceMu.Unlock()

	sess, err := h.Session(ctx)
	if err != nil {
		h.traceMu.Lock()
		h.traceRunning = false
		h.traceMu.Unlock()
		return nil, err
	}
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		h.traceMu.Lock()
		h.traceRunning = false
		h.traceMu.Unlock()
		return nil, err
	}

	// 记录起始 URL（reload 后回跳）
	origURL := ""
	if v, err := h.client.Eval(ctx, "location.href", 3*time.Second); err == nil {
		origURL, _ = v.(string)
	}

	fail := func(err error) (any, error) {
		h.traceMu.Lock()
		h.traceRunning = false
		h.traceMu.Unlock()
		return nil, err
	}
	if a.Reload {
		// 官方：先 about:blank 清状态（waitUntil: load）
		if _, err := h.client.Call(ctx, wsURL, "Page.navigate", map[string]any{"url": "about:blank"}, navTimeoutDefault); err != nil {
			return fail(err)
		}
	}
	if _, err := sess.Call(ctx, "Tracing.start", map[string]any{
		"traceConfig": map[string]any{
			"recordMode":         "recordUntilFull",
			"includedCategories": traceCategories,
		},
		"bufferSize": 1200 * 1000,
	}, 10*time.Second); err != nil {
		return fail(fmt.Errorf("Tracing.start 失败: %w", err))
	}
	if a.Reload && origURL != "" && origURL != "about:blank" {
		if _, err := h.client.Call(ctx, wsURL, "Page.navigate", map[string]any{"url": origURL}, navTimeoutDefault); err != nil {
			return fail(err)
		}
	}
	if a.AutoStop {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return fail(ctx.Err())
		}
		return h.stopTrace(ctx, a.FilePath)
	}
	return map[string]any{"result": "The performance trace is being recorded. Use performance_stop_trace to stop it."}, nil
}

// stopTrace 停止采集并汇总（官方 stopTracingAndAppendOutput）。
func (h *Hub) stopTrace(ctx context.Context, filePath string) (any, error) {
	h.traceMu.Lock()
	if !h.traceRunning {
		h.traceMu.Unlock()
		return map[string]any{"result": "No performance trace is running."}, nil
	}
	h.traceMu.Unlock()

	sess, err := h.Session(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := sess.Call(ctx, "Tracing.end", map[string]any{}, 5*time.Second); err != nil {
		return nil, fmt.Errorf("Tracing.end 失败: %w", err)
	}
	// 等待 tracingComplete（事件泵关闭 traceDone）
	select {
	case <-h.traceDone:
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("等待 Tracing.tracingComplete 超时")
	}
	h.traceMu.Lock()
	events := h.traceEvents
	h.traceRunning = false
	h.traceMu.Unlock()

	summary := summarizeTrace(events)
	h.traceMu.Lock()
	h.lastTrace = summary
	h.traceMu.Unlock()

	out := map[string]any{"result": "The performance trace has been stopped.", "summary": summary}
	if filePath != "" {
		data, _ := json.Marshal(map[string]any{"traceEvents": events})
		if err := os.WriteFile(filePath, data, 0o644); err != nil {
			out["saveError"] = err.Error()
		} else {
			out["result"] = fmt.Sprintf("The performance trace has been stopped. The raw trace data was saved to %s.", filePath)
		}
	}
	return out, nil
}

// analyzeInsight 分析最近 trace（精简：返回核心指标项）。
func (h *Hub) analyzeInsight(insightSetID, insightName string) (any, error) {
	h.traceMu.Lock()
	last := h.lastTrace
	h.traceMu.Unlock()
	if last == nil {
		return map[string]any{"result": "No recorded traces found. Record a performance trace so you have Insights to analyze."}, nil
	}
	name := strings.TrimSpace(insightName)
	if v, ok := last.Metrics[name]; ok {
		return map[string]any{"insightSetId": insightSetID, "insightName": name, "value": v, "unit": metricUnit(name)}, nil
	}
	if name == "EventCount" {
		return map[string]any{"insightSetId": insightSetID, "insightName": name, "value": last.EventCount}, nil
	}
	return map[string]any{
		"insightSetId": insightSetID,
		"available":    append([]string{"EventCount"}, metricNames(last)...),
		"result":       fmt.Sprintf("未找到洞察 %q；本实现可用的指标见 available 列表", name),
	}, nil
}

// metricNames 摘要中的指标名列表。
func metricNames(s *TraceSummary) []string {
	names := make([]string, 0, len(s.Metrics))
	for k := range s.Metrics {
		names = append(names, k)
	}
	return names
}

// metricUnit 指标单位。
func metricUnit(name string) string {
	switch name {
	case "CLS":
		return "score"
	case "EventCount":
		return "count"
	default:
		return "ms"
	}
}

// --- trace 事件采集与摘要提取（hub 事件泵调用） ---

// onTraceData Tracing.dataCollected：累积 trace 事件。
func (h *Hub) onTraceData(params json.RawMessage) {
	var p struct {
		Value []map[string]any `json:"value"`
	}
	if cdp.JSONUnmarshal(params, &p) != nil {
		return
	}
	h.traceMu.Lock()
	h.traceEvents = append(h.traceEvents, p.Value...)
	h.traceMu.Unlock()
}

// onTraceComplete Tracing.tracingComplete：通知 stopTrace 收尾。
func (h *Hub) onTraceComplete() {
	h.traceMu.Lock()
	if h.traceDone != nil {
		close(h.traceDone)
		h.traceDone = nil
	}
	h.traceMu.Unlock()
}

// summarizeTrace 从 trace 事件提取核心指标（官方 PerformanceTrace 处理器的
// 精简等价：FCP/LCP/CLS + 采集窗口；完整 insights 处理器为 DevTools TS 大件，
// 按 N/A 交代表述不移植）。
func summarizeTrace(events []map[string]any) *TraceSummary {
	s := &TraceSummary{EventCount: len(events), Metrics: map[string]float64{}}
	var navTS float64
	cls := 0.0
	lcpTS := 0.0
	for _, ev := range events {
		name, _ := ev["name"].(string)
		ts, _ := ev["ts"].(float64)
		cat, _ := ev["cat"].(string)
		switch {
		case name == "navigationStart" && navTS == 0:
			navTS = ts
			if args, ok := ev["args"].(map[string]any); ok {
				if frame, ok := args["frame"].(map[string]any); ok {
					if u, ok := frame["url"].(string); ok {
						s.URL = u
					}
				}
			}
		case name == "firstContentfulPaint":
			s.Metrics["FCP"] = ms(ts - navTS)
		case name == "largestContentfulPaint::Candidate" && ts > lcpTS:
			lcpTS = ts
			s.Metrics["LCP"] = ms(ts - navTS)
		case name == "LayoutShift" && strings.Contains(cat, "devtools.timeline"):
			if args, ok := ev["args"].(map[string]any); ok {
				if data, ok := args["data"].(map[string]any); ok {
					if had, _ := data["had_recent_input"].(bool); !had {
						if score, ok := data["score"].(float64); ok {
							cls += score
						}
					}
				}
			}
		}
		if ts > 0 && (ts < s.StartTime || s.StartTime == 0) {
			s.StartTime = ts
		}
		if ts > s.EndTime {
			s.EndTime = ts
		}
	}
	s.Metrics["CLS"] = cls
	s.DurationMS = ms(s.EndTime - s.StartTime)
	s.Insights = []string{"FCP", "LCP", "CLS", "Duration"}
	return s
}

// ms CDP 微秒时间戳 → 毫秒。
func ms(us float64) float64 { return us / 1000 }
