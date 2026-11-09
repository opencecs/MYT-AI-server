package browser

// state_test.go — network / console / navigation / emulation 类单测：
// 环形缓冲、分页与过滤、CDP 命令序列、navigate_page 与 emulate 参数映射。

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/mcpserver/cdp"
)

// feedNetEvent 注入一条 Network 事件。
func feedNetEvent(h *Hub, method string, payload map[string]any) {
	b, _ := json.Marshal(payload)
	h.dispatch(cdp.Event{Method: method, Params: b})
}

// TestNetworkBufferEvents 事件 → 缓冲：requestWillBeSent/responseReceived/
// loadingFailed 的字段映射。
func TestNetworkBufferEvents(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	feedNetEvent(h, "Network.requestWillBeSent", map[string]any{
		"requestId": "c1", "timestamp": 1.0,
		"request": map[string]any{"url": "http://x/api", "method": "POST",
			"headers": map[string]any{"Cookie": "a=b"}},
	})
	feedNetEvent(h, "Network.responseReceived", map[string]any{
		"requestId": "c1", "type": "xhr",
		"response": map[string]any{"status": 200, "headers": map[string]any{"Set-Cookie": "k=v"}},
	})
	feedNetEvent(h, "Network.requestWillBeSent", map[string]any{
		"requestId": "c2", "timestamp": 1.5,
		"request": map[string]any{"url": "http://x/bad", "method": "GET"},
	})
	feedNetEvent(h, "Network.loadingFailed", map[string]any{"requestId": "c2", "errorText": "net::ERR_FAILED", "timestamp": 2.0})

	items := h.network.list()
	if len(items) != 2 {
		t.Fatalf("缓冲应 2 条，实际 %d", len(items))
	}
	r := items[0]
	if r.ReqID != 1 || r.Method != "POST" || r.ResourceType != "xhr" || r.Status != 200 {
		t.Fatalf("字段映射异常: %+v", r)
	}
	if r.ReqHeaders["Cookie"] != "a=b" || r.RespHeaders["Set-Cookie"] != "k=v" {
		t.Fatalf("请求/响应头映射异常: %+v", r)
	}
	if items[1].Failed != "net::ERR_FAILED" {
		t.Fatalf("loadingFailed 映射异常: %+v", items[1])
	}
}

// TestNetworkBufferRing 环形缓冲：超过 500 条丢最旧，reqid 连续自增。
func TestNetworkBufferRing(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	for i := 0; i < netBufferCap+5; i++ {
		feedNetEvent(h, "Network.requestWillBeSent", map[string]any{
			"requestId": string(rune('a'+i%26)) + time.Now().Format("150405.000000000") + string(rune(i)),
			"timestamp": float64(i),
			"request":   map[string]any{"url": "http://x/", "method": "GET"},
		})
	}
	items := h.network.list()
	if len(items) != netBufferCap {
		t.Fatalf("环形缓冲应保持 %d 条，实际 %d", netBufferCap, len(items))
	}
	if items[0].ReqID != 6 { // 505 条后最旧为第 6 条
		t.Fatalf("最旧条目 reqid = %d, want 6（丢了 5 条最旧）", items[0].ReqID)
	}
	if items[len(items)-1].ReqID != int64(netBufferCap+5) {
		t.Fatalf("最新 reqid = %d, want %d", items[len(items)-1].ReqID, netBufferCap+5)
	}
}

// TestListNetworkRequestsPaging 分页与资源类型过滤。
func TestListNetworkRequestsPaging(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	feedNetEvent(h, "Network.requestWillBeSent", map[string]any{
		"requestId": "d1", "request": map[string]any{"url": "http://x/1", "method": "GET"}})
	feedNetEvent(h, "Network.responseReceived", map[string]any{
		"requestId": "d1", "type": "xhr", "response": map[string]any{"status": 200}})
	feedNetEvent(h, "Network.requestWillBeSent", map[string]any{
		"requestId": "d2", "request": map[string]any{"url": "http://x/2", "method": "GET"}})
	feedNetEvent(h, "Network.responseReceived", map[string]any{
		"requestId": "d2", "type": "document", "response": map[string]any{"status": 200}})

	out := h.listNetworkRequests(listNetworkRequestsArgs{PageSize: 1}).(map[string]any)
	if out["total"] != 2 {
		t.Fatalf("total = %v, want 2", out["total"])
	}
	if reqs, _ := out["requests"].([]map[string]any); len(reqs) != 1 {
		t.Fatalf("pageSize=1 应只返回 1 条")
	}
	out = h.listNetworkRequests(listNetworkRequestsArgs{ResourceTypes: []string{"xhr"}}).(map[string]any)
	if out["total"] != 1 {
		t.Fatalf("xhr 过滤后 total = %v, want 1", out["total"])
	}
}

