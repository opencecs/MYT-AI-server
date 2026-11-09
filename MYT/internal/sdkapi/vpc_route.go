package sdkapi

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// DomainRouteEntry 域名中转路由条目
type DomainRouteEntry struct {
	Domains []string `json:"domains"`
	VpcID   int       `json:"vpcID"`
}

// DomainRoute 域名中转路由查询结果
type DomainRoute struct {
	Routes []DomainRouteEntry `json:"routes"`
}

// GetDomainRoutes 查询容器的域名中转路由
func (c *Client) GetDomainRoutes(containerID string) (*DomainRoute, error) {
	path := fmt.Sprintf("/mytVpc/domainRoute?containerID=%s", url.QueryEscape(containerID))
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
	var dr DomainRoute
	if err := json.Unmarshal(resp.Data, &dr); err != nil {
		return nil, nil
	}
	return &dr, nil
}

// SetDomainRoutes 设置容器的域名中转路由，传空数组可清空所有路由
func (c *Client) SetDomainRoutes(containerID string, routes []DomainRouteEntry) error {
	body, err := c.postJSON("/mytVpc/domainRoute", map[string]any{
		"containerID": containerID,
		"routes":      routes,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ClearDomainRoutes 清除容器的所有域名中转路由
func (c *Client) ClearDomainRoutes(containerID string) error {
	body, err := c.deleteWithQuery(fmt.Sprintf("/mytVpc/domainRoute?containerID=%s", url.QueryEscape(containerID)))
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}
