package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// HttpProxy 代理前端 HTTP 请求到 MYT Agent 服务（绕过浏览器 CORS）
// method: GET/POST/PUT/DELETE，path: /api/...，body: 可选 JSON body
// 遇 401（session 过期）自动续期后重试一次。
func (s *MytAgentService) HttpProxy(method, path, body string) (string, error) {
	base := "http://119.96.73.226:8891"
	doReq := func(tok string) (*http.Response, error) {
		var req *http.Request
		var err error
		if body != "" {
			req, err = http.NewRequest(method, base+path, strings.NewReader(body))
		} else {
			req, err = http.NewRequest(method, base+path, nil)
		}
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		return s.client.Do(req)
	}

	tok, err := s.ensureToken()
	if err != nil {
		return "", err
	}
	resp, err := doReq(tok)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	// 401：清 session 强制续期后重试一次
	if resp.StatusCode == 401 {
		resp.Body.Close()
		s.mu.Lock()
		s.sessionToken = ""
		s.mu.Unlock()
		tok2, err2 := s.ensureToken()
		if err2 != nil {
			return "", err2
		}
		resp, err = doReq(tok2)
		if err != nil {
			return "", fmt.Errorf("请求失败: %w", err)
		}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return string(respBody), nil
}

// HttpProxyBlob 代理 HTTP 请求并返回 base64 编码的响应体（用于音频/图片等二进制数据）
// 遇 401（session 过期）自动续期后重试一次。
func (s *MytAgentService) HttpProxyBlob(method, path, body string) (string, error) {
	base := "http://119.96.73.226:8891"
	doReq := func(tok string) (*http.Response, error) {
		var req *http.Request
		var err error
		if body != "" {
			req, err = http.NewRequest(method, base+path, strings.NewReader(body))
		} else {
			req, err = http.NewRequest(method, base+path, nil)
		}
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		return s.client.Do(req)
	}

	tok, err := s.ensureToken()
	if err != nil {
		return "", err
	}
	resp, err := doReq(tok)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	if resp.StatusCode == 401 {
		resp.Body.Close()
		s.mu.Lock()
		s.sessionToken = ""
		s.mu.Unlock()
		tok2, err2 := s.ensureToken()
		if err2 != nil {
			return "", err2
		}
		resp, err = doReq(tok2)
		if err != nil {
			return "", fmt.Errorf("请求失败: %w", err)
		}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// 返回 base64 编码的二进制数据
	return "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString(respBody), nil
}

// AsrRecognize 通过 WebSocket 代理 ASR 语音识别
func (s *MytAgentService) AsrRecognize(audioData []byte) (string, error) {
	tok, err := s.ensureToken()
	if err != nil {
		return "", err
	}

	asrURL := "ws://119.96.73.226:8891/api/asr?token=" + tok
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, asrURL, nil)
	if err != nil {
		return "", fmt.Errorf("ASR 连接失败: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// 发送 START
	if err := conn.Write(ctx, websocket.MessageText, []byte("START")); err != nil {
		return "", fmt.Errorf("ASR 发送START失败: %w", err)
	}
	// 发送音频数据
	if err := conn.Write(ctx, websocket.MessageBinary, audioData); err != nil {
		return "", fmt.Errorf("ASR 发送音频失败: %w", err)
	}
	// 发送 STOP
	if err := conn.Write(ctx, websocket.MessageText, []byte("STOP")); err != nil {
		return "", fmt.Errorf("ASR 发送STOP失败: %w", err)
	}

	// 接收识别结果（等待最终结果或超时）
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return "", fmt.Errorf("ASR 读取失败: %w", err)
		}
		var msg struct {
			IsFinal   bool   `json:"is_final"`
			Event     string `json:"event"`
			Sentences []struct {
				Text string `json:"text"`
			} `json:"sentences"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.IsFinal || msg.Event == "stopped" {
			var result strings.Builder
			for _, s := range msg.Sentences {
				result.WriteString(s.Text)
			}
			return result.String(), nil
		}
	}
}

func (s *MytAgentService) HttpProxyUpload(fileName string, fileData []byte) (string, error) {
	tok, err := s.ensureToken()
	if err != nil {
		return "", err
	}

	// 构造 multipart body
	var buf strings.Builder
	boundary := "----MytAgentBoundary" + fmt.Sprintf("%d", time.Now().UnixNano())
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"" + fileName + "\"\r\n")
	buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	body := buf.String() + string(fileData) + "\r\n--" + boundary + "--\r\n"

	req, err := http.NewRequest("POST", "http://119.96.73.226:8891/api/upload", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("上传失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return string(respBody), nil
}