// TestGetNetworkRequestBody get_network_request：拉取响应体
// （Network.getResponseBody → responseBody）。
func TestGetNetworkRequestBody(t *testing.T) {
	h, _ := newTestHub(t, func(rec *recorder, method string, params json.RawMessage) (any, error) {
		if method == "Network.getResponseBody" {
			return map[string]any{"body": "aGVsbG8=", "base64Encoded": true}, nil
		}
		return stdHandler(rec, method, params)
	})
	feedNetEvent(h, "Network.requestWillBeSent", map[string]any{
		"requestId": "e1", "request": map[string]any{"url": "http://x/data", "method": "GET"}})
	out, err := h.getNetworkRequest(ctxBg(), 1)
	if err != nil {
		t.Fatalf("getNetworkRequest 失败: %v", err)
	}
	m := out.(map[string]any)
	if m["url"] != "http://x/data" {
		t.Fatalf("url = %v", m["url"])
	}
	if m["responseBody"] != "hello" {
		t.Fatalf("base64 响应体应解码为 hello，got %v", m["responseBody"])
	}
}

// TestConsoleBufferEvents console 采集：consoleAPICalled/exceptionThrown/
// Log.entryAdded 的字段映射与类型过滤。
func TestConsoleBufferEvents(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	feedNetEvent(h, "Runtime.consoleAPICalled", map[string]any{
		"type": "log", "timestamp": 1.0,
		"args": []any{map[string]any{"type": "string", "value": "hello"}},
	})
	feedNetEvent(h, "Runtime.exceptionThrown", map[string]any{
		"timestamp": 2.0,
		"exceptionDetails": map[string]any{
			"text": "Uncaught", "exception": map[string]any{"description": "TypeError: x is not a function"}}})
	feedNetEvent(h, "Log.entryAdded", map[string]any{
		"entry": map[string]any{"level": "warning", "text": "bad cert", "source": "network", "timestamp": 3.0}})

	items := h.console.list()
	if len(items) != 3 {
		t.Fatalf("缓冲应 3 条，实际 %d", len(items))
	}
	if items[0].Type != "log" || items[0].Text != "hello" {
		t.Fatalf("consoleAPICalled 映射异常: %+v", items[0])
	}
	if items[1].Type != "error" || !strings.Contains(items[1].Text, "TypeError") {
		t.Fatalf("exceptionThrown 映射异常: %+v", items[1])
	}
	if items[2].Type != "warning" || !strings.Contains(items[2].Text, "[network]") {
		t.Fatalf("Log.entryAdded 映射异常: %+v", items[2])
	}
	// msgid 自增 + get
	if m := h.console.get(2); m == nil || !strings.Contains(m.Text, "TypeError") {
		t.Fatal("get(2) 应返回异常消息")
	}
}

// TestNavigatePageReloadSequence navigate_page(reload)：Page.reload + ignoreCache
// 参数映射；url 类型 → Page.navigate。
func TestNavigatePageReloadSequence(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	out, err := h.navigatePage(ctxBg(), navigatePageArgs{Type: "reload", IgnoreCache: true})
	if err != nil {
		t.Fatalf("navigatePage 失败: %v", err)
	}
	var call *recordedCall
	for i := range rec.calls {
		if rec.calls[i].Method == "Page.reload" {
			call = &rec.calls[i]
		}
	}
	if call == nil || call.Params["ignoreCache"] != true {
		t.Fatalf("Page.reload ignoreCache 参数异常: %v", call)
	}
	if !strings.Contains(out.(map[string]any)["result"].(string), "Successfully reloaded") {
		t.Fatalf("reload 响应文案异常: %v", out)
	}
	if _, err := h.navigatePage(ctxBg(), navigatePageArgs{Type: "url", URL: "http://x/y"}); err != nil {
		t.Fatalf("navigate url 失败: %v", err)
	}
	found := false
	for _, c := range rec.calls {
		if c.Method == "Page.navigate" && c.Params["url"] == "http://x/y" {
			found = true
		}
	}
	if !found {
		t.Fatal("type=url 应调用 Page.navigate")
	}
	// 无 url 无 type → 官方报错
	if _, err := h.navigatePage(ctxBg(), navigatePageArgs{}); err == nil ||
		!strings.Contains(err.Error(), "Either URL or a type is required") {
		t.Fatalf("期望官方空参报错, got %v", err)
	}
}

