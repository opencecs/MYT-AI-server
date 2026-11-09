package container

import (
	"fmt"
	"io"
	"net/http"

	"github.com/moyunteng/myt/internal/util"
)

// --- 截图与操作类 API ---

// Screenshot 轻量截图（JPG格式，适合状态监控）
func (c *Client) Screenshot(quality int) ([]byte, error) {
	rawURL := c.buildURL(fmt.Sprintf("/snapshot?quality=%d", quality))
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("容器截图失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取截图数据失败: %w", err)
	}
	return data, nil
}

// AutoClick 自动点击/按键操作
func (c *Client) AutoClick(action string, id, x, y int, code ...int) error {
	path := fmt.Sprintf("/autoclick?action=%s&id=%d&x=%d&y=%d", action, id, x, y)
	if len(code) > 0 {
		path += fmt.Sprintf("&code=%d", code[0])
	}
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("容器点击失败: %v", result)
	}
	return nil
}

// GetClipboard 获取剪贴板
func (c *Client) GetClipboard() (string, error) {
	result, err := c.get("/clipboard")
	if err != nil {
		return "", err
	}
	if data, ok := result["data"].(map[string]interface{}); ok {
		if text, ok := data["text"].(string); ok {
			return text, nil
		}
	}
	return "", nil
}

// SetClipboard 设置剪贴板
func (c *Client) SetClipboard(text string) error {
	path := fmt.Sprintf("/clipboard?cmd=2&text=%s", text)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置剪贴板失败: %v", result)
	}
	return nil
}

// DeviceSnap 设备截图（多分辨率级别）
func (c *Client) DeviceSnap(level int) ([]byte, error) {
	rawURL := c.buildURL(fmt.Sprintf("/?task=snap&level=%d", level))
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("设备截图失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
