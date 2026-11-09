package sdkapi

import (
	"encoding/json"
	"fmt"
)

// VPN 客户端管理（设备端 SDK API：/mytVpn/*）

// VpnStatus VPN 客户端状态
type VpnStatus struct {
	Installed bool `json:"installed"` // 是否已安装（二进制文件存在）
	Running   bool `json:"running"`   // 是否运行中（进程存活）
	Enabled   bool `json:"enabled"`   // 是否已启用（状态文件标记为1）
}

// GetVpnStatus 查询 VPN 客户端状态（GET /mytVpn/status）
func (c *Client) GetVpnStatus() (*VpnStatus, error) {
	body, err := c.get("/mytVpn/status")
	if err != nil {
		return nil, err
	}
	var s VpnStatus
	// 优先按标准 {code,data} 响应解析
	if resp, e := parseAPIResponse(body); e == nil {
		if e := json.Unmarshal(resp.Data, &s); e == nil {
			return &s, nil
		}
	}
	// 回退：直接解析裸对象 {"installed":true,...}
	if e := json.Unmarshal(body, &s); e != nil {
		return nil, fmt.Errorf("解析VPN状态失败: %w (body: %s)", e, string(body))
	}
	return &s, nil
}

// StartVpn 启动 VPN 客户端
func (c *Client) StartVpn() error {
	body, err := c.postJSON("/mytVpn/start", nil)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// StopVpn 停止 VPN 客户端
func (c *Client) StopVpn() error {
	body, err := c.postJSON("/mytVpn/stop", nil)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ReinstallVpn 重装 VPN 客户端（即安装/重安装）
func (c *Client) ReinstallVpn() error {
	body, err := c.postJSON("/mytVpn/reinstall", nil)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// UninstallVpn 卸载 VPN 客户端
func (c *Client) UninstallVpn() error {
	body, err := c.postJSON("/mytVpn/uninstall", nil)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}
