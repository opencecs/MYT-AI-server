import i18n from '../../i18n'
import type { TaskQueueItem, TaskStatus } from '../../hooks/taskQueue/types'

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)}MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)}GB`
}

export function timeText(ts: number | null | undefined) {
  if (!ts) return ''
  return new Date(ts).toLocaleTimeString(i18n.language, { hour12: false })
}

// v2 设备标识显示映射（device_id 序列号 → IP）：任务创建时存的是操作标识
// （v2=序列号，本地=IP），显示时若命中设备表则换算为 IP——序列号对用户无意义。
// 映射由 TaskQueueProvider 挂载时建立并随 devicelist:updated 刷新。
let deviceIdToIP: Map<string, string> | null = null

export function setDeviceIdToIPMap(entries: { device_id: string; ip: string }[]) {
  const m = new Map<string, string>()
  for (const d of entries) {
    if (d.device_id && d.ip) m.set(d.device_id, d.ip)
  }
  deviceIdToIP = m
}

export function displayDeviceId(id: string): string {
  if (!deviceIdToIP || !id) return id
  return deviceIdToIP.get(id) ?? id
}

export function targetDisplay(task: TaskQueueItem) {
  const t = i18n.t.bind(i18n)
  if (task.type === 'uploadFile' && task.targets?.length) {
    const names: string[] = []
    for (const tg of task.targets as Record<string, unknown>[]) {
      for (const m of ((tg.machines || []) as Record<string, unknown>[])) {
        if (m.name) names.push(String(m.name))
      }
    }
    const u = [...new Set(names)]
    return u.length > 1 ? `${u[0]}${t('taskQueue.truncation.andMore', { count: u.length })}` : u[0] || ''
  }
  if (task.type === 'uploadImage' && task.targets?.length) {
    const ips = (task.targets as Record<string, unknown>[]).map(tg => displayDeviceId(String(tg.deviceIP || '')))
    const u = [...new Set(ips)]
    return u.length > 1 ? `${u[0]}${t('taskQueue.truncation.andMoreDevices', { count: u.length })}` : u[0] || ''
  }
  if (task.type === 'importAndroid' && task.machineName) {
    const slot = task.slotIndex != null ? ` → ${t('taskQueue.progress.slot', { slot: task.slotIndex })}` : ''
    return `${task.machineName}${slot}`
  }
  if (task.type === 'exportAndroid' && task.machineName) {
    return task.machineName as string
  }
  if (task.type === 'moveAndroid' && task.machineName) {
    const dst = task.dstIP ? ` → ${displayDeviceId(task.dstIP)}` : ''
    const slot = task.targetSlot != null ? ` ${t('taskQueue.progress.slot', { slot: task.targetSlot })}` : ''
    return `${task.machineName}${dst}${slot}`
  }
  if (task.deviceIps?.length > 1) return `${displayDeviceId(task.deviceIps[0])}${t('taskQueue.truncation.andMoreDevices', { count: task.deviceIps.length })}`
  if (task.deviceIps?.length === 1) return displayDeviceId(task.deviceIps[0])
  if (task.type === 'create' && task.targets?.length) {
    const slots = (task.targets as Record<string, unknown>[]).map(tg => t('taskQueue.progress.slot', { slot: tg.slot }))
    return slots.length > 1 ? `${slots[0]}${t('taskQueue.truncation.andMore', { count: slots.length })}` : slots[0] || ''
  }
  if (task.type === 'copy' && task.sourceName) return task.sourceName as string
  return ''
}

export function targetFullList(task: TaskQueueItem): string[] {
  const t = i18n.t.bind(i18n)
  if (task.type === 'uploadFile' && task.targets?.length) {
    const items: string[] = []
    for (const tg of task.targets as Record<string, unknown>[]) {
      for (const m of ((tg.machines || []) as Record<string, unknown>[])) {
        if (m.name) items.push(String(m.name))
      }
    }
    return items
  }
  if (task.type === 'uploadImage' && task.targets?.length) {
    return (task.targets as Record<string, unknown>[]).map(tg => displayDeviceId(String(tg.deviceIP || '')))
  }
  if (task.deviceIps?.length) return task.deviceIps.map(displayDeviceId)
  if (task.type === 'create' && task.targets?.length) {
    return (task.targets as Record<string, unknown>[]).map(tg => t('taskQueue.progress.slot', { slot: tg.slot }))
  }
  return []
}

export function isTerminal(status: TaskStatus) {
  return status === 'completed' || status === 'failed' || status === 'canceled'
}
