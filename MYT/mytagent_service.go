package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// MytAgentService MYT Agent 远程服务接入层。
//
// 鉴权：调 119.96.73.226:8891 /api/ai/login（账号密码）拿网关签发的 session JWT，
// 该 JWT 用于 WS/HTTP 调智能体网关接口。网关只认它自己签发的 session_token。
type MytAgentService struct {
	cm         *config.Manager
	app        *application.App
	client     *http.Client
	paymentSvc *PaymentServiceWrapper // 预留（账号密码来源），当前不使用

	mu           sync.RWMutex
	sessionToken string // 网关签发的 session JWT（15 分钟有效）
	refreshToken string // 续期 token（7 天有效，一次性轮换）
	uid          string
	tokenAt      time.Time
	refreshMu    sync.Mutex // 串行化 refresh（refresh_token 一次性，防并发烧掉）
}

// AgentConnInfo 前端连接 MYT Agent 所需的信息
type AgentConnInfo struct {
	BaseURL      string `json:"baseUrl"`
	WsURL        string `json:"wsUrl"`
	EventsURL    string `json:"eventsUrl"`
	SessionToken string `json:"sessionToken"`
	UID          string `json:"uid"`
}

func NewMytAgentService(cm *config.Manager) *MytAgentService {
	return &MytAgentService{
		cm:     cm,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *MytAgentService) SetApp(app *application.App) { s.app = app }

// SetPaymentService 注入授权服务（提供统一鉴权 token）。
func (s *MytAgentService) SetPaymentService(p *PaymentServiceWrapper) { s.paymentSvc = p }

// GetAuthCredentials 返回魔云腾账号密码（供 cloudapi 网关登录复用）
func (s *MytAgentService) GetAuthCredentials() (string, string) {
	return s.cm.GetAuthCredentials()
}

// ensureToken 返回有效的网关 session JWT。
// session 未过期直接返回；过期先用 refresh_token 续期（/api/ai/refresh）；
// refresh 失败或无 refresh 时回退账号密码重登。
func (s *MytAgentService) ensureToken() (string, error) {
	s.mu.RLock()
	tok := s.sessionToken
	age := time.Since(s.tokenAt)
	rt := s.refreshToken
	s.mu.RUnlock()
	if tok != "" && age < 10*time.Minute {
		return tok, nil
	}

	// 串行续期：refresh_token 一次性，防并发烧掉
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	// double-check：等待锁期间可能已有其他 goroutine 完成续期
	s.mu.RLock()
	tok = s.sessionToken
	age = time.Since(s.tokenAt)
	rt = s.refreshToken
	s.mu.RUnlock()
	if tok != "" && age < 10*time.Minute {
		return tok, nil
	}

	// 优先用 refresh_token 续期
	if rt != "" {
		if newTok, _, err := s.refresh(rt); err == nil && newTok != "" {
			return newTok, nil
		}
	}

	// refresh 失败或无 refresh：账号密码重登
	uname, pwd := s.cm.GetAuthCredentials()
	if uname == "" || pwd == "" {
		return "", errNotLoggedIn
	}
	if err := s.login(uname, pwd); err != nil {
		return "", err
	}
	s.mu.RLock()
	tok = s.sessionToken
	s.mu.RUnlock()
	return tok, nil
}

// refresh 调 POST /api/ai/refresh 用 refresh_token 换新 session_token + 新 refresh_token。
func (s *MytAgentService) refresh(refreshToken string) (string, string, error) {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	resp, err := s.client.Post("http://119.96.73.226:8891/api/ai/refresh", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return "", "", fmt.Errorf("MYT Agent 续期请求失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("MYT Agent 续期失败 (HTTP %d)", resp.StatusCode)
	}
	var result struct {
		OK           bool   `json:"ok"`
		Error        string `json:"error"`
		SessionToken string `json:"session_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("MYT Agent 续期响应解析失败: %w", err)
	}
	if !result.OK || result.SessionToken == "" {
		if result.Error != "" {
			return "", "", fmt.Errorf("MYT Agent 续期失败: %s", result.Error)
		}
		return "", "", fmt.Errorf("MYT Agent 续期失败")
	}
	s.mu.Lock()
	s.sessionToken = result.SessionToken
	if result.RefreshToken != "" {
		s.refreshToken = result.RefreshToken
	}
	s.tokenAt = time.Now()
	s.mu.Unlock()
	return result.SessionToken, result.RefreshToken, nil
}

// login 调 119.96.73.226:8891 /api/ai/login 登录网关，拿网关签发的 session JWT。
func (s *MytAgentService) login(username, password string) error {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := s.client.Post("http://119.96.73.226:8891/api/ai/login", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("MYT Agent 登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("MYT Agent 登录失败 (HTTP %d)", resp.StatusCode)
	}

	var result struct {
		OK           bool   `json:"ok"`
		Message      string `json:"message"`
		UID          string `json:"uid"`
		SessionToken string `json:"session_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("MYT Agent 登录响应解析失败: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("MYT Agent 登录失败: %s", result.Message)
	}
	if result.SessionToken == "" {
		return fmt.Errorf("MYT Agent 登录返回空 token")
	}

	s.mu.Lock()
	s.sessionToken = result.SessionToken
	s.refreshToken = result.RefreshToken
	s.uid = result.UID
	s.tokenAt = time.Now()
	s.mu.Unlock()

	return nil
}

// GetConnectionInfo 返回前端连接 MYT Agent 的信息（自动登录/刷新网关 session JWT）
func (s *MytAgentService) GetConnectionInfo() (*AgentConnInfo, error) {
	tok, err := s.ensureToken()
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	uid := s.uid
	s.mu.RUnlock()
	return s.buildConnInfo(tok, uid), nil
}

// GetSessionToken 返回当前网关 session JWT（供 cloudapi 适配层等复用）。
func (s *MytAgentService) GetSessionToken() (string, error) {
	return s.ensureToken()
}

func (s *MytAgentService) buildConnInfo(token, uid string) *AgentConnInfo {
	base := "http://119.96.73.226:8891"
	wsBase := strings.Replace(base, "http://", "ws://", 1)
	return &AgentConnInfo{
		BaseURL:      base,
		WsURL:        wsBase + "/ws?token=" + token,
		EventsURL:    wsBase + "/events?token=" + token,
		SessionToken: token,
		UID:          uid,
	}
}

// --- HTTP/WS 代理方法已拆分到 mytagent_service_proxy.go ---
// HttpProxy / HttpProxyBlob / AsrRecognize / HttpProxyUpload
