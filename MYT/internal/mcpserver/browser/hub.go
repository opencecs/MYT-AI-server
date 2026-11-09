// Package browser — 浏览器类 MCP 工具（chrome-devtools-mcp 的 1:1 移植）：
// input/navigation/network/debugging/emulation/performance 各域。
// 依赖 cdp（传输层）与 uimap（快照/uid 机制），由根包 mcpserver 装配。
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
	"github.com/moyunteng/myt/internal/mcpserver/uimap"
)

// cdpUnavailableMsg CDP 未就绪时的统一提示。
const cdpUnavailableMsg = "界面控制需重启客户端后可用（CDP 未就绪：客户端启动时未开启远程调试，或窗口尚未创建）"

// Hub 浏览器类工具的状态中枢：uid 引用表、page target 选择、事件会话、
// JS dialog 状态、network/console 环形缓冲、trace 状态（实现 uimap.RefSink）。
type Hub struct {
	client *cdp.Client
	snap   *uimap.Collector

	mu     sync.Mutex
	refs   uimap.RefTable // uid → 元素身份（ui_map 全量重建；动作按身份在页面内重新定位）
	curWS  string         // 当前选中 page target 的 wsURL（空 = 首个 page target）
	sess   *cdp.Session   // 当前 target 的事件会话（惰性建立）
	sessWS string         // sess 所属 target
	pumpOn bool           // 事件泵是否已启动

	dialog     *DialogInfo // 当前打开的 JS dialog（Page.javascriptDialogOpening）
	autoDialog string      // 执行期间的 dialog 自动应答策略（accept/dismiss/prompt 文本）
	network    *netBuffer
	console    *conBuffer

	traceMu      sync.Mutex
	traceRunning bool
	traceEvents  []map[string]any // Tracing.dataCollected 累积
	traceDone    chan struct{}    // tracingComplete 信号
	lastTrace    *TraceSummary
}

// DialogInfo 打开中的 JS dialog（官方 page.getDialog 的等价物）。
type DialogInfo struct {
	Type    string // alert/confirm/prompt/beforeunload
	Message string
}

// NewHub 创建状态中枢（snap 为 uimap 快照编排器，includeSnapshot 复用）。
func NewHub(client *cdp.Client, snap *uimap.Collector) *Hub {
	return &Hub{
		client:  client,
		snap:    snap,
		refs:    uimap.RefTable{},
		network: newNetBuffer(),
		console: newConBuffer(),
	}
}

// ReplaceRefs 实现 uimap.RefSink：每次快照全量替换 uid 身份表。
func (h *Hub) ReplaceRefs(refs uimap.RefTable) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refs = refs
}

// lookupRef 查 uid 对应的元素身份；未命中说明 uid 已过期。
func (h *Hub) lookupRef(ref string) (uimap.RefInfo, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	info, ok := h.refs[ref]
	return info, ok
}

// cdpGate 校验 CDP 可用性。
func (h *Hub) cdpGate(ctx context.Context) error {
	if !h.client.Ready(ctx) {
		return errors.New(cdpUnavailableMsg)
	}
	return nil
}

// CurrentWS 当前操作的 page target wsURL（select_page 可切换；缺省首个）。
func (h *Hub) CurrentWS(ctx context.Context) (string, error) {
	h.mu.Lock()
	cur := h.curWS
	h.mu.Unlock()
	if cur != "" {
		return cur, nil
	}
	return h.client.FirstPageTarget(ctx)
}

// selectPage 切换当前 page target。
func (h *Hub) selectPage(wsURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.curWS != wsURL {
		h.closeSessionLocked()
	}
	h.curWS = wsURL
}

// closeSessionLocked 关闭事件会话（调用方持锁）。
func (h *Hub) closeSessionLocked() {
	if h.sess != nil {
		h.sess.Close()
		h.sess, h.sessWS, h.pumpOn = nil, "", false
	}
}

