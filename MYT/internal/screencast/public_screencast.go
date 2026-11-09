// Package screencast 公网投屏：通过 8081 网关 mobile API 获取 RTSP 端口组，
// go_legacy 以本地模式同款参数（-ip -vport -cport -rtsp_tcp）直连投屏。
//
// 鉴权：Authorization: Bearer 网关 session JWT（由 tokenFn 注入，
// 与 cloudapi 适配层共用 mytAgentSvc.GetSessionToken，实测两网关互通）。
// 设备 RTSP 侧的 Digest 认证（u001/p001 固件默认）由 go_legacy 自动应答，客户端无感。
package screencast

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/moyunteng/myt/internal/cloudapi"
)

// mobileGatewayURL B 端网关（设备列表/SDK 透传/mobile 端口），与 cloudapi 包保持同源。
const mobileGatewayURL = cloudapi.GatewayURL

// PublicScreencastMgr 公网投屏管理器
type PublicScreencastMgr struct {
	// tokenFn 返回网关 session JWT（由 setup.go 注入 mytAgentSvc.GetSessionToken）
	tokenFn func() (string, error)
	client  *http.Client
}

// NewPublicScreencastMgr 创建公网投屏管理器。
// credentialsFn 保留以兼容旧签名（返回账号密码），内部不再使用。
func NewPublicScreencastMgr(credentialsFn func() (string, string), tokenFn func() (string, error)) *PublicScreencastMgr {
	return &PublicScreencastMgr{
		tokenFn: tokenFn,
		client:  &http.Client{Timeout: 20 * time.Second},
	}
}

// SetTokenFn 注入授权 token 获取函数
func (m *PublicScreencastMgr) SetTokenFn(fn func() (string, error)) { m.tokenFn = fn }

// ensureToken 获取授权 token
func (m *PublicScreencastMgr) ensureToken() (string, error) {
	if m.tokenFn == nil {
		return "", fmt.Errorf("未配置授权 token 获取方式")
	}
	tok, err := m.tokenFn()
	if err != nil {
		return "", fmt.Errorf("网关鉴权失败: %w", err)
	}
	if tok == "" {
		return "", fmt.Errorf("授权 token 为空")
	}
	return tok, nil
}

// SessionToken 返回当前网关 session JWT
func (m *PublicScreencastMgr) SessionToken() (string, error) {
	return m.ensureToken()
}

// MobilePorts 网关 mobile API 返回的 RTSP 端口组
type MobilePorts struct {
	VideoHost   string
	VideoPort   int
	ControlHost string
	ControlPort int
	ExpiresIn   int // 秒，实测 600
}

// GetMobilePorts 调 GET 网关 /v1/devices/{id}/mobile?index={n} 获取投屏端口组。
// 网关实测偶发 500/502/超时，失败按 2s/4s 间隔重试，最多 3 次。
func (m *PublicScreencastMgr) GetMobilePorts(deviceID string, index int) (*MobilePorts, error) {
	url := fmt.Sprintf("%s/v1/devices/%s/mobile?index=%d", mobileGatewayURL, deviceID, index)
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		mp, err := m.fetchMobilePorts(url)
		if err == nil {
			return mp, nil
		}
		lastErr = err
		log.Printf("[screencast-public] mobile 端口第 %d 次获取失败: %v", attempt, err)
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}
	return nil, fmt.Errorf("获取 %s 坑位 %d 投屏端口失败: %w", deviceID, index, lastErr)
}
func (m *PublicScreencastMgr) fetchMobilePorts(url string) (*MobilePorts, error) {
	tok, err := m.ensureToken()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		body := string(raw)
		if len(body) > 200 {
			body = body[:200]
		}
		return nil, fmt.Errorf("mobile API HTTP %d: %s", resp.StatusCode, body)
	}
	var result struct {
		VideoHost   string `json:"video_host"`
		VideoPort   int    `json:"video_port"`
		ControlHost string `json:"control_host"`
		ControlPort int    `json:"control_port"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("解析 mobile 响应失败: %w", err)
	}
	if result.VideoPort == 0 || result.ControlPort == 0 {
		return nil, fmt.Errorf("mobile 响应缺少 video/control 端口: %s", string(raw))
	}
	if result.VideoHost == "" {
		result.VideoHost = result.ControlHost
	}
	if result.ExpiresIn <= 0 {
		result.ExpiresIn = 600
	}
	log.Printf("[screencast-public] mobile 端口: video=%s:%d control=%s:%d expires=%ds",
		result.VideoHost, result.VideoPort, result.ControlHost, result.ControlPort, result.ExpiresIn)
	return &MobilePorts{
		VideoHost:   result.VideoHost,
		VideoPort:   result.VideoPort,
		ControlHost: result.ControlHost,
		ControlPort: result.ControlPort,
		ExpiresIn:   result.ExpiresIn,
	}, nil
}
