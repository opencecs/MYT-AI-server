package browser

// navigate.go — navigation 类工具（复刻 chrome-devtools-mcp src/tools/pages.ts）：
// list_pages / select_page / new_page / close_page / navigate_page / wait_for
// + EMULATION 类的 resize_page（官方同在 pages.ts）。
// WebView2 适配：list/select 按 CDP /json 枚举/切换（1:1）；new/close 经
// browser endpoint 的 Target.createTarget/closeTarget（多 target 支持度运行时
// 验证，失败明确报错）；resize 用 Emulation.setDeviceMetricsOverride（无窗口域）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

// closePageError 官方 CLOSE_PAGE_ERROR 同款文案。
const closePageError = "The last open page cannot be closed. It is fine to keep it open."

// navTimeoutDefault 官方 puppeteer 默认导航超时。
const navTimeoutDefault = 30 * time.Second

type navigatePageArgs struct {
	Type               string `json:"type,omitempty" jsonschema_desc:"导航方式：url（默认）/back/forward/reload"`
	URL                string `json:"url,omitempty" jsonschema_desc:"目标 URL（仅 type=url）"`
	IgnoreCache        bool   `json:"ignoreCache,omitempty" jsonschema_desc:"reload 时是否忽略缓存"`
	HandleBeforeUnload string `json:"handleBeforeUnload,omitempty" jsonschema_desc:"导航触发的 beforeunload dialog 自动处理：accept（默认）/dismiss"`
	TimeoutMS          int    `json:"timeout,omitempty" jsonschema_desc:"最长等待毫秒数，0 用默认（30000）"`
}

type newPageArgs struct {
	URL        string `json:"url" jsonschema_desc:"新页面要加载的 URL"`
	Background bool   `json:"background,omitempty" jsonschema_desc:"true 时不切换为当前操作目标，默认 false（前台）"`
	TimeoutMS  int    `json:"timeout,omitempty" jsonschema_desc:"最长等待毫秒数，0 用默认（30000）"`
}

type selectPageArgs struct {
	PageIdx      int  `json:"pageId" jsonschema_desc:"要选为操作目标的页面序号（来自 list_pages）"`
	BringToFront bool `json:"bringToFront,omitempty" jsonschema_desc:"true 时聚焦该页面并置顶"`
}

type closePageArgs struct {
	PageIdx int `json:"pageId" jsonschema_desc:"要关闭的页面序号（来自 list_pages）"`
}

type waitForArgs struct {
	Texts     []string `json:"text" jsonschema_desc:"非空文本列表，任一出现在页面即返回"`
	TimeoutMS int      `json:"timeout,omitempty" jsonschema_desc:"最长等待毫秒数，0 用默认（30000）"`
}

// registerNavigateTools 注册 navigation 类工具。
func registerNavigateTools(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "list_pages",
		"列出打开的页面（这是复刻 chrome-devtools-mcp 的 list_pages 工具，行为与官方一致："+
			"枚举 page target，标注当前选中项）。无破坏性。",
		func(ctx context.Context, _ struct{}) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			return h.listPages(ctx)
		}, reg.ReadOnly())

	reg.Reg(srv, sink, "select_page",
		"选择后续工具操作的目标页面（这是复刻 chrome-devtools-mcp 的 select_page 工具，行为与官方一致）。"+
			"pageId 来自 list_pages 的序号。",
		func(ctx context.Context, a selectPageArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			return h.selectPageByID(ctx, a.PageIdx, a.BringToFront)
		}, reg.ReadOnly())

	reg.Reg(srv, sink, "new_page",
		"新开页面并加载 URL（这是复刻 chrome-devtools-mcp 的 new_page 工具，经 CDP "+
			"Target.createTarget 实现；WebView2 单窗口宿主多 target 支持度以运行时为准，失败时明确报错）。",
		func(ctx context.Context, a newPageArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if a.URL == "" {
				return nil, errors.New("url 不能为空")
			}
			return h.newPage(ctx, a.URL, a.Background, timeoutOrDefault(a.TimeoutMS, navTimeoutDefault))
		})

	reg.Reg(srv, sink, "close_page",
		"按序号关闭页面（这是复刻 chrome-devtools-mcp 的 close_page 工具，经 CDP "+
			"Target.closeTarget 实现）。最后一个页面不可关闭（与官方一致）。",
		func(ctx context.Context, a closePageArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			out, err := h.closePage(ctx, a.PageIdx)
			if err != nil {
				return nil, err
			}
			if out == nil {
				return map[string]any{"result": closePageError}, nil
			}
			return out, nil
		})

	reg.Reg(srv, sink, "navigate_page",
		"导航当前页面：到 URL / 后退 / 前进 / 刷新（这是复刻 chrome-devtools-mcp 的 "+
			"navigate_page 工具，行为与官方一致）。type 缺省为 url（此时 url 必填）。",
		func(ctx context.Context, a navigatePageArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if a.Type == "" && a.URL == "" {
				return nil, errors.New("Either URL or a type is required.")
			}
			if a.Type == "" {
				a.Type = "url"
			}
			return h.navigatePage(ctx, a)
		})

	reg.Reg(srv, sink, "wait_for",
		"等待指定文本出现在页面上（这是复刻 chrome-devtools-mcp 的 wait_for 工具，行为与官方一致："+
			"轮询页面文本，任一命中即返回并附快照）。text 为非空文本列表。",
		func(ctx context.Context, a waitForArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if len(a.Texts) == 0 {
				return nil, errors.New("text 不能为空")
			}
			return h.waitFor(ctx, a.Texts, timeoutOrDefault(a.TimeoutMS, navTimeoutDefault))
		}, reg.ReadOnly())

}

