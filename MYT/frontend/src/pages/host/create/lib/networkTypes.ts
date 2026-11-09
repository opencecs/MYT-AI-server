// 容器创建相关网络类型（网卡 / VPC）

// 私有网卡（myt_bridge 网桥）
export interface PrivateNic {
  name: string
  cidr: string
}

// VPC 节点
export interface VpcNode {
  id: number
  protocol: string
  remarks: string
  profile: string
}

// VPC 分组（内嵌节点列表）
export interface VpcGroup {
  id: number
  alias: string
  url: string
  type: number
  vpcs: { list: VpcNode[] }
}

// 展平后的 VPC 选项：分组 -> 节点列表
export interface FlatVpcOption {
  groupId: number
  groupAlias: string
  nodeId: number
  nodeRemarks: string
}
