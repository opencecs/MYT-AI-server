/**
 * 截图卡片「容器比例」的页面级统计 hook。
 *
 * ## 为什么需要它
 *
 * 截图区容器比例是**整页统一**的（同一页面所有卡片同高，卡片墙才整齐），但各 ROM 的
 * 截图宽高比并不一致：V3/local 多为 9:16（720x1280 / 1080x1920 / 540x960），V4(tobox)
 * 是 9:20（720x1600），个别还有 9:18 等。容器比例一旦写死成 9:16，9:20 的图在容器里
 * 用 `object-contain` 会按高度铺满、宽度不足 → **左右各留一条黑边**；改成 `object-cover`
 * 则又按高度裁掉上下（状态栏/底部导航被切）。
 *
 * 出路：让容器比例匹配**当前页面主流截图比例**，图片用 `object-cover`。比例一致时零裁切
 * 零黑边；个别不同比例的 ROM 会有极轻微裁切（可接受）。跨页面自适应：V4 页面统计到 9:20、
 * V3 页面统计到 9:16，无需按模式写死。
 *
 * ## 统计口径
 *
 * 样本来自**真正渲染出来的 `<img>` 的 `onLoad`**（`naturalWidth/naturalHeight`，零额外开销：
 * 卡片本来就要渲染这张图，不额外 new Image 预加载）。卡片通过 `reportImageSize(key, w, h)`
 * 上报，本 hook 负责：
 *  1. **按 key 去重**：同一张卡（key = `ip_indexNum`）每轮换图都会 onLoad，比例没变就提前返回；
 *  2. **分桶计数**：比例按 3 位小数分桶（0.5625 / 0.45 / 0.5 各自一桶），累计计数只增不减，
 *     因此结果单调收敛，不会来回抖；
 *  3. **取众数**：票数最高的桶即主流比例；同票时优先保持当前比例（迟滞），否则取与当前最接近的；
 *  4. **样本下限**：少于 `MIN_SAMPLES` 票不采纳（避免首张异常比例把整页高度带偏）。
 *
 * 已知取舍：票数只按 key 增减，**不随列表筛选/切换设备清空**。因此用户在一页里先后看
 * 不同 ROM 的设备时，旧设备的票仍留在池中——这恰好符合「本页主流」的语义（谁多听谁的），
 * 且避免筛选一变就重置比例导致卡片高度跳动。只有部署模式切换（换了一批 ROM）才整体作废。
 *
 * 注意：`object-cover` 本身就消除了黑边，比例统计只决定「裁多少」。所以统计没收敛、
 * 或样本太少时退回兜底比例也只是轻微裁切，不会重新出现黑边。
 */

import { useCallback, useEffect, useRef, useState } from 'react'

/** 比例分桶精度：3 位小数足以区分 9:16(0.5625) / 9:20(0.45) / 9:18(0.5) 这类常见 ROM 比例。 */
const BUCKET_SCALE = 1000

/**
 * 采纳统计结果前至少需要的票数。
 *
 * 取 1：页面上只有一张卡（单设备单实例）时也能按它自己的真实比例定容器，
 * 不会因为样本不足而一直用兜底比例（V4 兜底 9:20 遇上 9:16 的 ROM 会横向裁掉约 20%）。
 * 混合分辨率页面靠 `pickDominant` 的迟滞（同票优先保持当前比例）稳定收敛，不会来回跳。
 */
export const MIN_SAMPLES = 1

/**
 * 归一化竖屏宽高比（宽/高，恒 ≤ 1）。
 *
 * 统一按「短边/长边」算：截图理论上都是竖屏（横屏是 CSS 旋转展示），万一个别 ROM 返回
 * 横图，也归一到同一个竖屏比例口径，不至于把它算成 1.78 这种离谱值去撑爆容器。
 */
export function portraitRatioOf(w: number, h: number): number | null {
  if (!w || !h || w <= 0 || h <= 0) return null
  return Math.round((Math.min(w, h) / Math.max(w, h)) * BUCKET_SCALE) / BUCKET_SCALE
}

/**
 * 挑战者需要领先当前比例票数的倍数（迟滞）。混合分辨率页面上，比例接近的两派票数会随
 * 滚动加载交替领先；没有迟滞就会来回翻，整页卡片高度反复跳动。
 */
const HYSTERESIS = 1.2

/** 票数最高的桶；同票优先「离当前最近」的桶，且挑战者必须明显超过当前比例的票数。 */
function pickDominant(counts: Map<number, number>, current: number): number | null {
  let bestCount = 0
  for (const c of counts.values()) if (c > bestCount) bestCount = c
  if (bestCount < MIN_SAMPLES) return null
  let best: number | null = null
  for (const [bucket, c] of counts) {
    if (c !== bestCount) continue
    if (best === null) { best = bucket; continue }
    if (Math.abs(bucket - current) < Math.abs(best - current)) best = bucket
  }
  if (best === null || best === current) return current
  // 当前比例还一票没有（= 还是兜底值）：第一张图就能定比例
  const currentCount = counts.get(current) ?? 0
  if (currentCount > 0 && bestCount < currentCount * HYSTERESIS) return current
  return best
}

/**
 * 统计页面主流截图比例。
 *
 * @param fallbackRatio 统计收敛前的兜底竖屏比例（宽/高，如 9:16 → 0.5625）
 * @param resetKey      样本失效的边界（部署模式）：模式切换 = 换了一批 ROM，旧样本作废
 * @returns `[主流竖屏比例, 图片尺寸上报函数]`
 */
export function useDominantAspect(fallbackRatio: number, resetKey: string): [number, (key: string, w: number, h: number) => void] {
  const [ratio, setRatio] = useState(fallbackRatio)
  // 当前比例的同步副本：onLoad 回调（事件，非渲染期）里要做同票比较，不能依赖闭包里的旧 state
  const ratioRef = useRef(ratio)
  ratioRef.current = ratio
  // key → 该卡最近一次上报的比例桶（去重 + 换桶时扣旧票）
  const samplesRef = useRef<Map<string, number>>(new Map())
  // 比例桶 → 票数
  const countsRef = useRef<Map<number, number>>(new Map())

  // 模式切换：换了一批 ROM，旧样本全部作废，回到兜底比例重新统计
  useEffect(() => {
    samplesRef.current.clear()
    countsRef.current.clear()
    ratioRef.current = fallbackRatio
    setRatio(fallbackRatio)
  }, [fallbackRatio, resetKey])

  const reportImageSize = useCallback((key: string, w: number, h: number) => {
    const bucket = portraitRatioOf(w, h)
    if (bucket === null) return
    const samples = samplesRef.current
    const prev = samples.get(key)
    if (prev === bucket) return // 同一张卡换图但比例没变：不做任何事（避免每轮 poll 都重算）
    const counts = countsRef.current
    if (prev !== undefined) {
      const left = (counts.get(prev) ?? 1) - 1
      if (left > 0) counts.set(prev, left)
      else counts.delete(prev)
    }
    samples.set(key, bucket)
    counts.set(bucket, (counts.get(bucket) ?? 0) + 1)

    const best = pickDominant(counts, ratioRef.current)
    // 只在比例真的变化时才 setState —— 否则每张图 onLoad 都会触发一轮整页重渲染
    if (best !== null && best !== ratioRef.current) {
      ratioRef.current = best
      setRatio(best)
    }
  }, [])

  return [ratio, reportImageSize]
}
