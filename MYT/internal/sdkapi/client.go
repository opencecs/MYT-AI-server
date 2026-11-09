package sdkapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// DevicePort 设备服务端口
const DevicePort = 8000

// DevicePortV4 V4 设备服务端口（V4 SDK，提供 GET /health）
const DevicePortV4 = 8088

// DeviceUser 设备认证用户名
const DeviceUser = "admin"

// DeviceTransport 设备通信共享 Transport
var DeviceTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:        500,
	MaxIdleConnsPerHost: 16,
	MaxConnsPerHost:     16,
	IdleConnTimeout:     120 * time.Second,
	DisableCompression:  true,
}

type Client struct {
	BaseURL    string
	Username   string
	Password   string
	httpClient *http.Client
	ctx        context.Context

	// public 模式（公网版）：走线上服务透传，Bearer token + device_id 路由
	deviceID string // public 模式路由用的设备 ID
	token    string // public 模式 Bearer session_token
	isPublic bool

	// v2 模式（v2route.go）：设备语义由主机 81 端口（超级 SDK）承接，
	// 请求经 v2Rewrite 翻译为 dc_api，响应 {code:200} 归一为 {code:0}
	v2HostIP string

	// errorMsg 非空时表示这是一个"错误客户端"，所有请求立即返回此错误
	errorMsg string
}

func (c *Client) SetPassword(password string) {
	c.Password = password
}

func (c *Client) SetContext(ctx context.Context) {
	c.ctx = ctx
}

func (c *Client) requestContext() context.Context {
	if c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

// defaultTimeout 默认请求超时。LAN ARM 设备高负载时（批量创建/容器过渡态）
// 简单 GET 也可能超过 8s，过短会误判失败（如镜像存在性检查超时→白白重推镜像）。
const defaultTimeout = 15 * time.Second

func NewClient(deviceIP string) *Client {
	return &Client{
		BaseURL:  fmt.Sprintf("http://%s:%d", deviceIP, DevicePort),
		Username: DeviceUser,
		Password: "",
		httpClient: &http.Client{
			Timeout:   defaultTimeout,
			Transport: DeviceTransport,
		},
	}
}

func NewClientWithTimeout(deviceIP string, timeout time.Duration) *Client {
	return &Client{
		BaseURL:  fmt.Sprintf("http://%s:%d", deviceIP, DevicePort),
		Username: DeviceUser,
		Password: "",
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: DeviceTransport,
		},
	}
}

// NewV2Client 创建 v2 模式客户端：指向主机 81 端口（超级 SDK），
// 请求经 v2Rewrite 翻译为 dc_api 等价操作（v2route.go 有对照表）。
func NewV2Client(hostIP string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		BaseURL:  fmt.Sprintf("http://%s:81", hostIP),
		v2HostIP: hostIP,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: DeviceTransport,
		},
	}
}

// NewV4Client 创建 v4 模式客户端：指向 V4 设备的 8088 端口（与 /health 同端口）。
// 接口路径先按 8000 同款风格拼接（buildURL 直连 BaseURL+path）；鉴权方式未知，
// 先不带 Basic Auth（Password 为空时 setAuth 不会发认证头）。
// TODO(v4): V4 SDK 接口清单待确认——路径前缀/鉴权/响应结构可能与 8000 不同，
// 确认后统一在此调整（必要时仿 v2route.go 加翻译层）。
func NewV4Client(deviceIP string) *Client {
	return NewV4ClientWithTimeout(deviceIP, defaultTimeout)
}

// NewV4ClientWithTimeout 同 NewV4Client，带自定义超时（长耗时操作如格式化/上传）
func NewV4ClientWithTimeout(deviceIP string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		BaseURL: fmt.Sprintf("http://%s:%d", deviceIP, DevicePortV4),
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: DeviceTransport,
		},
	}
}

// 公网网关地址
// cloudBaseURL 公网模式 B端直连地址（设备 API 透传）
const cloudBaseURL = "http://119.96.73.226:8892"

// NewPublicClient 创建公网模式客户端（走网关 /v1/devices/{deviceID}{path} 透传到设备）。
// deviceID 为目标设备 ID；token 为网关 access_token；devicePassword 为设备 SDK 密码（可空）。
// 接口路径与本地直连一致（设备 SDK 原路径），网关原样透传。
func NewPublicClient(deviceID, token, devicePassword string) *Client {
	return &Client{
		BaseURL:    cloudBaseURL,
		deviceID:   deviceID,
		token:      token,
		isPublic:   true,
		Password:   devicePassword,
		httpClient: &http.Client{Timeout: defaultTimeout, Transport: DeviceTransport},
	}
}

// NewPublicClientWithTimeout 公网模式带超时
func NewPublicClientWithTimeout(deviceID, token, devicePassword string, timeout time.Duration) *Client {
	return &Client{
		BaseURL:    cloudBaseURL,
		deviceID:   deviceID,
		token:      token,
		isPublic:   true,
		Password:   devicePassword,
		httpClient: &http.Client{Timeout: timeout, Transport: DeviceTransport},
	}
}

// NewErrorClient 创建一个"错误客户端"，所有请求立即返回指定错误。
// 用于空标识符等场景，避免 http://:8000 无意义连接刷屏。
func NewErrorClient(msg string) *Client {
	return &Client{errorMsg: msg}
}

// buildURL 按模式拼接请求 URL：
// public 走网关 /v1/devices/{deviceID}/passthrough{path}（path 为设备 SDK 原路径，网关经 /passthrough 透传到设备）
// local 走设备 BaseURL+path
func (c *Client) buildURL(path string) string {
	if c.isPublic {
		return c.BaseURL + "/v1/devices/" + c.deviceID + "/passthrough" + path
	}
	return c.BaseURL + path
}

// setAuth 按模式设置鉴权：public 走网关 Bearer + X-Device-Password，local 走 Basic Auth
func (c *Client) setAuth(req *http.Request) {
	if c.isPublic {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "*/*")
		if c.Password != "" {
			req.Header.Set("X-Device-Password", c.Password)
		}
	} else if c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
}

// DoRequest 通用设备请求（导出版，供 SdkApiServiceWrapper 代理调用）。
// method: GET/POST/PUT/DELETE；path: 设备 SDK 路径；body: POST/PUT 请求体 JSON（可为空）。
// local/public 模式由 buildURL/setAuth 自动适配；v2 模式由底层 get/postJSON 等
// 入口统一翻译（v2route.go，覆盖 Client 自身方法与 DeviceFetch 全部调用方）。
func (c *Client) DoRequest(method, path string, body []byte) ([]byte, error) {
	switch method {
	case "GET":
		return c.get(path)
	case "POST":
		if len(body) > 0 {
			return c.postJSON(path, json.RawMessage(body))
		}
		return c.postJSON(path, nil)
	case "PUT":
		if len(body) > 0 {
			return c.putJSON(path, json.RawMessage(body))
		}
		return c.putJSON(path, nil)
	case "DELETE":
		return c.deleteWithQuery(path)
	}
	return nil, fmt.Errorf("unsupported method: %s", method)
}
