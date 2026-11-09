/**
 * 标签配色：按标签名 hash 到固定调色板，保证「同名同色、跨页面一致」。
 *
 * 不存储颜色：标签是自由文本，颜色由名字派生即可，避免再维护一份色板配置
 * （与「标签库由数据聚合」同一取舍）。
 *
 * 色板均为浅底 + 深字 + 描边（对比度 ≥ 4.5:1），与仓库现有状态标签
 * （bg-*-50 / text-*-600|700）视觉语言一致。
 */
export interface TagStyle {
  /** 容器类名：浅底 + 深字 + 描边 */
  chip: string
  /** 纯色点（用于紧凑场景，如设备树云机子项） */
  dot: string
}

const PALETTE: TagStyle[] = [
  { chip: 'bg-blue-50 text-blue-700 border-blue-200', dot: 'bg-blue-500' },
  { chip: 'bg-teal-50 text-teal-700 border-teal-200', dot: 'bg-teal-500' },
  { chip: 'bg-amber-50 text-amber-700 border-amber-200', dot: 'bg-amber-500' },
  { chip: 'bg-purple-50 text-purple-700 border-purple-200', dot: 'bg-purple-500' },
  { chip: 'bg-rose-50 text-rose-700 border-rose-200', dot: 'bg-rose-500' },
  { chip: 'bg-green-50 text-green-700 border-green-200', dot: 'bg-green-500' },
  { chip: 'bg-indigo-50 text-indigo-700 border-indigo-200', dot: 'bg-indigo-500' },
  { chip: 'bg-cyan-50 text-cyan-700 border-cyan-200', dot: 'bg-cyan-500' },
  { chip: 'bg-orange-50 text-orange-700 border-orange-200', dot: 'bg-orange-500' },
  { chip: 'bg-fuchsia-50 text-fuchsia-700 border-fuchsia-200', dot: 'bg-fuchsia-500' },
]

/** djb2 变体：短字符串分布均匀，同标签名稳定同色 */
function hashName(name: string): number {
  let hash = 5381
  for (let i = 0; i < name.length; i++) {
    hash = ((hash << 5) + hash + name.charCodeAt(i)) >>> 0
  }
  return hash
}

export function tagStyle(name: string): TagStyle {
  return PALETTE[hashName(name) % PALETTE.length]
}
