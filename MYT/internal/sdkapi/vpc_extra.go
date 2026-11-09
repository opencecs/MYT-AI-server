package sdkapi

import (
	"encoding/json"
	"fmt"
)

// AddVpcRuleBatch 批量添加容器到VPC组
func (c *Client) AddVpcRuleBatch(names []string, vpcID int) error {
	payload := map[string]any{
		"names": names,
		"vpcID": vpcID,
	}
	body, err := c.postJSON("/mytVpc/addRule/batch", payload)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// DeleteVpcRuleBatch 批量从VPC组移除容器
func (c *Client) DeleteVpcRuleBatch(names []string) error {
	payload := map[string]any{
		"name": names,
	}
	body, err := c.postJSON("/mytVpc/delRule/batch", payload)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// GetGlobalDomainFilters 获取全局域名过滤列表
func (c *Client) GetGlobalDomainFilters() (*DomainFilter, error) {
	body, err := c.get("/mytVpc/domainFilter/global")
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, nil
	}
	if resp.Data == nil || len(resp.Data) == 0 {
		return nil, nil
	}
	// data 是 {"domains":[...]} 结构，不是列表型 {"count":N,"list":[...]}
	// 不能用 parseData（它会走 parseListData 试图从 list 字段取值，导致 domains 丢失）
	var df DomainFilter
	if err := json.Unmarshal(resp.Data, &df); err != nil {
		return nil, nil
	}
	return &df, nil
}

// SetGlobalDomainFilter 设置全局域名过滤
func (c *Client) SetGlobalDomainFilter(domains []string) error {
	body, err := c.postJSON("/mytVpc/domainFilter/global", map[string]any{
		"domains": domains,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ClearGlobalDomainFilter 清除全局域名过滤
func (c *Client) ClearGlobalDomainFilter() error {
	body, err := c.deleteWithQuery("/mytVpc/domainFilter/global")
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// Socks5Node SOCKS5 代理节点配置
type Socks5Node struct {
	Remarks       string `json:"remarks,omitempty"`
	SocksIP       string `json:"socksIp,omitempty"`
	SocksPort     int    `json:"socksPort,omitempty"`
	SocksUser     string `json:"socksUser,omitempty"`
	SocksPassword string `json:"socksPassword,omitempty"`
}

// SetVpcSocks 设置 VPC Socks5 代理
func (c *Client) SetVpcSocks(alias string, nodes []Socks5Node) error {
	payload := map[string]any{
		"alias": alias,
		"list":  nodes,
	}
	body, err := c.postJSON("/mytVpc/socks", payload)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// EnableWhiteListDNS 启用白名单DNS
func (c *Client) EnableWhiteListDNS(ruleID int, enable bool, whiteListDns []string) error {
	payload := map[string]any{
		"ruleID":       ruleID,
		"enable":       enable,
		"whiteListDns": whiteListDns,
	}
	body, err := c.postJSON("/mytVpc/whiteListDns", payload)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// EnableVpcRule 启用/停用云机VPC绑定（绑定关系保留：停用后流量恢复直连，重新启用即恢复走节点）
func (c *Client) EnableVpcRule(ruleID int, enable bool) error {
	payload := map[string]any{
		"ruleID": ruleID,
		"enable": enable,
	}
	body, err := c.postJSON("/mytVpc/enableRule", payload)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// VpcProfile VPC 节点配置（对应设备端 ProfileItem，PUT /mytVpc 的 profile 字段）。
// 仅编辑弹窗会用到的字段参与序列化；其余字段按需补充。修改后设备端自动重生成 xray 配置并同步云机。
type VpcProfile struct {
	ConfigType  int    `json:"configType,omitempty"`
	Remarks     string `json:"remarks,omitempty"`
	Server      string `json:"server,omitempty"`
	ServerPort  string `json:"serverPort,omitempty"`
	Password    string `json:"password,omitempty"`
	Method      string `json:"method,omitempty"`
	Flow        string `json:"flow,omitempty"`
	Network     string `json:"network,omitempty"`
	HeaderType  string `json:"headerType,omitempty"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	Security    string `json:"security,omitempty"`
	SNI         string `json:"sni,omitempty"`
	ALPN        string `json:"alpn,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Insecure    *bool  `json:"insecure,omitempty"`
}

// UpdateVpcConfig 更新VPC配置（编辑节点）。remarks 为空则不下发该字段；
// profile 按文档为必填对象，nil 视为参数错误。
func (c *Client) UpdateVpcConfig(vpcID int, remarks string, profile *VpcProfile) error {
	if profile == nil {
		return fmt.Errorf("profile 不能为空")
	}
	payload := map[string]any{
		"vpcID":   vpcID,
		"profile": profile,
	}
	if remarks != "" {
		payload["remarks"] = remarks
	}
	body, err := c.putJSON("/mytVpc", payload)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// DeleteVpc 删除VPC网络
func (c *Client) DeleteVpc(vpcID int) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/mytVpc?vpcID=%d", vpcID))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}
