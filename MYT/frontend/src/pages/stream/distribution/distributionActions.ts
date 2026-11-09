// 分发/推流通用逻辑：容器端口解析（与后端 internal/ports 语义一致）、
// 设备侧 modifydev 配置链、P2P 启停、状态检查、投屏。
// 行为对齐 pc-android-client StreamManagement.vue（confirmDistribute / startP2PDistribution /
// stopP2PDistribution / checkSingleDistributionStatus / openProjectionForDistribution）。
import * as StreamService from '../../../../bindings/github.com/moyunteng/myt/streamservicewrapper'
import * as ScreencastService from '../../../../bindings/github.com/moyunteng/myt/screencastservicewrapper'
import type { AndroidInfo, DeviceState } from '../../../types/index'
import type { Distribution } from '../streamTypes'

// ---------- 容器端口解析（对齐 MYT/internal/ports/ports.go） ----------

const PORT_MYTOS = 9082    // 安卓 API / MYTOS
const PORT_CAMERA = 10006  // 摄像头 TCP

// isMacvlanDirect：networkName 含 myt → 容器 IP 直连（SDK 端已对 myt 网络容器统一做端口映射）
function isMacvlanDirect(m: AndroidInfo | null | undefined): boolean {
  if (!m) return false
  return (m.networkName || '').toLowerCase().includes('myt')
}

// 控制面主机：macvlan 直连用容器 IP，否则设备 IP（公网 IP:端口 形态剥端口）
export function getControlHost(deviceIp: string, m: AndroidInfo | null | undefined): string {
  if (isMacvlanDirect(m) && m?.containerIP) return m.containerIP
  const ip = deviceIp || ''
  return ip.includes(':') ? ip.split(':')[0] : ip
}

// 映射端口：桥接直接用容器端口，NAT 查 portBindings，无映射返回 null（不公式兜底）
function resolveMappedPort(m: AndroidInfo | null | undefined, containerPort: number): number | null {
  if (!m) return null
  if (isMacvlanDirect(m)) return containerPort
  if (m.portBindings) {
    for (const proto of ['tcp', 'udp']) {
      const b = m.portBindings[`${containerPort}/${proto}`]
      const p = b && b.length > 0 ? Number(b[0].HostPort) : 0
      if (p > 0) return p
    }
  }
  return null
}

export function resolveMytosPort(m: AndroidInfo | null | undefined): number | null {
  return resolveMappedPort(m, PORT_MYTOS)
}

export function resolveCameraPort(m: AndroidInfo | null | undefined): number | null {
  return resolveMappedPort(m, PORT_CAMERA)
}

// ---------- 流地址构造（pc-android buildStreamPath / buildProtocolType 同构） ----------

export function buildStreamPath(localIP: string, protocol: string, streamName: string): string {
  if (protocol === 'rtmp') return `rtmp://${localIP}:1935/live/${streamName}`
  return `http://${localIP}:8083/live/${streamName}.flv`
}

export function buildProtocolType(protocol: string): string {
  return protocol === 'rtmp' ? 'rtmp' : 'webrtc'
}

// ---------- 通用 HTTP 代理 ----------

export async function proxyGet(url: string): Promise<any> {
  const result = await StreamService.ProxyHttpGet(url) as unknown
  if (typeof result === 'string') {
    try { return JSON.parse(result) } catch { return { code: 500, reason: 'Request Parse Error' } }
  }
  return result || { code: 500, reason: 'Empty Response' }
}

// ---------- 设备 / 云机解析 ----------

export function findMachine(androids: AndroidInfo[], deviceIp: string, cloudMachineId: string): AndroidInfo | null {
  if (!cloudMachineId) return null
  return androids.find(a => a.ip === deviceIp && (a.id === cloudMachineId || a.name === cloudMachineId)) || null
}

// ---------- 分发链（转发流 / 修改流共用） ----------

