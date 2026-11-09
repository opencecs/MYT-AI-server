// Package mcpserver 提供客户端自建 MCP（Model Context Protocol）server：
// streamable HTTP 传输，127.0.0.1 监听 + Bearer token 鉴权，
// 工具分两类：UI 级（走 CDP 控制客户端界面）与业务级（透传各服务 Wrapper 方法）。
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/browser"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/guide"
	"github.com/moyunteng/myt/internal/mcpserver/tools"
	"github.com/moyunteng/myt/internal/mcpserver/uimap"
	"github.com/moyunteng/myt/internal/mcpserver/winshot"
)

// Options server 构造参数
type Options struct {
	Host           string // 监听地址（空 = 127.0.0.1；可填 0.0.0.0 或内网 IP 供局域网访问）
	PreferredPort  int    // 首选端口（被占则自动回退随机端口；0 = 直接随机）
	WebviewDataDir string // WebView2 用户数据目录（DevToolsActivePort 文件所在）
	Token          string // 初始鉴权 token（可为空，启动前用 SetToken 注入）
}

// Server MCP server：持有 mcp.Server 与 HTTP 监听，支持热启停。
type Server struct {
	opts Options

	mu        sync.Mutex
	mcpServer *mcp.Server
	httpSrv   *http.Server
	port      int
	host      string // 实际监听地址（Start 时确定，空 = 127.0.0.1）
	running   bool
	toolCount int
	tools     []*mcp.Tool // 已注册工具（供测试/诊断检查 annotations）

	// uid 映射：ui_map 生成的 uid（N_M）→ 元素身份（RefTable），每次 ui_map 全量重建。
	// uid 分配与跨快照复用规则照抄 chrome-devtools-mcp TextSnapshot.ts（见 tools_uimap.go）。
	refMu sync.Mutex
	refs  map[string]int64

	// 官方 TextSnapshot 的跨快照 uid 复用表（uniqueBackendNodeIdToMcpId）与
	// 快照号计数器（nextSnapshotId），由 uidAllocator 使用。
	uidReuse   map[string]string
	nextSnapID int

	token atomic.Value // string，运行中可热替换（RegenerateMCPToken 不重启）

	cdp *cdp.Client
}

// New 创建 server（不启动）。deps 提供业务级工具背后的服务能力，可为 nil（仅 UI 工具）。
func New(opts Options, deps *BizDeps) *Server {
	s := &Server{opts: opts, nextSnapID: 1} // 官方 TextSnapshot.nextSnapshotId 从 1 起
	if opts.Token != "" {
		s.token.Store(opts.Token)
	}
	s.cdp = cdp.NewClient(opts.WebviewDataDir)
	s.mcpServer = mcp.NewServer(&mcp.Implementation{
		Name:    "myt-client",
		Title:   "魔云腾客户端",
		Version: "1.0.0",
	}, &mcp.ServerOptions{Instructions: serverInstructions})

	snapshot := uimap.NewCollector(s.cdp, nil)
	// Hub（click 等 uid 解析）与快照 refs 的 sink 必须是同一实例：
	// ui_map 产出的 uid 表经 snapshot.SetSink(hub) 灌进 Hub，
	// 否则 hub.lookupRef 恒 miss、click 恒报「uid 已过期」。
	hub := browser.NewHub(s.cdp, snapshot)
	snapshot.SetSink(hub)
	// 业务级工具（透传各服务 Wrapper 方法）
	if deps != nil {
		tools.Register(s.mcpServer, s.recordTool, deps)
	}
	// 浏览器类工具（chrome-devtools-mcp 移植：input/navigation/network/
	// debugging/emulation/performance + ui_map 快照）
	uimap.Register(s.mcpServer, s.recordTool, snapshot)
	browser.Register(s.mcpServer, s.recordTool, hub)
	// 遗留 UI 工具（screenshot/dom_query，早于浏览器类移植存在）
	registerUITools(s)
	// 界面元素字典：resource（guide://ui-dictionary 全文）+ get_ui_guide 工具（按章节查询）
	guide.Register(s.mcpServer, s.recordTool)
	// 投屏播放器窗口截图（screenshot_window；deps.Screencast == nil 时不注册）
	if deps != nil {
		winshot.Register(s.mcpServer, s.recordTool, deps.Screencast)
	}
	return s
}

