// Package cloudapi 公网版（public 模式）网关适配层。
//
// local 模式客户端直连设备 SDK API（http://<设备IP>:8000 + Basic Auth）；
// public 模式客户端走网关透传，统一用 Authorization: Bearer <授权token>
// + X-Device-Password 路由到真实设备。
//
// 鉴权统一使用授权 token（moyunteng.com 签发，由 PaymentServiceWrapper.GetToken 提供），
// 网关侧已改造为接受该 token，客户端不再调用 /v1/auth/login、/v1/auth/refresh。
//
// 设备接口契约：
//   - 设备列表：GET /v1/devices → {devices:[...], total}
//   - 设备操作：{METHOD} /v1/devices/{deviceID}{path} + Bearer + X-Device-Password
package cloudapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// 网关地址（B端直连，不走 CDN）
const GatewayURL = "http://119.96.73.226:8892"

// gatewayHTTP 全局共享 HTTP 客户端（连接池复用，60s 超时）
var gatewayHTTP = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	},
}

// Client 网关客户端。线程安全，token 通过 tokenFn 注入（授权 token）。
type Client struct {
	mu         sync.RWMutex
	tokenFn    func() (string, error) // 授权 token 获取入口
	cachedToken string               // 缓存本次取到的 token（供 IsLoggedIn/AccessToken 兼容）
}

// NewClient 创建网关客户端（未配置 token 来源，需调 SetTokenFn 注入）。
func NewClient() *Client {
	return &Client{}
}

// SetTokenFn 注入授权 token 获取函数（由 setup.go 注入 paymentSvc.GetToken）。
func (c *Client) SetTokenFn(fn func() (string, error)) {
	c.mu.Lock()
	c.tokenFn = fn
	c.mu.Unlock()
}

// IsLoggedIn 是否已配置 token 来源且能取到 token
func (c *Client) IsLoggedIn() bool {
	tok, err := c.token()
	return err == nil && tok != ""
}

// AccessToken 返回当前授权 token（兼容旧调用方，同步取一次 token）
func (c *Client) AccessToken() string {
	tok, _ := c.token()
	return tok
}

// token 取授权 token（优先缓存，无缓存则调 tokenFn）。
func (c *Client) token() (string, error) {
	c.mu.RLock()
	fn := c.tokenFn
	cached := c.cachedToken
	c.mu.RUnlock()
	if cached != "" {
		return cached, nil
	}
	if fn == nil {
		return "", fmt.Errorf("网关客户端未配置 token 来源")
	}
	tok, err := fn()
	if err != nil || tok == "" {
		return "", fmt.Errorf("获取授权 token 失败: %w", err)
	}
	c.mu.Lock()
		c.cachedToken = tok
	c.mu.Unlock()
	return tok, nil
}

// EnsureValidToken 返回有效授权 token（兼容旧调用方，等价于 token()）。
func (c *Client) EnsureValidToken() (string, error) {
	return c.token()
}

// DeviceAction 转发设备请求到网关（透传到真实设备）。
// method: GET/POST/DELETE/PUT；path: 设备 SDK 原路径（如 /android/start）；
// deviceID: 目标设备 ID；body: 请求体（可为 nil）；devicePassword: 设备 SDK 密码（可为空）。
// 返回网关透传的设备原始响应。遇 401 清除缓存 token 重试一次。
func (c *Client) DeviceAction(method, path, deviceID string, body interface{}, devicePassword string) ([]byte, int, error) {
	return c.deviceActionWithRetry(method, path, deviceID, body, devicePassword, true)
}

func (c *Client) deviceActionWithRetry(method, path, deviceID string, body interface{}, devicePassword string, allowRetry bool) ([]byte, int, error) {
	if deviceID == "" {
		return nil, 0, fmt.Errorf("device_id 必填")
	}
	token, err := c.token()
	if err != nil {
		return nil, 0, err
	}

	targetURL := fmt.Sprintf("%s/v1/devices/%s%s", GatewayURL, deviceID, path)
	var reqBody io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reqBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, targetURL, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "*/*")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if devicePassword != "" {
		req.Header.Set("X-Device-Password", devicePassword)
	}

	start := time.Now()
	resp, err := gatewayHTTP.Do(req)
	if err != nil {
		util.Debugf("[gateway] %s %s → 失败: %v (%s)", method, targetURL, err, time.Since(start).Round(time.Millisecond))
		return nil, 0, fmt.Errorf("网关请求失败: %w", err)
	}
	util.Debugf("[gateway] %s %s → %d (%s)", method, targetURL, resp.StatusCode, time.Since(start).Round(time.Millisecond))
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	// 401 清除缓存 token 重试一次（授权 token 由 payment 侧自动刷新）
	if resp.StatusCode == http.StatusUnauthorized && allowRetry {
		c.mu.Lock()
			c.cachedToken = ""
		c.mu.Unlock()
		return c.deviceActionWithRetry(method, path, deviceID, body, devicePassword, false)
	}
	return respBody, resp.StatusCode, nil
}
