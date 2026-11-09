package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Client 裸 CDP 客户端：从 WebView2 用户数据目录的 DevToolsActivePort 文件
// 拿随机调试端口，GET /json 列 targets，短连接执行 JSON-RPC 命令（用完即断，
// 避开 Chromium 同 target 的 WS 连接数限制）。
type Client struct {
	dataDir string
	port    atomic.Int32 // 0 = 未知
}

// NewCDPClient 创建 CDP 客户端。dataDir 为 WebviewUserDataPath。
func NewClient(dataDir string) *Client {
	return &Client{dataDir: dataDir}
}

// devToolsActivePortFiles 返回 DevToolsActivePort 候选路径（按优先级）：
// WebView2 实际写在 <dataDir>/EBWebView/DevToolsActivePort（EBWebView 子目录），
// 根目录路径作为回退兼容（部分旧版本可能直接写在用户数据目录）。
func (c *Client) devToolsActivePortFiles() []string {
	return []string{
		filepath.Join(c.dataDir, "EBWebView", "DevToolsActivePort"),
		filepath.Join(c.dataDir, "DevToolsActivePort"),
	}
}

// discoverPort 从 DevToolsActivePort 文件读端口（首行为端口数字），
// 依次探测候选路径，取第一个存在且内容有效的文件。
func (c *Client) discoverPort() (int, error) {
	if c.dataDir == "" {
		return 0, fmt.Errorf("未配置 WebView2 用户数据目录")
	}
	var readErr error
	for _, path := range c.devToolsActivePortFiles() {
		data, err := os.ReadFile(path)
		if err != nil {
			readErr = err
			continue
		}
		port, perr := parsePortLine(string(data))
		if perr != nil {
			return 0, fmt.Errorf("%s: %w", path, perr)
		}
		return port, nil
	}
	return 0, fmt.Errorf("读取 DevToolsActivePort 失败（EBWebView/ 与根目录均未找到）: %w", readErr)
}

// parsePortLine 解析 DevToolsActivePort 文件内容（首行为端口数字）。
func parsePortLine(data string) (int, error) {
	line := strings.TrimSpace(strings.SplitN(data, "\n", 2)[0])
	var port int
	if _, err := fmt.Sscanf(line, "%d", &port); err != nil || port <= 0 {
		return 0, fmt.Errorf("DevToolsActivePort 内容无效: %q", line)
	}
	return port, nil
}

// probeTimeout 单次 GET /json 探测的超时。
const probeTimeout = 2 * time.Second

// jsonURL 构造 CDP HTTP 端点的 /json 地址。
func jsonURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/json", port)
}

// pingPort 探测该端口上的 CDP HTTP 端点是否可达（GET /json 返回 200）。
func (c *Client) pingPort(ctx context.Context, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jsonURL(port), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// WaitPort 轮询等待 CDP 端口出现且可达（WebView2 启动后写入文件），最多 timeout。
// 发现的端口必须通过 GET /json 连通性验证：文件里可能残留上一轮进程的陈旧端口，
// 验证失败视为本轮发现失败，继续轮询等待文件被当前进程重写。
func (c *Client) WaitPort(timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		port, err := c.discoverPort()
		if err == nil && c.pingPort(context.Background(), port) {
			c.port.Store(int32(port))
			return port, nil
		}
		if err == nil {
			err = fmt.Errorf("端口 %d 不可达（可能是残留的陈旧端口）", port)
		}
		lastErr = err
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("等待 CDP 端口超时（%v）: %w", timeout, lastErr)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Port 返回已知端口（不触发发现）。
func (c *Client) Port() int { return int(c.port.Load()) }

// rediscoverPort 作废缓存端口，重新从 DevToolsActivePort 文件发现并缓存。
func (c *Client) rediscoverPort() (int, error) {
	c.port.Store(0)
	port, err := c.discoverPort()
	if err != nil {
		return 0, err
	}
	c.port.Store(int32(port))
	return port, nil
}

// currentPort 返回缓存端口；无缓存则发现并缓存。
func (c *Client) currentPort() (int, error) {
	if port := c.Port(); port != 0 {
		return port, nil
	}
	return c.rediscoverPort()
}

// Ready 判断 CDP 是否可用：有端口且 GET /json 可达。
// 缓存端口探测失败（如残留上一轮进程的陈旧端口）时作废缓存重新发现，最多重试一次。
func (c *Client) Ready(ctx context.Context) bool {
	port, err := c.currentPort()
	if err != nil {
		return false
	}
	if c.pingPort(ctx, port) {
		return true
	}
	fresh, err := c.rediscoverPort()
	if err != nil {
		return false
	}
	return c.pingPort(ctx, fresh)
}

// cdpTarget CDP /json 返回的 target 条目（只取需要的字段）。
type Target struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// PageTargets 列出所有 page target（排除 devtools 自身页面）。
// 缓存端口请求失败（连接拒绝/超时/非 200，如残留上一轮进程的陈旧端口）时，
// 作废缓存重新发现端口并重试一次；再失败返回原始错误。
func (c *Client) PageTargets(ctx context.Context) ([]Target, error) {
	port, err := c.currentPort()
	if err != nil {
		return nil, fmt.Errorf("CDP 端口未知（界面控制需重启客户端后可用）: %w", err)
	}
	targets, firstErr := c.fetchTargets(ctx, port)
	if firstErr == nil {
		return c.filterPages(targets)
	}
	fresh, rerr := c.rediscoverPort()
	if rerr != nil {
		return nil, fmt.Errorf("访问 CDP /json 失败（重新发现端口亦失败）: %w", firstErr)
	}
	targets, err = c.fetchTargets(ctx, fresh)
	if err != nil {
		return nil, firstErr // 只重试一次，返回原始错误
	}
	return c.filterPages(targets)
}

// fetchTargets GET /json 拉取 target 列表（传输失败/非 200/解析失败均报错）。
func (c *Client) fetchTargets(ctx context.Context, port int) ([]Target, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jsonURL(port), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("访问 CDP /json 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CDP /json 返回 %d", resp.StatusCode)
	}
	var targets []Target
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("解析 CDP targets 失败: %w", err)
	}
	return targets, nil
}

