import { useCallback, useState } from 'react'

const STORAGE_KEY = 'myt.cardScale'
const DEFAULT_SCALE = 80
const MIN = 25
const MAX = 150

/** 读取持久化的缩放值：非法（NaN/超范围 25-150）回退默认 80 */
function loadScale(): number {
  try {
    const saved = Number(localStorage.getItem(STORAGE_KEY))
    if (Number.isFinite(saved) && saved >= MIN && saved <= MAX) return saved
  } catch { /* localStorage 不可用（如隐私模式）：回退默认 */ }
  return DEFAULT_SCALE
}

/**
 * 卡片缩放滑条持久化（HostManagement / MaintenancePage 共享同一 key，全局记忆）。
 * 对齐 android-client 惯例（CloudManagement.vue 的 cloudManagement_screenshotScale：
 * localStorage + 加载/保存），key 统一前缀 myt.，与 i18n('language') 等既有用法同机制。
 */
export function useCardScale(): [number, (v: number) => void] {
  const [cardScale, setCardScaleState] = useState<number>(loadScale)
  const setCardScale = useCallback((v: number) => {
    setCardScaleState(v)
    try {
      localStorage.setItem(STORAGE_KEY, String(v))
    } catch { /* 写入失败静默：仅丢失持久化，不影响本次会话 */ }
  }, [])
  return [cardScale, setCardScale]
}
