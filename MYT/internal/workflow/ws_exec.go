package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const sshRootUser = "root"
const sshRootPass = "myt"

// 网关地址（与 cloudapi/client.go 一致）
const cloudGatewayURL = "http://119.96.73.226:8892"

// PublicSSHConfig 公网模式 SSH 透传配置（由 main 包注入）
type PublicSSHConfig struct {
	DeviceID string // 设备 ID（公网模式下替代 IP）
	Token   string // 网关 access_token
}

// publicSSHGetter 公网配置回调（由 main 包注入，返回 nil 表示 local 模式）
var publicSSHGetter func(host string) *PublicSSHConfig

// SetPublicSSHGetter 注入公网配置回调
func SetPublicSSHGetter(fn func(host string) *PublicSSHConfig) {
	publicSSHGetter = fn
}

// rootSetup 记录每台设备是否已设置 root 凭证并开启登录
var (
	rootSetup     = map[string]bool{}
	rootSetupLock sync.Mutex
)

// wsMessage WebSocket SSH 消息协议（文档：/link/ssh）
type wsMessage struct {
	Type     string `json:"type"`
	IP       string `json:"ip,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Data     string `json:"data,omitempty"`
	Cols     int    `json:"cols,omitempty"`
	Rows     int    `json:"rows,omitempty"`
}

// buildHTTPURL 按模式拼接 HTTP 请求 URL
// local: http://{host}:{port}{path}
// public: {gateway}/v1/devices/{deviceID}/passthrough{path}
func buildHTTPURL(host string, port int, path string, pub *PublicSSHConfig) string {
	if pub != nil {
		return fmt.Sprintf("%s/v1/devices/%s/passthrough%s", cloudGatewayURL, pub.DeviceID, path)
	}
	return fmt.Sprintf("http://%s:%d%s", host, port, path)
}

// buildWSURL 按模式拼接 WebSocket URL
// local: ws://{host}:{port}{path}
// public: ws://{gateway}/v1/devices/{deviceID}/passthrough{path}
func buildWSURL(host string, port int, path string, pub *PublicSSHConfig) string {
	if pub != nil {
		// 网关 WebSocket：http → ws 协议替换
		wsURL := strings.Replace(cloudGatewayURL, "http://", "ws://", 1)
		return fmt.Sprintf("%s/v1/devices/%s/passthrough%s", wsURL, pub.DeviceID, path)
	}
	return fmt.Sprintf("ws://%s:%d%s", host, port, path)
}

// setAuthHeaders 按模式设置鉴权头
func setAuthHeaders(req *http.Request, pub *PublicSSHConfig) {
	if pub != nil {
		req.Header.Set("Authorization", "Bearer "+pub.Token)
	}
}

// setWSHeaders 按模式设置 WebSocket 鉴权头
func setWSHeaders(hdr *http.Header, pub *PublicSSHConfig) {
	if pub != nil {
		hdr.Set("Authorization", "Bearer "+pub.Token)
	}
}

// changeRootPwd 通过 HTTP API 将 root 密码设为 myt（POST /link/ssh/changePwd）
func changeRootPwd(host string, port int, pub *PublicSSHConfig) error {
	url := buildHTTPURL(host, port, "/link/ssh/changePwd", pub)
	body, _ := json.Marshal(map[string]string{"username": sshRootUser, "password": sshRootPass})
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeaders(req, pub)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[ws_exec] changeRootPwd %s 请求失败: %v", host, err)
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("设置root密码失败(HTTP %d)", resp.StatusCode)
	}
	return nil
}

// switchRoot 开关 SSH root 登录（POST /link/ssh/switchRoot）
func switchRoot(host string, port int, enable bool, pub *PublicSSHConfig) error {
	url := buildHTTPURL(host, port, "/link/ssh/switchRoot", pub)
	body, _ := json.Marshal(map[string]bool{"enable": enable})
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeaders(req, pub)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[ws_exec] switchRoot %s enable=%v 请求失败: %v", host, enable, err)
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("切换root登录失败(HTTP %d)", resp.StatusCode)
	}
	return nil
}

// setupRoot 为设备设置 root 密码并开启 root 登录
func setupRoot(host string, port int, pub *PublicSSHConfig) error {
	if err := changeRootPwd(host, port, pub); err != nil {
		return fmt.Errorf("设置root密码失败: %w", err)
	}
	if err := switchRoot(host, port, true, pub); err != nil {
		return fmt.Errorf("开启root登录失败: %w", err)
	}
	return nil
}

// CloseRootSSH 关闭设备的 root SSH 登录
func CloseRootSSH(host string, port int) {
	pub := getPublicConfig(host)
	if err := switchRoot(host, port, false, pub); err != nil {
		log.Printf("[ws_exec] 关闭root登录失败 %s: %v", host, err)
	}
}

// getPublicConfig 获取公网配置（nil 表示 local 模式）
func getPublicConfig(host string) *PublicSSHConfig {
	if publicSSHGetter != nil {
		return publicSSHGetter(host)
	}
	return nil
}

// dialWithRetry 带重试的 WebSocket 连接（设备端 WS 有并发限制）
func dialWithRetry(ctx context.Context, wsURL string, maxRetries int, pub *PublicSSHConfig) (*websocket.Conn, error) {
	var lastErr error
	opts := &websocket.DialOptions{}
	if pub != nil {
		opts.HTTPHeader = http.Header{}
		setWSHeaders(&opts.HTTPHeader, pub)
	}
	for i := 0; i < maxRetries; i++ {
		dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
		conn, _, err := websocket.Dial(dialCtx, wsURL, opts)
		dialCancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if i < maxRetries-1 {
			select {
			case <-time.After(1 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("WebSocket 连接失败(重试%d次): %w", maxRetries, lastErr)
}

// WSExec 通过 WebSocket SSH 终端执行命令（使用 root/myt）
func WSExec(ctx context.Context, host string, port int, command string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}

	pub := getPublicConfig(host)

	// 0. 设置 root 密码并开启 root 登录（每台设备只调一次）
	rootSetupLock.Lock()
	if !rootSetup[host] {
		rootSetupLock.Unlock()
		if err := setupRoot(host, port, pub); err != nil {
			log.Printf("[ws_exec] setupRoot %s 失败: %v（继续尝试SSH）", host, err)
		}
		rootSetupLock.Lock()
		rootSetup[host] = true
	}
	rootSetupLock.Unlock()

	wsURL := buildWSURL(host, port, "/link/ssh", pub)

	// 1. WebSocket 连接（带重试，设备端有并发 WS 限制）
	conn, err := dialWithRetry(ctx, wsURL, 3, pub)
	if err != nil {
		return "", err
	}
	closeConn := func() {
		conn.Close(websocket.StatusNormalClosure, "")
	}

	// 启动持续读取 goroutine
	type wsMsg struct {
		mt   websocket.MessageType
		data []byte
		err  error
	}
	msgCh := make(chan wsMsg, 32)
	go func() {
		for {
			mt, msg, err := conn.Read(ctx)
			select {
			case msgCh <- wsMsg{mt: mt, data: msg, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	// 2. 发送 login（公网模式下 IP 填 device_id）
	loginIP := host
	if pub != nil {
		loginIP = pub.DeviceID
	}
	loginMsg := wsMessage{Type: "login", IP: loginIP, Port: 22, User: sshRootUser, Password: sshRootPass}
	loginBytes, _ := json.Marshal(loginMsg)
	writeCtx, writeCancel := context.WithTimeout(ctx, 3*time.Second)
	if err := conn.Write(writeCtx, websocket.MessageText, loginBytes); err != nil {
		writeCancel()
		closeConn()
		return "", fmt.Errorf("发送 login 失败: %w", err)
	}
	writeCancel()

	// 3. 等待"成功连接到"
	loginOK := false
	loginDeadline := time.After(10 * time.Second)
	for !loginOK {
		select {
		case m := <-msgCh:
			if m.err != nil {
				closeConn()
				return "", fmt.Errorf("login 读取失败: %w", m.err)
			}
			if m.mt == websocket.MessageText {
				text := string(m.data)
				if strings.Contains(text, "成功连接到") || strings.Contains(text, "connected") {
					loginOK = true
				} else if strings.Contains(text, "失败") || strings.Contains(text, "failed") {
					closeConn()
					return "", fmt.Errorf("SSH 登录失败: %s", stripANSI(text))
				}
			}
		case <-loginDeadline:
			closeConn()
			return "", fmt.Errorf("SSH 登录超时(10s)")
		case <-ctx.Done():
			closeConn()
			return "", fmt.Errorf("SSH 登录被取消: %w", ctx.Err())
		}
	}

	// 4. 发送 resize
	resizeMsg := wsMessage{Type: "resize", Cols: 200, Rows: 50}
	resizeBytes, _ := json.Marshal(resizeMsg)
	writeCtx2, writeCancel2 := context.WithTimeout(ctx, 3*time.Second)
	conn.Write(writeCtx2, websocket.MessageText, resizeBytes)
	writeCancel2()

	// 5. 发送命令
	stdinMsg := wsMessage{Type: "stdin", Data: command + "\r"}
	stdinBytes, _ := json.Marshal(stdinMsg)
	writeCtx3, writeCancel3 := context.WithTimeout(ctx, 3*time.Second)
	if err := conn.Write(writeCtx3, websocket.MessageText, stdinBytes); err != nil {
		writeCancel3()
		closeConn()
		return "", fmt.Errorf("发送命令失败: %w", err)
	}
	writeCancel3()

	// 6. 收集输出
	var outputBuf strings.Builder
	echoSeen := false
	promptAfterEcho := false
	echoPos := 0
	outputDeadline := time.After(timeout)
	var bufferTimer <-chan time.Time

	for {
		select {
		case m := <-msgCh:
			if m.err != nil {
				goto done
			}
			if m.mt == websocket.MessageBinary {
				clean := stripANSI(string(m.data))
				outputBuf.WriteString(clean)
				if !echoSeen && strings.Contains(clean, command) {
					echoSeen = true
					echoPos = outputBuf.Len()
				}
			} else {
				text := stripANSI(string(m.data))
				if strings.Contains(text, "失败") {
					outputBuf.WriteString(text)
				}
			}
			if outputBuf.Len() > maxOutputSize*2 {
				goto done
			}
			if echoSeen && !promptAfterEcho {
				raw := outputBuf.String()
				afterEcho := ""
				if echoPos < len(raw) {
					afterEcho = raw[echoPos:]
				}
				if strings.Contains(afterEcho, ":~#") || strings.Contains(afterEcho, ":~$") {
					promptAfterEcho = true
					bufferTimer = time.After(800 * time.Millisecond)
				}
			}
		case <-bufferTimer:
			if promptAfterEcho {
				goto done
			}
		case <-outputDeadline:
			goto done
		case <-ctx.Done():
			goto done
		}
	}

done:
	// 7. 发 exit 并立即关闭连接
	exitMsg := wsMessage{Type: "stdin", Data: "exit\r"}
	exitBytes, _ := json.Marshal(exitMsg)
	exitCtx, exitCancel := context.WithTimeout(ctx, 2*time.Second)
	conn.Write(exitCtx, websocket.MessageText, exitBytes)
	exitCancel()
	closeConn()

	time.Sleep(200 * time.Millisecond)

	result := cleanOutput(outputBuf.String(), command)
	if len(result) > maxOutputSize {
		result = result[:maxOutputSize]
	}

	return result, nil
}

// cleanOutput 清理输出：去掉欢迎语、回显命令行和末尾 prompt
func cleanOutput(output, command string) string {
	result := output

	result = strings.ReplaceAll(result, "Myt OS Pro!", "")
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	lastIdx := strings.LastIndex(result, command)
	if lastIdx >= 0 {
		afterCmd := result[lastIdx+len(command):]
		if nlIdx := strings.Index(afterCmd, "\n"); nlIdx >= 0 {
			before := result[:lastIdx]
			after := afterCmd[nlIdx+1:]
			if strings.Contains(before, ":~#") || strings.Contains(before, ":~$") {
				result = after
			} else {
				result = before + after
			}
		}
	} else if nlIdx := strings.Index(result, "\n"); nlIdx >= 0 {
		result = result[nlIdx+1:]
	}

	result = strings.TrimRight(result, " \t\n\r")
	lines := strings.Split(result, "\n")
	if len(lines) > 0 {
		last := lines[len(lines)-1]
		if strings.Contains(last, ":~#") || strings.Contains(last, ":~$") ||
			strings.HasSuffix(last, "# ") || strings.HasSuffix(last, "$ ") {
			if len(lines) == 1 {
				return ""
			}
			result = strings.Join(lines[:len(lines)-1], "\n")
		}
	}

	return strings.TrimSpace(result)
}

// stripANSI 去除 ANSI 转义序列
func stripANSI(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) {
					if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
						i++
						break
					}
					i++
				}
			}
			continue
		}
		if s[i] == '\r' {
			i++
			continue
		}
		buf.WriteByte(s[i])
		i++
	}
	return buf.String()
}
