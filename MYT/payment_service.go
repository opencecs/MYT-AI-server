package main

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/account/payment"
	"github.com/moyunteng/myt/internal/clientreport"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/util"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const tokenRefreshInterval = 30 * time.Minute

// errNotLoggedIn 未登录哨兵错误。需要授权 token 的功能收到此错误时，
// 前端应识别“请先登录”弹登录弹窗。
var errNotLoggedIn = errors.New("请先登录魔云腾账号")

type PaymentServiceWrapper struct {
	service      *payment.Service
	config       *config.Manager
	app          *application.App
	discoverSvc  *DiscoverServiceWrapper
	appSvc       *AppService            // 用于退出登录时自动切回 local 模式
	clientReport *clientreport.Reporter // 客户端启动上报（登录成功后补报 uid）
	token        string                 // moyunteng.com 授权 token（授权页数据用）
	uid          string                 // 当前登录账号 uid（供启动上报使用）
	tokenMu      sync.RWMutex
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

func NewPaymentServiceWrapper(s *payment.Service, cm *config.Manager) *PaymentServiceWrapper {
	return &PaymentServiceWrapper{service: s, config: cm, stopCh: make(chan struct{})}
}

func (s *PaymentServiceWrapper) SetApp(app *application.App) {
	s.app = app
}

func (s *PaymentServiceWrapper) SetDiscoverService(ds *DiscoverServiceWrapper) {
	s.discoverSvc = ds
}

// SetAppService 注入 AppService（退出登录时若在公网模式则自动切回 local）。
func (s *PaymentServiceWrapper) SetAppService(a *AppService) { s.appSvc = a }

// setClientReport 注入客户端启动上报器：登录成功后触发一次补报，让上报带上 uid。
// 刻意不导出：Reporter 是 internal 类型，导出会让 wails bindings 去生成前端无法表达的类型。
func (s *PaymentServiceWrapper) setClientReport(r *clientreport.Reporter) {
	s.clientReport = r
}

// currentUID 返回当前登录账号 uid（未登录返回空串），供客户端启动上报使用。
func (s *PaymentServiceWrapper) currentUID() string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return s.uid
}

// StartAutoRefresh 每30分钟主动刷新 token，同步刷新 AI token
func (s *PaymentServiceWrapper) StartAutoRefresh() {
	s.wg.Add(1)
	util.GoSafe(func() {
		defer s.wg.Done()
		ticker := time.NewTicker(tokenRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.refreshToken()
			case <-s.stopCh:
				return
			}
		}
	})
}

func (s *PaymentServiceWrapper) StopAutoRefresh() {
	close(s.stopCh)
	s.wg.Wait()
}

// refreshToken 用保存的账号密码重新登录
func (s *PaymentServiceWrapper) refreshToken() (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	s.token = ""
	uname, pwd := s.config.GetAuthCredentials()
	if uname == "" || pwd == "" {
		return "", errNotLoggedIn
	}
	result, err := s.service.LoginFull(uname, pwd)
	if err != nil {
		return "", err
	}
	s.token = result.Token
	s.uid = result.Uid

	return result.Token, nil
}

// ensureToken 确保有有效的 token，没有则自动用保存的账号密码登录
func (s *PaymentServiceWrapper) ensureToken() (string, error) {
	s.tokenMu.RLock()
	if s.token != "" {
		t := s.token
		s.tokenMu.RUnlock()
		return t, nil
	}
	s.tokenMu.RUnlock()

	return s.refreshToken()
}

// GetToken 返回当前有效的授权 token（moyunteng.com 签发，供授权页数据使用）。
// 注意：智能体/73网关/公网投屏已改用智能体网关 session JWT（见 MytAgentService.GetSessionToken），
// 此处保留用于兼容历史调用方。未登录时返回 errNotLoggedIn。
func (s *PaymentServiceWrapper) GetToken() (string, error) {
	s.tokenMu.RLock()
	tok := s.token
	s.tokenMu.RUnlock()
	if tok != "" {
		return tok, nil
	}
	uname, pwd := s.config.GetAuthCredentials()
	if uname == "" || pwd == "" {
		return "", errNotLoggedIn
	}
	return s.refreshToken()
}

