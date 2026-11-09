import { formatOsVer, parseVersion, isEmulatorMirrorType, hasV3Marker, hasV3MarkerSaved } from '../../../mirror/constants'
import type { MirrorItem, SavedMirror, PhoneModel } from '../../../../types/index'

// 镜像来源：本地优先，统一展示类型
export type MirrorSource = 'local' | 'online' | 'special'

export interface DisplayMirror {
  id: string
  name: string
  url: string
  os_ver: string
  source: MirrorSource
}

export const DEVICE_PORT = 8000

export const DNS_OPTIONS = [
  { labelKey: 'dnsCustom', value: '223.5.5.5' },
  { labelKey: 'dnsGoogle', value: '8.8.8.8' },
  { labelKey: 'dnsCustomOption', value: 'custom' },
] as const

export const SANDBOX_OPTIONS = ['16GB', '32GB', '64GB']

// 可选安卓版本（与设备镜像源对齐，写死避免过滤后丢版本）
export const ANDROID_VERSIONS = ['10', '11', '12', '13', '14', '15', '16', '17']

export const RESOLUTION_OPTIONS = [
  { labelKey: 'res720', value: '720x1280x320' },
  { labelKey: 'res1080', value: '1080x1920x420' },
  { labelKey: 'res1200', value: '1200x1920x240' },
  { labelKey: 'res1600', value: '1600x2560x320' },
  { labelKey: 'customResolution', value: 'custom' },
  { labelKey: 'modelResolution', value: 'model' },
] as const

// 默认分辨率（720P）：创建表单初值 + 切换 Tab 时 'model' 的重置目标
export const DEFAULT_RESOLUTION = '720x1280x320'

// 分辨率可选项：'model'（跟随机型分辨率）只在模拟器 Tab 展示，容器 Tab 不展示。
// 创建弹窗渲染下拉与「切 Tab 校验当前值是否仍可选」共用同一份规则，避免两处判定漂移。
export function resolutionOptions(isSimulator: boolean) {
  return RESOLUTION_OPTIONS.filter(o => isSimulator || o.value !== 'model')
}

