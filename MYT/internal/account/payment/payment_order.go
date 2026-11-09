package payment

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
)

// CreateOrder 创建订单，rabbet 为 JSON 格式 {"设备ID":["坑位号1","坑位号2"]}
func (s *Service) CreateOrder(rabbet, packageID, money, payType, token string) (*OrderResult, error) {
	data := map[string]string{
		"rabbet":  rabbet,
		"package": packageID,
		"money":   money,
		"paytype": payType,
		"token":   token,
	}

	body, err := s.postAPI("get_order_v2", data)
	if err != nil {
		log.Printf("[payment] CreateOrder 请求失败: err=%v", err)
		return nil, err
	}

	var raw struct {
		Code    any             `json:"code"`
		Msg     string          `json:"msg"`
		QRCode  string          `json:"qrcode"`
		OrderID string          `json:"order_id"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析订单响应失败: %w", err)
	}

	if err := checkResponse(raw.Code, body); err != nil {
		if errors.Is(err, ErrTokenExpired) {
			return nil, err
		}
		return nil, fmt.Errorf("创建订单失败: %s", raw.Msg)
	}

	qrcode := raw.QRCode
	orderID := raw.OrderID
	if (orderID == "" || qrcode == "") && len(raw.Data) > 0 {
		// data 可能是对象或数组，兼容处理
		var dataOrderID, dataQRCode string
		if raw.Data[0] == '[' {
			// 数组格式：取第一个元素的 order_id 和 qrcode
			var arr []struct {
				OrderID string `json:"order_id"`
				QRCode  string `json:"qrcode"`
			}
			if err := json.Unmarshal(raw.Data, &arr); err == nil && len(arr) > 0 {
				dataOrderID = arr[0].OrderID
				dataQRCode = arr[0].QRCode
			}
		} else {
			// 对象格式
			var obj struct {
				OrderID string `json:"order_id"`
				QRCode  string `json:"qrcode"`
			}
			if err := json.Unmarshal(raw.Data, &obj); err == nil {
				dataOrderID = obj.OrderID
				dataQRCode = obj.QRCode
			}
		}
		if dataOrderID != "" {
			orderID = dataOrderID
		}
		if dataQRCode != "" {
			qrcode = dataQRCode
		}
	}

	codeStr := fmt.Sprintf("%v", raw.Code)
	return &OrderResult{
		Code:    codeStr,
		Msg:     raw.Msg,
		QRCode:  qrcode,
		OrderID: orderID,
	}, nil
}

// QueryOrderStatus 查询订单支付状态
func (s *Service) QueryOrderStatus(orderID, token string) (*OrderStatus, error) {
	data := map[string]string{
		"id":    orderID,
		"token": token,
	}

	body, err := s.postAPI("query_order", data)
	if err != nil {
		log.Printf("[payment] QueryOrderStatus 请求失败: orderID=%s err=%v", orderID, err)
		return nil, err
	}

	var apiResp struct {
		Code any         `json:"code"`
		Data OrderStatus `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		log.Printf("[payment] QueryOrderStatus 解析失败: orderID=%s body=%s err=%v", orderID, truncateBody(body), err)
		return nil, fmt.Errorf("解析订单状态响应失败: %w", err)
	}

	if err := checkResponse(apiResp.Code, body); err != nil {
		log.Printf("[payment] QueryOrderStatus 业务错误: orderID=%s code=%v err=%v", orderID, apiResp.Code, err)
		return nil, err
	}

	log.Printf("[payment] QueryOrderStatus: orderID=%s state=%d", orderID, apiResp.Data.State)
	return &apiResp.Data, nil
}
