package main

import (
	"strconv"
	"time"

	"github.com/moyunteng/myt/internal/sdkapi"
)

type VpcServiceWrapper struct {
	sdkApiSvc *SdkApiServiceWrapper
}

func NewVpcServiceWrapper(sdkApiSvc *SdkApiServiceWrapper) *VpcServiceWrapper {
	return &VpcServiceWrapper{sdkApiSvc: sdkApiSvc}
}

func (s *VpcServiceWrapper) ListVpcGroups(deviceIP string) ([]sdkapi.VpcGroup, error) {
	return s.sdkApiSvc.newClient(deviceIP).ListVpcGroups()
}

func (s *VpcServiceWrapper) CreateVpcGroup(deviceIP, alias, url string, source int, addresses []string, viaVpcIds []int, viaAddresses []string) error {
	return s.sdkApiSvc.newClient(deviceIP).CreateVpcGroup(sdkapi.CreateVpcGroupRequest{
		Alias:        alias,
		URL:          url,
		Source:       source,
		Addresses:    addresses,
		ViaVpcIds:    viaVpcIds,
		ViaAddresses: viaAddresses,
	})
}

func (s *VpcServiceWrapper) DeleteVpcGroup(deviceIP, idStr string) error {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return err
	}
	return s.sdkApiSvc.newClient(deviceIP).DeleteVpcGroup(id)
}

func (s *VpcServiceWrapper) UpdateVpcGroup(deviceIP, idStr string) error {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return err
	}
	return s.sdkApiSvc.newClient(deviceIP).UpdateVpcGroup(id)
}

func (s *VpcServiceWrapper) UpdateVpcGroupAlias(deviceIP, idStr, newAlias string) error {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return err
	}
	return s.sdkApiSvc.newClient(deviceIP).UpdateVpcGroupAlias(id, newAlias)
}

func (s *VpcServiceWrapper) GetContainerRules(deviceIP string) ([]sdkapi.VpcNodeRule, error) {
	return s.sdkApiSvc.newClient(deviceIP).GetContainerRules()
}

func (s *VpcServiceWrapper) AddVpcRule(deviceIP, name string, vpcID int) error {
	return s.sdkApiSvc.newClient(deviceIP).AddVpcRule(name, vpcID)
}

func (s *VpcServiceWrapper) AddVpcRuleBatch(deviceIP string, names []string, vpcID int) error {
	return s.sdkApiSvc.newClient(deviceIP).AddVpcRuleBatch(names, vpcID)
}

func (s *VpcServiceWrapper) DeleteVpcRule(deviceIP, name string) error {
	return s.sdkApiSvc.newClient(deviceIP).DeleteVpcRule(name)
}

func (s *VpcServiceWrapper) DeleteVpcRuleBatch(deviceIP string, names []string) error {
	return s.sdkApiSvc.newClient(deviceIP).DeleteVpcRuleBatch(names)
}

func (s *VpcServiceWrapper) DeleteVpc(deviceIP, vpcIDStr string) error {
	id, err := strconv.Atoi(vpcIDStr)
	if err != nil {
		return err
	}
	return s.sdkApiSvc.newClient(deviceIP).DeleteVpc(id)
}

// UpdateVpcConfig 编辑VPC节点（PUT /mytVpc），保存后设备端自动重生成 xray 配置并同步云机
func (s *VpcServiceWrapper) UpdateVpcConfig(deviceIP string, vpcID int, remarks string, profile sdkapi.VpcProfile) error {
	return s.sdkApiSvc.newClient(deviceIP).UpdateVpcConfig(vpcID, remarks, &profile)
}

func (s *VpcServiceWrapper) TestVpcLatency(deviceIP, address string) (*sdkapi.VpcTestResult, error) {
	// 测速是长耗时操作（设备端逐条探测代理链路），不走 15s 全局超时，专用 60s
	return s.sdkApiSvc.newClientWithTimeout(deviceIP, 60*time.Second).TestVpcLatency(address)
}

func (s *VpcServiceWrapper) EnableWhiteListDNS(deviceIP string, ruleID int, enable bool, whiteListDns []string) error {
	return s.sdkApiSvc.newClient(deviceIP).EnableWhiteListDNS(ruleID, enable, whiteListDns)
}

func (s *VpcServiceWrapper) EnableVpcRule(deviceIP string, ruleID int, enable bool) error {
	return s.sdkApiSvc.newClient(deviceIP).EnableVpcRule(ruleID, enable)
}

