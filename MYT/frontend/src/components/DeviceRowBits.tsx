import { formatSize } from '../utils/format'
import { deviceSeriesLetter } from '../utils/deviceSeries'

/**
 * 设备列表行的两个共用件（DeviceItem / DeviceListItem 共用，避免两处复制同样的类名串）。
 *
 * 系列徽标：20×20 白底方块 + 4px 圆角 + **状态色描边**（在线绿 / 待认证黄 / 离线红），
 * 内容为系列字母（`deviceSeriesLetter`），悬浮（title）显示完整型号。
 * 描边用亮一档、文字用深一档同色系：白底上 12px 粗体需 ≥4.5:1，
 * green-700 4.8:1 / red-700 6.4:1 / amber-700 5.3:1 均达标。
 */

/** 设备状态 → 徽标描边与文字色：0 在线绿 / 2 待认证黄 / 其余（含 1 离线）红 */
function badgeStatusClass(state?: number): string {
  if (state === 0) return 'border-green-500 text-green-700'
  if (state === 2) return 'border-amber-400 text-amber-700'
  return 'border-red-400 text-red-700'
}

/** 系列徽标：白底 + 状态色描边，内容为型号系列字母，悬浮显示完整型号 */
export function SeriesBadge({ deviceType, state }: { deviceType?: string; state?: number }) {
  return (
    <span
      className={`w-5 h-5 rounded shrink-0 inline-flex items-center justify-center text-[12px] font-bold leading-none bg-white border-[1.5px] ${badgeStatusClass(state)}`}
      title={deviceType ? deviceType.toUpperCase() : undefined}
    >
      {deviceSeriesLetter(deviceType)}
    </span>
  )
}

/** 磁盘占用：细进度条 + 右侧容量文字（与改动前一致） */
export function DiskPill({ used, total }: { used?: number; total?: number }) {
  const pct = !total || total <= 0 ? 0 : Math.round(((used ?? 0) / total) * 100)
  const fill = pct > 90 ? 'bg-red-500' : pct > 70 ? 'bg-yellow-400' : 'bg-gray-400'
  return (
    <div className="flex items-center gap-1.5 flex-1 min-w-0">
      <div className="flex-1 h-1 bg-gray-100 rounded-full overflow-hidden">
        <div className={`h-full rounded-full ${fill} will-change-transform`} style={{ transform: `scaleX(${pct / 100})`, transformOrigin: 'left' }} />
      </div>
      <span className="text-[10px] text-gray-400 whitespace-nowrap">{formatSize(used ?? 0)}/{formatSize(total ?? 0)}</span>
    </div>
  )
}
