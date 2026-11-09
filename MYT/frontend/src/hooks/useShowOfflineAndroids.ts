import { useSyncExternalStore } from 'react'

const STORAGE_KEY = 'myt.showOfflineAndroids'

/** 模块级状态：所有订阅组件共享同一份（设置页切换 → 云机/主机 Tab 立即重渲染） */
let value = load()
const listeners = new Set<() => void>()

function load(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'true'
  } catch { /* localStorage 不可用：回退默认（仅运行中） */ }
  return false
}

const getSnapshot = () => value

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

function set(next: boolean) {
  if (next === value) return
  try {
    localStorage.setItem(STORAGE_KEY, String(next))
  } catch { /* 写入失败静默：仅丢失持久化，不影响本次会话 */ }
  value = next
  for (const l of listeners) l()
}

/**
 * 「云机展示：运行中 / 全部」设置（持久化 localStorage，key 与 useCardScale 同前缀 myt.）。
 * 模块级 store + useSyncExternalStore：设置页与消费组件（云机 Tab / 主机 Tab）共享同一份
 * 状态——设置页切换后消费端立即重渲染，无需刷新或事件转发。
 * 默认 false = 仅显示运行中（含 starting）云机，保持既有行为。
 */
export function useShowOfflineAndroids(): [boolean, (v: boolean) => void] {
  return [useSyncExternalStore(subscribe, getSnapshot), set]
}