// SetToken 热替换鉴权 token（下次请求即生效，无需重启）。
func (s *Server) SetToken(t string) {
	if t == "" {
		return
	}
	s.token.Store(t)
}

// Start 启动 HTTP 监听。幂等：已运行时直接返回 nil。
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}

	host := s.opts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	// 防御性校验：绑定非回环地址时强制要求已配置 token（明文暴露给局域网，无 token 等于裸奔）。
	if !isLoopbackHost(host) {
		if t, _ := s.token.Load().(string); t == "" {
			return fmt.Errorf("MCP server 监听 %s 需要鉴权 token：请先在设置页启用 MCP 服务生成 token", host)
		}
	}

	ln, port, err := listenWithFallback(host, s.opts.PreferredPort)
	if err != nil {
		return fmt.Errorf("MCP server 监听失败: %w", err)
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.mcpServer }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", s.authMiddleware(handler))

	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.port = port
	s.host = host
	s.running = true
	srv := s.httpSrv
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[mcpserver] 异常退出: %v", err)
		}
	}()
	log.Printf("[mcpserver] 已启动 http://%s:%d/mcp（工具 %d 个）", displayHost(host), port, s.toolCount)
	return nil
}

// Stop 优雅停止。幂等：未运行时直接返回。
func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	srv := s.httpSrv
	s.running = false
	s.httpSrv = nil
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
	log.Printf("[mcpserver] 已停止")
}

// Running 返回 server 是否在监听。
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Port 返回实际监听端口（未运行时 0）。
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// URL 返回实际访问地址（未运行时空串）。绑定 0.0.0.0 时用首个非回环 IPv4 拼接，
// 便于用户直接复制使用（0.0.0.0 本身不能作为连接地址）。
func (s *Server) URL() string {
	h, p := s.listenAddr()
	if p == 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d/mcp", h, p)
}

// Host 返回实际监听地址（未运行时返回配置值，均空则 127.0.0.1）。
func (s *Server) Host() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.host != "" {
		return s.host
	}
	if s.opts.Host != "" {
		return s.opts.Host
	}
	return "127.0.0.1"
}

// listenAddr 返回（用于展示的 host, 端口）。0.0.0.0/:: 替换为本机首个非回环 IPv4。
func (s *Server) listenAddr() (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return "", 0
	}
	return displayHost(s.host), s.port
}

// displayHost 监听地址 → 可连接地址：回环原样返回；0.0.0.0/:: 用首个非回环 IPv4；其余原样。
func displayHost(listenHost string) string {
	if isLoopbackHost(listenHost) || listenHost == "" {
		if listenHost == "" {
			return "127.0.0.1"
		}
		return listenHost
	}
	if listenHost == "0.0.0.0" || listenHost == "::" {
		if ip := firstNonLoopbackIPv4(); ip != "" {
			return ip
		}
		return listenHost // 取不到本机 IP 时原样返回（用户需自行替换）
	}
	return listenHost
}

// firstNonLoopbackIPv4 返回本机首个非回环 IPv4（取不到返回空串）。
func firstNonLoopbackIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip := ipnet.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}

// isLoopbackHost 判断 host 是否为回环地址（127.x / ::1 / localhost）。
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ToolCount 返回已注册工具数。
func (s *Server) ToolCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.toolCount
}

// CDPStatus 返回 CDP 端口与可用性（供 UI 级工具与状态查询）。
func (s *Server) CDPStatus() (port int, ready bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.cdp.Port(), s.cdp.Ready(ctx)
}

// WaitCDPReady 阻塞等待 CDP 端口出现（最多 timeout），server 启动后调用。
func (s *Server) WaitCDPReady(timeout time.Duration) (int, error) {
	return s.cdp.WaitPort(timeout)
}