// withRetry 执行 API 调用，若 token 过期则自动刷新后重试一次
func (s *PaymentServiceWrapper) withRetry(fn func(string) error) error {
	token, err := s.ensureToken()
	if err != nil {
		return err
	}
	err = fn(token)
	if err != nil && errors.Is(err, payment.ErrTokenExpired) {
		token, err2 := s.refreshToken()
		if err2 != nil {
			return err2
		}
		return fn(token)
	}
	return err
}

// Login 用提供的账号密码登录，AI token 后台异步刷新
func (s *PaymentServiceWrapper) Login(username, password string) error {
	result, loginErr := s.service.LoginFull(username, password)
	if loginErr != nil {
		return loginErr
	}

	s.tokenMu.Lock()
	s.token = result.Token
	s.uid = result.Uid
	s.tokenMu.Unlock()
	if err := s.config.SetAuthCredentials(username, password); err != nil {
		return err
	}

	// 登录成功，立即通知前端
	if s.app != nil {
		s.app.Event.Emit("auth:status", true)
	}

	// 登录成功，启动绑定状态 30 分钟定时轮询（全量批量单请求）
	if s.discoverSvc != nil {
		s.discoverSvc.StartBindStatusPoll()
	}

	// 启动上报在启动时可能没有 uid，登录后补报一次（当天已用相同 uid 报过则内部跳过）
	if s.clientReport != nil {
		s.clientReport.ReportNow("login")
	}

	return s.config.Save()
}

// Logout 清除 token 和登录凭证（记住的账号密码不受影响）
func (s *PaymentServiceWrapper) Logout() {
	s.tokenMu.Lock()
	s.token = ""
	s.uid = ""
	s.tokenMu.Unlock()
	if err := s.config.ClearAuthCredentials(); err != nil {
		log.Printf("[payment] 清除认证凭证失败: %v", err)
	}
	if s.discoverSvc != nil {
		s.discoverSvc.StopBindStatusPoll()
	}
	// 公网模式依赖登录态，退出登录后自动切回本地模式
	if s.appSvc != nil && s.config.GetDeployMode() == "public" {
		if err := s.appSvc.SetDeployMode("local"); err != nil {
			log.Printf("[payment] 退出登录后切回本地模式失败: %v", err)
		}
	}
	if s.app != nil {
		s.app.Event.Emit("auth:status", false)
	}
}

// OnCacheCleared 清理缓存后的登出同步（凭证已被 ClearAllCache 清除，只需同步内存状态）
func (s *PaymentServiceWrapper) OnCacheCleared() {
	s.tokenMu.Lock()
	s.token = ""
	s.uid = ""
	s.tokenMu.Unlock()
	if s.discoverSvc != nil {
		s.discoverSvc.StopBindStatusPoll()
	}
	if s.app != nil {
		s.app.Event.Emit("auth:status", false)
	}
}

// GetPhoneVCode 获取手机验证码，返回 vkey
func (s *PaymentServiceWrapper) GetPhoneVCode(phone string) (string, error) {
	return s.service.GetPhoneVCode(phone)
}

