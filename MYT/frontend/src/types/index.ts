export interface DeviceState {
  ip: string
  device_type: string
  device_id: string
  device_name: string
  version: string
  port?: number
  state: number     // 0=在线, 1=离线, 2=认证失败
  order: number     // 0=未勾选, >0=勾选顺序
  disk_used?: number
  disk_total?: number
  bind_status?: number // 0=未绑定, 1=已绑定, 2=被绑定
  super_sdk_ready?: boolean // v2 模式：超级 SDK（81）就绪状态；缺省=未就绪或未知
}

export type ViewMode = 'screenshot' | 'slot'
export type Orientation = 'portrait' | 'landscape'

export interface MirrorItem {
  id: string
  name: string
  url: string
  ttype2: string[]
  os_ver: string
  sys_ver: string
  sys_ver_des: string
  udesc: string
}

export interface SavedMirror {
  id: string
  name: string
  url: string
  ttype2: string
  os_ver: string
  sys_ver: string
  sys_ver_des: string
  udesc: string
  saved_at: string
}

export interface PushResult {
  deviceIP: string
  success: boolean
  error?: string
}

export interface AndroidInfo {
  id: string
  name: string
  status: string
  image: string
  indexNum: number
  deviceId: string
  ip: string
  networkName?: string
  containerIP?: string
  portBindings?: Record<string, { HostIp: string; HostPort: string }[]>
  modelPath?: string
  cpuUsage?: number   // 容器 CPU 占用百分比（running 时有效）
  memUsage?: number   // 容器内存使用量（字节，running 时有效）
  memLimit?: number   // 容器内存上限（字节，0=无限制）
  diskUsage?: number  // 容器磁盘使用量（字节，running 时有效）
  /** 容器标签（设备端下发：WS 快照/周期推送 + REST /android 同源）；旧本地坑位标签已停止消费 */
  tags?: string[]
  /** 设备档案（V4 专属：WS snapshot 下发 / configure 后乐观回填）。V3 恒为 undefined */
  profile?: AndroidProfile
}

/** 云机设备档案（V4 tobox_player DeviceProfile，字段 snake_case 与服务端一致） */
export interface AndroidProfile {
  latitude: number
  longitude: number
  country: string
  operator_numeric: string
  operator_name: string
  cell_id: string
  phone_number: string
  root: boolean
}

export interface SlotExpiry {
  index: number
  expire_time: string
  state: number  // 0=正常, 1=即将到期, 2=已过期
}

export interface SlotInfo {
  index: number
  expire_time: string
  status: string
  state: number
}

export interface VpcNode {
  id: number
  protocol: string
  remarks: string
  profile: string
}

/** VPC 节点配置（PUT /mytVpc 的 profile 字段，对应 Go sdkapi.VpcProfile） */
export interface VpcProfile {
  configType?: number
  remarks?: string
  server?: string
  serverPort?: string
  password?: string
  method?: string
  flow?: string
  network?: string
  headerType?: string
  host?: string
  path?: string
  security?: string
  sni?: string
  alpn?: string
  fingerprint?: string
  insecure?: boolean
}

export interface VpcGroup {
  id: number
  alias: string
  url: string
  type: number
  vpcs: { list: VpcNode[] }
}

export interface VpcNodeRule {
  id: number
  containerID: string
  containerIP: string
  containerName: string
  containerState: string
  status: number
  groupName: string
  vpcRemarks: string
  whiteListDns: string[]
  /** 绑定开关（新固件返回）：true-启用（流量走该节点），false-停用（绑定保留，流量直连）。老固件缺省=启用。 */
  enabled?: boolean
}

export interface DomainRouteEntry {
  domains: string[]
  vpcID: number
}

export interface DomainRoute {
  routes: DomainRouteEntry[]
}

export interface PrivateNic {
  name: string
  cidr: string
}

export interface PublicNicData {
  gw: string
  subnet: string
  type: 'physical' | 'macvlan'
  isNew: boolean
}

export interface NetworkInfo {
  gateway: string
  networkCIDR: string
  interface: string
}

export interface PhoneModel {
  id: string
  name: string
  android_version: string
}

export interface CountryCode {
  countryName: string
  countryCode: string
}

export interface PhoneModelEntry {
  name: string
  mtimestamp: number
}

export interface DeviceGroup {
  id: string
  name: string
  deviceIds: string[]
  createdAt: string
  updatedAt: string
}

export interface AdbResultItem {
  device_id: string
  success: boolean
  output?: string
  error?: string
}
