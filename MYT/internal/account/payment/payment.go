package payment

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/moyunteng/myt/internal/account/slot"
	"github.com/moyunteng/myt/internal/auth"
	"github.com/moyunteng/myt/internal/util"
)

var ErrTokenExpired = fmt.Errorf("token已过期")
var ErrAPIFailed = fmt.Errorf("API请求失败")

// truncateBody 截断 body 日志，避免输出过长
func truncateBody(body []byte) string {
	if len(body) > 500 {
		return string(body[:500]) + "..."
	}
	return string(body)
}

// token过期相关的错误码/消息关键词
var tokenExpiredIndicators = []string{"token", "过期", "expired", "unauthorized", "未登录"}

func isSuccessCode(code any) bool {
	return util.IsSuccessCode(code)
}

// isTokenExpiredError 判断错误信息是否指示 token 过期
func isTokenExpiredError(body []byte) bool {
	lower := strings.ToLower(string(body))
	for _, indicator := range tokenExpiredIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// checkResponse 检查 API 响应的 code 字段，区分 token 过期和普通失败
// 优先从 body 中提取 msg 字段作为用户友好的错误信息
func checkResponse(code any, body []byte) error {
	if isSuccessCode(code) {
		return nil
	}
	if isTokenExpiredError(body) {
		return ErrTokenExpired
	}
	codeStr := fmt.Sprintf("%v", code)
	// 尝试从响应体中提取 msg 字段，给前端展示更友好的错误信息
	var respWithMsg struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal(body, &respWithMsg) == nil && respWithMsg.Msg != "" {
		return fmt.Errorf("操作失败: %s", respWithMsg.Msg)
	}
	bodyPreview := string(body)
	if len(bodyPreview) > 200 {
		bodyPreview = bodyPreview[:200] + "..."
	}
	return fmt.Errorf("API请求失败(code=%s): %s", codeStr, bodyPreview)
}

type Service struct {
	apiURL  string
	client  *http.Client
	slotSvc *slot.Service
}

func NewService() *Service {
	return &Service{
		apiURL: "https://www.moyunteng.com/api/api.php",
		client: util.NewHTTPClient(),
	}
}

func (s *Service) SetSlotService(svc *slot.Service) {
	s.slotSvc = svc
}

// Login 登录获取 token（委托给 auth.LoginAPI）
func (s *Service) Login(username, password string) (string, error) {
	result, err := s.LoginFull(username, password)
	if err != nil {
		return "", err
	}
	return result.Token, nil
}

// LoginFull 登录并返回完整结果（含 uid）。
// auth.LoginAPI 本就返回 uid，Login 此前丢弃了它；客户端启动上报需要 uid，
// 故单独暴露完整结果，Login 保持原签名以免影响既有调用方。
func (s *Service) LoginFull(username, password string) (*auth.LoginResult, error) {
	result, err := auth.LoginAPI(s.client, username, password)
	if err != nil {
		return nil, fmt.Errorf("%s", util.FriendlyNetError(err))
	}
	if result.Code != "200" {
		return nil, fmt.Errorf("%s", translateLoginCode(result.Code, result.Msg))
	}
	return result, nil
}

func translateLoginCode(code, msg string) string {
	switch code {
	case "3036":
		return "账号不存在"
	case "3037":
		return "密码错误"
	default:
		if msg != "" {
			return msg
		}
		return "登录失败"
	}
}

// GetPhoneVCode 获取手机验证码，返回 vkey
func (s *Service) GetPhoneVCode(phone string) (string, error) {
	result, err := auth.GetPhoneVCodeAPI(s.client, phone)
	if err != nil {
		return "", fmt.Errorf("%s", util.FriendlyNetError(err))
	}
	if !util.IsSuccessCode(result.Code) {
		msg := result.Msg
		if msg == "" {
			msg = "获取验证码失败"
		}
		return "", fmt.Errorf("%s", msg)
	}
	return result.VKey, nil
}

// Register 用户注册
func (s *Service) Register(phone, password, vcode, vkey string) error {
	result, err := auth.RegisterAPI(s.client, phone, password, vcode, vkey)
	if err != nil {
		return fmt.Errorf("%s", util.FriendlyNetError(err))
	}
	if !util.IsSuccessCode(result.Code) {
		msg := result.Msg
		if msg == "" {
			msg = "注册失败"
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// ResetPassword 重置密码（忘记密码）
func (s *Service) ResetPassword(phone, password, vcode, vkey string) error {
	result, err := auth.ResetPasswordAPI(s.client, phone, password, vcode, vkey)
	if err != nil {
		return fmt.Errorf("%s", util.FriendlyNetError(err))
	}
	if !util.IsSuccessCode(result.Code) {
		msg := result.Msg
		if msg == "" {
			msg = "重置密码失败"
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (s *Service) postAPI(apiType string, data map[string]string) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化请求数据失败: %w", err)
	}
	log.Printf("[payment] postAPI: type=%s, data=%s", apiType, string(jsonData)[:min(len(jsonData), 300)])
	params := url.Values{}
	params.Set("type", apiType)
	params.Set("data", string(jsonData))
	return util.MoyuntengPost(s.client, s.apiURL, params)
}

// Package 套餐
type Package struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Price string `json:"price"`
	Day   string `json:"day,omitempty"`
}

// DeviceBindInfo 设备绑定状态
type DeviceBindInfo struct {
	DeviceID   string `json:"device_id"`
	BindStatus int    `json:"bind_status"` // 0=未绑定, 1=已绑定(当前账号), 2=被其他账号绑定
}

// SlotExpireInfo 坑位到期信息
type SlotExpireInfo struct {
	SlotIndex int    `json:"slot_index"`
	ExpireTs  string `json:"expire_ts"`  // Unix秒级时间戳字符串，"0"表示无/已过期
	State     int    `json:"state"`      // 0=正常, 1=即将到期(3天内), 2=已过期
}

// DeviceSlotInfo 设备坑位到期信息
type DeviceSlotInfo struct {
	DeviceID string          `json:"device_id"`
	Slots    []SlotExpireInfo `json:"slots"`
}

// OrderResult 订单结果
type OrderResult struct {
	Code    string `json:"code"`
	Msg     string `json:"msg"`
	QRCode  string `json:"qrcode"`
	OrderID string `json:"order_id,omitempty"`
}

// OrderStatus 订单状态
type OrderStatus struct {
	State   int    `json:"state"`
	OrderID string `json:"id"`
}

// GetPackages/GetDeviceBindStatus/BindHost/UnbindHost/GetSlotExpireInfo 已拆分到 payment_package.go
// CreateOrder/QueryOrderStatus 已拆分到 payment_order.go