// modifydev cmd=4 + camera start（pc-android confirmDistribute / syncDistributionParameters 同构）
// localIP 须为面向目标设备可达的源地址（TUN 代理机器上通用 localIP 可能不可达 → 黑屏）
export async function applyDistribution(params: {
  localIP: string
  deviceIp: string
  machine: AndroidInfo
  streamName: string
  protocol: string
  resolution: string
}): Promise<void> {
  const { localIP, deviceIp, machine, streamName, protocol, resolution } = params
  const mytosPort = resolveMytosPort(machine)
  if (!mytosPort) throw new Error('mytosPortMissing')
  const baseUrl = `http://${getControlHost(deviceIp, machine)}:${mytosPort}`
  const setUrl = `${baseUrl}/modifydev?cmd=4&type=${buildProtocolType(protocol)}&path=${encodeURIComponent(buildStreamPath(localIP, protocol, streamName))}&resolution=${resolution}`
  const res = await proxyGet(setUrl)
  if (res?.code !== 200) throw new Error(res?.reason || 'setFailed')
  try { await proxyGet(`${baseUrl}/camera?cmd=start`) } catch { /* 设备侧相机启动失败不阻断分发 */ }
}

// modifydev cmd=4&type=camera（不带 path）+ camera start —— P2P 添加 / p2p 参数同步用
export async function applyCameraMode(params: {
  deviceIp: string
  machine: AndroidInfo
  resolution: string
}): Promise<void> {
  const { deviceIp, machine, resolution } = params
  const mytosPort = resolveMytosPort(machine)
  if (!mytosPort) throw new Error('mytosPortMissing')
  const baseUrl = `http://${getControlHost(deviceIp, machine)}:${mytosPort}`
  const res = await proxyGet(`${baseUrl}/modifydev?cmd=4&type=camera&resolution=${resolution}`)
  if (res?.code !== 200) throw new Error(res?.reason || 'setFailed')
  try { await proxyGet(`${baseUrl}/camera?cmd=start`) } catch { /* 同上 */ }
}

// ---------- P2P 启停 ----------

// P2P 添加即启动（pc-android confirmP2PDistribute 同构）
export async function addAndStartP2P(params: {
  localIP: string
  deviceIp: string
  machine: AndroidInfo
  streamName: string
  listenPort: number
  resolution: string
}): Promise<string> {
  const { localIP, deviceIp, machine, streamName, listenPort, resolution } = params
  const mytosPort = resolveMytosPort(machine)
  const cameraPort = resolveCameraPort(machine)
  if (!mytosPort) throw new Error('mytosPortMissing')
  if (!cameraPort) throw new Error('cameraPortMissing')
  await applyCameraMode({ deviceIp, machine, resolution })
  const p2pHost = getControlHost(deviceIp, machine) || deviceIp
  const res = await StreamService.StartP2P(p2pHost, cameraPort, streamName, listenPort) as any
  if (!res?.success) throw new Error(res?.message || 'p2pStartFailed')
  return res?.listenUrl || `srt://${localIP}:${listenPort}`
}

// P2P 重新启动（pc-android startP2PDistribution 同构：cmd=4 带 srt 路径，老镜像回退 cmd=14）
export async function startP2PDistribution(params: {
  localIP: string
  deviceIp: string
  machine: AndroidInfo
  streamName: string
  listenPort: number
  resolution: string
}): Promise<string> {
  const { localIP, deviceIp, machine, streamName, listenPort, resolution } = params
  const mytosPort = resolveMytosPort(machine)
  const cameraPort = resolveCameraPort(machine)
  if (!mytosPort) throw new Error('mytosPortMissing')
  if (!cameraPort) throw new Error('cameraPortMissing')
  const baseUrl = `http://${getControlHost(deviceIp, machine)}:${mytosPort}`
  const listenUrl = `srt://${localIP}:${listenPort}`
  const setUrl = `${baseUrl}/modifydev?cmd=4&type=camera&path=${encodeURIComponent(listenUrl)}&resolution=${resolution}`
  const res = await proxyGet(setUrl)
  if (res?.code !== 200) {
    const reason = res?.reason || ''
    if (reason.includes('path is empty')) {
      const fb = await proxyGet(`${baseUrl}/modifydev?cmd=14&type=camera`)
      if (fb?.code !== 200) throw new Error('oldMirror')
    } else {
      throw new Error(reason || 'setFailed')
    }
  }
  try { await proxyGet(`${baseUrl}/camera?cmd=stop`) } catch { /* 忽略 */ }
  const p2pHost = getControlHost(deviceIp, machine) || deviceIp
  const r = await StreamService.StartP2P(p2pHost, cameraPort, streamName, listenPort) as any
  if (!r?.success) throw new Error(r?.message || 'p2pStartFailed')
  return r?.listenUrl || listenUrl
}

