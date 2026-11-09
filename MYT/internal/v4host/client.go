package v4host

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

const (
	// DefaultPort V4 设备管理服务端口（V4 SDK 监听 8088，提供 GET /health）
	DefaultPort = 8088
	// healthPath V4 设备健康检查接口
	healthPath = "/health"
	// healthTimeout 单台探测超时：局域网 /health 正常 300ms 内回，1.5s 足以容忍
	// TCP 重传（RTO 首次重传约 1s）；4s 太保守会让不可达候选白拖整轮扫描
	healthTimeout = 1500 * time.Millisecond
	// healthQuickTimeout V2/V3 名候选的降级探测超时：这些候选大概率不是 tebox
	//（仅广播名后缀命中，8088 多半无服务），用长超时逐台白等会把整轮扫描拖慢；
	// 真正装了 tebox 的 V3 盒子 /health 在局域网内 300ms 内必回，500ms 已足够，
	// 误杀概率可忽略（见 scan.go 的降级探测说明）
	healthQuickTimeout = 500 * time.Millisecond
	// rawLogLimit 原始响应体日志上限（首次接入期按实际格式微调用，不刷屏）
	rawLogLimit = 512
	// maxBodySize 响应体读取上限（/health 预期是几百字节，防异常大包）
	maxBodySize = 64 << 10
)

// Info V4 设备 /health 探测结果。
//
// V4 契约已确认（tobox_player/api.json）：/health 只返回 {"status":"ok"}，**无设备标识**。
// 因此 DeviceID 恒等于 IP（DeviceIDFromIP 恒 true，保留字段仅为兼容既有调用方/前端类型）。
type Info struct {
	IP             string `json:"ip"`
	DeviceID       string `json:"deviceId"`
	DeviceName     string `json:"deviceName"`
	Model          string `json:"model"`
	DeviceIDFromIP bool   `json:"deviceIdFromIp"`
	RawBody        string `json:"rawBody"`
}

// probeTransport 内网直连共享 Transport：不走系统代理（代理会让局域网探测全部失败）
var probeTransport = &http.Transport{
	Proxy: nil,
	DialContext: (&net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:        64,
	MaxIdleConnsPerHost: 4,
	IdleConnTimeout:     60 * time.Second,
	DisableCompression:  true,
}

var healthClient = &http.Client{Transport: probeTransport}

// Health 探测单台 V4 设备：GET http://{ip}:8088/health。
// 判定：HTTP 200 + 响应体非空 + 合法 JSON 对象，三者缺一即失败（空体/非 JSON 也算失败）。
func Health(ctx context.Context, ip string) (*Info, error) {
	return healthAtTimeout(ctx, healthURL(ip), ip, healthTimeout)
}

// HealthQuick Health 的短超时变体（500ms）：用于扫描时对「广播名后缀判为
// V2/V3」候选的降级探测——这些候选大概率 8088 上没有 tebox，短超时把探测失败
// 的白等压到最低；已在列表的候选更不会走到这里（扫描层先按 IP 排除）。
// 判定标准与 Health 完全一致，只是超时更短。
func HealthQuick(ctx context.Context, ip string) (*Info, error) {
	return healthAtTimeout(ctx, healthURL(ip), ip, healthQuickTimeout)
}

// healthURL 拼接 /health 地址（导出给日志/测试复用）
func healthURL(ip string) string {
	return fmt.Sprintf("http://%s:%d%s", ip, DefaultPort, healthPath)
}

// healthAt 对指定 URL 做一次健康检查（url 与 ip 分离，便于 httptest 注入）
func healthAt(ctx context.Context, url, ip string) (*Info, error) {
	return healthAtTimeout(ctx, url, ip, healthTimeout)
}

// healthAtTimeout 对指定 URL 做一次健康检查，超时由调用方指定
// （url 与 ip 分离、超时参数化，便于 httptest 注入与短超时变体复用）
func healthAtTimeout(ctx context.Context, url, ip string, timeout time.Duration) (*Info, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 /health 请求失败: %w", err)
	}
	resp, err := healthClient.Do(req)
	if err != nil {
		log.Printf("[v4] /health 请求失败: ip=%s url=%s err=%v", ip, url, err)
		return nil, fmt.Errorf("/health 不可达: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		log.Printf("[v4] /health 读取响应失败: ip=%s err=%v", ip, err)
		return nil, fmt.Errorf("/health 读取响应失败: %w", err)
	}
	// 首次接入期必须看到真实响应体：V4 /health 字段格式未定稿，日志是唯一的
	// 格式校准依据（只打前 512 字节，避免异常大包刷屏）
	log.Printf("[v4] /health 响应: ip=%s status=%d body=%q", ip, resp.StatusCode, truncateBody(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/health 返回 HTTP %d", resp.StatusCode)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("/health 返回空响应体")
	}
	info, err := parseInfo(body, ip)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// truncateBody 截断到 rawLogLimit 字节（按字节安全边界，不切坏 UTF-8 无妨——仅日志用）
func truncateBody(body []byte) string {
	if len(body) <= rawLogLimit {
		return string(body)
	}
	return string(body[:rawLogLimit]) + "...(truncated)"
}
