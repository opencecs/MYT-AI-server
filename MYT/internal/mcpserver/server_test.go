package mcpserver

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- token 中间件 ---

func TestAuthMiddleware(t *testing.T) {
	s := &Server{}
	s.token.Store("secret-token")
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name   string
		header string
		query  string
		want   int
	}{
		{"无 token 401", "", "", http.StatusUnauthorized},
		{"错误 token 401", "Bearer wrong", "", http.StatusUnauthorized},
		{"query 错误 token 401", "", "token=wrong", http.StatusUnauthorized},
		{"Bearer 200", "Bearer secret-token", "", http.StatusOK},
		{"query token 200", "", "token=secret-token", http.StatusOK},
		{"token 非空但未配置时拒绝", "", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9099/mcp?"+c.query, nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("期望 %d，实际 %d", c.want, rec.Code)
			}
		})
	}
}

func TestAuthMiddlewareNoTokenConfigured(t *testing.T) {
	// 未配置 token（空）时所有请求拒绝（fail-closed）
	s := &Server{}
	h := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9099/mcp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未配置 token 时期望 401，实际 %d", rec.Code)
	}
}

// --- 端口回退 ---

func TestListenWithFallback(t *testing.T) {
	// 占住一个端口，再用它作首选端口 → 应回退到随机可用端口
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("占用端口失败: %v", err)
	}
	defer blocker.Close()
	occupied := blocker.Addr().(*net.TCPAddr).Port

	ln, port, err := listenWithFallback("127.0.0.1", occupied)
	if err != nil {
		t.Fatalf("listenWithFallback 失败: %v", err)
	}
	defer ln.Close()
	if port == occupied {
		t.Fatalf("期望回退到其他端口，实际仍为 %d", port)
	}
	if port <= 0 {
		t.Fatalf("端口非法: %d", port)
	}
}

func TestListenWithFallbackPreferredFree(t *testing.T) {
	// 首选端口空闲时优先使用它
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测端口失败: %v", err)
	}
	free := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	ln, port, err := listenWithFallback("127.0.0.1", free)
	if err != nil {
		t.Fatalf("listenWithFallback 失败: %v", err)
	}
	defer ln.Close()
	if port != free {
		t.Fatalf("期望使用首选端口 %d，实际 %d", free, port)
	}
}

// --- Start/Stop 幂等 ---

func TestStartStopIdempotent(t *testing.T) {
	s := New(Options{PreferredPort: 0, Token: "t"}, nil)
	if err := s.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if err := s.Start(); err != nil { // 重复 Start 不报错
		t.Fatalf("重复 Start 应幂等: %v", err)
	}
	if !s.Running() || s.Port() == 0 || s.URL() == "" {
		t.Fatalf("启动状态异常: running=%v port=%d url=%q", s.Running(), s.Port(), s.URL())
	}
	if host := s.Host(); host != "127.0.0.1" {
		t.Fatalf("默认 host 期望 127.0.0.1，实际 %q", host)
	}
	s.Stop()
	s.Stop() // 重复 Stop 不报错
	if s.Running() {
		t.Fatal("Stop 后仍为运行状态")
	}
}

// --- 监听地址 ---

func TestStartNonLoopbackRequiresToken(t *testing.T) {
	// 防御性校验：非回环地址 + 无 token → 拒绝启动
	s := New(Options{Host: "0.0.0.0", PreferredPort: 0}, nil)
	err := s.Start()
	if err == nil {
		s.Stop()
		t.Fatal("非回环地址无 token 时应拒绝启动")
	}
	if !s.Running() {
		t.Log("启动被拒绝（符合预期）")
	} else {
		s.Stop()
	}
}

func TestStartNonLoopbackWithToken(t *testing.T) {
	s := New(Options{Host: "0.0.0.0", PreferredPort: 0, Token: "t"}, nil)
	if err := s.Start(); err != nil {
		t.Fatalf("非回环地址 + token 应可启动: %v", err)
	}
	defer s.Stop()
	if host := s.Host(); host != "0.0.0.0" {
		t.Fatalf("Host 期望 0.0.0.0，实际 %q", host)
	}
	// URL 不应直接用 0.0.0.0（不能作为连接地址）
	if u := s.URL(); strings.Contains(u, "0.0.0.0") {
		t.Fatalf("URL 不应包含 0.0.0.0: %q", u)
	}
}

func TestDisplayHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "127.0.0.1"},
		{"127.0.0.1", "127.0.0.1"},
		{"localhost", "localhost"},
		{"192.168.1.5", "192.168.1.5"}, // 内网 IP 原样
	}
	for _, c := range cases {
		if got := displayHost(c.in); got != c.want {
			t.Errorf("displayHost(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
	// 0.0.0.0 应替换为本机非回环 IPv4（若取得到）
	if got := displayHost("0.0.0.0"); got == "0.0.0.0" {
		t.Log("本机无非回环 IPv4，displayHost(0.0.0.0) 原样返回")
	} else if got == "" || net.ParseIP(got) == nil || net.ParseIP(got).IsLoopback() {
		t.Errorf("displayHost(0.0.0.0) = %q，应为非回环 IPv4", got)
	}
}

// 确保 mcp 包在测试二进制中被引用（工具注册计数 sanity check）
func TestToolRegistration(t *testing.T) {
	s := New(Options{Token: "t"}, nil)
	if n := s.ToolCount(); n < 6 {
		t.Fatalf("UI 工具应至少注册 6 个，实际 %d", n)
	}
	t.Logf("仅 UI 工具注册数: %d", s.ToolCount())
}

// TestBrowserToolAnnotations 浏览器类工具的 annotations：
// 只读工具标 readOnly；click/fill 等无只读标注；evaluate_script 标 destructive。
func TestBrowserToolAnnotations(t *testing.T) {
	s := New(Options{Token: "t"}, nil)
	ann := map[string]*mcp.ToolAnnotations{}
	s.mu.Lock()
	for _, tool := range s.tools {
		ann[tool.Name] = tool.Annotations
	}
	s.mu.Unlock()

	readOnly := []string{"ui_map", "screenshot", "dom_query", "list_pages", "select_page",
		"list_network_requests", "list_console_messages", "get_console_message",
		"wait_for", "performance_analyze_insight"}
	for _, name := range readOnly {
		a := ann[name]
		if a == nil || !a.ReadOnlyHint {
			t.Errorf("%s 应标注 readOnlyHint=true", name)
		}
	}
	notReadOnly := []string{"click", "click_at", "drag", "fill", "fill_form", "upload_file",
		"navigate_page", "new_page", "close_page", "handle_dialog", "emulate",
		"performance_start_trace", "performance_stop_trace"}
	for _, name := range notReadOnly {
		if a := ann[name]; a != nil && a.ReadOnlyHint {
			t.Errorf("%s 不应标注 readOnlyHint", name)
		}
	}
	// 高危工具：evaluate_script 应标 destructiveHint（device_fetch 见 tools 包测试）
	for _, name := range []string{"evaluate_script"} {
		a := ann[name]
		if a == nil || a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("%s 应标注 destructiveHint=true", name)
		}
	}
}

// TestBrowserToolsRegistered 浏览器类工具全部注册（对照官方清单的已移植项）。
func TestBrowserToolsRegistered(t *testing.T) {
	s := New(Options{Token: "t"}, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	want := []string{
		// input
		"click", "click_at", "hover", "drag", "fill", "fill_form", "upload_file",
		"type_text", "press_key", "scroll", "handle_dialog",
		// navigation
		"navigate_page", "new_page", "list_pages", "select_page", "close_page", "wait_for",
		// network
		"list_network_requests", "get_network_request",
		// emulation
		"emulate", "resize_page",
		// performance
		"performance_start_trace", "performance_stop_trace", "performance_analyze_insight",
		// debugging
		"list_console_messages", "get_console_message", "evaluate_script", "ui_map",
	}
	registered := map[string]bool{}
	for _, tool := range s.tools {
		registered[tool.Name] = true
	}
	for _, name := range want {
		if !registered[name] {
			t.Errorf("缺少工具 %s", name)
		}
	}
}
