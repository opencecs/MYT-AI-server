import { TaskQueueItem, TaskStatus, CreateTaskInput, DeviceProgress } from './types'

export function genId(): string {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

export function eventData<T>(evt: unknown): T {
  return ((evt as { data?: T })?.data || evt) as T
}

export function isTerminal(status: TaskStatus): boolean {
  return status === 'completed' || status === 'failed' || status === 'canceled'
}

// ====== 辅助函数 ======

export function extractDeviceIps(targets: unknown[]): string[] {
  const ips = new Set<string>()
  for (const t of targets) {
    const obj = t as Record<string, unknown>
    const ip = obj.deviceIp || obj.deviceIP || obj.ip || (obj.device as Record<string, unknown>)?.ip
    if (typeof ip === 'string') ips.add(ip)
  }
  return Array.from(ips)
}

export function initDeviceProgress(input: CreateTaskInput, deviceIps: string[]): Record<string, DeviceProgress> {
  const dp: Record<string, DeviceProgress> = {}
  if (input.type === 'uploadImage') {
    for (const ip of deviceIps) dp[ip] = { total: 1, completed: 0, failed: 0 }
  } else if (input.type === 'uploadFile') {
    // 按设备统计云机数
    const targets = input.targets || []
    for (const t of targets) {
      const obj = t as Record<string, unknown>
      const ip = (obj.deviceIp || obj.deviceIP || obj.ip || '') as string
      const machines = (obj.machines as unknown[]) || []
      if (ip && machines.length > 0) {
        dp[ip] = dp[ip] || { total: 0, completed: 0, failed: 0 }
        dp[ip].total += machines.length
      }
    }
  }
  return dp
}

// ====== 后端任务合并辅助 ======

// 后端任务 ID 格式："0102150405_1"（时间戳_序号）；本地任务 ID 是 Date.now().toString(36)+random
export function isBackendTaskId(id: string): boolean {
  return /^\d{10}_\d+$/.test(id)
}

// upsertBackendTask：用后端任务更新或插入列表（后端任务为唯一来源）
export function upsertBackendTask(prev: TaskQueueItem[], t: TaskQueueItem): TaskQueueItem[] {
  const idx = prev.findIndex(x => x.id === t.id)
  if (idx < 0) return [t, ...prev].slice(0, 200)
  const next = [...prev]
  next[idx] = t
  return next
}

// mergeBackendTasks：初始化合并后端任务与本地任务（按 ID 去重，后端优先）
export function mergeBackendTasks(local: TaskQueueItem[], backend: TaskQueueItem[]): TaskQueueItem[] {
  const ids = new Set(backend.map(t => t.id))
  const kept = local.filter(t => !ids.has(t.id))
  return [...backend, ...kept].slice(0, 200)
}
