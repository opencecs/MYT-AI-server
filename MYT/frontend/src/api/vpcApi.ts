import * as VpcService from '../../bindings/github.com/moyunteng/myt/vpcservicewrapper'
import { VpcGroup, VpcNodeRule, DomainRouteEntry, DomainRoute, VpcProfile } from '../types/index'

// --- VPC 分组 ---

export async function ListVpcGroups(ip: string): Promise<VpcGroup[]> {
  const groups = await VpcService.ListVpcGroups(ip)
  // bindings 返回强类型 VpcGroup[]（vpcs 可能是 JSON 字符串），经 normalizeGroup 归一化
  return (groups as unknown as Record<string, unknown>[]).map(normalizeGroup) as VpcGroup[]
}

export async function CreateVpcGroup(ip: string, alias: string, url: string, source: number, addresses: string[], viaVpcIds: number[] = [], viaAddresses: string[] = []): Promise<void> {
  await VpcService.CreateVpcGroup(ip, alias, url, source, addresses, viaVpcIds, viaAddresses)
}

export async function DeleteVpcGroup(ip: string, id: number): Promise<void> {
  await VpcService.DeleteVpcGroup(ip, String(id))
}

export async function UpdateVpcGroup(ip: string, id: number): Promise<void> {
  await VpcService.UpdateVpcGroup(ip, String(id))
}

export async function UpdateVpcGroupAlias(ip: string, id: number, newAlias: string): Promise<void> {
  await VpcService.UpdateVpcGroupAlias(ip, String(id), newAlias)
}

export async function DeleteVpc(ip: string, vpcID: number): Promise<void> {
  await VpcService.DeleteVpc(ip, String(vpcID))
}

/** 编辑VPC节点（PUT /mytVpc），保存后设备端自动重生成 xray 配置并同步云机 */
export async function UpdateVpcConfig(ip: string, vpcID: number, remarks: string, profile: VpcProfile): Promise<void> {
  await VpcService.UpdateVpcConfig(ip, vpcID, remarks, profile)
}

// --- VPC 容器规则 ---

export async function GetContainerRules(ip: string): Promise<VpcNodeRule[]> {
  return VpcService.GetContainerRules(ip) as Promise<VpcNodeRule[]>
}

export async function AddVpcRule(ip: string, name: string, vpcID: number): Promise<void> {
  await VpcService.AddVpcRule(ip, name, vpcID)
}

export async function AddVpcRuleBatch(ip: string, names: string[], vpcID: number): Promise<void> {
  await VpcService.AddVpcRuleBatch(ip, names, vpcID)
}

export async function DeleteVpcRule(ip: string, name: string): Promise<void> {
  await VpcService.DeleteVpcRule(ip, name)
}

export async function DeleteVpcRuleBatch(ip: string, names: string[]): Promise<void> {
  await VpcService.DeleteVpcRuleBatch(ip, names)
}

export async function EnableWhiteListDNS(ip: string, ruleID: number, enable: boolean, whiteListDns: string[] = []): Promise<void> {
  await VpcService.EnableWhiteListDNS(ip, ruleID, enable, whiteListDns)
}

/** 启用/停用云机VPC绑定（绑定关系保留：停用后流量直连，重新启用恢复走节点）。 */
export async function EnableVpcRule(ip: string, ruleID: number, enable: boolean): Promise<void> {
  await VpcService.EnableVpcRule(ip, ruleID, enable)
}

// --- VPC 测速 ---

export async function TestVpcLatency(ip: string, address: string): Promise<{ latency: string; msg: string }> {
  const result = await VpcService.TestVpcLatency(ip, address)
  return result as { latency: string; msg: string }
}

// --- 域名过滤 ---

export async function GetDomainFilters(ip: string, containerID: string): Promise<{ domains: string[] } | null> {
  const result = await VpcService.GetDomainFilters(ip, containerID)
  return result as { domains: string[] } | null
}

export async function SetDomainFilter(ip: string, containerID: string, domains: string[]): Promise<void> {
  await VpcService.SetDomainFilter(ip, containerID, domains)
}

