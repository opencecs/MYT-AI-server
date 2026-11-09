import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { DeviceState, SlotExpiry } from '../../../../../types/index'
import { formatSize } from '../../../../../utils/format'
import { formatElapsed } from '../crossDeviceMove'

/** 实时计时器 hook */
export function useLiveTimer(active: boolean): string {
  const [text, setText] = useState('')
  const startRef = useRef(0)
  useEffect(() => {
    if (!active) { setText(''); return }
    startRef.current = Date.now()
    const id = setInterval(() => setText(formatElapsed(Date.now() - startRef.current)), 1000)
    return () => clearInterval(id)
  }, [active])
  return text
}

/** 槽位颜色配置 */
export function slotColor(idx: number, exp?: SlotExpiry) {
  if (!exp || exp.state === undefined) return { border: 'border-gray-200', dot: 'bg-gray-300' }
  if (exp.state === 0) return { border: 'border-green-300', dot: 'bg-green-400' }
  if (exp.state === 1) return { border: 'border-yellow-300', dot: 'bg-yellow-400' }
  return { border: 'border-red-300', dot: 'bg-red-400' }
}

/** 磁盘工具函数 */
function diskPercent(d: DeviceState) {
  if (!d.disk_total || d.disk_total <= 0) return 0
  return Math.round((d.disk_used ?? 0) / d.disk_total * 100)
}

function diskBarColor(pct: number) {
  if (pct > 90) return 'bg-red-500'
  if (pct > 70) return 'bg-yellow-400'
  return 'bg-blue-500'
}

/** 槽位选择按钮 */
export function SlotButton({ idx, isCurrent, isOccupied, isSelected, onClick, expiryMap }: {
  idx: number; isCurrent: boolean; isOccupied: boolean; isSelected: boolean; onClick: () => void; expiryMap: Map<number, SlotExpiry>
}) {
  const { t } = useTranslation()
  const sc = slotColor(idx, expiryMap.get(idx))
  return (
    <button onClick={onClick} disabled={isCurrent || isOccupied}
      className={`aspect-square rounded-lg border-2 transition-all flex flex-col items-center justify-center gap-0.5
        ${isCurrent ? 'bg-green-50 text-green-600 border-green-400' : isOccupied ? 'bg-gray-100 text-gray-400 border-gray-200 cursor-not-allowed' : isSelected ? 'bg-blue-500 text-white border-blue-500 shadow-sm' : 'bg-white hover:bg-gray-50 ' + sc.border}`}
    >
      <span className={`w-1.5 h-1.5 rounded-full ${isSelected ? 'bg-white' : isCurrent ? 'bg-green-400' : sc.dot}`} />
      <span className="text-xs font-semibold leading-none">{idx}</span>
      {isCurrent && <span className="text-[9px] leading-none text-green-500">{t('host:move.current')}</span>}
      {isOccupied && !isCurrent && <span className="text-[9px] leading-none">{t('host:copy.occupied')}</span>}
    </button>
  )
}

/** 设备列表项 */
export function DeviceItem({ device: d, isCurrent: isCur, isSelected, onClick }: {
  device: DeviceState; isCurrent: boolean; isSelected: boolean; onClick: () => void
}) {
  const { t } = useTranslation()
  const pct = diskPercent(d)
  return (
    <div
      onClick={onClick}
      className={`px-3 py-2 cursor-pointer border-b border-gray-50 hover:bg-gray-50 transition-colors ${
        isSelected ? 'bg-blue-50 border-l-2 border-l-blue-500' : ''
      }`}
    >
      <div className="flex items-center gap-2">
        <span className={`w-2 h-2 rounded-full shrink-0 ${d.state === 0 ? 'bg-green-500' : 'bg-red-400'}`} />
        <span className="text-xs text-gray-800 truncate">{d.ip}</span>
        {isCur && <span className="text-[9px] text-blue-500 bg-blue-50 px-1 rounded shrink-0">{t('host:move.current')}</span>}
        <span className="text-[10px] text-gray-400 bg-gray-100 px-1 rounded ml-auto shrink-0">{d.device_type?.toUpperCase()}</span>
      </div>
      {d.disk_total != null && d.disk_total > 0 && (
        <div className="flex items-center gap-1 mt-1 pl-4 pointer-events-none">
          <div className="flex-1 h-1 bg-gray-100 rounded-full overflow-hidden">
            <div className={`h-full rounded-full ${diskBarColor(pct)}`} style={{ width: `${pct}%` }} />
          </div>
          <span className="text-[10px] text-gray-400 whitespace-nowrap">{formatSize(d.disk_used ?? 0)}/{formatSize(d.disk_total)}</span>
        </div>
      )}
    </div>
  )
}