// CancelUser 注销当前账号（不可逆）：需短信验证码，成功后清除登录态。
// 清理流程与 Logout 一致（token/凭证/绑定轮询/切回本地模式/auth:status 事件）。
func (s *PaymentServiceWrapper) CancelUser(vcode, vkey string) (*payment.CancelAccountResult, error) {
	var result *payment.CancelAccountResult
	err := s.withRetry(func(token string) error {
		r, e := s.service.CancelUser(token, vcode, vkey)
		if e != nil {
			return e
		}
		result = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 注销成功即退出登录：清 token/凭证、停轮询、公网模式切回本地、通知前端
	s.Logout()
	log.Printf("[payment] 账号已注销: uid=%s, host_num=%d", result.Uid, result.HostNum)
	return result, nil
}

// Register 用户注册
func (s *PaymentServiceWrapper) Register(phone, password, vcode, vkey string) error {
	return s.service.Register(phone, password, vcode, vkey)
}

// ResetPassword 重置密码（忘记密码）
func (s *PaymentServiceWrapper) ResetPassword(phone, password, vcode, vkey string) error {
	return s.service.ResetPassword(phone, password, vcode, vkey)
}

// GetRememberedCredentials 获取记住的账号密码，返回 "username\npassword"
func (s *PaymentServiceWrapper) GetRememberedCredentials() string {
	uname, pwd := s.config.GetRememberedCredentials()
	if uname == "" {
		return ""
	}
	return uname + "\n" + pwd
}

// ClearRememberedCredentials 清除记住的账号密码
func (s *PaymentServiceWrapper) ClearRememberedCredentials() {
	s.config.ClearRememberedCredentials()
}

// SetRememberedCredentials 保存记住的账号密码
func (s *PaymentServiceWrapper) SetRememberedCredentials(username, password string) {
	s.config.SetRememberedCredentials(username, password)
}

// AutoLogin 如果有记住的账号密码，自动登录
func (s *PaymentServiceWrapper) AutoLogin() bool {
	rUname, rPwd := s.config.GetRememberedCredentials()
	if rUname == "" || rPwd == "" {
		return false
	}
	if err := s.Login(rUname, rPwd); err != nil {
		return false
	}
	return true
}

// IsLoggedIn 返回是否已登录（有保存的凭证）
func (s *PaymentServiceWrapper) IsLoggedIn() bool {
	uname, pwd := s.config.GetAuthCredentials()
	return uname != "" && pwd != ""
}

// GetUsername 返回当前登录的用户名
func (s *PaymentServiceWrapper) GetUsername() string {
	uname, _ := s.config.GetAuthCredentials()
	return uname
}

func (s *PaymentServiceWrapper) GetPackages() ([]payment.Package, error) {
	var result []payment.Package
	err := s.withRetry(func(token string) error {
		pkgs, e := s.service.GetPackages(token)
		if e != nil {
			return e
		}
		result = pkgs
		return nil
	})
	return result, err
}

// GetDeviceBindStatus 查询设备绑定状态
func (s *PaymentServiceWrapper) GetDeviceBindStatus(deviceIDs []string) ([]payment.DeviceBindInfo, error) {
	var result []payment.DeviceBindInfo
	err := s.withRetry(func(token string) error {
		r, e := s.service.GetDeviceBindStatus(deviceIDs, token)
		if e != nil {
			return e
		}
		result = r
		return nil
	})
	return result, err
}

// BindHost 绑定单台设备到当前账号
func (s *PaymentServiceWrapper) BindHost(deviceID string) error {
	err := s.withRetry(func(token string) error {
		return s.service.BindHost(deviceID, token)
	})
	if err != nil {
		return err
	}
	// 绑定成功，立即更新本地缓存状态
	s.config.UpdateBindStatus(deviceID, 1)
	return nil
}

// UnbindHost 批量解绑设备，需要短信验证码（vcode/vkey）
func (s *PaymentServiceWrapper) UnbindHost(deviceIDs []string, vcode, vkey string) error {
	err := s.withRetry(func(token string) error {
		return s.service.UnbindHost(deviceIDs, token, vcode, vkey)
	})
	if err != nil {
		return err
	}
	// 解绑成功，更新本地缓存状态为未绑定
	for _, id := range deviceIDs {
		s.config.UpdateBindStatus(id, 0)
	}
	return nil
}

// GetSlotExpireInfo 查询设备坑位到期信息（无需 token）
func (s *PaymentServiceWrapper) GetSlotExpireInfo(deviceIDs []string) ([]payment.DeviceSlotInfo, error) {
	return s.service.GetSlotExpireInfo(deviceIDs)
}

// CreateOrder/QueryOrderStatus/GetUserAtt/ScoreExchangeTermTime 已拆分到 payment_service_order.go
// SlotServiceWrapper 已拆分到 slot_service.go
