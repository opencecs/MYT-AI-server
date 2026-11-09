/**
 * 截图缓存写入 + 「非渲染集」旧图清理（`ScreenshotView` 的 screenshotMap 专用）。
 *
 * 策略（2026 修订，用户拍板）：**渲染中的图永不淘汰、无上限**。此前有过「200 张 /
 * 150MB 双上限 + 保护集保底淘汰」的版本，保护集自身超限时仍会淘汰最旧的保护图，
 * 导致可见卡片闪「加载中」——现已整段移除。内存释放只靠一件事：清理**不再被渲染**
 * 的旧图（取消勾选、下线等残留），这是保留的核心机制。
 *
 * ⚠️ 内存量级：渲染集内所有截图常驻缓存。几百台设备同时勾选时，缓存可能回到
 * 几百 MB（历史上「白屏事故」的根因之一即缓存无上限）。若将来再次出现内存压力，
 * 治本方案是渲染层虚拟化（只渲染视口内卡片），而不是重新引入淘汰上限。
 *
 * 为什么用 Record 而非 Map：key 形如 `"192.168.1.10_1"`（非整数下标），JS 对象的
 * 自有属性顺序即插入顺序——写入前 delete 再赋值即可把该 key 移到属性顺序尾部，
 * 天然就是「从旧到新」顺序，不需要额外维护链表/Map。
 */

export interface ScreenshotLruResult {
  /** 写入后的新缓存（属性顺序 = 从旧到新的最近使用顺序）。 */
  next: Record<string, string>
  /**
   * 被清理的 key（仅供诊断/统计）。
   *
   * 注意：调用方**不要**据此清理 versionRef。清理只表示「本地没图了」，后端仍持有该版本；
   * 清掉版本号会让下一轮 poll 把它判为「未同步」→ 全量重拉（画面抖动）。
   * 现在的策略是保留版本号，由渲染侧对「有状态但缺图」的卡片定向补拉，
   * 见 screenshotOnDemand.ts。
   */
  evicted: string[]
}

/**
 * 把一批截图写入缓存并清理非渲染集的旧图（纯函数，不修改 prev）。
 *
 * @param prev    现有缓存
 * @param updates key → dataUrl；空值（undefined/null/''）表示删除该 key
 * @param protectedKeys 渲染保护集（当前正在渲染的卡片 key）。**永不淘汰**——
 *   正在显示的图一旦被清掉，就会走「显示加载中 → 按需补拉 → 回来又被清」的抖动/活锁。
 *   保护集之外的旧图（已取消勾选、已下线等不再渲染的 key）按最旧先清理。
 *
 * 「先删再写」同时承担两件事：把命中的 key 移到尾部（最近使用），以及删除语义。
 * 本轮写入的 key 与 protectedKeys 一样受保护，不会被本轮清理（否则刚取回的图会被立刻丢掉）。
 */
export function writeScreenshotLru(
  prev: Record<string, string>,
  updates: Record<string, string | null | undefined>,
  protectedKeys: Iterable<string> = [],
): ScreenshotLruResult {
  const next: Record<string, string> = { ...prev }
  const writeKeys = Object.keys(updates)
  for (const key of writeKeys) {
    const dataUrl = updates[key]
    delete next[key] // 先删：既是「移到尾部」，也让下面按需写入生效
    if (dataUrl) next[key] = dataUrl
  }

  // 受保护集 = 本轮写入 + 调用方声明的渲染中 key。两处叠加：刚取回的图不能被本轮
  // 清理（历史行为），正在渲染的图不能被清理（防闪烁）。
  const guarded = new Set(writeKeys)
  for (const key of protectedKeys) if (next[key]) guarded.add(key)

  const evicted: string[] = []
  // 只清理非受保护的旧图（未被渲染的缓存残留）：取消勾选、下线等场景的内存回收点。
  for (const key of Object.keys(next)) {
    if (guarded.has(key)) continue
    delete next[key]
    evicted.push(key)
  }
  return { next, evicted }
}