// Session 返回当前 target 的持久事件会话（惰性建立，切换 target 后重建；
// 同时启动 network/console/dialog/trace 事件泵）。
func (h *Hub) Session(ctx context.Context) (*cdp.Session, error) {
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sess != nil && h.sessWS == wsURL {
		return h.sess, nil
	}
	h.closeSessionLocked()
	sess, err := h.client.Dial(wsURL)
	if err != nil {
		return nil, fmt.Errorf("建立 CDP 事件会话失败（WebView2 需支持多调试连接）: %w", err)
	}
	h.sess = sess
	h.sessWS = wsURL
	// 事件采集所需域：Network/Runtime/Log/Page
	for _, domain := range []string{"Network.enable", "Runtime.enable", "Log.enable", "Page.enable"} {
		if _, err := sess.Call(ctx, domain, map[string]any{}, 5*time.Second); err != nil {
			log.Printf("[mcpserver/browser] %s 失败: %v", domain, err)
		}
	}
	h.startPump(sess)
	return sess, nil
}

// startPump 启动事件分发循环（每会话一次）。
func (h *Hub) startPump(sess *cdp.Session) {
	if h.pumpOn {
		return
	}
	h.pumpOn = true
	events, cancel := sess.Subscribe()
	go func() {
		defer cancel()
		for ev := range events {
			h.dispatch(ev)
		}
	}()
}

// dispatch 把 CDP 事件分发给各采集器。
func (h *Hub) dispatch(ev cdp.Event) {
	switch {
	case strings.HasPrefix(ev.Method, "Network."):
		h.network.onEvent(ev.Method, ev.Params)
	case strings.HasPrefix(ev.Method, "Runtime.consoleAPICalled"),
		strings.HasPrefix(ev.Method, "Runtime.exceptionThrown"),
		ev.Method == "Log.entryAdded":
		h.console.onEvent(ev.Method, ev.Params)
	case ev.Method == "Page.javascriptDialogOpening":
		h.onDialogOpening(ev.Params)
	case ev.Method == "Tracing.dataCollected":
		h.onTraceData(ev.Params)
	case ev.Method == "Tracing.tracingComplete":
		h.onTraceComplete()
	}
}

// onDialogOpening 记录打开的 dialog；autoDialog 生效时立即自动应答。
func (h *Hub) onDialogOpening(params json.RawMessage) {
	var info struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if cdp.JSONUnmarshal(params, &info) != nil {
		return
	}
	h.mu.Lock()
	h.dialog = &DialogInfo{Type: info.Type, Message: info.Message}
	auto := h.autoDialog
	h.mu.Unlock()
	if auto == "" {
		return
	}
	// 自动应答：accept（非枚举值 = window.prompt 应答文本，官方 dialogAction 语义）
	accept := auto == "accept" || auto != "dismiss"
	promptText := ""
	if accept && auto != "accept" {
		promptText = auto
	}
	sess := h.currentSession()
	if sess == nil {
		return
	}
	p := map[string]any{"accept": accept}
	if promptText != "" {
		p["promptText"] = promptText
	}
	_, _ = sess.Call(context.Background(), "Page.handleJavaScriptDialog", p, 5*time.Second)
	h.ClearDialog()
}

// currentSession 当前事件会话（无则 nil；自动应答路径用，不校验 target）。
func (h *Hub) currentSession() *cdp.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sess
}

// sessionIf 返回指定 target 的会话（无则 nil）。
func (h *Hub) sessionIf(wsURL string) *cdp.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sess != nil && h.sessWS == wsURL {
		return h.sess
	}
	return nil
}

// GetDialog 当前打开的 dialog（官方 page.getDialog）。
func (h *Hub) GetDialog() *DialogInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dialog
}

// ClearDialog 清除 dialog 状态（官方 page.clearDialog）。
func (h *Hub) ClearDialog() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dialog = nil
}

// settleTime 动作后的事件沉淀等待（官方 waitForEventsAfterAction 等价简化）。
const settleTime = 300 * time.Millisecond

