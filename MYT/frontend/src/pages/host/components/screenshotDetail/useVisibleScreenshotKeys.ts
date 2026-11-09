/**
 * 截图网格「视口激活」hook（flat 布局专用）。
 *
 * 背景：flat 网格可能渲染几百张运行中卡片，此前 renderedKeysRef（LRU 保护集）
 * 与按需补拉都以「全部渲染中的卡」为口径——视口外的卡也占着缓存、也参与补拉。
 *
 * 改造（业界通行的「可见才养」策略）：
 *  - IntersectionObserver 观察每张卡片容器（data-key 标记），维护视口内（含
 *    rootMargin 200px 预加载带）的 key 集合 visibleKeys；
 *  - LRU 保护集只放视口内 key → 视口外旧图被 writeScreenshotLru 清理（省内存）；
 *  - 按需补拉只补视口内缺图（滚入时再拉，滚入瞬间显示上一张回落图，不闪）。
 *
 * 观察 target 是 flat 网格的直接子卡片容器（ScreenshotCard 最外层 div 是 fragment 包裹，
 * 观察者需要稳定元素），由调用方在卡片外层套 `data-key` 容器；observer 在回调里按
 * `data-key` 反查 key。注意 IntersectionObserver 回调是高频事件（滚动/截图换图都触发
 * entry 变化），处理必须 O(entries)，绝不全量重扫。
 */

import { useEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'

/** 预加载带：视口外 200px 内的卡也算「可见」，提前养图，滚入时无加载感。 */
const ROOT_MARGIN = '200px'

/**
 * 观察滚动容器内所有 `[data-key]` 元素，返回「视口内（含预加载带）」的截图 key 集合。
 *
 * @param scrollRef 滚动容器（IntersectionObserver root；null 时退回视口 root）
 * @param observeRef 网格容器 ref：其变更（挂载/换布局）触发重扫，把新增卡片纳入观察
 * @param deps 重扫触发条件（卡片集合变化等，如 androids.length）
 */
export function useVisibleScreenshotKeys(
  scrollRef: RefObject<HTMLElement | null>,
  observeRef: RefObject<HTMLElement | null>,
  deps: unknown[],
): Set<string> {
  const [visibleKeys, setVisibleKeys] = useState<Set<string>>(() => new Set())
  // pending 用 ref 聚合同一轮回调的增删：observer 回调里不能 setState 累积读旧值
  //（Set 状态更新函数读到的是上一轮快照，entries 内先 add 后 delete 的抖动会出错）。
  const pendingRef = useRef<{ add: Set<string>; del: Set<string> } | null>(null)

  useEffect(() => {
    const grid = observeRef.current
    if (!grid) return
    const observer = new IntersectionObserver(
      entries => {
        let pending = pendingRef.current
        if (!pending) {
          pending = { add: new Set<string>(), del: new Set<string>() }
          pendingRef.current = pending
        }
        for (const entry of entries) {
          const key = (entry.target as HTMLElement).dataset.key
          if (!key) continue
          pending.del.delete(key)
          if (entry.isIntersecting) pending.add.add(key)
          else pending.del.add(key)
        }
        // 微任务落账：同轮回调先离开后进入的 key 抖动按最终状态记账
        queueMicrotask(() => {
          const p = pendingRef.current
          if (!p) return
          pendingRef.current = null
          setVisibleKeys(prev => {
            let changed = false
            const next = new Set(prev)
            for (const k of p.del) {
              if (next.delete(k)) changed = true
            }
            for (const k of p.add) {
              if (!next.has(k)) { next.add(k); changed = true }
            }
            return changed ? next : prev
          })
        })
      },
      { root: scrollRef.current ?? null, rootMargin: ROOT_MARGIN, threshold: 0 },
    )

    const scan = () => {
      observer.disconnect()
      for (const el of grid.querySelectorAll<HTMLElement>('[data-key]')) {
        observer.observe(el)
      }
    }
    scan()
    // 网格内卡片增删（搜索/筛选/开关机）后 DOM 里有新 data-key 元素，重扫纳入观察。
    // MutationObserver 比 deps 数组精确（筛选词每敲一个字母都变化，但 DOM 不一定变）。
    const mo = new MutationObserver(scan)
    mo.observe(grid, { childList: true, subtree: true })

    return () => { mo.disconnect(); observer.disconnect() }
    // deps 由调用方声明（卡片集合签名）；observeRef/scrollRef 是稳定 ref 不列入
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return visibleKeys
}
