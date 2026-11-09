package sdkapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moyunteng/myt/internal/util"
)

// AuthError 表示设备 API 返回认证失败（code=61）
type AuthError struct {
	Code    string
	Message string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("认证失败: code=%s msg=%s", e.Code, e.Message)
}

type APIResponse struct {
	Code    json.RawMessage `json:"code"`
	Msg     string          `json:"msg"`
	Message string          `json:"message"` // 设备 SDK V3 使用 "message" 字段
	Data    json.RawMessage `json:"data"`
}

// ErrorMsg 返回错误消息，优先使用 message（设备 SDK V3），fallback 到 msg
func (r *APIResponse) ErrorMsg() string {
	if r.Message != "" {
		return r.Message
	}
	return r.Msg
}

type ListResponse struct {
	Count int             `json:"count"`
	List  json.RawMessage `json:"list"`
}

// isSuccessCode 检查 API 返回的 code 是否表示成功
// code 可能是数值(0)或字符串("0", "200")
func isSuccessCode(raw json.RawMessage) bool {
	var code any
	if err := json.Unmarshal(raw, &code); err != nil {
		return false
	}
	return util.IsSuccessCode(code)
}

// codeFromRaw 从 json.RawMessage 提取 code 值
func codeFromRaw(raw json.RawMessage) any {
	var code any
	_ = json.Unmarshal(raw, &code)
	return code
}

func parseAPIResponse(body []byte) (*APIResponse, error) {
	// 空响应容错：部分设备接口返回空 body 表示成功
	if len(body) == 0 {
		return &APIResponse{}, nil
	}
	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("解析API响应失败: %w (body: %s)", err, preview)
	}
	if !isSuccessCode(resp.Code) {
		if util.IsAuthCode(codeFromRaw(resp.Code)) {
			return nil, &AuthError{Code: string(resp.Code), Message: resp.ErrorMsg()}
		}
		return nil, fmt.Errorf("API error: code=%s msg=%s", string(resp.Code), resp.ErrorMsg())
	}
	return &resp, nil
}

func parseListData(data json.RawMessage, v any) error {
	var lr ListResponse
	if err := json.Unmarshal(data, &lr); err != nil {
		return json.Unmarshal(data, v)
	}
	return json.Unmarshal(lr.List, v)
}

// parseSSEResponse 解析 SSE 流式响应，检查最终结果
// 格式: data: {"status":"copying",...}\n\ndata: {"status":"done","success":[...]}\n\n
// 或: event: error\ndata: {"message":"xxx","status":"error"}\n\n
func parseSSEResponse(body []byte) error {
	lines := strings.Split(string(body), "\n")
	var lastData string
	var lastEvent string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event:") {
			lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" {
				lastData = data
			}
		}
	}
	if lastEvent == "error" {
		return fmt.Errorf("复制失败: %s", lastData)
	}
	if lastData != "" {
		var result struct {
			Status  string   `json:"status"`
			Message string   `json:"message"`
			Success []string `json:"success"`
		}
		if err := json.Unmarshal([]byte(lastData), &result); err == nil {
			if result.Status == "error" {
				return fmt.Errorf("复制失败: %s", result.Message)
			}
			// status=done 或 status=success 都算成功
			return nil
		}
	}
	return fmt.Errorf("复制响应解析失败: %s", string(body))
}

// parseMutationResponse 检查 mutation 操作的 API 响应是否成功
func (c *Client) parseMutationResponse(body []byte) error {
	_, err := parseAPIResponse(body)
	return err
}

// SetDevicePassword 调用设备 /auth/password 接口设置密码
func (c *Client) SetDevicePassword(newPassword, confirmPassword string) error {
	payload := map[string]string{
		"newPassword":     newPassword,
		"confirmPassword": confirmPassword,
	}
	_, err := c.postJSON("/auth/password", payload)
	return err
}

// CloseAuth 调用设备 /auth/close 接口关闭密码认证
func (c *Client) CloseAuth() error {
	_, err := c.postJSON("/auth/close", nil)
	return err
}
