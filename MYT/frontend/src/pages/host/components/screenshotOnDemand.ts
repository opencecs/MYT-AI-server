/**
 * 截图「按需拉取」（`ScreenshotView` 的 screenshotMap 补充通道）。
 *
 * 背景：screenshotMap 的写入走 screenshotLru.ts——渲染中的图永不淘汰，非渲染旧图
 * 会被清理；此外 poll 失败会把 key 写成 null（本地无图）。清掉/缺失只意味着
 * 「本地没有图了」，后端仍有这张截图。此前清理时会连 versionRef 一并清掉，下一轮 poll
 * 于是把它判为「未同步」→ 触发**全量**重拉（几百张图一起回来），
 * 表现为画面抖动 + 截图区反复闪「加载中」。
 *
 * 改造后两头分工：
 *  - 清理**保留** versionRef，poll 的版本对比不再因缺图而抖（见 ScreenshotView 的
 *    applyScreenshotUpdates）；
 *  - 真正渲染到某张卡、且它 statusMap 已是 'screenshot' 却没有图（被清理或尚未取回），
 *    才定向补拉**这一批** key —— 即本模块。
 *
 * 三条防抖措施：
 *  1) **key 级 in-flight 集合与 poll 共享**：同一 key 不会同时被 poll 与按需通道各拉一次；
 *     正在飞的 key 也不会重复发起。
 *  2) **单轮合批上限**（ON_DEMAND_BATCH_MAX）：超限的 key 留在缺失集合里，等这一批写回缓存
 *     （screenshotMap 变化 → 缺失集合重算 → effect 再触发）自然接力，不会一次性把所有缺失
 *     的图全拉回来。
 *  3) **按 key 冷却**（ON_DEMAND_COOLDOWN_MS）：某 key 补拉失败（或拉回空值）后，冷却期内
 *     不再重复请求，把抖动压成低速自愈而不是死循环。
 *
 * 注意：本通道**只往缓存写图，不动 statusMap**。卡片状态是「容器是否有截图」的语义，
 * 不能因为本地淘汰而回退（否则 readyKeys 掉线 → 批量投屏按钮失效）；缺图时的「加载中」
 * 由 ScreenshotCard 按「有状态但无 dataUrl」自然呈现。
 */

import { useEffect, useRef } from 'react'
import type { RefObject } from 'react'
import * as ScreenshotService from '../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import type { CardStatus } from '../../../utils/containerStatus'

/** 单轮按需补拉的最大张数（其余留给下一轮接力，避免一次拉回过多 IPC 载荷）。 */
export const ON_DEMAND_BATCH_MAX = 24
/** 同一 key 的补拉冷却（毫秒）：防止失败/空结果场景下高频重试抖动。 */
export const ON_DEMAND_COOLDOWN_MS = 30_000

/**
 * 从「当前实际渲染的运行中卡片 key」里挑出「后端有截图（statusMap=‘screenshot’）但本地缓存
 * 无图（被清理或尚未取回）」的 key。纯函数，不做任何副作用，便于推理与复用（device/flat 两布局同源）。
 *
 * renderedKeys 允许调用方先按视口过滤（flat 布局的视口激活：视口外缺图不补拉，
 * 滚入视口时 visibleKeys 变化 → 调用方重算 → 自然补拉），本函数不做内嵌过滤假设。
 */
export function collectMissingScreenshotKeys(
  renderedKeys: string[],
  statusMap: Record<string, CardStatus>,
  screenshotMap: Record<string, string>,
): string[] {
  const missing: string[] = []
  for (const key of renderedKeys) {
    if (screenshotMap[key]) continue
    if (statusMap[key] !== 'screenshot') continue
    missing.push(key)
  }
  return missing
}

export interface ScreenshotOnDemandOptions {
  /** 当前渲染中「有截图状态但缓存无图」的 key（由 collectMissingScreenshotKeys 得到）。 */
  missingKeys: string[]
  /** 与 poll 共享的 key 级 in-flight 集合（引用稳定）。 */
  inFlightKeys: RefObject<Set<string>>
  /** 取回后写入截图缓存（宿主传入，见 screenshotLru.ts）。 */
  applyScreenshots: (updates: Record<string, string>) => void
}

/**
 * 按需补拉缺失截图。missingKeys 内容变化才触发（用 join 出的签名做依赖，避免数组引用抖动
 * 引起空跑）；本轮取回的图写入缓存后 missingKeys 会自然收敛，effect 不会自激。
 */
export function useScreenshotOnDemand({ missingKeys, inFlightKeys, applyScreenshots }: ScreenshotOnDemandOptions): void {
  const requestedAtRef = useRef<Map<string, number>>(new Map())
  const mountedRef = useRef(true)
  const signature = missingKeys.join('\n')

  useEffect(() => {
    mountedRef.current = true
    return () => { mountedRef.current = false }
  }, [])

  useEffect(() => {
    const cooldown = requestedAtRef.current
    if (!signature) {
      cooldown.clear()
      return
    }
    const now = Date.now()
    const missing = signature.split('\n')
    // 冷却表只保留「当前仍缺失」的 key：已补回（或已不渲染）的条目及时清理，避免无限增长
    const alive = new Set(missing)
    for (const key of [...cooldown.keys()]) if (!alive.has(key)) cooldown.delete(key)

    const inFlight = inFlightKeys.current
    const batch: string[] = []
    for (const key of missing) {
      if (batch.length >= ON_DEMAND_BATCH_MAX) break
      const at = cooldown.get(key)
      if (at !== undefined && now - at < ON_DEMAND_COOLDOWN_MS) continue
      if (inFlight?.has(key)) continue
      batch.push(key)
    }
    if (batch.length === 0) return

    for (const key of batch) {
      cooldown.set(key, now)
      inFlight?.add(key)
    }
    ScreenshotService.GetScreenshots(batch)
      .then((data) => {
        if (!mountedRef.current) return
        const raw = data as Record<string, string> | null | undefined
        const updates: Record<string, string> = {}
        for (const key of batch) {
          const url = raw?.[key]
          if (url) updates[key] = url
        }
        if (Object.keys(updates).length > 0) applyScreenshots(updates)
      })
      .catch(() => { /* 失败不致命：冷却过后由下一轮渲染/事件重试 */ })
      .finally(() => {
        for (const key of batch) inFlight?.delete(key)
      })
  }, [signature, applyScreenshots, inFlightKeys])
}
