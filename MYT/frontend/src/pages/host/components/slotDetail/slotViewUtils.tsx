// SlotView 工具函数、类型与常量
import { AndroidInfo, MirrorItem, SlotExpiry } from '../../../../types/index'
import i18n from '../../../../i18n'
import { statusLabel, statusTagCls } from '../../../../utils/containerStatus'

export interface SlotGroup {
  index: number
  androids: AndroidInfo[]
  expireSlot: SlotExpiry | undefined
}

export const isMytNetwork = (name?: string) =>
  name?.toLowerCase().includes('myt') ?? false

/** 状态 tag（Element 风格浅底深字）：配色与文案统一走 containerStatus（全状态支持） */
export const statusBadge = (status: string) => (
  <span className={`text-xs px-1.5 py-0.5 rounded whitespace-nowrap ${statusTagCls(status)}`}>
    {statusLabel(status, i18n.t)}
  </span>
)

export const parseImage = (image: string) => {
  const idx = image.lastIndexOf(':')
  if (idx === -1) return { mirror: image, ver: '' }
  return { mirror: image.slice(0, idx), ver: image.slice(idx + 1) }
}

export const buildMirrorLookup = (mirrors: MirrorItem[]) => {
  const map = new Map<string, MirrorItem>()
  for (const m of mirrors) map.set(m.url, m)
  return map
}

// 列序：勾选 28 / 备注 180 / 镜像名 150 / 类型 70 / 版本 80 / 网络 100 / 容器IP 130 / 状态 110 / 标签 120 / 机型 1fr / 操作 72
export const COL = 'grid grid-cols-[28px_180px_150px_70px_80px_100px_130px_110px_120px_1fr_72px] gap-x-2 items-center'

/** 行含扩展操作（SlotView rowActions 注入，如主机 tab 的日志/缓存清理）时的列网格：操作列加宽。 */
export const COL_WIDE = 'grid grid-cols-[28px_180px_150px_70px_80px_100px_130px_110px_120px_1fr_96px] gap-x-2 items-center'

/**
 * 摊平模式的列网格：在勾选与备注之间插一列「坑位」。
 *
 * 用于「每个坑位至多一台在跑」的多设备总览（云机 tab 的列表）——此时坑位分组头下只有一行，
 * 层级纯属冗余，改成一行一实例、坑位号进列。列宽顺序：
 * 勾选 28 / 坑位 84 / 备注 180 / 镜像名 150 / 类型 70 / 版本 80 / 网络 100 / 容器IP 130 / 状态 110 / 标签 120 / 机型 1fr / 操作 72
 *
 * 注：摊平模式不配 `rowActions`（那是主机 tab 的注入，主机 tab 不走摊平），故不需要加宽变体。
 */
export const COL_FLAT = 'grid grid-cols-[28px_84px_180px_150px_70px_80px_100px_130px_110px_120px_1fr_72px] gap-x-2 items-center'

/**
 * 云机层级内边距：坑位头（checkbox + 「坑位 N」）用 px-4 贴左，
 * 其下的列表头、云机行、空态提示统一右移一级，体现「该坑位下的云机」二级从属关系。
 * 三者必须同值——列表头与云机行共用同一列网格，padding 不一致会逐列错位。
 */
export const SLOT_CHILD_PAD = 'pl-10 pr-4'

/** 摊平模式的内边距：行是顶层（不再嵌在坑位分组下），与坑位头同级用 px-4。 */
export const FLAT_PAD = 'px-4'
