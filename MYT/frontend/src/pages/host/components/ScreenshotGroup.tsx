import { memo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo, DeviceState, SlotExpiry } from '../../../types/index'
import { ScreenshotCard, ScreenshotCardProps } from './ScreenshotCard'
import SlotPlaceholderCard from './screenshotDetail/SlotPlaceholderCard'
import SwitchAndroidModal from '../operations/system/SwitchAndroidModal'
import { type CardStatus } from '../../../utils/containerStatus'

/** 渲染一个截图卡片所需的数据（从父组件透传，避免分组组件持有截图数据）。 */
export interface GroupCardData {
  android: AndroidInfo
  dataUrl: string | null
  /** 截图二进制 URL（asset server 通道，img src 优先用）；null = 后端无图/通道不可用。 */
  imageUrl: string | null
  status: CardStatus
  note: string
}

interface Props {
  /** 分组标题：本地模式=设备 IP，公网模式=设备 ID。 */
  groupTitle: string
  /** 该设备分组下的实例卡片数据（已按搜索框过滤）。 */
  cards: GroupCardData[]
  /** 卡片渲染所需的公共 props（除每张卡片的 android/dataUrl/status/note/selected/deviceInfo/slotExpiry 外）。 */
  cardCommon: Omit<ScreenshotCardProps, 'android' | 'dataUrl' | 'status' | 'note' | 'selected' | 'deviceInfo' | 'slotExpiry' | 'deviceId'>
  /** 所有设备（按 device_id 建 Map，卡片/占位按 deviceId 查 deviceInfo，O(1)）。 */
  deviceById: Map<string, DeviceState>
  /** 槽位到期映射（按卡片 deviceId 查 slotExpiry）。 */
  slotExpiry: Record<string, SlotExpiry[]>
  /** 当前选中的实例 id 集合（判断卡片 selected + 分组全选状态）。 */
  selectedAndroidIds: Set<string>
  /** 切换实例选中。 */
  onToggleAndroidSelect: (id: string) => void
  /** 分组全选切换：传入该组所有实例 id，根据当前选中状态全选↔全不选。 */
  onToggleGroupSelect: (ids: string[]) => void
  /** 备注变更回调。 */
  onNoteChange: (id: string, value: string) => void
  /** 卡片最小宽度（px，用于网格列宽）。 */
  cardMinPx: number
  /** 缩放系数（滑条值/100），卡片内 padding/字号随其 calc 缩放（android-client --screenshot-scale 同源）。 */
  cardScaleVar?: number
  /** 空坑位列表（展示占位卡片，显示坑位号+授权状态）。 */
  emptySlots?: number[]
  /** 关机实例占用的坑位列表（显示虚线占位，位置固定，不进 cards 不可选）。 */
  offlineSlots?: number[]
  /** 坑位 → 占位实例的容器状态（占位卡按真实状态显示「异常退出/已创建/已关机」文案）。 */
  offlineStatus?: Map<number, string>
  /** 该分组的设备 ID（用于空坑位查 slotExpiry 授权状态）。 */
  groupDeviceId?: string
}

/** ScreenshotGroup 渲染单个设备分组的截图卡片。
 *  从 ScreenshotView 提取，让父组件只管筛选+分组，本组件只管一组卡片的布局。 */
