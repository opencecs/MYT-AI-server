import { memo } from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo } from '../../../../types/index'
import { Tag } from 'lucide-react'
import { tagStyle } from '../../../../utils/tagColor'
import { containerDisplayName } from '../../../../utils/containerName'
import { isRunningStatus } from '../../../../utils/containerStatus'
import { statusBadge } from '../slotDetail/slotViewUtils'
interface Props {
  android: AndroidInfo
  selected: boolean
  /** 打开标签编辑浮层；不传则只读（右键菜单仍可编辑） */
  onEditTags?: (x: number, y: number, android: AndroidInfo) => void
  /** 容器状态（android.status）；非 running 时在名称后显示状态 tag（running 不显示保持干净） */
  status?: string
  /** 缩放系数（滑条值/100）：字号/间距随其 calc 缩放（android-client header-light 同源） */
  scale?: number
  /** 实例备注：有备注时优先显示备注（与 SlotView / SwitchAndroidModal 同规则） */
  note?: string
  onToggleSelect: (id: string) => void
}

/**
 * 卡片 header 信息条（复刻 android-client .cloud-machine-card-header-light）：
 * 勾选圈 + #坑位号 + 实例名(truncate) + 标签紧凑chip(icon+数量) + 状态tag。
 * 2026-10-10 二次调整：备份 tag 移到截图区右上角角标（ScreenshotCard）、
 * 标签从 chips(max2) 压缩为「icon+数量」（名称是主要信息，必须优先保证可读——
 * 80% 缩放卡宽 120px 时 chips+N备份 tag 会把名称挤到十几 px）。
 * 2026-10-17 增状态 tag：非 running 状态（开机中/重启中/重置中/已创建/已关机/异常退出/
 * 删除中）在名称后常显——用户看一眼 header 就知道这卡为什么没有截图。空间策略：
 * 状态 tag 优先于标签 chip（两者同处名称右侧、都 shrink-0，窄卡时名称让位）；
 * 有状态 tag 时隐藏标签 chip——坑位标签仍可从右键菜单查看/编辑，不丢入口。
 * 名称显示：备注优先（note || containerDisplayName），与 SlotView L299 / SwitchAndroidModal
 * L165 同规则；hover title 保留容器全名（android.name），备注不替代全名提示。
 * 样式值来自 CloudManagement.vue：bg #f8f9fa、border-b #e9ecef、
 * 字号 max(7px,11px*scale)、padding 4px*scale 8px*scale。
 */
const CardHeader = memo(function CardHeader({ android, selected, onEditTags, status, scale = 1, note = '', onToggleSelect }: Props) {
  const { t } = useTranslation()
  // 源码公式（CloudManagement.vue L2568-2586），下限防极小缩放时不可读
  const font = Math.max(7, Math.round(11 * scale))
  const padY = Math.max(2, Math.round(4 * scale))
  const padX = Math.max(4, Math.round(8 * scale))
  // 标签显示源与编辑浮层同源：设备端 android.tags（2026-10-27 标签切设备端后，
  // 本地坑位映射 slot_tags.json 已停止消费——继续读它会让 chip 恒空/与浮层不同源）
  const cardTags = android.tags ?? []
  const hasTags = cardTags.length > 0
  // 非 running 才显示状态 tag（运行中不显示，保持 header 干净）；tag 优先于标签 chip
  const showStatus = !!status && !isRunningStatus(status)
  // 状态 tag 比标签 chip 大（文案 tag vs icon chip），有 tag 时标签 chip 让位隐藏
  const showTagChip = !showStatus && (hasTags || !!onEditTags)

  return (
    <div className="flex items-center gap-1 bg-gray-50 border-b border-gray-200 font-semibold text-gray-600"
      style={{ fontSize: font, padding: `${padY}px ${padX}px` }}
      onClick={e => { e.stopPropagation(); onToggleSelect(android.id) }}
      title={selected ? t('host:screenshot.clickToDeselect') : t('host:screenshot.clickToSelect')}>
      {/* 勾选圈 + 坑位号（源码 el-checkbox :label="i" 显示坑位编号）；紧凑：圈 12px、坑位号紧跟无空格 */}
      <span className="shrink-0 flex items-center gap-0.5 cursor-pointer">
        {selected ? (
          <span className="w-3 h-3 rounded-full bg-blue-500 flex items-center justify-center shadow shadow-blue-500/30">
            <svg className="w-2 h-2 text-white" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><polyline points="20 6 9 17 4 12" /></svg>
          </span>
        ) : (
          <span className="w-3 h-3 rounded-full bg-white/70 backdrop-blur-sm border border-gray-300/80 shadow-sm" />
        )}
        <span className="text-[10px] font-normal text-gray-400">#{android.indexNum}</span>
      </span>
      {/* 实例名：主要信息，flex-1 保证最大空间，truncate + title 提示全名（源码 .instance-name 同位）。
          显示优先级：备注 > containerDisplayName 短名（与 SlotView/SwitchAndroidModal 同规则，
          有备注用户认备注）；hover title 恒为容器全名 android.name，不受备注影响 */}
      <span className="flex-1 min-w-0 truncate font-semibold text-gray-700" title={android.name}>{note || containerDisplayName(android.name, android.deviceId, android.indexNum)}</span>
      {/* 状态 tag：非 running 常显（shrink-0，名称让位）——statusBadge 统一文案+配色
          （slotViewUtils，与列表/坑位视图同源），尺寸随缩放收一点避免挤爆窄卡 */}
      {showStatus && (
        <span className="shrink-0" style={{ fontSize: Math.max(8, Math.round(10 * scale)) }}>
          {statusBadge(status!)}
        </span>
      )}
      {/* 标签：紧凑「icon+数量」chip（空间占用最小），点击开编辑浮层；有标签时用首标签配色。
          有状态 tag 时不显示（tag 优先，空间冲突时 chip 让位；标签走右键菜单仍可编辑） */}
      {showTagChip && (
        <span
          className={'shrink-0 flex items-center gap-0.5 rounded border cursor-pointer whitespace-nowrap ' + (hasTags ? tagStyle(cardTags[0]).chip : 'border-gray-200 text-gray-300')}
          style={{ fontSize: Math.max(7, Math.round(10 * scale)), padding: '0 3px' }}
          onClick={onEditTags ? (e) => { e.stopPropagation(); onEditTags(e.clientX, e.clientY, android) } : undefined}
          title={hasTags ? cardTags.join('、') : (onEditTags ? t('host:tags.setTags') : undefined)}
        >
          <Tag className="w-2.5 h-2.5" />
          {hasTags && <span>{cardTags.length}</span>}
        </span>
      )}
    </div>
  )
})

export default CardHeader
