package cloudapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// GatewayDevice 网关 /v1/devices 返回的设备信息（无 name，name 在 myt-agent deviceStore）
type GatewayDevice struct {
	DeviceID    string `json:"device_id"`
	Type        string `json:"type"`
	Online      bool   `json:"online"`
	SlotCount   int    `json:"slot_count"`
	ActiveSlots int    `json:"active_slots"`
	VirtualIP   string `json:"virtual_ip"`
}

// ListDevices 从网关获取设备列表。
// 调 GET /v1/devices + Bearer，返回 {devices:[...], total}。
// 遇 401 自动 refresh + 重试一次。
func (c *Client) ListDevices() ([]GatewayDevice, error) {
	return c.listDevicesWithRetry(true)
}

// ListDevicesWithTotal 与 ListDevices 相同，额外返回网关声明的 total。
// 调用方据此区分「账号下确实没有设备」（total==0，可整体清空列表）与
// 「网关返回了设备但都不在线」（保留旧条目，仅置离线）。
func (c *Client) ListDevicesWithTotal() ([]GatewayDevice, int, error) {
	return c.listDevicesWithTotalRetry(true)
}

func (c *Client) listDevicesWithRetry(allowRetry bool) ([]GatewayDevice, error) {
	devices, _, err := c.listDevicesWithTotalRetry(allowRetry)
	return devices, err
}

func (c *Client) listDevicesWithTotalRetry(allowRetry bool) ([]GatewayDevice, int, error) {
	token, err := c.token()
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest("GET", GatewayURL+"/v1/devices", nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	start := time.Now()
	resp, err := gatewayHTTP.Do(req)
	if err != nil {
		util.Debugf("[gateway] GET /v1/devices → 失败: %v (%s)", err, time.Since(start).Round(time.Millisecond))
		return nil, 0, fmt.Errorf("网关设备列表查询失败: %w", err)
	}
	util.Debugf("[gateway] GET /v1/devices → %d (%s)", resp.StatusCode, time.Since(start).Round(time.Millisecond))
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// 401 清除缓存 token 重试一次（授权 token 由 payment 侧自动刷新）
	if resp.StatusCode == http.StatusUnauthorized && allowRetry {
		c.mu.Lock()
		c.cachedToken = ""
		c.mu.Unlock()
		return c.listDevicesWithTotalRetry(false)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("网关设备列表失败 (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Devices []GatewayDevice `json:"devices"`
		Total   int             `json:"total"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, 0, fmt.Errorf("网关设备列表解析失败: %w", err)
	}
	return result.Devices, result.Total, nil
}
