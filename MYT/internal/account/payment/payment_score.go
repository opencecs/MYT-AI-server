package payment

import (
	"encoding/json"
	"fmt"
)

// UserAtt 用户属性（积分等）
type UserAtt struct {
	Score json.Number `json:"score"`
}

// FloatScore 返回积分数值（API可能返回字符串或数字）
func (u *UserAtt) FloatScore() float64 {
	f, _ := u.Score.Float64()
	return f
}

// GetUserAtt 获取用户属性（积分等）
func (s *Service) GetUserAtt(token string) (*UserAtt, error) {
	data := map[string]string{"token": token}
	body, err := s.postAPI("get_user_att", data)
	if err != nil {
		return nil, fmt.Errorf("获取用户属性失败: %w", err)
	}

	var result struct {
		Code any    `json:"code"`
		Msg  string `json:"msg"`
		Data UserAtt `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析用户属性失败: %w", err)
	}
	if !isSuccessCode(result.Code) {
		return nil, fmt.Errorf("获取用户属性失败: %s", result.Msg)
	}
	return &result.Data, nil
}

// ScoreExchangeRequest 积分兑换请求
type ScoreExchangeRequest struct {
	Rabbet  string `json:"rabbet"`
	Package string `json:"package"`
	Money   string `json:"money"`
	PayType string `json:"paytype"`
	Token   string `json:"token"`
}

// ScoreExchangeTermTime 积分支付兑换实例时长
func (s *Service) ScoreExchangeTermTime(req ScoreExchangeRequest) error {
	data := map[string]string{
		"rabbet":  req.Rabbet,
		"package": req.Package,
		"money":   req.Money,
		"paytype": req.PayType,
		"token":   req.Token,
	}
	body, err := s.postAPI("score_exchange_term_time", data)
	if err != nil {
		return fmt.Errorf("积分支付失败: %w", err)
	}

	var result struct {
		Code any    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析积分支付响应失败: %w", err)
	}
	if !isSuccessCode(result.Code) {
		return fmt.Errorf("积分支付失败: %s", result.Msg)
	}
	return nil
}