func (s *VpcServiceWrapper) GetDomainFilters(deviceIP, containerID string) (*sdkapi.DomainFilter, error) {
	return s.sdkApiSvc.newClient(deviceIP).GetDomainFilters(containerID)
}

func (s *VpcServiceWrapper) SetDomainFilter(deviceIP, containerID string, domains []string) error {
	return s.sdkApiSvc.newClient(deviceIP).SetDomainFilter(containerID, domains)
}

func (s *VpcServiceWrapper) ClearDomainFilter(deviceIP, containerID string) error {
	return s.sdkApiSvc.newClient(deviceIP).ClearDomainFilter(containerID)
}

func (s *VpcServiceWrapper) GetGlobalDomainFilters(deviceIP string) (*sdkapi.DomainFilter, error) {
	return s.sdkApiSvc.newClient(deviceIP).GetGlobalDomainFilters()
}

func (s *VpcServiceWrapper) SetGlobalDomainFilter(deviceIP string, domains []string) error {
	return s.sdkApiSvc.newClient(deviceIP).SetGlobalDomainFilter(domains)
}

func (s *VpcServiceWrapper) ClearGlobalDomainFilter(deviceIP string) error {
	return s.sdkApiSvc.newClient(deviceIP).ClearGlobalDomainFilter()
}

func (s *VpcServiceWrapper) GetDomainDirects(deviceIP, containerID string) (*sdkapi.DomainDirect, error) {
	return s.sdkApiSvc.newClient(deviceIP).GetDomainDirects(containerID)
}

func (s *VpcServiceWrapper) SetDomainDirect(deviceIP, containerID string, domains []string) error {
	return s.sdkApiSvc.newClient(deviceIP).SetDomainDirect(containerID, domains)
}

func (s *VpcServiceWrapper) ClearDomainDirect(deviceIP, containerID string) error {
	return s.sdkApiSvc.newClient(deviceIP).ClearDomainDirect(containerID)
}

// --- 域名中转路由 ---

func (s *VpcServiceWrapper) GetDomainRoutes(deviceIP, containerID string) (*sdkapi.DomainRoute, error) {
	return s.sdkApiSvc.newClient(deviceIP).GetDomainRoutes(containerID)
}

func (s *VpcServiceWrapper) SetDomainRoutes(deviceIP, containerID string, routes []sdkapi.DomainRouteEntry) error {
	return s.sdkApiSvc.newClient(deviceIP).SetDomainRoutes(containerID, routes)
}

func (s *VpcServiceWrapper) ClearDomainRoutes(deviceIP, containerID string) error {
	return s.sdkApiSvc.newClient(deviceIP).ClearDomainRoutes(containerID)
}

// --- 私有网卡 ---

func (s *VpcServiceWrapper) ListPrivateNics(deviceIP string) ([]sdkapi.PrivateNic, error) {
	return s.sdkApiSvc.newClient(deviceIP).ListPrivateNics()
}

func (s *VpcServiceWrapper) CreatePrivateNic(deviceIP, customName, cidr string) error {
	return s.sdkApiSvc.newClient(deviceIP).CreatePrivateNic(customName, cidr)
}

func (s *VpcServiceWrapper) UpdatePrivateNic(deviceIP, name, newCidr string) error {
	return s.sdkApiSvc.newClient(deviceIP).UpdatePrivateNic(name, newCidr)
}

func (s *VpcServiceWrapper) DeletePrivateNic(deviceIP, name string) error {
	return s.sdkApiSvc.newClient(deviceIP).DeletePrivateNic(name)
}

// --- 公有网卡 ---

func (s *VpcServiceWrapper) GetPublicNicInfo(deviceIP string) (*sdkapi.PublicNicInfo, error) {
	return s.sdkApiSvc.newClient(deviceIP).GetPublicNicInfo()
}

func (s *VpcServiceWrapper) UpdateMacVlan(deviceIP, gw, subnet string) error {
	return s.sdkApiSvc.newClient(deviceIP).UpdateMacVlan(gw, subnet)
}

func (s *VpcServiceWrapper) CreateMacVlan(deviceIP, gw, subnet string) error {
	return s.sdkApiSvc.newClient(deviceIP).CreateMacVlan(gw, subnet)
}

func (s *VpcServiceWrapper) DeleteMacVlan(deviceIP string) error {
	return s.sdkApiSvc.newClient(deviceIP).DeleteMacVlan()
}

func (s *VpcServiceWrapper) GetNetworkInfo(deviceIP string) (*sdkapi.NetworkInfo, error) {
	return s.sdkApiSvc.newClient(deviceIP).GetNetworkInfo()
}