export async function ClearDomainFilter(ip: string, containerID: string): Promise<void> {
  await VpcService.ClearDomainFilter(ip, containerID)
}

export async function GetGlobalDomainFilters(ip: string): Promise<{ domains: string[] } | null> {
  const result = await VpcService.GetGlobalDomainFilters(ip)
  return result as { domains: string[] } | null
}

export async function SetGlobalDomainFilter(ip: string, domains: string[]): Promise<void> {
  await VpcService.SetGlobalDomainFilter(ip, domains)
}

export async function ClearGlobalDomainFilter(ip: string): Promise<void> {
  await VpcService.ClearGlobalDomainFilter(ip)
}

// --- 域名直连 ---

export async function GetDomainDirects(ip: string, containerID: string): Promise<{ domains: string[] } | null> {
  const result = await VpcService.GetDomainDirects(ip, containerID)
  return result as { domains: string[] } | null
}

export async function SetDomainDirect(ip: string, containerID: string, domains: string[]): Promise<void> {
  await VpcService.SetDomainDirect(ip, containerID, domains)
}

export async function ClearDomainDirect(ip: string, containerID: string): Promise<void> {
  await VpcService.ClearDomainDirect(ip, containerID)
}

// --- 域名中转路由 ---

export async function GetDomainRoutes(ip: string, containerID: string): Promise<DomainRoute | null> {
  const result = await VpcService.GetDomainRoutes(ip, containerID)
  return result as DomainRoute | null
}

export async function SetDomainRoutes(ip: string, containerID: string, routes: DomainRouteEntry[]): Promise<void> {
  await VpcService.SetDomainRoutes(ip, containerID, routes)
}

export async function ClearDomainRoutes(ip: string, containerID: string): Promise<void> {
  await VpcService.ClearDomainRoutes(ip, containerID)
}

// --- 网卡管理 ---

interface PrivateNic { name: string; cidr: string }
interface NicInfo { netWork_eth0?: Record<string, unknown>; macVlan?: Record<string, unknown> }
interface NetworkInfo { gateway: string; networkCIDR: string; interface: string }

export async function ListPrivateNics(ip: string): Promise<PrivateNic[]> {
  return VpcService.ListPrivateNics(ip) as Promise<PrivateNic[]>
}

export async function CreatePrivateNic(ip: string, customName: string, cidr: string): Promise<void> {
  await VpcService.CreatePrivateNic(ip, customName, cidr)
}

export async function UpdatePrivateNic(ip: string, name: string, newCidr: string): Promise<void> {
  await VpcService.UpdatePrivateNic(ip, name, newCidr)
}

export async function DeletePrivateNic(ip: string, name: string): Promise<void> {
  await VpcService.DeletePrivateNic(ip, name)
}

export async function GetPublicNicInfo(ip: string): Promise<NicInfo | null> {
  const result = await VpcService.GetPublicNicInfo(ip)
  return result as NicInfo | null
}

export async function CreateMacVlan(ip: string, gw: string, subnet: string): Promise<void> {
  await VpcService.CreateMacVlan(ip, gw, subnet)
}

export async function UpdateMacVlan(ip: string, gw: string, subnet: string): Promise<void> {
  await VpcService.UpdateMacVlan(ip, gw, subnet)
}

export async function DeleteMacVlan(ip: string): Promise<void> {
  await VpcService.DeleteMacVlan(ip)
}

export async function GetNetworkInfo(ip: string): Promise<NetworkInfo | null> {
  const result = await VpcService.GetNetworkInfo(ip)
  return result as NetworkInfo | null
}

// --- 辅助 ---

function normalizeGroup(g: Record<string, unknown>): VpcGroup {
  if (typeof g.vpcs === 'string') {
    try { g.vpcs = JSON.parse(g.vpcs as string) } catch { g.vpcs = { list: [] } }
  }
  if (!(g.vpcs as Record<string, unknown>)?.list) g.vpcs = { list: (g.vpcs as unknown[]) || [] }
  return g as VpcGroup
}
