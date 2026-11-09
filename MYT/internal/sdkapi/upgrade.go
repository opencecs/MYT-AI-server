package sdkapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UploadSDK 流式上传 SDK zip 文件到设备
// POST /server/upgrade/upload, multipart/form-data, 字段名 file
// 使用 io.Pipe 流式传输，避免将整个文件读入内存
func (c *Client) UploadSDK(zipPath string) error {
	log.Printf("[sdkapi] 开始上传SDK到 %s, 文件: %s", c.BaseURL, zipPath)

	f, err := os.Open(zipPath)
	if err != nil {
		return fmt.Errorf("打开SDK文件失败: %w", err)
	}
	defer f.Close()

	// pipe 流式写入，不预占内存
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		defer pw.Close()
		defer writer.Close()
		part, partErr := writer.CreateFormFile("file", filepath.Base(zipPath))
		if partErr != nil {
			pw.CloseWithError(partErr)
			return
		}
		if _, copyErr := io.Copy(part, f); copyErr != nil {
			pw.CloseWithError(copyErr)
		}
	}()

	req, err := http.NewRequestWithContext(c.requestContext(), "POST", c.buildURL("/server/upgrade/upload"), pr)
	if err != nil {
		return fmt.Errorf("创建上传请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuth(req)
	// 不设 ContentLength，使用 chunked transfer encoding，流式发送

	uploadClient := &http.Client{Timeout: 120 * time.Second, Transport: DeviceTransport}
	resp, err := uploadClient.Do(req)
	if err != nil {
		return fmt.Errorf("上传SDK失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	log.Printf("[sdkapi] 上传响应: HTTP %d, body: %s", resp.StatusCode, truncateStr(string(respBody), 200))

	// HTTP 状态码先行判定：非 2xx 一律失败——设备端老版本没有 /server/upgrade/
	// upload 接口时返回 404 + 纯文本 "Not Found"，JSON 解析失败会跳过 code 判定
	// 直接落「上传成功」（bug：404 被误判成功，实际什么都没装）。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("认证失败，请检查设备密码")
		}
		return fmt.Errorf("设备不支持上传接口(HTTP %d)，可能为老版本 SDK: %s", resp.StatusCode, truncateStr(string(respBody), 100))
	}

	// 2xx 后再按设备 API 约定判业务码（body 非 JSON 时保守视为失败——成功响应
	// 应该是 JSON；纯文本 2xx 多半是网关/代理的异常响应）
	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("上传响应非 JSON: %s", truncateStr(string(respBody), 100))
	}
	if !isSuccessCode(apiResp.Code) {
		return fmt.Errorf("上传失败: %s", apiResp.Msg)
	}

	log.Printf("[sdkapi] 上传成功: %s", c.BaseURL)

	return nil
}

// TriggerDeviceUpgrade 触发设备自升级（设备从云端拉取最新SDK，客户端无需上传文件）
// GET /server/upgrade，SSE 流式响应。
// 返回 (是否触发了升级, 错误)：
//   - isUpgraded=true  表示设备已下载并替换（收到 event:complete）
//   - isUpgraded=false 表示"已是最新版本"或出错
func (c *Client) TriggerDeviceUpgrade() (bool, error) {
	req, err := http.NewRequestWithContext(c.requestContext(), "GET", c.BaseURL+"/server/upgrade", nil)
	if err != nil {
		return false, err
	}
	if c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}

	// SSE 流，超时设长一点（下载 35MB）
	client := &http.Client{Timeout: 5 * time.Minute, Transport: DeviceTransport}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("触发升级失败: %w", err)
	}
	defer resp.Body.Close()

	// 逐行读取 SSE 流，找 event:complete 或 event:error 或 JSON 提示
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var gotComplete, gotError bool
	var errMsg string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "event: complete":
			gotComplete = true
		case line == "event: error":
			gotError = true
		case strings.HasPrefix(line, "data: "):
			payload := strings.TrimPrefix(line, "data: ")
			if gotError {
				var m struct {
					Msg string `json:"msg"`
				}
				if json.Unmarshal([]byte(payload), &m) == nil && m.Msg != "" {
					errMsg = m.Msg
				} else {
					errMsg = payload
				}
			}
			// data 行也可能是"当前已是最新版本"（普通JSON，非SSE）
			if !gotComplete && !gotError {
				var r struct {
					Code int `json:"code"`
					Data struct {
						Msg string `json:"msg"`
					} `json:"data"`
				}
				if json.Unmarshal([]byte(payload), &r) == nil && r.Code == 0 {
					// "当前已是最新版本" 等
					if strings.Contains(r.Data.Msg, "最新") {
						return false, nil // 无需升级
					}
				}
			}
		}
	}

	if gotComplete {
		return true, nil // 升级已触发并完成
	}
	if gotError {
		return false, fmt.Errorf("设备升级失败: %s", errMsg)
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("读取升级响应失败: %w", err)
	}
	return false, nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