// timeoutOrDefault 毫秒超时兜底。
func timeoutOrDefault(ms int, def time.Duration) time.Duration {
	if ms <= 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}

// pageEntry list_pages 输出行。
type pageEntry struct {
	PageIdx  int    `json:"pageId"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Selected bool   `json:"selected"`
}

// listPages 枚举 page target（官方 list_pages）。
func (h *Hub) listPages(ctx context.Context) (any, error) {
	targets, err := h.client.PageTargets(ctx)
	if err != nil {
		return nil, err
	}
	cur, _ := h.CurrentWS(ctx)
	pages := make([]pageEntry, 0, len(targets))
	for i, t := range targets {
		pages = append(pages, pageEntry{PageIdx: i, Title: t.Title, URL: t.URL, Selected: t.WebSocketDebuggerURL == cur})
	}
	return map[string]any{"pages": pages}, nil
}

// selectPageByID 切换操作目标（官方 select_page）。
func (h *Hub) selectPageByID(ctx context.Context, idx int, bringToFront bool) (any, error) {
	targets, err := h.client.PageTargets(ctx)
	if err != nil {
		return nil, err
	}
	if idx < 0 || idx >= len(targets) {
		return nil, fmt.Errorf("pageId %d 不存在（共 %d 个页面，先调 list_pages）", idx, len(targets))
	}
	t := targets[idx]
	h.selectPage(t.WebSocketDebuggerURL)
	if bringToFront {
		wsURL, err := h.CurrentWS(ctx)
		if err == nil {
			_, _ = h.client.Call(ctx, wsURL, "Page.bringToFront", map[string]any{}, 5*time.Second)
		}
	}
	return h.listPages(ctx)
}

// newPage 经 browser endpoint 的 Target.createTarget 新开页面并导航（官方 new_page）。
func (h *Hub) newPage(ctx context.Context, url string, background bool, timeout time.Duration) (any, error) {
	bws, err := h.client.BrowserWS(ctx)
	if err != nil {
		return nil, fmt.Errorf("new_page 需要 browser endpoint（WebView2 可能不支持）: %w", err)
	}
	res, err := h.client.Call(ctx, bws, "Target.createTarget", map[string]any{"url": "about:blank"}, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Target.createTarget 失败（WebView2 可能不支持多 target，new_page 不可用）: %w", err)
	}
	var out struct {
		TargetID string `json:"targetId"`
	}
	if err := cdp.JSONUnmarshal(res, &out); err != nil {
		return nil, err
	}
	// 找到新 target 的 ws 并导航
	deadline := time.Now().Add(5 * time.Second)
	var wsURL string
	for {
		targets, terr := h.client.PageTargets(ctx)
		if terr == nil {
			for _, t := range targets {
				if t.ID == out.TargetID {
					wsURL = t.WebSocketDebuggerURL
				}
			}
		}
		if wsURL != "" || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if wsURL == "" {
		return nil, fmt.Errorf("新 target %s 未出现在 page 列表", out.TargetID)
	}
	if _, err := h.client.Call(ctx, wsURL, "Page.navigate", map[string]any{"url": url}, timeout); err != nil {
		return nil, err
	}
	if !background {
		h.selectPage(wsURL)
	}
	pages, _ := h.listPages(ctx)
	return pages, nil
}

// closePage 关闭指定页面（官方 close_page；最后一页不可关）。
func (h *Hub) closePage(ctx context.Context, idx int) (any, error) {
	targets, err := h.client.PageTargets(ctx)
	if err != nil {
		return nil, err
	}
	if len(targets) <= 1 {
		return nil, nil // 官方文案由调用方包装
	}
	if idx < 0 || idx >= len(targets) {
		return nil, fmt.Errorf("pageId %d 不存在（共 %d 个页面，先调 list_pages）", idx, len(targets))
	}
	t := targets[idx]
	bws, err := h.client.BrowserWS(ctx)
	if err != nil {
		return nil, fmt.Errorf("close_page 需要 browser endpoint: %w", err)
	}
	if _, err := h.client.Call(ctx, bws, "Target.closeTarget", map[string]any{"targetId": t.ID}, 10*time.Second); err != nil {
		return nil, err
	}
	// 关闭的是当前 target 时清空选择
	h.mu.Lock()
	if h.curWS == t.WebSocketDebuggerURL {
		h.closeSessionLocked()
		h.curWS = ""
	}
	h.mu.Unlock()
	pages, _ := h.listPages(ctx)
	return pages, nil
}

// navigatePage 导航当前页（官方 navigate_page：url/back/forward/reload +
// beforeunload 自动处理 + initScript 生命周期）。
func (h *Hub) navigatePage(ctx context.Context, a navigatePageArgs) (any, error) {
	if a.Type == "" && a.URL == "" {
		return nil, errors.New("Either URL or a type is required.")
	}
	if a.Type == "" {
		a.Type = "url"
	}
	timeout := timeoutOrDefault(a.TimeoutMS, navTimeoutDefault)
	wsURL, err := h.CurrentWS(ctx)
	if err != nil {
		return nil, err
	}
	// beforeunload 自动应答策略（官方 handleDialog: {beforeunload: action}）
	action := a.HandleBeforeUnload
	if action == "" {
		action = "accept"
	}
	h.mu.Lock()
	h.autoDialog = action
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.autoDialog = ""
		h.mu.Unlock()
	}()

	var navErr error
	switch a.Type {
	case "url":
		if a.URL == "" {
			return nil, errors.New("A URL is required for navigation of type=url.")
		}
		_, navErr = h.client.Call(ctx, wsURL, "Page.navigate", map[string]any{"url": a.URL}, timeout)
	case "back":
		_, navErr = h.client.Eval(ctx, "history.back()", timeout)
	case "forward":
		_, navErr = h.client.Eval(ctx, "history.forward()", timeout)
	case "reload":
		params := map[string]any{}
		if a.IgnoreCache {
			params["ignoreCache"] = true
		}
		_, navErr = h.client.Call(ctx, wsURL, "Page.reload", params, timeout)
	default:
		return nil, fmt.Errorf("不支持的导航类型 %q（url/back/forward/reload）", a.Type)
	}
	_ = h.afterAction(ctx, func() error { return nil })

	out := map[string]any{}
	switch {
	case navErr == nil:
		switch a.Type {
		case "url":
			out["result"] = fmt.Sprintf("Successfully navigated to %s.", a.URL)
		case "back":
			out["result"] = "Successfully navigated back."
		case "forward":
			out["result"] = "Successfully navigated forward."
		case "reload":
			out["result"] = "Successfully reloaded the page."
		}
	default:
		// 官方：导航失败不报错，写入响应行（可继续操作）
		out["result"] = fmt.Sprintf("Unable to navigate in the selected page: %v.", navErr)
		out["error"] = navErr.Error()
	}
	if h.GetDialog() != nil && (a.Type == "url" || a.Type == "reload") {
		out["dialogHandled"] = fmt.Sprintf("%s a beforeunload dialog.", map[bool]string{true: "Accepted", false: "Dismissed"}[action == "accept"])
		h.ClearDialog()
	}
	return out, nil
}

// waitFor 轮询页面文本（官方 waitForTextOnPage：text 数组任一命中即返回）。
func (h *Hub) waitFor(ctx context.Context, texts []string, timeout time.Duration) (any, error) {
	deadline := time.Now().Add(timeout)
	literal, _ := jsonMarshalStrings(texts)
	expr := fmt.Sprintf(`(() => { const t = document.body ? document.body.innerText : ''; return [%s].some(s => t.includes(s)); })()`, literal)
	for {
		v, err := h.client.Eval(ctx, expr, 5*time.Second)
		if err == nil {
			if hit, ok := v.(bool); ok && hit {
				out := map[string]any{"result": fmt.Sprintf("Element matching one of %s found.", mustJSON(texts))}
				h.includeSnapshot(ctx, out)
				return out, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("等待 %v 内未在页面找到任何文本 %v", timeout, texts)
		}
		select {
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// jsonMarshalStrings 文本列表 → JS 数组字面量（waitFor 注入用）。
func jsonMarshalStrings(ss []string) (string, error) {
	b, err := json.Marshal(ss)
	return string(b), err
}

// mustJSON 忽略错误的 JSON 序列化（响应文案用）。
func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
