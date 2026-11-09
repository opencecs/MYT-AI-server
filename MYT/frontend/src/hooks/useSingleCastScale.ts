import { useCallback, useEffect, useState } from 'react'
import * as ScreencastService from '../../bindings/github.com/moyunteng/myt/screencastservicewrapper'

const STORAGE_KEY = 'myt.singleCastScale'
const DEFAULT_SCALE = 70
const MIN = 35
const MAX = 100

/** 读取 localStorage 持久化值：非法（NaN/超范围 35-100）回退默认 70（对齐壳子画面 798 高） */
function loadScale(): number {
  try {
    const saved = Number(localStorage.getItem(STORAGE_KEY))
    if (Number.isFinite(saved) && saved >= MIN && saved <= MAX) return saved
  } catch { /* localStorage 不可用（如隐私模式）：回退默认 */ }
  return DEFAULT_SCALE
}

/**
 * 单台投屏窗口大小持久化（localStorage 即时生效 + Go config 落档）。
 * Go 侧投屏链路以 config 为准（竖屏单台 ComputeSinglePortraitLayout 定尺寸：
 * 画面高基准 1140 × 倍率，默认 0.7 → 画面 448x798 高度对齐壳子），
 * localStorage 仅用于 UI 滑条回显；Go 写档失败静默——下次投屏仍用旧值，可接受。
 */
export function useSingleCastScale(): [number, (v: number) => void] {
  const [scale, setScaleState] = useState<number>(loadScale)

  // 挂载时以 Go config 为准回显（多窗口/重装 localStorage 过期场景）
  useEffect(() => {
    ScreencastService.GetSingleCastScale()
      .then((v: number) => {
        const pct = Math.round(v * 100)
        if (pct >= MIN && pct <= MAX) {
          setScaleState(pct)
          try { localStorage.setItem(STORAGE_KEY, String(pct)) } catch { /* 静默 */ }
        }
      })
      .catch(() => {})
  }, [])

  const setScale = useCallback((v: number) => {
    setScaleState(v)
    try { localStorage.setItem(STORAGE_KEY, String(v)) } catch { /* 静默 */ }
    ScreencastService.SetSingleCastScale(v / 100).catch(() => {})
  }, [])

  return [scale, setScale]
}
