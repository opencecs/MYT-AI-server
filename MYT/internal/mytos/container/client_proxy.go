package container

import (
	"fmt"
)

// --- 代理类 API ---

// QueryProxy 查询代理状态
func (c *Client) QueryProxy() (map[string]interface{}, error) {
	return c.get("/proxy")
}

// StartProxy 启动代理
func (c *Client) StartProxy(ip string, port int, usr, pwd string, proxyType int) error {
	path := fmt.Sprintf("/proxy?cmd=2&ip=%s&port=%d&usr=%s&pwd=%s&type=%d",
		ip, port, usr, pwd, proxyType)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("启动代理失败: %v", result)
	}
	return nil
}

// StopProxy 停止代理
func (c *Client) StopProxy() error {
	result, err := c.get("/proxy?cmd=3")
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("停止代理失败: %v", result)
	}
	return nil
}
