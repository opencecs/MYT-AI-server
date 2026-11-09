import { DeviceState } from '../types/index'

/**
 * 设备对象字段级比较（不用 JSON.stringify）：心跳事件 500ms 一轮 × 几百台设备，
 * stringify 深比较在此量级下 CPU 开销显著。
 * 新增影响 DeviceItem/DeviceListItem 渲染的字段时须同步加入比较。
 */
export function deviceEqual(a: DeviceState, b: DeviceState): boolean {
  return a.device_id === b.device_id
    && a.ip === b.ip
    && a.state === b.state
    && a.order === b.order
    && a.device_type === b.device_type
    && a.device_name === b.device_name
    && a.version === b.version
    && a.bind_status === b.bind_status
    && a.disk_used === b.disk_used
    && a.disk_total === b.disk_total
}

/**
 * 引用稳定合并：未变化的设备复用旧对象 → DeviceItem/DeviceListItem 的 memo 生效，
 * 心跳/列表事件只重渲染真正变化的行。
 * 事件 payload 每次都是全新 JSON 对象（引用全新），直接 setDevices 会让 memo
 * 全部失效——2026-09-18 卡顿根因（DeviceSidebar 4 份 × 136 卡片 × 2 次/秒）。
 *
 * @returns 变化时返回合并后的新数组（未变项保留旧引用）；无任何变化时返回 prev
 */
export function mergeDeviceList(prev: DeviceState[], next: DeviceState[]): DeviceState[] {
  if (prev.length !== next.length) return next
  const prevMap = new Map(prev.map(d => [d.device_id, d]))
  let changed = false
  const merged = next.map(nd => {
    const old = prevMap.get(nd.device_id)
    if (!old || !deviceEqual(old, nd)) {
      changed = true
      return nd
    }
    return old
  })
  return changed ? merged : prev
}
