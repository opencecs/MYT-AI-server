import { useSyncExternalStore } from 'react'

const SWITCH_KEY = 'myt.deviceSingleGroup'

/** 模块级状态：所有订阅组件共享同一份（设置页切换 → 右键菜单/拖拽立即生效） */
let singleGroup = loadSwitch()
const listeners = new Set<() => void>()

function loadSwitch(): boolean {
  try {
    const raw = localStorage.getItem(SWITCH_KEY)
    if (raw !== null) return raw === 'true'
  } catch { /* localStorage 不可用：回退默认（仅运行中） */ }
  return true
}

const getSnapshot = () => singleGroup

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

function setSingleGroup(next: boolean) {
  if (next === singleGroup) return
  try {
    localStorage.setItem(SWITCH_KEY, String(next))
  } catch { /* 写入失败静默：仅丢失持久化，不影响本次会话 */ }
  singleGroup = next
  for (const l of listeners) l()
}

/**
 * 「设备单分组」设置（持久化 localStorage，key 前缀 myt. 与其他 hook 一致）。
 * 模块级 store + useSyncExternalStore：设置页与消费组件共享同一份状态。
 * 默认 true（单分组）= 完全保持现状：菜单组名点击即切换分组、拖拽移动语义；
 * 关闭后为多分组：组名点击仅添加、菜单多「移除分组」逐组项、拖拽仅添加。
 * 不清洗存量数据：单分组模式下设备按其所属的第一个分组展示/切换。
 */
export function useDeviceGroupMode(): [boolean, (v: boolean) => void] {
  return [useSyncExternalStore(subscribe, getSnapshot), setSingleGroup]
}