function ScreenshotGroupImpl({ groupTitle, cards, cardCommon, deviceById, slotExpiry, selectedAndroidIds, onToggleAndroidSelect, onToggleGroupSelect, onNoteChange, cardMinPx, cardScaleVar = 1, emptySlots = [], offlineSlots = [], offlineStatus, groupDeviceId = '' }: Props) {
  const { t } = useTranslation()
  const [switchSlot, setSwitchSlot] = useState<number | null>(null)
  const [hideEmpty, setHideEmpty] = useState(false)
  const shownEmptySlots = hideEmpty ? [] : emptySlots
  if (cards.length === 0 && shownEmptySlots.length === 0 && offlineSlots.length === 0) return null

  // 该组全选状态：仅开机实例参与全选判断（关机实例不可选）
  // 该组全选状态：cards 仅含开机实例，直接取其 id 判断
  const groupIds = cards.map(c => c.android.id)
  const allSelected = groupIds.length > 0 && groupIds.every(id => selectedAndroidIds.has(id))
  const someSelected = groupIds.some(id => selectedAndroidIds.has(id))

  return (
    <section className="mb-4">
      {/* 分组标题：全选框 + 设备 IP/ID + 实例数。不吸顶，随内容滚动。 */}
      <div className="mb-2 flex items-center gap-2 px-1 py-1">
        <input
          type="checkbox"
          checked={allSelected}
          ref={el => { if (el) el.indeterminate = !allSelected && someSelected }}
          onChange={() => onToggleGroupSelect(groupIds)}
          className="size-4 accent-blue-500 cursor-pointer"
          title={allSelected ? t('host:screenshot.clickToDeselect') : t('host:screenshot.clickToSelect')}
        />
        <span className="text-sm font-medium text-gray-700">{groupTitle}</span>
        <span className="text-xs text-gray-400">
          {cards.length} {t('host:screenshot.instanceCount')}
        </span>
        {emptySlots.length > 0 && (
          <button
            onClick={() => setHideEmpty(v => !v)}
            className="text-[11px] text-gray-400 hover:text-blue-500 transition-colors ml-1"
            title={hideEmpty ? t('host:screenshot.showEmptySlots') : t('host:screenshot.hideEmptySlots')}
          >
            {hideEmpty ? t('host:screenshot.showEmptySlots') : t('host:screenshot.hideEmptySlots')} ({emptySlots.length})
          </button>
        )}
      </div>
      <div
        className="grid"
        style={{ ['--card-scale' as string]: cardScaleVar, gridTemplateColumns: `repeat(auto-fill, minmax(min(${cardMinPx}px, 100%), 1fr))`, gap: `calc(10px * var(--card-scale))` }}
      >
        {/* 开机卡片 + 关机占位 + 空闲占位，统一按坑位号排序混排，保证位置固定 */}
        {([
          ...cards.map(c => ({ kind: 'card' as const, slot: c.android.indexNum, card: c })),
          ...offlineSlots.map(s => ({ kind: 'offline' as const, slot: s })),
          ...shownEmptySlots.map(s => ({ kind: 'empty' as const, slot: s })),
        ]).sort((a, b) => a.slot - b.slot).map(item => {
          if (item.kind === 'card') {
            const { android, dataUrl, imageUrl, status, note } = item.card
            return (
              <ScreenshotCard
                key={android.id}
                {...cardCommon}
                android={android}
                androids={cardCommon.androids}
                allAndroids={cardCommon.allAndroids}
                deviceId={android.deviceId}
                deviceInfo={deviceById.get(android.deviceId)}
                slotExpiry={slotExpiry[android.deviceId] ?? []}
                dataUrl={dataUrl}
                imageUrl={imageUrl}
                status={status}
                note={note}
                selected={selectedAndroidIds.has(android.id)}
                onToggleSelect={onToggleAndroidSelect}
                onNoteChange={onNoteChange}
              />
            )
          }
          // 关机占位与空闲占位：统一三段式外框结构（与 ScreenshotCard 一致），
          // 点击占位区弹「切换实例」，切换按钮同入口（源码空坑位无点击，pc-client 保留既有入口）
          const slotIdx = item.slot
          const placeholderOnCreate = cardCommon.onCreateInstance && cardCommon.devices.length > 0
            ? (d: DeviceState, slot: number) => cardCommon.onCreateInstance?.(d, slot)
            : undefined
          // 占位卡的创建目标 = 该分组设备（groupDeviceId）
          const groupDevice = deviceById.get(groupDeviceId)
          return (
            <SlotPlaceholderCard
              key={`${item.kind}_${slotIdx}`}
              kind={item.kind === 'offline' ? 'offline' : 'empty'}
              slotIdx={slotIdx}
              aspectRatio={cardCommon.aspectRatio}
              scale={cardCommon.scale ?? 1}
              androidStatus={offlineStatus?.get(slotIdx)}
              onCreate={groupDevice && placeholderOnCreate ? () => placeholderOnCreate(groupDevice, slotIdx) : undefined}
              onOpen={() => setSwitchSlot(slotIdx)}
            />
          )
        })}
      </div>
      {switchSlot !== null && (
        <SwitchAndroidModal
          androids={(cardCommon.allAndroids ?? []).filter(a => a.indexNum === switchSlot && a.deviceId === groupDeviceId)}
          slot={switchSlot}
          onClose={() => setSwitchSlot(null)}
        />
      )}
    </section>
  )
}

/**
 * 分组级 memo 比较器。
 *
 * 截图每推来一轮，父组件的 filteredGroups 都会算出全新的 cards/emptySlots/offlineSlots
 * 数组；默认 memo 只比引用 → 所有分组（115 台设备就是 115 个分组）每轮全部重渲染，
 * 再把整棵卡片树交给 React diff。这里按内容逐项比对，本轮没有任何一张图变化的分组
 * 直接跳过，把每轮的重渲染范围压到「真正有变化的那一两个分组」。
 */
function groupPropsEqual(prev: Props, next: Props): boolean {
  if (prev.groupTitle !== next.groupTitle) return false
  if (prev.groupDeviceId !== next.groupDeviceId) return false
  if (prev.cardMinPx !== next.cardMinPx) return false
  // 其余 props 父组件已 useMemo / useState 稳定化，按引用判定即可
  if (prev.cardCommon !== next.cardCommon) return false
  if (prev.deviceById !== next.deviceById) return false
  if (prev.slotExpiry !== next.slotExpiry) return false
  if (prev.selectedAndroidIds !== next.selectedAndroidIds) return false
  if (prev.onToggleAndroidSelect !== next.onToggleAndroidSelect) return false
  if (prev.onToggleGroupSelect !== next.onToggleGroupSelect) return false
  if (prev.onNoteChange !== next.onNoteChange) return false
  if (prev.cardScaleVar !== next.cardScaleVar) return false
  if (!cardDataListEqual(prev.cards, next.cards)) return false
  if (!numberListEqual(prev.emptySlots, next.emptySlots)) return false
  if (!numberListEqual(prev.offlineSlots, next.offlineSlots)) return false
  if (prev.offlineStatus !== next.offlineStatus) return false
  return true
}

function cardDataListEqual(prev: GroupCardData[], next: GroupCardData[]): boolean {
  if (prev.length !== next.length) return false
  for (let i = 0; i < prev.length; i++) {
    const a = prev[i], b = next[i]
    // android 引用来自 androidList（内容未变时父组件复用旧数组），dataUrl 为字符串，
    // 任一变化都说明这张卡的画面/状态真的更新了，必须放行重渲染
    if (a.android !== b.android) return false
    if (a.dataUrl !== b.dataUrl) return false
    if (a.status !== b.status) return false
    if (a.note !== b.note) return false
  }
  return true
}

function numberListEqual(prev: number[] = [], next: number[] = []): boolean {
  if (prev.length !== next.length) return false
  for (let i = 0; i < prev.length; i++) if (prev[i] !== next[i]) return false
  return true
}

export const ScreenshotGroup = memo(ScreenshotGroupImpl, groupPropsEqual)