// afterAction 执行动作并等待事件沉淀（官方 waitForEventsAfterAction 的简化：
// 固定沉淀窗口，保证 action 触发的事件进入缓冲后响应才返回）。
func (h *Hub) afterAction(ctx context.Context, action func() error) error {
	if err := action(); err != nil {
		return err
	}
	select {
	case <-time.After(settleTime):
	case <-ctx.Done():
	}
	return nil
}

// handleDialogArgs handle_dialog 参数。
type handleDialogArgs struct {
	Action     string `json:"action" jsonschema_desc:"accept 或 dismiss"`
	PromptText string `json:"promptText,omitempty" jsonschema_desc:"可选：window.prompt 的应答文本"`
}

// registerDialogTool 注册 handle_dialog（官方 pages.ts 的 handleDialog：
// 操作当前打开的 JS dialog；无 dialog 时报 'No open dialog found'）。
func registerDialogTool(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "handle_dialog",
		"处理浏览器 JS dialog（alert/confirm/prompt/beforeunload）（这是复刻 chrome-devtools-mcp 的 "+
			"handle_dialog 工具，行为与官方一致：经 Page.handleJavaScriptDialog 应答；无打开的 dialog 时报错）。"+
			"dialog 打开时会阻断其他 UI 操作，先处理 dialog 再继续。",
		func(ctx context.Context, a handleDialogArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if err := h.respondDialog(ctx, a.Action, a.PromptText); err != nil {
				return nil, err
			}
			return map[string]any{"result": fmt.Sprintf("Successfully %sd the dialog", a.Action)}, nil
		})
}

// respondDialog 应答当前 dialog（官方 dialog.accept/dismiss）。
func (h *Hub) respondDialog(ctx context.Context, action, promptText string) error {
	if h.GetDialog() == nil {
		return errors.New("No open dialog found")
	}
	sess, err := h.Session(ctx)
	if err != nil {
		return err
	}
	p := map[string]any{"accept": action == "accept"}
	if promptText != "" {
		p["promptText"] = promptText
	}
	if _, err := sess.Call(ctx, "Page.handleJavaScriptDialog", p, 5*time.Second); err != nil {
		h.ClearDialog() // 官方：dialog 可能已被用户在界面处理，忽略该错误
		return nil
	}
	h.ClearDialog()
	return nil
}

// includeSnapshot 追加最新快照文本（官方 response.includeSnapshot）。
func (h *Hub) includeSnapshot(ctx context.Context, out map[string]any) {
	if h.snap == nil {
		return
	}
	res, err := h.snap.Collect(ctx, uimap.Args{})
	if err != nil {
		out["snapshotError"] = err.Error()
		return
	}
	if m, ok := res.(map[string]any); ok {
		out["snapshot"] = m["tree"]
	}
}

// Register 聚合注册全部浏览器类工具（server.go 装配入口）。
// h 由调用方创建并接线为 uimap.Collector 的 RefSink（见 server.go）。
func Register(srv *mcp.Server, sink reg.Sink, h *Hub) {
	registerPointerTools(srv, sink, h)
	registerDialogTool(srv, sink, h)
	registerFormTools(srv, sink, h)
	registerNavigateTools(srv, sink, h)
	registerNetworkTools(srv, sink, h)
	registerConsoleTools(srv, sink, h)
	registerEmulateTools(srv, sink, h) // emulate + resize_page + 键盘/滚动（env.go）
	registerEnvInputTools(srv, sink, h)
	registerPerfTools(srv, sink, h)
}

// --- evaluate_script 辅助（console.go 的脚本执行通道共用） ---

// remoteObjectID 从 Runtime.evaluate 返回中取 objectId。
func remoteObjectID(res json.RawMessage) (string, error) {
	var out struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := cdp.JSONUnmarshal(res, &out); err != nil {
		return "", err
	}
	if out.ExceptionDetails != nil {
		return "", fmt.Errorf("JS 执行异常: %s", out.ExceptionDetails.Text)
	}
	if out.Result.ObjectID == "" {
		return "", errors.New("表达式未返回可引用对象")
	}
	return out.Result.ObjectID, nil
}
