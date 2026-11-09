import type { MirrorItem, SavedMirror } from '../../types/index'

export type TabType = 'online' | 'downloaded'
export type OsFilter = 'all' | string
export type SeriesFilter = 'qrc' | 'p1'
export type MirrorTypeFilter = 'emulator' | 'container'

export const OS_VER_MAP: Record<string, string> = {
  and10: '安卓 10', and11: '安卓 11', and12: '安卓 12',
  and13: '安卓 13', and14: '安卓 14', and15: '安卓 15', and16: '安卓 16',
  and17: '安卓 17',
}

export function formatOsVer(osVer: string): string {
  // 1. 标准格式 and10/and14 直接查表
  if (OS_VER_MAP[osVer]) return OS_VER_MAP[osVer]
  // 2. 自定义镜像 tag 兜底：从镜像名前缀提取安卓版本。
  //    镜像 tag 格式如 Q10_10_Pixel3_gms_xp_dev / P14_vivo_V2302A / Q16_16_Xiaomi_...
  //    规律：Q 或 P 后紧跟的数字 = 安卓版本（Q10/P10=安卓10, Q16/P16=安卓16）。
  //    提取后转成“安卓 X”统一展示，避免把原始 tag 当版本号显示。
  const m = osVer.match(/^[QP](\d+)/i)
  if (m) return `安卓 ${m[1]}`
  return osVer
}

export const SERIES_OPTIONS: [SeriesFilter, string][] = [['qrc', 'seriesQRC'], ['p1', 'seriesP']]
export const MIRROR_TYPE_OPTIONS: [MirrorTypeFilter, string][] = [['emulator', 'emulator'], ['container', 'container']]
export const OS_VERSIONS = ['all', 'and10', 'and11', 'and12', 'and13', 'and14', 'and15', 'and16', 'and17']

export const OS_VER_ORDER: Record<string, number> = {
  and17: 17, and16: 16, and15: 15, and14: 14, and13: 13, and12: 12, and11: 11, and10: 10,
}

export function parseVersion(name: string): number {
  const match = name.match(/v?(\d+)\.(\d+)\.?(\d+)?/i)
  if (!match) return 0
  const major = parseInt(match[1])
  const minor = parseInt(match[2])
  const patch = parseInt(match[3] || '0')
  return major * 10000 + minor * 100 + patch
}

/** ttype2 数组中是否有严格以 _v3 结尾的项（_v30 / _v3_cecs 不算） */
export function hasV3Marker(ttype2: string[]): boolean {
  return ttype2.some(t => t.trim().toLowerCase().endsWith('_v3'))
}

/** saved 镜像 ttype2 是逗号分隔字符串 */
export function hasV3MarkerSaved(ttype2: string): boolean {
  if (!ttype2) return false
  return ttype2.split(',').map(t => t.trim().toLowerCase()).some(t => t.endsWith('_v3'))
}

/** 统一判定镜像是否为模拟器类型（在线/已下载通用）
 *  规则：含 _v3 标记 且（sys_ver=='5' 或 特质版）→ 模拟器；其余 → 容器
 *  ttype2 兼容 string[]（在线）与 string（已下载，逗号分隔）两种形态 */
export function isEmulatorMirrorType(m: { sys_ver: string; sys_ver_des?: string; ttype2?: string[] | string }): boolean {
  const t = m.ttype2
  const hasV3 = Array.isArray(t) ? hasV3Marker(t) : hasV3MarkerSaved(t ?? '')
  if (!hasV3) return false
  return m.sys_ver === '5' || m.sys_ver_des === '特质版'
}

/** 模拟器镜像：ttype2 含 _v3 且（sys_ver=='5' 或 特质版） */
export function isEmulatorMirror(m: MirrorItem): boolean {
  return isEmulatorMirrorType(m)
}

/** 容器镜像：ttype2 含 _v3 且非模拟器 */
export function isContainerMirror(m: MirrorItem): boolean {
  return hasV3Marker(m.ttype2) && !isEmulatorMirrorType(m)
}

/** saved 镜像判定（同上） */
export function isEmulatorMirrorSaved(m: SavedMirror): boolean {
  return isEmulatorMirrorType(m)
}

export function isContainerMirrorSaved(m: SavedMirror): boolean {
  return hasV3MarkerSaved(m.ttype2) && !isEmulatorMirrorType(m)
}

/** ttype2 中有 p1 开头的项 → P 系列（与模拟器/容器维度正交） */
export function isPSeries(ttype2: string[]): boolean {
  return ttype2.some(t => t.trim().toLowerCase().startsWith('p1'))
}

export function isPSeriesSaved(ttype2: string): boolean {
  return ttype2.split(',').map(t => t.trim().toLowerCase()).some(t => t.startsWith('p1'))
}