export const inputCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200 focus:border-blue-400 focus:outline-none'
export const selectCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200'
export const labelCls = 'text-xs text-gray-500 mb-1 block'

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)}KB`
  if (bytes < 1073741824) return `${(bytes / 1048576).toFixed(1)}MB`
  return `${(bytes / 1073741824).toFixed(2)}GB`
}

export const sortMirrorsDesc = <T extends { name: string }>(list: T[]): T[] =>
  [...list].sort((a, b) => parseVersion(b.name) - parseVersion(a.name))

// 判断是否特质版镜像（sys_ver_des == "特质版"）
export function isSpecialMirror(m: MirrorItem): boolean {
  return m.sys_ver_des === '特质版'
}

// 合并在线/本地镜像，按 URL 去重（本地优先），本地排最前，特质版独立标源
export function mergeMirrors(online: MirrorItem[], local: SavedMirror[]): DisplayMirror[] {
  const localSet = new Set(local.map(m => m.url))
  const result: DisplayMirror[] = []

  // 本地镜像排最前
  for (const m of sortMirrorsDesc(local)) {
    result.push({ id: m.id, name: m.name, url: m.url, os_ver: m.os_ver, source: 'local' })
  }
  // 在线镜像：URL 与本地重复的跳过；特质版标 special，其余标 online
  for (const m of sortMirrorsDesc(online)) {
    if (localSet.has(m.url)) continue
    result.push({ id: m.id, name: m.name, url: m.url, os_ver: m.os_ver, source: isSpecialMirror(m) ? 'special' : 'online' })
  }
  return result
}

// 模拟器镜像：含 _v3 且模拟器类型（sys_ver=='5' 或 特质版）且 ttype2 匹配设备机型
export function filterMirrorsByDevice(mirrors: MirrorItem[], deviceType: string): MirrorItem[] {
  const lower = deviceType.toLowerCase()
  return mirrors.filter(m => isEmulatorMirrorType(m) && m.ttype2.some(t => t.trim().toLowerCase().startsWith(lower + '_')))
}

// 容器镜像：含 _v3 且非模拟器类型 且 ttype2 匹配设备机型
export function filterContainerMirrorsByDevice(mirrors: MirrorItem[], deviceType: string): MirrorItem[] {
  const lower = deviceType.toLowerCase()
  return mirrors.filter(m => hasV3Marker(m.ttype2) && !isEmulatorMirrorType(m) && m.ttype2.some(t => t.trim().toLowerCase().startsWith(lower + '_')))
}

// 根据镜像 URL 反查类型：'emulator' | 'container' | 'unknown'
// 优先查在线镜像列表，其次查已下载镜像列表。特质版算模拟器。
// 第三步兜底：tag 系列匹配——第三方镜像源（如 registry.suyunfei.com）的 URL 不在
// 官方列表里，但 tag 去掉末尾时间戳（如 Q14_v3_all_202609011215 → Q14_v3_all）后
// 与官方同系列镜像一致，借用其类型（实测 suyunfei 源系列全部命中）。
export function getMirrorTypeByUrl(imageUrl: string, mirrors: MirrorItem[], savedMirrors: SavedMirror[]): 'emulator' | 'container' | 'unknown' {
  const m = mirrors.find(it => it.url === imageUrl)
  if (m) return isEmulatorMirrorType(m) ? 'emulator' : 'container'
  const s = savedMirrors.find(it => it.url === imageUrl)
  if (s) return isEmulatorMirrorType(s) ? 'emulator' : 'container'
  // tag 系列兜底：同系列（时间戳不同）的在线镜像借用类型
  const stem = mirrorTagStem(imageUrl)
  if (stem) {
    const same = mirrors.find(it => mirrorTagStem(it.url) === stem)
    if (same) return isEmulatorMirrorType(same) ? 'emulator' : 'container'
  }
  return 'unknown'
}

// mirrorTagStem 提取镜像 URL 的 tag 并去掉末尾时间戳段：Q14_v3_all_202609011215 → Q14_v3_all
function mirrorTagStem(imageUrl: string): string {
  const idx = imageUrl.lastIndexOf(':')
  const tag = idx >= 0 ? imageUrl.slice(idx + 1) : imageUrl
  return tag.replace(/_\d{8,14}$/, '')
}

// parseAndroidVersionFromImage 从镜像 URL 的 tag 提取安卓版本（更新弹窗初始化用）。
// tag 里没有 "andN" 字样（如 P16_16_Xiaomi、Q14_v3_all），旧正则 /and(\d+)/ 恒落空 →
// 版本恒默认 14。规律（与 formatOsVer 一致）：P/Q 后紧跟的数字 = 安卓版本。
export function parseAndroidVersionFromImage(imageUrl: string): string {
  const idx = imageUrl.lastIndexOf(':')
  const tag = idx >= 0 ? imageUrl.slice(idx + 1) : imageUrl
  const m = tag.match(/^[PQ](\d+)/i)
  if (m) return m[1]
  const legacy = imageUrl.match(/and(\d+)/i)
  if (legacy) return legacy[1]
  return ''
}

export function filterMirrorsByVersion(mirrors: MirrorItem[], osVer: string): MirrorItem[] {
  return mirrors.filter(m => m.os_ver === osVer)
}

export function filterLocalMirrors(savedMirrors: SavedMirror[], deviceType: string, osVer: string): SavedMirror[] {
  const lower = deviceType.toLowerCase()
  return savedMirrors.filter(m => {
    if (!hasV3MarkerSaved(m.ttype2)) return false
    const types = m.ttype2.split(',').map(t => t.trim().toLowerCase())
    if (!types.some(t => t.startsWith(lower + '_'))) return false
    return osVer === '' || m.os_ver === osVer
  })
}

// 本地镜像按模拟器/容器类型过滤（含 _v3 且模拟器类型 → 模拟器，其余 → 容器）
export function filterLocalMirrorsByType(savedMirrors: SavedMirror[], deviceType: string, osVer: string, mirrorType: 'emulator' | 'container'): SavedMirror[] {
  return filterLocalMirrors(savedMirrors, deviceType, osVer).filter(m =>
    mirrorType === 'emulator' ? isEmulatorMirrorType(m) : !isEmulatorMirrorType(m)
  )
}

export function filterModelsByVersion(models: PhoneModel[], androidVersion: string): PhoneModel[] {
  if (!androidVersion) return models
  return models.filter(m => !m.android_version || String(m.android_version) === androidVersion)
}

export { formatOsVer }

export const toggleCls = 'relative w-8 h-4 rounded-full transition-colors cursor-pointer'
export const toggleDotCls = 'absolute top-0.5 left-0.5 w-3 h-3 rounded-full bg-white transition-transform shadow-sm'