// authMiddleware 校验 Authorization: Bearer <token> 或 ?token=<token>，无 token 401。
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := s.token.Load().(string)
		provided := ""
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			provided = strings.TrimPrefix(auth, "Bearer ")
		}
		if provided == "" {
			provided = r.URL.Query().Get("token")
		}
		if token == "" || provided != token {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// listenWithFallback 绑定 host:preferred（host 空 = 127.0.0.1），端口被占则回退随机端口
// （参照 proxy.Service.Start）。
func listenWithFallback(host string, preferred int) (net.Listener, int, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	if preferred > 0 {
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, preferred))
		if err == nil {
			return ln, ln.Addr().(*net.TCPAddr).Port, nil
		}
		log.Printf("[mcpserver] %s 端口 %d 被占用，回退随机端口", host, preferred)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:0", host))
	if err != nil {
		return nil, 0, err
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, nil
}

// recordTool 记录已注册工具（计数 + 引用，供 ToolCount/测试/诊断）。
func (s *Server) recordTool(t *mcp.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolCount++
	s.tools = append(s.tools, t)
}

// serverInstructions server 级使用说明（Initialize 时随 instructions 字段下发）。
const serverInstructions = `这是「魔云腾客户端」桌面的 MCP server，可操作客户端 UI（经 CDP）与设备/云机业务。

工具选用优先级：
1. 业务操作一律优先用业务工具（create_android / batch_* / list_* / get_* 等），它们走正式 API，可靠且可追踪；
2. UI 工具（ui_map / click / type_text / press_key / screenshot）用于视觉验证、以及业务工具未覆盖的场景。

观察循环（任何 UI 操作都以此为本）：
1. 操作前先调 ui_map，第一眼先读 summary：它就是页面的人类描述（这是哪个页、有哪些区块、建议先做什么），直接按它的建议行动；
2. summary 不够用时再看明细：state（viewport/modal/activeTab）与 interactiveElements（全部可见可交互元素，含 uid）；
3. state.modal 存在时先处理弹窗再做别的：自更新弹窗每次启动/重载都会弹出并挡住全部交互，
   按 modal.suggested 的 uid 点击（通常是「稍后提醒」）即可；
4. 目标元素按 name/role 找到后，点击/输入用 uid 定位（click/fill/fill_form 的 uid 参数传 uid 字符串；
   type_text/press_key 兼容 uid 或 ref），不要用屏幕坐标（dom_query 坐标仅作逃生舱）；
5. 操作后再调一次 ui_map（或 screenshot 做视觉确认）验证结果，形成「观察 → 分析 → 操作 → 再观察」的闭环。

界面元素字典（summary 负责「当前页面是什么」，字典负责「每个元素干什么」）：
- 需要了解某页面/某元素的作用时，读取 resource guide://ui-dictionary（或调用 get_ui_guide 工具按章节查询），
  不要凭猜测操作；get_ui_guide 的 section 参数传页面 key（host/agent/mirror 等）取单章。

浏览器类工具（chrome-devtools-mcp 移植，行为与官方一致）：
- 页面导航/多页：navigate_page / list_pages / select_page / new_page / close_page / wait_for；
- 表单与交互：click / click_at / hover / drag / fill / fill_form（表单优先 fill_form 一次填完）/ upload_file / handle_dialog；
- 网络与调试：list_network_requests / get_network_request（环形缓冲 500 条）/ list_console_messages / get_console_message；
- evaluate_script 为任意 JS 执行（高危），仅在窄接口工具无法满足时使用；
- 环境：emulate（网络/CPU 节流、UA、视口等）；性能：performance_start_trace / performance_stop_trace / performance_analyze_insight；
- 事件类工具（network/console/dialog/performance）依赖持久 CDP 会话，首次调用自动建立。

已知坑：
- 左侧分组可能处于折叠状态，目标元素不可见时先让用户展开分组，或找分组展开开关；
- 客户端窗口最小化时 screenshot 会超时，需先让用户还原窗口；
- uid 会随界面刷新过期：click 报「uid 已过期」时重新调一次 ui_map。`
