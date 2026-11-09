package main

import "github.com/moyunteng/myt/internal/account/payment"

// --- 订单与积分 ---

func (s *PaymentServiceWrapper) CreateOrder(rabbet, packageID, money, payType string) (*payment.OrderResult, error) {
	var result *payment.OrderResult
	err := s.withRetry(func(token string) error {
		r, e := s.service.CreateOrder(rabbet, packageID, money, payType, token)
		if e != nil {
			return e
		}
		result = r
		return nil
	})
	return result, err
}

func (s *PaymentServiceWrapper) QueryOrderStatus(orderID string) (*payment.OrderStatus, error) {
	var result *payment.OrderStatus
	err := s.withRetry(func(token string) error {
		r, e := s.service.QueryOrderStatus(orderID, token)
		if e != nil {
			return e
		}
		result = r
		return nil
	})
	return result, err
}

func (s *PaymentServiceWrapper) GetUserAtt() (float64, error) {
	token, err := s.ensureToken()
	if err != nil {
		return 0, err
	}
	att, err := s.service.GetUserAtt(token)
	if err != nil {
		return 0, err
	}
	return att.FloatScore(), nil
}

func (s *PaymentServiceWrapper) ScoreExchangeTermTime(rabbet, packageID, money string) error {
	token, err := s.ensureToken()
	if err != nil {
		return err
	}
	return s.service.ScoreExchangeTermTime(payment.ScoreExchangeRequest{
		Rabbet:  rabbet,
		Package: packageID,
		Money:   money,
		PayType: "score",
		Token:   token,
	})
}
