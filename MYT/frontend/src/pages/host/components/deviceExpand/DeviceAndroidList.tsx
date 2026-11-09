import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import type { AndroidInfo } from '../../../../types/index'
import { useDropdownEnter } from '../../../../hooks/useModalAnimation'
import { containerDisplayName } from '../../../../utils/containerName'
import { isStatusVisible } from '../../../../utils/containerStatus'

interface DeviceAndroidListProps {
  /** 该设备下的云机列表（androidGroups[deviceId]，已按坑位号排序） */
  androids: AndroidInfo[]
  /** 已勾选的云机 id 集合（侧栏过滤器，勾选框选中态来源） */
  checkedIds: Set<string>
  onToggleAndroid: (androidId: string) => void
  /** 「云机展示=全部」设置（与 MultiDeviceSlotView.showOffline 同源）：开启时关机容器也进列表 */
  showOffline?: boolean
  /** 所属设备是否被勾选（设备级勾选）：true 时容器行全部显示为已勾选 */
  deviceChecked?: boolean
}

/**
 * 设备行展开区：该设备运行中（running/starting）的容器列表，可勾选。
 * 单行紧凑样式（2026-09-27）：状态点 + 勾选圈 + 容器短名 + 右侧坑位号 + 常显 + 按钮。
 * - 勾选是「侧栏过滤器」：写入 sidebarAndroidIds 决定右侧展示范围（同时同步批量勾选）；
 *   联动判断在 DeviceListItem（那里持有设备勾选状态）。
 * - 右侧数字 = 坑位号 indexNum（数据里无「每容器实例数」字段）。
 * - 展开动画复用 GroupTreeSection 的 useDropdownEnter 范式。
 */
function DeviceAndroidList({
  androids, checkedIds, onToggleAndroid, showOffline = false, deviceChecked = false,
}: DeviceAndroidListProps) {
  const { t } = useTranslation()
  const panelRef = useRef<HTMLDivElement>(null)
  useDropdownEnter(panelRef, true)

  // 可见容器：showOffline（设置「云机展示=全部」）时全状态可见；关闭时开机相关 + created
  // （isStatusVisible，见 containerStatus.ts 口径）。关机容器灰点区分（与运行中绿点/
  // 开机中琥珀点对应）。
  const visible = androids.filter(a => isStatusVisible(a.status, showOffline))

  return (
    <div ref={panelRef} className="pb-1" onClick={(e) => e.stopPropagation()}>
      {visible.map(a => {
        // 选中态 = sidebar 过滤器命中 || 所属设备被勾选（设备级勾选时全部容器行
        // 视觉勾上，与设备行三态 ✓ 对齐；点击行为见 DeviceListItem.handleAndroidToggle）
        const checked = deviceChecked || checkedIds.has(a.id)
        const starting = a.status === 'starting'
        const stopped = a.status !== 'running' && a.status !== 'starting'
        return (
          <div
            key={a.id}
            onClick={() => onToggleAndroid(a.id)}
            className={`group flex items-center gap-1.5 pl-6 pr-2 py-0.5 cursor-pointer text-xs rounded transition-colors ${
              checked ? 'bg-blue-50/70' : 'hover:bg-gray-50'
            }`}
          >
            {/* 状态点：运行中绿 / 开机中琥珀（闪烁）/ 关机灰（showOffline=全部时可见） */}
            <span
              className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                stopped ? 'bg-gray-300'
                  : starting ? 'bg-amber-400 animate-pulse' : 'bg-green-500'
              }`}
              title={starting ? t('host:deviceList.starting') : stopped ? t('host:deviceList.offline') : undefined}
            />
            <input
              type="checkbox"
              checked={checked}
              readOnly
              aria-label={`#${a.indexNum}`}
              className="w-3 h-3 rounded border-gray-300 text-blue-500 focus:ring-blue-400 pointer-events-none shrink-0"
            />
            <span className={`truncate min-w-0 ${stopped ? 'text-gray-400' : 'text-gray-700'}`} title={a.name}>
              {containerDisplayName(a.name, a.deviceId, a.indexNum)}
            </span>
            <span className="ml-auto text-[10px] text-gray-400 tabular-nums shrink-0" title={`#${a.indexNum}`}>
              {a.indexNum}
            </span>
          </div>
        )
      })}
    </div>
  )
}

export default DeviceAndroidList
