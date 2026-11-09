package sdkapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SSEProgress SSE 进度事件
type SSEProgress struct {
	Progress float64 `json:"progress"`
	Current  int64   `json:"current"`
	Total    int64   `json:"total"`
}

// isSSEBody 判断响应体是否为 SSE 格式
func isSSEBody(body []byte) bool {
	return len(body) > 5 && strings.HasPrefix(string(body), "data:")
}

// parseSSEEvents 从 SSE 响应体解析所有事件数据
func parseSSEEvents(body []byte) [][]byte {
	var events [][]byte
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, []byte(line[6:]))
		} else if strings.HasPrefix(line, "data:") {
			events = append(events, []byte(line[5:]))
		}
	}
	return events
}

// scanSSEStream 逐行读取 SSE 响应流，实时回调，返回最后一个事件
func scanSSEStream(resp *http.Response, onEvent func(body []byte)) []byte {
	var lastEvent []byte
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			eventData := []byte(line[6:])
			if onEvent != nil {
				onEvent(eventData)
			}
			lastEvent = eventData
		} else if strings.HasPrefix(line, "data:") {
			eventData := []byte(line[5:])
			if onEvent != nil {
				onEvent(eventData)
			}
			lastEvent = eventData
		}
	}
	return lastEvent
}

// doPostSSE 发送 POST 请求并处理 SSE 响应（真流式）。
// 响应嗅探：Content-Type 为 event-stream 走真流式；否则整体读回，
// body 以 data: 开头（未标 Content-Type 的 SSE）按缓冲事件回放；否则原样返回（同步 JSON）。
func (c *Client) doPostSSE(path string, payload any, onEvent func(body []byte), timeout time.Duration) ([]byte, error) {
	if c.errorMsg != "" {
		return nil, fmt.Errorf("%s", c.errorMsg)
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(c.requestContext(), "POST", c.buildURL(path), strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout, Transport: DeviceTransport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s 失败: %s", path, friendlyNetError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("POST %s 失败: HTTP %d, %s", path, resp.StatusCode, string(body))
	}

	ct := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(ct, "text/event-stream")

	if isSSE {
		return scanSSEStream(resp, onEvent), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// fallback: body 实际是 SSE 格式但 Content-Type 没标明
	if isSSEBody(body) {
		events := parseSSEEvents(body)
		var lastEvent []byte
		for _, evt := range events {
			if onEvent != nil {
				onEvent(evt)
			}
			lastEvent = evt
		}
		return lastEvent, nil
	}

	return body, nil
}

// doGetSSE 发送 GET 请求并处理 SSE 响应（真流式）
func (c *Client) doGetSSE(path string, onEvent func(body []byte)) ([]byte, error) {
	if c.errorMsg != "" {
		return nil, fmt.Errorf("%s", c.errorMsg)
	}
	req, err := http.NewRequestWithContext(c.requestContext(), "GET", c.buildURL(path), nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	client := &http.Client{Timeout: 5 * time.Minute, Transport: DeviceTransport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s 失败: %s", path, friendlyNetError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GET %s 失败: HTTP %d, %s", path, resp.StatusCode, string(body))
	}

	ct := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(ct, "text/event-stream")

	if isSSE {
		return scanSSEStream(resp, onEvent), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if isSSEBody(body) {
		events := parseSSEEvents(body)
		var lastEvent []byte
		for _, evt := range events {
			if onEvent != nil {
				onEvent(evt)
			}
			lastEvent = evt
		}
		return lastEvent, nil
	}

	return body, nil
}