// TestListAndSelectPages list_pages/select_page：target 枚举与当前选中切换。
func TestListAndSelectPages(t *testing.T) {
	h, _ := newTestHub(t, stdHandler)
	out, err := h.listPages(ctxBg())
	if err != nil {
		t.Fatalf("listPages 失败: %v", err)
	}
	pages, _ := out.(map[string]any)["pages"].([]pageEntry)
	if len(pages) != 1 || pages[0].PageIdx != 0 {
		t.Fatalf("mock 只应有 1 个页面: %+v", pages)
	}
	// select 越界报错
	if _, err := h.selectPageByID(ctxBg(), 5, false); err == nil {
		t.Fatal("越界 pageId 应报错")
	}
}

// TestWaitForFound wait_for：页面文本命中即返回并附快照（此处无 snapshot
// collector，只验证命中逻辑）。
func TestWaitForFound(t *testing.T) {
	h, _ := newTestHub(t, stdHandler) // stdHandler 对 innerText 检查返回 true
	out, err := h.waitFor(ctxBg(), []string{"创建云机"}, 2*time.Second)
	if err != nil {
		t.Fatalf("waitFor 失败: %v", err)
	}
	if !strings.Contains(out.(map[string]any)["result"].(string), "found") {
		t.Fatalf("命中响应文案异常: %v", out)
	}
}

// TestEmulateParameterMapping emulate：各子能力的 CDP 命令与参数映射。
func TestEmulateParameterMapping(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	sess, err := h.Session(ctxBg())
	if err != nil {
		t.Fatalf("Session 失败: %v", err)
	}
	if err := h.emulate(ctxBg(), sess, emulateArgs{
		NetworkConditions: "Slow 3G",
		CPUThrottlingRate: 4,
		Geolocation:       "22.5,114.0",
		ColorScheme:       "dark",
		Viewport:          "390x844x3,mobile,touch",
		ExtraHTTPHeaders:  `{"X-Test":"1"}`,
	}); err != nil {
		t.Fatalf("emulate 失败: %v", err)
	}
	check := func(method string, key string, want any) {
		t.Helper()
		for _, c := range rec.calls {
			if c.Method == method {
				if reflect.DeepEqual(c.Params[key], want) {
					return
				}
				t.Fatalf("%s 参数 %s = %v, want %v", method, key, c.Params[key], want)
			}
		}
		t.Fatalf("缺少 %s 调用", method)
	}
	check("Network.emulateNetworkConditions", "latency", 400.0)
	check("Emulation.setCPUThrottlingRate", "rate", 4.0)
	check("Emulation.setGeolocationOverride", "latitude", 22.5)
	check("Emulation.setEmulatedMedia", "features", []any{map[string]any{"name": "prefers-color-scheme", "value": "dark"}})
	check("Emulation.setDeviceMetricsOverride", "mobile", true)
	check("Network.setExtraHTTPHeaders", "headers", map[string]any{"X-Test": "1"})
	// 非法参数
	if err := h.emulate(ctxBg(), sess, emulateArgs{NetworkConditions: "5G"}); err == nil {
		t.Fatal("不支持的 networkConditions 应报错")
	}
	if err := h.emulate(ctxBg(), sess, emulateArgs{Geolocation: "999,0"}); err == nil {
		t.Fatal("非法纬度应报错")
	}
}

// TestResizePageSequence resize_page → Emulation.setDeviceMetricsOverride。
func TestResizePageSequence(t *testing.T) {
	h, rec := newTestHub(t, stdHandler)
	if err := h.resizePage(ctxBg(), 800, 600); err != nil {
		t.Fatalf("resizePage 失败: %v", err)
	}
	var call *recordedCall
	for i := range rec.calls {
		if rec.calls[i].Method == "Emulation.setDeviceMetricsOverride" {
			call = &rec.calls[i]
		}
	}
	if call == nil || call.Params["width"] != 800.0 || call.Params["height"] != 600.0 {
		t.Fatalf("resize 参数异常: %v", call)
	}
}
