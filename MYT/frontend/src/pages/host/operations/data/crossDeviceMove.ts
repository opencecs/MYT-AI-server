import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import i18n from '../../../../i18n'

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)}MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)}GB`
}

export function formatElapsed(ms: number): string {
  const s = Math.floor(ms / 1000)
  if (s < 60) return i18n.t('host:timeFormat.seconds', { s })
  return i18n.t('host:timeFormat.minutes', { m: Math.floor(s / 60), s: s % 60 })
}

export interface RemoteDeviceInfo {
  ip: string
  name: string
  online: boolean
  slotCount: number
  occupiedSlots: Set<number>
}

/**
 * 按需获取单个设备的槽位信息（经 Go 代理，自动适配 local/public 模式）。
 * local 模式：device.ip 为局域网 IP。public 模式：device.ip 为空，用 device_id 走网关透传。
 * 占用只计开机中的容器（running/starting）——被 stopped 容器占的坑位允许移动覆盖（与创建实例同规则）。
 */
export async function fetchSingleDeviceSlots(
  device: { ip: string; device_name: string; device_id: string },
  getSlotCount: (deviceID: string) => Promise<number>,
): Promise<RemoteDeviceInfo> {
  const list = await SdkApiService.ListAndroids(device.device_id)
  const runningList: { indexNum: number; status: string }[] = list
  const occupied = new Set(
    runningList
      .filter(a => a.status === 'running' || a.status === 'starting')
      .map(a => a.indexNum),
  )
  let sc = 12
  try { sc = await getSlotCount(device.device_id) } catch {}
  return {
    ip: device.ip || device.device_id,
    name: device.device_name?.toUpperCase() || device.device_id,
    online: true,
    slotCount: sc,
    occupiedSlots: occupied,
  }
}
