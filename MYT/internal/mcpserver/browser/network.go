package browser

// network.go — network 类工具（复刻 chrome-devtools-mcp src/tools/network.ts）：
// list_network_requests / get_network_request。
// 采集层：事件会话（hub.Session）订阅 Network.* 事件，内存环形缓冲
// （上限 500 条，官方按最近 3 次导航保留，此处统一为环形缓冲并在响应注明）。
// 响应体按需经 Network.getResponseBody 拉取（不落盘，官方 filePath 保存为可选，
// 此处返回内联）。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
)

// netBufferCap 网络请求环形缓冲上限。
const netBufferCap = 500

// netRequest 单条网络请求记录（官方 networkRequests 的等价字段）。
type netRequest struct {
	ReqID        int64             `json:"reqid"` // MCP 侧自增序号（官方 reqid）
	CdpID        string            `json:"-"`     // CDP requestId（拉取响应体用）
	URL          string            `json:"url"`
	Method       string            `json:"method"`
	ResourceType string            `json:"resourceType"`
	Status       int               `json:"status,omitempty"`
	ReqHeaders   map[string]string `json:"requestHeaders,omitempty"`
	RespHeaders  map[string]string `json:"responseHeaders,omitempty"`
	Failed       string            `json:"failed,omitempty"` // loadingFailed 的 errorText
	TimeStamp    float64           `json:"timestamp"`
}

// netBuffer 网络请求环形缓冲。
type netBuffer struct {
	mu    sync.Mutex
	seq   int64
	items []*netRequest // 按 ReqID 升序（环形裁剪）
	byCdp map[string]*netRequest
}

func newNetBuffer() *netBuffer {
	return &netBuffer{byCdp: map[string]*netRequest{}}
}

// add 新建请求记录（Network.requestWillBeSent）。
func (b *netBuffer) add(cdpID, url, method, resourceType string, ts float64) *netRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	r := &netRequest{ReqID: b.seq, CdpID: cdpID, URL: url, Method: method, ResourceType: resourceType, TimeStamp: ts}
	b.items = append(b.items, r)
	b.byCdp[cdpID] = r
	if len(b.items) > netBufferCap { // 环形裁剪：丢最旧
		old := b.items[0]
		b.items = b.items[1:]
		delete(b.byCdp, old.CdpID)
	}
	return r
}

// get 按 CDP requestId 取记录。
func (b *netBuffer) get(cdpID string) *netRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byCdp[cdpID]
}

// list 全量快照（按 reqid 升序）。
func (b *netBuffer) list() []*netRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*netRequest, len(b.items))
	copy(out, b.items)
	return out
}

// update 更新已有记录（responseReceived/loadingFinished/loadingFailed）。
func (b *netBuffer) update(cdpID string, fn func(*netRequest)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r := b.byCdp[cdpID]; r != nil {
		fn(r)
	}
}

// onEvent 处理 Network.* 事件（hub 事件泵调用）。
func (b *netBuffer) onEvent(method string, params json.RawMessage) {
	var p struct {
		RequestID string  `json:"requestId"`
		Type      string  `json:"type"`
		ErrorText string  `json:"errorText"`
		Timestamp float64 `json:"timestamp"`
		Request   *struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
		} `json:"request"`
		Response *struct {
			Status   int               `json:"status"`
			Headers  map[string]string `json:"headers"`
			MimeType string            `json:"mimeType"`
		} `json:"response"`
	}
	if cdp.JSONUnmarshal(params, &p) != nil {
		return
	}
	switch method {
	case "Network.requestWillBeSent":
		if p.Request != nil {
			b.add(p.RequestID, p.Request.URL, p.Request.Method, p.Type, p.Timestamp)
			b.update(p.RequestID, func(r *netRequest) { r.ReqHeaders = p.Request.Headers })
		}
	case "Network.responseReceived":
		if p.Response != nil {
			rt := p.Type
			b.update(p.RequestID, func(r *netRequest) {
				r.Status = p.Response.Status
				r.RespHeaders = p.Response.Headers
				if rt != "" {
					r.ResourceType = rt
				}
			})
		}
	case "Network.loadingFailed":
		b.update(p.RequestID, func(r *netRequest) { r.Failed = p.ErrorText })
	}
}

// filterableResourceTypes 官方可过滤资源类型（与官方枚举一致）。
var filterableResourceTypes = map[string]bool{
	"document": true, "stylesheet": true, "image": true, "media": true,
	"font": true, "script": true, "texttrack": true, "xhr": true, "fetch": true,
	"prefetch": true, "eventsource": true, "websocket": true, "manifest": true,
	"signedexchange": true, "ping": true, "cspviolationreport": true,
	"preflight": true, "fedcm": true, "other": true,
}

type listNetworkRequestsArgs struct {
	PageSize                 int      `json:"pageSize,omitempty" jsonschema_desc:"每页最多返回条数；缺省返回全部"`
	PageIdx                  int      `json:"pageIdx,omitempty" jsonschema_desc:"页号（0 起）；缺省第一页"`
	ResourceTypes            []string `json:"resourceTypes,omitempty" jsonschema_desc:"按资源类型过滤（document/stylesheet/image/xhr/fetch/script 等），空返回全部"`
	IncludePreservedRequests bool     `json:"includePreservedRequests,omitempty" jsonschema_desc:"官方语义为返回最近 3 次导航的请求；本实现为统一环形缓冲（500 条），此参数接受但无额外效果"`
}