// P2P 停止（pc-android stopP2PDistribution 同构）
export async function stopP2PDistribution(params: {
  deviceIp: string
  machine: AndroidInfo | null
  streamName: string
  mappedPort?: number
}): Promise<void> {
  const cameraPort = params.mappedPort && params.mappedPort > 0
    ? params.mappedPort
    : resolveCameraPort(params.machine)
  if (!cameraPort) throw new Error('cameraPortMissing')
  const p2pHost = getControlHost(params.deviceIp, params.machine) || params.deviceIp
  const res = await StreamService.StopP2P(p2pHost, cameraPort, params.streamName) as any
  if (!res?.success) throw new Error(res?.message || 'p2pStopFailed')
}

// ---------- 状态检查（pc-android checkSingleDistributionStatus 同构） ----------

export async function checkDistributionStatus(record: Distribution, ctx: {
  devices: DeviceState[]
  androids: AndroidInfo[]
}): Promise<Partial<Distribution>> {
  if (!ctx.devices.some(d => d.ip === record.deviceIp)) {
    return { status: 'unknown', statusReason: 'deviceOffline' }
  }
  const machine = findMachine(ctx.androids, record.deviceIp, record.cloudMachineId)
  if (record.protocol === 'p2p') {
    if (!machine) return { status: 'unknown', statusReason: 'machineNotFound' }
    const cameraPort = resolveCameraPort(machine)
    if (!cameraPort) return { status: 'unknown', statusReason: 'portNotMapped' }
    const p2pHost = getControlHost(record.deviceIp, machine) || record.deviceIp
    const res = await StreamService.GetP2PStatus(p2pHost, cameraPort, record.streamName) as any
    if (res?.running) return { status: 'active', statusReason: 'running' }
    return { status: 'inactive', statusReason: res?.message || 'stopped' }
  }
  if (!machine) return { status: 'unknown', statusReason: 'machineNotFound' }
  const mytosPort = resolveMytosPort(machine)
  if (!mytosPort) return { status: 'unknown', statusReason: 'portNotMapped' }
  const res = await proxyGet(`http://${getControlHost(record.deviceIp, machine)}:${mytosPort}/modifydev?cmd=5`)
  if (res?.code === 200) {
    const remotePath = (res.path || '').replace(/\\/g, '')
    const expected = record.protocol === 'rtmp'
      ? [`live/${record.streamName}`]
      : [`live/${record.streamName}.flv`, `live/${record.streamName}`]
    if (expected.some(k => remotePath.includes(k))) return { status: 'active', statusReason: 'normal' }
    if (!remotePath) return { status: 'inactive', statusReason: 'notConfigured' }
    return { status: 'inactive', statusReason: `pathMismatch:${remotePath}` }
  }
  return { status: 'unknown', statusReason: `queryFailed:${res?.reason || ''}` }
}

// ---------- 投屏（复用 ScreencastService 单台投屏链路） ----------

export async function projectDistribution(record: Distribution, androids: AndroidInfo[]): Promise<void> {
  const machine = findMachine(androids, record.deviceIp, record.cloudMachineId)
  if (!machine) throw new Error('machineNotFound')
  const running = await ScreencastService.IsProjectionRunning(record.deviceIp, machine.indexNum)
  if (running) {
    await ScreencastService.BringProjectionToFront(record.deviceIp, machine.indexNum)
    return
  }
  await ScreencastService.StartBatchIndividualProjections([
    { deviceIP: record.deviceIp, indexNum: machine.indexNum, orientation: 'portrait' },
  ], 1.0, true)
}

// ---------- 错误文案 ----------

type TFunc = (key: string, opts?: Record<string, unknown>) => string

export function distErrorText(e: unknown, t: TFunc): string {
  const msg = e instanceof Error ? e.message : String(e || '')
  switch (msg) {
    case 'mytosPortMissing': return t('stream:dist.portNotFound9082')
    case 'cameraPortMissing': return t('stream:dist.portNotFound10006')
    case 'oldMirror': return t('stream:dist.oldMirrorError')
    case 'setFailed': return t('stream:dist.setFailed')
    case 'p2pStartFailed': return t('stream:dist.startFailed')
    case 'p2pStopFailed': return t('stream:dist.stopFailed')
    case 'machineNotFound': return t('stream:dist.machineInfoNotFound')
    default: return msg || t('stream:dist.requestError')
  }
}
