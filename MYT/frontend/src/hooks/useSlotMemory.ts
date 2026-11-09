import { useSyncExternalStore } from 'react'

const SWITCH_KEY = 'myt.slotMemory'
const SLOTS_KEY = 'myt.lastCreateSlots'

/** 模块级状态：所有订阅组件共享同一份（设置页切换 → 创建弹窗立即生效） */
let enabled = loadSwitch()
const listeners = new Set<() => void>()

function loadSwitch(): boolean {
  try {
    return localStorage.getItem(SWITCH_KEY) === 'true'
  } catch { /* localStorage 不可用：回退默认（仅运行中） */ }
  return false
}

const getSnapshot = () => enabled

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

function setEnabled(next: boolean) {
  if (next === enabled) return
  try {
    localStorage.setItem(SWITCH_KEY, String(next))
  } catch { /* 写入失败静默：仅丢失持久化，不影响本次会话 */ }
  enabled = next
  for (const l of listeners) l()
}

/**
 * 「坑位记忆」设置（持久化 localStorage，key 前缀 myt. 与其他 hook 一致）。
 * 模块级 store + useSyncExternalStore：设置页与消费组件共享同一份状态。
 * 默认 false = 新建云机表单默认不选任何坑位，保持既有行为；
 * 开启后，再次打开创建表单自动回填上一次成功创建时选择的坑位。
 */
export function useSlotMemory(): [boolean, (v: boolean) => void] {
  return [useSyncExternalStore(subscribe, getSnapshot), setEnabled]
}

/**
 * 读取上一次创建成功时选择的坑位（跨设备共享一份记忆；回填时需按当前设备
 * allSlots 过滤有效性）。localStorage 不可用或无记录时返回空数组。
 */
export function getLastCreateSlots(): number[] {
  try {
    const raw = localStorage.getItem(SLOTS_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((v): v is number => typeof v === 'number' && v > 0)
  } catch { return [] }
}

/** 写入上一次创建成功时选择的坑位（去重、去非法值） */
export function setLastCreateSlots(slots: number[]) {
  const cleaned = Array.from(new Set(slots.filter(v => typeof v === 'number' && v > 0)))
  try {
    localStorage.setItem(SLOTS_KEY, JSON.stringify(cleaned))
  } catch { /* 写入失败静默：仅丢失记忆，不影响创建流程 */ }
}
