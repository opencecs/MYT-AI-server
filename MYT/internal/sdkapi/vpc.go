package sdkapi

import (
	"encoding/json"
	"fmt"
	"net/url"
)

type VpcNode struct {
	ID       int    `json:"id"`
	Protocol string `json:"protocol"`
	Remarks  string `json:"remarks"`
	Profile  string `json:"profile"`
}

type VpcListWrapper struct {
	List []VpcNode `json:"list"`
}

type VpcGroup struct {
	ID    int            `json:"id"`
	Alias string         `json:"alias"`
	URL   string         `json:"url"`
	Type  int            `json:"type"`
	Vpcs  VpcListWrapper `json:"vpcs"`
}

type VpcNodeRule struct {
	ID             int      `json:"id"`
	ContainerID    string   `json:"containerID"`
	ContainerIP    string   `json:"containerIP"`
	ContainerName  string   `json:"containerName"`
	ContainerState string   `json:"containerState"`
	Status         int      `json:"status"`
	GroupName      string   `json:"groupName"`
	VpcRemarks     string   `json:"vpcRemarks"`
	WhiteListDns   []string `json:"whiteListDns"`
	// Enabled 绑定开关（新固件返回）：true-启用（流量走该节点），false-停用（绑定保留，流量直连）。
	// 指针类型：老固件不返回该字段时为 nil，调用方按启用（现状语义）处理。
	Enabled *bool `json:"enabled,omitempty"`
}

type DomainFilter struct {
	Domains []string `json:"domains"`
}

type DomainDirect struct {
	Domains []string `json:"domains"`
}

func (c *Client) ListVpcGroups() ([]VpcGroup, error) {
	body, err := c.get("/mytVpc/group")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var list []VpcGroup
	if err := parseListData(resp.Data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// CreateVpcGroupRequest 创建VPC分组请求
type CreateVpcGroupRequest struct {
	Alias        string   `json:"alias"`
	URL          string   `json:"url"`
	Source       int      `json:"source"`
	Addresses    []string `json:"addresses"`
	ViaVpcIds    []int    `json:"viaVpcIds,omitempty"`
	ViaAddresses []string `json:"viaAddresses,omitempty"`
}

func (c *Client) CreateVpcGroup(req CreateVpcGroupRequest) error {
	body, err := c.postJSON("/mytVpc/group", req)
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) DeleteVpcGroup(id int) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/mytVpc/group?id=%d", id))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) UpdateVpcGroup(id int) error {
	body, err := c.postJSON("/mytVpc/group/update", map[string]int{"id": id})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) UpdateVpcGroupAlias(id int, newAlias string) error {
	body, err := c.postJSON("/mytVpc/group/alias", map[string]any{
		"id":       id,
		"newAlias": newAlias,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) GetContainerRules() ([]VpcNodeRule, error) {
	body, err := c.get("/mytVpc/containerRule")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var list []VpcNodeRule
	if err := parseListData(resp.Data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (c *Client) AddVpcRule(name string, vpcID int) error {
	body, err := c.postJSON("/mytVpc/addRule", map[string]any{
		"name":  name,
		"vpcID": vpcID,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) DeleteVpcRule(name string) error {
	body, err := c.postJSON("/mytVpc/delRule", map[string]string{"name": name})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) GetDomainFilters(containerID string) (*DomainFilter, error) {
	path := "/mytVpc/domainFilter"
	if containerID != "" {
		path = fmt.Sprintf("/mytVpc/domainFilter?containerID=%s", url.QueryEscape(containerID))
	}
	body, err := c.get(path)
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
	var df DomainFilter
	if err := json.Unmarshal(resp.Data, &df); err != nil {
		return nil, nil
	}
	return &df, nil
}

func (c *Client) SetDomainFilter(containerID string, domains []string) error {
	body, err := c.postJSON("/mytVpc/domainFilter", map[string]any{
		"containerID": containerID,
		"domains":     domains,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) ClearDomainFilter(containerID string) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/mytVpc/domainFilter?containerID=%s", url.QueryEscape(containerID)))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) GetDomainDirects(containerID string) (*DomainDirect, error) {
	path := "/mytVpc/domainDirect"
	if containerID != "" {
		path = fmt.Sprintf("/mytVpc/domainDirect?containerID=%s", url.QueryEscape(containerID))
	}
	body, err := c.get(path)
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
	var dd DomainDirect
	if err := json.Unmarshal(resp.Data, &dd); err != nil {
		return nil, nil
	}
	return &dd, nil
}

func (c *Client) SetDomainDirect(containerID string, domains []string) error {
	body, err := c.postJSON("/mytVpc/domainDirect", map[string]any{
		"containerID": containerID,
		"domains":     domains,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) ClearDomainDirect(containerID string) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/mytVpc/domainDirect?containerID=%s", url.QueryEscape(containerID)))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// VpcTestResult 测速结果
type VpcTestResult struct {
	Latency string `json:"latency"`
	Msg     string `json:"msg"`
}

func (c *Client) TestVpcLatency(address string) (*VpcTestResult, error) {
	body, err := c.get(fmt.Sprintf("/mytVpc/test?address=%s", url.QueryEscape(address)))
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var result VpcTestResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