// filterPages 过滤出 page target（排除 devtools 自身页面），为空时报错。
func (c *Client) filterPages(targets []Target) ([]Target, error) {
	pages := make([]Target, 0, len(targets))
	for _, t := range targets {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "devtools://") {
			pages = append(pages, t)
		}
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("无可用 page target（客户端窗口未就绪？）")
	}
	return pages, nil
}

// cdpReqID CDP JSON-RPC 请求 id 计数器（进程内自增，从 1 开始）。
// 不能用时间戳（UnixNano ≈ 1.76e18 > 2^53）：Chromium DevTools 服务端以
// JS double 存 id，超 2^53 丢精度导致回显 id 与请求不一致、响应被丢弃。
// 原子自增保证并发调用下 id 唯一，且在可预见寿命内远小于 2^53。
var reqID atomic.Int64

// nextCDPReqID 返回下一个 CDP 请求 id。
func nextReqID() int64 { return reqID.Add(1) }

// Call 对指定 target 短连接执行一条 CDP 命令，返回 result 原始 JSON。
func (c *Client) Call(ctx context.Context, wsURL, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("连接 CDP WebSocket 失败: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(16 << 20) // 16MB：主窗口截图 base64 约 380KB，默认 32KB 限制会掐断响应帧

	id := nextReqID()
	reqPayload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if err := conn.Write(ctx, websocket.MessageText, reqPayload); err != nil {
		return nil, fmt.Errorf("发送 CDP 命令失败: %w", err)
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取 CDP 响应失败: %w", err)
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // 忽略无法解析的帧（事件等）
		}
		if len(msg.ID) == 0 {
			continue // 事件帧
		}
		var gotID int64
		if err := json.Unmarshal(msg.ID, &gotID); err != nil || gotID != id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("CDP 命令 %s 失败: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

// FirstPageTarget 返回第一个 page target 的 WS URL（UI 工具默认目标）。
func (c *Client) FirstPageTarget(ctx context.Context) (string, error) {
	pages, err := c.PageTargets(ctx)
	if err != nil {
		return "", err
	}
	return pages[0].WebSocketDebuggerURL, nil
}

// Eval 在第一个 page target 上执行 JS（awaitPromise + returnByValue），返回结果值。
func (c *Client) Eval(ctx context.Context, expression string, timeout time.Duration) (any, error) {
	wsURL, err := c.FirstPageTarget(ctx)
	if err != nil {
		return nil, err
	}
	res, err := c.Call(ctx, wsURL, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"awaitPromise":  true,
		"returnByValue": true,
	}, timeout)
	if err != nil {
		return nil, err
	}
	var out struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	if out.ExceptionDetails != nil {
		return nil, fmt.Errorf("JS 执行异常: %s", out.ExceptionDetails.Text)
	}
	return out.Result.Value, nil
}

// JSONUnmarshal CDP 结果解析 helper（统一错误包装），供各子包复用。
func JSONUnmarshal(data json.RawMessage, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("解析 CDP 响应失败: %w", err)
	}
	return nil
}

// BrowserWS 返回 browser 级 WS endpoint（/json/version 的
// webSocketDebuggerUrl，Target.createTarget/closeTarget 等浏览器域命令用）。
func (c *Client) BrowserWS(ctx context.Context) (string, error) {
	port, err := c.currentPort()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/version", port), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("访问 CDP /json/version 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CDP /json/version 返回 %d", resp.StatusCode)
	}
	var out struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("CDP /json/version 未返回 webSocketDebuggerUrl")
	}
	return out.WebSocketDebuggerURL, nil
}
