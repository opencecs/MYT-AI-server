package proxy

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// 设备API服务的固定端口
const devicePort = 8000

// Service 提供设备HTTP/WebSocket反向代理服务
// 将前端的请求转发到设备，避免前端直接访问设备IP
type Service struct {
	port    int
	server  *http.Server
	mu      sync.Mutex
	started bool
}

// NewService 创建代理服务实例
func NewService() *Service {
	return &Service{}
}

// Start 启动本地反向代理HTTP服务器
// 优先绑定18430端口，若被占用则自动选择可用端口
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/proxy/", s.handleProxy)
	mux.HandleFunc("/upload-proxy/", s.handleUploadProxy)

	s.server = &http.Server{Handler: mux}

	const preferredPort = 18430
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	if err != nil {
		// 首选端口被占用，自动选择可用端口
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("启动代理服务失败: %w", err)
		}
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.started = true

	util.GoSafe(func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("代理服务异常退出: %v", err)
		}
	})

	log.Printf("投屏代理服务已启动，端口: %d", s.port)
	return nil
}

// Stop 停止代理服务
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		s.server.Close()
		s.started = false
	}
}

// Port 返回代理服务实际监听的端口
func (s *Service) Port() int {
	return s.port
}

// ProxyURL 返回通过代理访问设备指定路径的完整URL
func (s *Service) ProxyURL(deviceIP string, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/proxy/%s/%s", s.port, deviceIP, path)
}

// handleProxy 处理 /proxy/{deviceIP}/... 的请求
// URL格式: /proxy/{deviceIP}/rest/of/path -> http://{deviceIP}:8000/rest/of/path
// 支持HTTP请求和WebSocket升级（Go 1.20+ httputil.ReverseProxy 原生支持）
func (s *Service) handleProxy(w http.ResponseWriter, r *http.Request) {
	// 解析路径: /proxy/{deviceIP}/rest/of/path
	trimmed := strings.TrimPrefix(r.URL.Path, "/proxy/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "缺少设备IP", http.StatusBadRequest)
		return
	}

	deviceIP := parts[0]
	restPath := ""
	if len(parts) > 1 {
		restPath = parts[1]
	}

	// 安全检查：验证IP格式，防止SSRF攻击
	if net.ParseIP(deviceIP) == nil {
		http.Error(w, "无效的设备IP地址", http.StatusBadRequest)
		return
	}

	target, _ := url.Parse(fmt.Sprintf("http://%s:%d", deviceIP, devicePort))
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = "/" + restPath
		req.URL.RawPath = "/" + restPath
		// 保留原始查询参数
		req.URL.RawQuery = r.URL.RawQuery
	}

	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()
	defer func() {
		util.Debugf("[proxy] %s http://%s:%d/%s → %d (%s)", r.Method, deviceIP, devicePort, restPath,
			rec.status, time.Since(start).Round(time.Millisecond))
	}()
	proxy.ServeHTTP(rec, r)
}

// statusRecorder 包装 ResponseWriter 以捕获最终状态码（供访问日志用）。
// 透传 Flush（SSE/WebSocket 需要）；不拦截 Write。
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack 透传给底层（WebSocket 升级依赖；不透传则 ReverseProxy 报
// "can't switch protocols using non-Hijacker ResponseWriter"）。
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("底层 ResponseWriter 不支持 Hijacker")
}

// handleUploadProxy 处理 /upload-proxy/{host}/{port}/upload 的请求
// 将 multipart 上传请求转发到设备的 MYTOS 端口，绕过 CORS
func (s *Service) handleUploadProxy(w http.ResponseWriter, r *http.Request) {
	// CORS 预检
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/upload-proxy/")
	parts := strings.SplitN(trimmed, "/", 3)
	if len(parts) < 2 {
		http.Error(w, "URL格式: /upload-proxy/{host}/{port}/upload", http.StatusBadRequest)
		return
	}

	host := parts[0]
	portStr := parts[1]
	restPath := "upload"
	if len(parts) > 2 {
		restPath = parts[2]
	}

	if net.ParseIP(host) == nil {
		http.Error(w, "无效的IP地址", http.StatusBadRequest)
		return
	}

	target, _ := url.Parse(fmt.Sprintf("http://%s:%s", host, portStr))
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = "/" + restPath
		req.URL.RawPath = "/" + restPath
		req.URL.RawQuery = r.URL.RawQuery
	}

	// 修改响应以注入 CORS 头
	origModify := proxy.ModifyResponse
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		if origModify != nil {
			return origModify(resp)
		}
		return nil
	}

	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()
	defer func() {
		util.Debugf("[proxy] %s http://%s:%s/%s → %d (%s)", r.Method, host, portStr, restPath,
			rec.status, time.Since(start).Round(time.Millisecond))
	}()
	proxy.ServeHTTP(rec, r)
}
