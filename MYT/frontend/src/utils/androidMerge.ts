import type { AndroidInfo } from '../types/index'

/**
 * 容器列表字段级比较：androidlist:updated 事件每轮（~3s）推送全量列表，
 * JSON.stringify 深比较 + 整数组替换会让所有 ScreenshotCard 的 memo 击穿
 * （99 台卡顿根因之一）。只比对影响渲染的字段（deviceMerge.ts 同模式）：
 * 新增影响卡片渲染的字段时须同步加入比较。
 */
function androidEqual(a: AndroidInfo, b: AndroidInfo): boolean {
  if (!(a.id === b.id
    && a.name === b.name
    && a.status === b.status
    && a.indexNum === b.indexNum
    && a.ip === b.ip
    && a.deviceId === b.deviceId
    && a.image === b.image
    && a.networkName === b.networkName
    && a.containerIP === b.containerIP
    && a.modelPath === b.modelPath)) return false
  // tags 必须参与比较：标签编辑的乐观回填走 androidlist:updated 推送，卡片/浮层
  // 靠内容比对即时收敛（2026-10-30，漏比会让标签编辑「点了不生效」）
  const aTags = a.tags ?? []
  const bTags = b.tags ?? []
  if (aTags.length !== bTags.length || aTags.some((t, i) => t !== bTags[i])) return false
  // profile（V4 档案，字段全为原始值）：事件 payload 每轮都是新对象，按引用比会恒变；
  // 字段为空集/数量不等（结构变化）直接判不等
  if (a.profile === b.profile) return true
  if (!a.profile || !b.profile) return false
  const aKeys = Object.keys(a.profile)
  const bKeys = Object.keys(b.profile)
  if (aKeys.length !== bKeys.length) return false
  return aKeys.every(k => (a.profile as any)[k] === (b.profile as any)[k])
}

/**
 * 引用稳定合并（utils/deviceMerge.ts mergeDeviceList 同模式）：
 *  - 长度不同：直接用新数组（增删场景极少，不值得逐条比对）
 *  - 未变化的条目复用旧对象引用 → 卡片 memo 生效，事件推送只重渲染真正变化的卡；
 *  - 数组引用仅在确有条目内容变化时换新，否则返回 prev（setAndroidList 收敛为空操作）。
 */
export function mergeAndroidList(prev: AndroidInfo[], next: AndroidInfo[]): AndroidInfo[] {
  if (prev.length !== next.length) return next
  const prevMap = new Map(prev.map(a => [a.id, a]))
  let changed = false
  const merged = next.map(na => {
    const old = prevMap.get(na.id)
    if (!old || !androidEqual(old, na)) {
      changed = true
      return na
    }
    return old
  })
  return changed ? merged : prev
}