type getNetworkRequestArgs struct {
	ReqID int `json:"reqid" jsonschema_desc:"请求序号（来自 list_network_requests）"`
}

// registerNetworkTools 注册 network 类工具。
func registerNetworkTools(srv *mcp.Server, sink reg.Sink, h *Hub) {
	reg.Reg(srv, sink, "list_network_requests",
		"列出最近的网络请求（这是复刻 chrome-devtools-mcp 的 list_network_requests 工具，行为与官方一致："+
			"事件会话订阅 Network 域，环形缓冲最近 500 条；支持分页与资源类型过滤）。无破坏性。",
		func(ctx context.Context, a listNetworkRequestsArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if _, err := h.Session(ctx); err != nil {
				return nil, err
			}
			return h.listNetworkRequests(a), nil
		}, reg.ReadOnly())

	reg.Reg(srv, sink, "get_network_request",
		"按 reqid 获取网络请求详情（这是复刻 chrome-devtools-mcp 的 get_network_request 工具，行为与官方一致："+
			"含请求头（含 Cookie）与响应头（含 Set-Cookie）与响应体）。",
		func(ctx context.Context, a getNetworkRequestArgs) (any, error) {
			if err := h.cdpGate(ctx); err != nil {
				return nil, err
			}
			if _, err := h.Session(ctx); err != nil {
				return nil, err
			}
			return h.getNetworkRequest(ctx, a.ReqID)
		})
}

// listNetworkRequests 分页 + 类型过滤（官方 list_network_requests 分页语义）。
func (h *Hub) listNetworkRequests(a listNetworkRequestsArgs) any {
	all := h.network.list()
	filtered := make([]*netRequest, 0, len(all))
	want := map[string]bool{}
	for _, t := range a.ResourceTypes {
		want[t] = true
	}
	for _, r := range all {
		if len(want) > 0 && !want[r.ResourceType] {
			continue
		}
		filtered = append(filtered, r)
	}
	total := len(filtered)
	start, end := 0, total
	if a.PageSize > 0 {
		start = a.PageIdx * a.PageSize
		if start > total {
			start = total
		}
		end = start + a.PageSize
		if end > total {
			end = total
		}
	}
	page := filtered[start:end]
	// 输出脱敏：去掉内部 CdpID
	out := make([]map[string]any, 0, len(page))
	for _, r := range page {
		b, _ := json.Marshal(r)
		m := map[string]any{}
		_ = json.Unmarshal(b, &m)
		out = append(out, m)
	}
	return map[string]any{
		"requests": out,
		"total":    total,
		"note":     fmt.Sprintf("环形缓冲上限 %d 条；includePreservedRequests 在本实现中无额外效果", netBufferCap),
	}
}

// getNetworkRequest 单条详情 + 请求/响应体（官方 get_network_request）。
func (h *Hub) getNetworkRequest(ctx context.Context, reqID int) (any, error) {
	var target *netRequest
	for _, r := range h.network.list() {
		if r.ReqID == int64(reqID) {
			target = r
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("reqid %d 不存在（先调 list_network_requests；缓冲上限 %d 条）", reqID, netBufferCap)
	}
	sess, err := h.Session(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"reqid":           target.ReqID,
		"url":             target.URL,
		"method":          target.Method,
		"resourceType":    target.ResourceType,
		"status":          target.Status,
		"requestHeaders":  target.ReqHeaders,
		"responseHeaders": target.RespHeaders,
	}
	if target.Failed != "" {
		out["failed"] = target.Failed
	}
	// 请求体（POST 数据）
	if res, err := sess.Call(ctx, "Network.getRequestPostData", map[string]any{"requestId": target.CdpID}, 10*time.Second); err == nil {
		var body struct {
			PostData string `json:"postData"`
		}
		if cdp.JSONUnmarshal(res, &body) == nil && body.PostData != "" {
			out["requestBody"] = body.PostData
		}
	}
	// 响应体（base64 → 尽量解码为文本）
	if res, err := sess.Call(ctx, "Network.getResponseBody", map[string]any{"requestId": target.CdpID}, 10*time.Second); err == nil {
		var body struct {
			Body          string `json:"body"`
			Base64Encoded bool   `json:"base64Encoded"`
		}
		if cdp.JSONUnmarshal(res, &body) == nil && body.Body != "" {
			if body.Base64Encoded {
				if raw, derr := base64.StdEncoding.DecodeString(body.Body); derr == nil {
					out["responseBody"] = printableBody(raw)
				} else {
					out["responseBodyBase64"] = body.Body
				}
			} else {
				out["responseBody"] = printableBody([]byte(body.Body))
			}
		}
	}
	return out, nil
}

// printableBody 响应体截断为可读文本（超长截断，防响应爆量）。
func printableBody(raw []byte) string {
	const limit = 64 * 1024
	s := string(raw)
	if len(s) > limit {
		return s[:limit] + fmt.Sprintf("…（截断，原始 %d 字节）", len(raw))
	}
	if strings.Contains(s, "\x00") {
		return fmt.Sprintf("（二进制内容，%d 字节）", len(raw))
	}
	return s
}
