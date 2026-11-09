import { useTranslation } from 'react-i18next'
import { ShieldCheck } from 'lucide-react'
import { DeviceState } from '../../types/index'
import { SlotExpireInfo } from './types'
import { SlotCell } from './SlotCell'
import { bindBadgeInfo } from './bindStatus'

/**
 * 一台主机一行：头部（主机名/IP + 绑定徽章 + 状态图例）+ 实例格子横向流式排列。
 * 布局参照 pc-android-client 授权页（主机列 + 实例列 flex wrap + 状态图例）。
 */
interface Props {
  device: DeviceState
  slotCount: number
  slots: SlotExpireInfo[] | undefined
  bindStatus: number | undefined
  selectedSlots: Set<number>
  onToggleSlot: (deviceID: string, slotIndex: number) => void
}

export function HostLicenseCard({ device, slotCount, slots, bindStatus, selectedSlots, onToggleSlot }: Props) {
  const { t } = useTranslation()
  const badge = bindBadgeInfo(bindStatus)
  const BadgeIcon = badge.icon
  const isBound = bindStatus === 1
  const isQuerying = bindStatus === undefined

  return (
    <div className="bg-white rounded-xl border border-gray-100 shadow-sm hover:shadow-md transition-shadow overflow-hidden">
      {/* 头部：主机信息 + 绑定徽章 + 状态图例 */}
      <div className="flex items-center justify-between gap-3 px-4 py-2.5 bg-gradient-to-r from-gray-50/80 to-white border-b border-gray-100">
        <div className="flex items-center gap-2.5 min-w-0">
          <span className="w-2 h-2 rounded-full bg-green-500 shrink-0" />
          <span className="text-sm font-semibold text-gray-800 truncate">{device.ip}</span>
          <span className="text-[10px] text-gray-400 font-mono truncate">{device.device_id}</span>
          <span className="text-[10px] px-2 py-0.5 rounded-full bg-blue-50 text-blue-600 border border-blue-100 shrink-0">
            {t('payment:slotCount', { count: slotCount })}
          </span>
        </div>
        <div className="flex items-center gap-3 shrink-0">
          {/* 状态图例（对齐 pc-android-client） */}
          <div className="hidden lg:flex items-center gap-2 text-[10px] text-gray-400">
            <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-blue-500" />{t('payment:slotStatus.normal')}</span>
            <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-amber-500" />{t('payment:slotStatus.expiring')}</span>
            <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-red-500" />{t('payment:slotStatus.expired')}</span>
            <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-gray-300" />{t('payment:slot.unactivated')}</span>
          </div>
          <span className={`flex items-center gap-1 text-[10px] px-2 py-1 rounded-lg border ${badge.cls}`}>
            <BadgeIcon size={11} className={badge.spinning ? 'animate-spin' : ''} />
            {t(`payment:${badge.textKey}`)}
          </span>
        </div>
      </div>

      {/* 实例区 */}
      <div className="px-4 py-3">
        {isQuerying ? (
          <div className="py-4 flex items-center gap-2">
            <ShieldCheck size={14} className="text-gray-300 animate-pulse" />
            <div className="flex gap-1.5 flex-1">
              {Array.from({ length: 8 }, (_, i) => (
                <div key={i} className="w-[76px] h-9 rounded-lg bg-gray-100/80 animate-pulse" />
              ))}
            </div>
          </div>
        ) : !isBound ? (
          <div className="text-xs text-gray-400 py-4 text-center bg-gray-50/60 rounded-lg">
            {bindStatus === 2 ? t('payment:boundByOtherMsg') : t('payment:unboundMsg')}
          </div>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {Array.from({ length: slotCount }, (_, i) => {
              const slotNum = i + 1
              return (
                <SlotCell key={slotNum} slotNum={slotNum} slots={slots}
                  selected={selectedSlots.has(slotNum)} disabled={!isBound}
                  onClick={() => onToggleSlot(device.device_id, slotNum)} />
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
