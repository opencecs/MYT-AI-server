/**
 * 截图卡片网格的「最小列宽」计算 + 网格可用宽度测量。
 *
 * ## 为什么需要它（bug 复盘）
 *
 * 卡片宽度由网格列宽决定，截图区高度由容器的 CSS `aspect-ratio` 反推：
 *   竖屏：H = W / portraitRatio            （portraitRatio = 截图宽/高，9:20 → 0.45）
 *   横屏：H = W / (1/portraitRatio) = W × portraitRatio
 *
 * 上一版两种模式共用「固定最小列宽」（竖屏 150px、横屏 200px，均乘滑条 scale），于是 V4
 * （9:20）下：
 *   竖屏：W=150 → H = 150 / 0.45 ≈ 333px
 *   横屏：W=200 → H = 200 × 0.45 =  90px
 * 横屏截图区高度只有竖屏的约 1/4（V3 9:16：150/0.5625 = 267 vs 200×0.5625 = 113，约 2.4 倍），
 * 整张卡片视觉面积随之缩水一大圈 —— 即「切横屏后卡片小了很多」。
 *
 * ## 修法
 *
 * 横屏按「目标高度 = 竖屏截图区高度」反推列宽：
 *   W = H_target × (1/portraitRatio) = (PORTRAIT_BASE_PX / portraitRatio) / portraitRatio
 * 即列宽放大 (1/portraitRatio)² 倍（9:20 ≈ 4.9 倍、9:16 ≈ 3.2 倍）。列宽变大后
 * `repeat(auto-fill, minmax(列宽, 1fr))` 自然减少列数：卡片少而大，高度与竖屏相当。
 *
 * 再对列宽封顶（可用宽度 / LANDSCAPE_MIN_COLUMNS）：9:20 这类极窄比例反推出的列宽可达
 * 700px+，窗口不宽时一屏只放得下一张巨卡（滑条拉到 150% 尤其明显）。封顶保证一行至少两列。
 * 滑条仍全程生效：scale 直接乘在列宽上；封顶值只跟窗口宽度走，拉满时先碰天花板。
 */

/** 竖屏基准列宽（px，scale=1）。与 android-client CloudManagement.vue 的 150px 一致。 */
export const PORTRAIT_BASE_PX = 150
/** 横屏一行至少留几列（列宽封顶 = 可用宽度 / 该值）。 */
const LANDSCAPE_MIN_COLUMNS = 2

export interface CardMinWidthArgs {
  isPortrait: boolean
  /** 本页主流截图比例（宽/高，≤1），来自 useDominantAspect */
  portraitRatio: number
  /** 缩放系数（窗口大小滑条值 / 100） */
  scale: number
  /** 网格可用宽度（px）；0 = 尚未测量到，此时不做封顶 */
  availablePx: number
}

/**
 * 计算网格列宽下限（px），交给 `grid-template-columns: repeat(auto-fill, minmax(Npx, 1fr))`。
 *
 * 例（V4，portraitRatio = 0.45，scale = 0.8）：
 *   竖屏 → 150 × 0.8 = 120px，截图区高 120 / 0.45 ≈ 267px
 *   横屏 → 目标高 267px，列宽 = 267 / 0.45 ≈ 593px；
 *          网格宽 1500px 时 auto-fill 出 2 列（每列 745px），截图区高 ≈ 335px（与竖屏同量级）
 *
 * 例（V3，portraitRatio = 0.5625，scale = 0.8）：
 *   竖屏 → 120px，截图区高 120 / 0.5625 ≈ 213px
 *   横屏 → 目标高 213px，列宽 ≈ 379px；网格宽 1500px 时 3 列（每列 500px），高 ≈ 281px
 */
export function cardMinWidthPx({ isPortrait, portraitRatio, scale, availablePx }: CardMinWidthArgs): number {
  if (isPortrait) return Math.max(1, Math.round(PORTRAIT_BASE_PX * scale))
  // 目标高度 = 竖屏卡片在基准列宽下的截图区高度；横屏容器宽/高 = 1/portraitRatio，
  // 故列宽 = 目标高度 ÷ portraitRatio（= 竖屏列宽 ÷ portraitRatio²）。
  const base = (PORTRAIT_BASE_PX / portraitRatio) / portraitRatio
  // 封顶（在 scale 之前做）：保证一行至少 LANDSCAPE_MIN_COLUMNS 列。放在 scale 之前
  // 是为了让滑条全程生效——若对含 scale 的结果封顶，scale 拉大后先撞天花板、滑条失灵。
  const capped = availablePx > 0 ? Math.min(base, availablePx / LANDSCAPE_MIN_COLUMNS) : base
  return Math.max(1, Math.round(capped * scale))
}

/**
 * 测量网格容器的可用宽度（px），ResizeObserver 驱动（窗口缩放 / 侧栏折叠都会触发）。
 *
 * 只有横屏需要它（给列宽封顶），竖屏也订阅不影响结果（返回值被忽略）。
 * 返回清理函数（未挂载或无 ResizeObserver 时返回 undefined，effect 直接不清理）。
 */
export function observeWidth(el: HTMLElement | null, onWidth: (px: number) => void): (() => void) | undefined {
  if (!el || typeof ResizeObserver === 'undefined') return undefined
  const ro = new ResizeObserver(entries => {
    onWidth(Math.max(0, Math.round(entries[0]?.contentRect.width ?? 0)))
  })
  ro.observe(el)
  return () => ro.disconnect()
}
