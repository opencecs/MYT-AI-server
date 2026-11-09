package sdkapi

import (
	"fmt"
	"net/url"
)

// PrivateNic 私有网卡
type PrivateNic struct {
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}

// PublicNicInfo 公有网卡信息
type PublicNicInfo struct {
	NetWorkEth0 any `json:"netWork_eth0,omitempty"`
	MacVlan     any `json:"macVlan,omitempty"`
}

// ListPrivateNics 获取私有网卡列表
func (c *Client) ListPrivateNics() ([]PrivateNic, error) {
	body, err := c.get("/mytBridge")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var list []PrivateNic
	if err := parseListData(resp.Data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// CreatePrivateNic 创建私有网卡
func (c *Client) CreatePrivateNic(customName, cidr string) error {
	body, err := c.postJSON("/mytBridge", map[string]string{
		"customName": customName,
		"cidr":      cidr,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// UpdatePrivateNic 更新私有网卡
func (c *Client) UpdatePrivateNic(name, newCidr string) error {
	body, err := c.putJSON("/mytBridge", map[string]string{
		"name":    name,
		"newCidr": newCidr,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// DeletePrivateNic 删除私有网卡
func (c *Client) DeletePrivateNic(name string) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/mytBridge?name=%s", url.QueryEscape(name)))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// GetPublicNicInfo 获取公有网卡信息
func (c *Client) GetPublicNicInfo() (*PublicNicInfo, error) {
	body, err := c.get("/macvlan")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var info PublicNicInfo
	if err := parseData(resp.Data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// UpdateMacVlan 更新 MacVlan 配置
func (c *Client) UpdateMacVlan(gw, subnet string) error {
	body, err := c.putJSON("/macvlan", map[string]string{
		"gw":     gw,
		"subnet": subnet,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// CreateMacVlan 创建 MacVlan
func (c *Client) CreateMacVlan(gw, subnet string) error {
	body, err := c.postJSON("/macvlan", map[string]string{
		"gw":     gw,
		"subnet": subnet,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// DeleteMacVlan 删除 MacVlan
func (c *Client) DeleteMacVlan() error {
	body, err := c.deleteWithQuery("/macvlan")
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}
