package sdkapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CopyProgressCallback 复制进度回调，每收到一个 SSE data 事件时调用
type CopyProgressCallback func(current, total int, status, message string)

// CopyAndroid 复制安卓云机（一次性读取，无进度回调）
func (c *Client) CopyAndroid(name string, indexNum, count int) error {
	path := fmt.Sprintf("/android/copy?name=%s", url.QueryEscape(name))
	if indexNum > 0 {
		path += fmt.Sprintf("&indexNum=%d", indexNum)
	}
	if count > 0 {
		path += fmt.Sprintf("&count=%d", count)
	}
	body, err := c.get(path)
	if err != nil {
		return err
	}
	// /android/copy 返回 SSE 流，解析最后一个 data: 行
	return parseSSEResponse(body)
}

// CopyAndroidStream 复制安卓云机（流式 SSE，带进度回调）。
// 通过 buildURL + setAuth 构造请求，兼容 local 直连和 public 网关透传。
// onProgress 回调在每个 SSE data 事件触发，可为 nil。
func (c *Client) CopyAndroidStream(name string, indexNum, count int, onProgress CopyProgressCallback) error {
	if c.errorMsg != "" {
		return fmt.Errorf("%s", c.errorMsg)
	}

	path := fmt.Sprintf("/android/copy?name=%s", url.QueryEscape(name))
	if indexNum > 0 {
		path += fmt.Sprintf("&indexNum=%d", indexNum)
	}
	if count > 0 {
		path += fmt.Sprintf("&count=%d", count)
	}

	ctx, cancel := context.WithTimeout(c.requestContext(), 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.buildURL(path), nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("copy request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("copy failed (HTTP %d)", resp.StatusCode)
	}

	// 流式读取 SSE
	scanner := bufio.NewScanner(resp.Body)
	var lastData string
	var lastEvent string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event:") {
			lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			lastData = data
			// 解析进度并回调
			if onProgress != nil {
				var p struct {
					Current int    `json:"current"`
					Total   int    `json:"total"`
					Status  string `json:"status"`
					Message string `json:"message"`
				}
				if json.Unmarshal([]byte(data), &p) == nil {
					onProgress(p.Current, p.Total, p.Status, p.Message)
				}
			}
		}
	}

	if lastEvent == "error" {
		return fmt.Errorf("copy failed: %s", lastData)
	}
	if lastData != "" {
		var result struct {
			Status  string   `json:"status"`
			Message string   `json:"message"`
			Success []string `json:"success"`
		}
		if json.Unmarshal([]byte(lastData), &result) == nil {
			if result.Status == "error" {
				return fmt.Errorf("copy failed: %s", result.Message)
			}
			// status=done 或 status=success 都算成功
			return nil
		}
	}
	return fmt.Errorf("copy response parse failed: %s", lastData)
}

// ResetAndroid 重置安卓容器
func (c *Client) ResetAndroid(name string, start bool) error {
	payload := map[string]any{
		"name":  name,
		"start": start,
	}
	body, err := c.putJSON("/android", payload)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// SwitchModel 切换云机机型
func (c *Client) SwitchModel(name, modelID, localModel, modelStatic string, latitude, longitude float64, countryCode string) error {
	payload := map[string]any{"name": name}
	if modelID != "" {
		payload["modelId"] = modelID
	}
	if localModel != "" {
		payload["localModel"] = localModel
	}
	if modelStatic != "" {
		payload["modelStatic"] = modelStatic
	}
	if latitude != 0 {
		payload["latitude"] = latitude
	}
	if longitude != 0 {
		payload["longitude"] = longitude
	}
	if countryCode != "" {
		payload["countryCode"] = countryCode
	}
	body, err := c.postJSON("/android/switchModel", payload)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ChangeImage 批量切换容器镜像
func (c *Client) ChangeImage(containerNames []string, image string) (string, string, error) {
	payload := map[string]any{
		"containerNames": containerNames,
		"image":         image,
	}
	body, err := c.postJSON("/android/change-image", payload)
	if err != nil {
		return "", "", err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return "", "", err
	}
	var result struct {
		TaskID  string `json:"taskId"`
		Message string `json:"message"`
	}
	if err := parseData(resp.Data, &result); err != nil {
		return "", "", err
	}
	return result.TaskID, result.Message, nil
}

// PruneImages 清理未使用的镜像
func (c *Client) PruneImages() (int, string, error) {
	body, err := c.postJSON("/android/pruneImages", nil)
	if err != nil {
		return 0, "", err
	}
	// 容错：空body或非JSON响应
	if len(body) == 0 {
		return 0, "0B", nil
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return 0, "", err
	}
	// 空data容错
	if len(resp.Data) == 0 {
		return 0, "0B", nil
	}
	var result struct {
		PruneCount   int    `json:"pruneCount"`
		ReleaseSpace string `json:"releaseSpace"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return 0, "", fmt.Errorf("解析清理结果失败: %w", err)
	}
	return result.PruneCount, result.ReleaseSpace, nil
}

// GetTaskStatus 获取异步任务状态
func (c *Client) GetTaskStatus(taskID string) (string, error) {
	path := fmt.Sprintf("/task?taskId=%s", url.QueryEscape(taskID))
	body, err := c.get(path)
	if err != nil {
		return "", err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return "", err
	}
	return string(resp.Data), nil
}

// parseData 解析 data 字段到目标结构
func parseData(data []byte, v any) error {
	return parseListData(data, v)
}
