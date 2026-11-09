import { memo, useCallback } from 'react'
import type { AndroidInfo, DeviceState } from '../../../types/index'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { SeriesBadge } from '../../../components/DeviceRowBits'
import { isStatusVisible, isRunnableStatus } from '../../../utils/containerStatus'
import { getDeviceAlerts } from '../../../utils/deviceAlerts'
import DeviceAndroidList from './deviceExpand/DeviceAndroidList'

interface DeviceListItemProps {
  device: DeviceState
  isChecked: boolean
  isSingleSelected: boolean
  isMultiSelect: boolean
  lowSdk?: boolean
  note: string | undefined
  /** 该设备下的云机列表（无云机/未加载时不显示展开箭头） */
  androids?: AndroidInfo[]
  isExpanded: boolean
  onToggleExpand: (deviceId: string) => void
  /** 已勾选的云机 id 集合（侧栏过滤器 sidebarAndroidIds，checkbox 选中态来源） */
  checkedAndroidIds: Set<string>
  /** 容器勾选切换（侧栏过滤器）；勾选（非取消）时联动勾选所属设备 */
  onToggleAndroidSelect: (androidId: string) => void
  /** 设备已勾选时点容器行（取消该容器）→ 从设备勾选中排除该容器所属设备 */
  onExcludeAndroidFromDevice: (androidId: string, deviceId: string) => void
  onSelect: (device: DeviceState) => void
  onToggleSelect: (deviceId: string) => void
  onCreateInstance: (device: DeviceState) => void
  /** 「云机展示=全部」设置：开启时关机容器也进可见集合（展开区 + 三态勾选口径） */
  showOffline?: boolean
  onAuth: (device: DeviceState) => void
  onDelete: (device: DeviceState) => void
  onNavigateToPayment: () => void
  loggedIn: boolean
}

/**
 * 设备列表单行（2026-09-27 单行紧凑改造）：展开箭头 + 勾选框 + 系列徽标 + 名称/IP
 * + 容器数 (N) + hover 浮出的操作（+/认证/×）。
 * - 原第二行（磁盘条 DiskPill + 绑定徽章）随单行化移除：磁盘占用不展示（可在
 *   设备详情查看），绑定状态徽章暂删（如需恢复可挂 hover 或行尾）。
 * - 行内操作按钮：+（新建实例，常显）、认证、×（移除设备，hover 浮出）。
 * memo 化（2026-09-05）：几百台设备时 devicestats 心跳事件频繁全量刷新列表，
 * 行组件不 memo 会导致每秒十几次 330 行全量重渲染。
 * 生效前提：device 对象引用稳定（useHostEvents.mergeDeviceData 按字段合并
 * 复用旧对象）+ 回调引用稳定（useHostManagement/DeviceList 的 useCallback）。
 */
const DeviceListItem = memo(function DeviceListItem({
  device, isChecked, isSingleSelected, isMultiSelect, lowSdk, note,
  androids, isExpanded, onToggleExpand, checkedAndroidIds, onToggleAndroidSelect,
  onExcludeAndroidFromDevice,
  onSelect, onToggleSelect, onCreateInstance, showOffline = false,
  onAuth, onDelete,
  onNavigateToPayment, loggedIn,
}: DeviceListItemProps) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const isOnline = device.state === 0
  const isAuthFail = device.state === 2
  // 只有已知有云机的设备才显示展开箭头（androidGroups 只含有云机的设备）
  const hasAndroids = !!androids && androids.length > 0
  // 容器数口径：showOffline（「云机展示=全部」）显示全量；关闭时只计运行相关
  // （isRunnableStatus，与可见性谓词 isStatusVisible 同源），避免「(N) 全量 vs
  // 展开区只有运行中」的数字对不上。
  const androidCount = showOffline
    ? (androids?.length ?? 0)
    : (androids?.filter(a => isRunnableStatus(a.status)).length ?? 0)

  // 设备级告警（未购买实例 / 被其他账号绑定 / 磁盘快满）→「+」左侧 ⚠️，
  // hover title 逐行显示所有告警文案
  const alerts = getDeviceAlerts(device, { loggedIn })

  // 容器勾选点击分派（2026-09-27）：
  // - 设备**未勾选**：走原链路——写侧栏过滤器（sidebarAndroidIds）+ 同步批量勾选，
  //   勾容器不代表勾设备（2026-09-26 用户要求，两级选择独立）。
  // - 设备**已勾选**（容器行此时显示为全勾）：点容器行 = 取消该容器 → 设备级选择
  //   粒度是「整台设备」（order 链路无单容器维度），走排除语义：把该容器所属设备
  //   从设备勾选中摘除（设备变半选/空，三态由 checkedCount 驱动），并把该容器从
  //   sidebar 过滤器/批量集合移除（由 hook 统一处理），避免「视觉上还勾着」残留。
  const handleAndroidToggle = useCallback((androidId: string) => {
    if (isChecked) {
      onExcludeAndroidFromDevice(androidId, device.device_id)
    } else {
      onToggleAndroidSelect(androidId)
    }
  }, [isChecked, device.device_id, onExcludeAndroidFromDevice, onToggleAndroidSelect])

  // 设备行勾选框三态（2026-09-27）：显示口径与右侧展示范围（effectiveAndroids 收窄
  // 规则）对齐——某设备下 sidebar 勾了任一容器 → 右侧只展示被勾的那些，设备框显示
  // 半选；全没勾 → 设备级语义（勾了设备 = 全部容器进范围 → ✓，没勾 → 空）。
  //   全部可见容器被勾选（checkedCount === visibleCount）→ ✓
  //   部分被勾选（0 < checkedCount < visibleCount）→ −（indeterminate）
  //   全不选（checkedCount === 0）→ 沿用设备级勾选态 isChecked
  // **仅影响显示**：点击仍走原 toggle 设备链路（行 onClick → onToggleSelect），语义不变。
  // 口径与 DeviceAndroidList 的 visible 一致：showOffline（「云机展示=全部」）时关机容器
  // 也计入可见集合（三态的「全部被勾选/部分被勾选」覆盖关机行）。
  const visibleAndroids = androids?.filter(
    a => isStatusVisible(a.status, showOffline)) ?? []
  const checkedCount = visibleAndroids.reduce(
    (n, a) => n + (checkedAndroidIds.has(a.id) ? 1 : 0), 0)
  const indeterminate = visibleAndroids.length > 0 && checkedCount > 0
    && checkedCount < visibleAndroids.length
  const checked = visibleAndroids.length === 0 || checkedCount === 0
    ? isChecked
    : checkedCount === visibleAndroids.length

  return (
    <div
      onClick={() => {
        onSelect(device)
        if (isMultiSelect) {
          onToggleSelect(device.device_id)
        }
      }}
      className={`border-b border-gray-50 transition-colors ${
        isMultiSelect ? (isChecked ? 'bg-blue-50' : '') : (isSingleSelected ? 'bg-blue-50 border-l-2 border-l-blue-500' : '')
      }`}
    >
      {/* 设备行本体：flex items-center（hover 高亮在本行生效，不覆盖展开区） */}
      <div
        className="group flex items-center gap-2 pl-3 pr-2 py-1.5 cursor-pointer hover:bg-gray-50"
      >
      {isMultiSelect && (
        <input
          type="checkbox"
          checked={checked}
          // 三态半选：受控 checked 之外由 ref 设 indeterminate（DOM 属性无对应 React prop）
          ref={(el) => { if (el) el.indeterminate = indeterminate }}
          readOnly
          aria-checked={indeterminate ? 'mixed' : checked}
          aria-label={t('a11y.selectDevice', { ip: mode === 'public' ? device.device_id : device.ip })}
          className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500 focus:ring-blue-400 pointer-events-none shrink-0"
        />
      )}
      {hasAndroids ? (
        <button
          onClick={(e) => { e.stopPropagation(); onToggleExpand(device.device_id) }}
          className="p-0.5 text-gray-400 hover:text-blue-500 transition-colors shrink-0"
          title={t('host:deviceList.expandAndroids')}
          aria-label={t('host:deviceList.expandAndroids')}
          aria-expanded={isExpanded}
        >
          <svg
            className={`w-3 h-3 transition-transform ${isExpanded ? 'rotate-90' : ''}`}
            viewBox="0 0 12 12" fill="currentColor"
          >
            <path d="M4 2l4 4-4 4z"/>
          </svg>
        </button>
      ) : (
        <span className="w-4 shrink-0" />
      )}
      <SeriesBadge deviceType={device.device_type} state={device.state} />
      <span
        className="flex-1 min-w-0 text-sm text-gray-800 truncate transition-colors"
        title={note ? (mode === "public" ? device.device_id : device.ip) + " - " + note : t('host:deviceList.setNote')}
      >
        {note || (mode === "public" ? device.device_id : device.ip)}
      </span>
      {lowSdk && (
        <span className="text-[10px] text-orange-600 bg-orange-50 px-1 rounded shrink-0" title={t('host:deviceList.lowSdkHint')}>
          {t('host:deviceList.lowSdk')}
        </span>
      )}
      {/* 容器数徽章（androidGroups[deviceId].length；showOffline=「云机展示=全部」时
          显示全量，关闭时只计运行相关——与展开区/右侧展示的可见性口径一致） */}
      {hasAndroids && (
        <span
          className="text-[10px] text-gray-400 tabular-nums shrink-0"
          title={t('host:deviceList.androidCount', { count: androidCount })}
        >
          ({androidCount})
        </span>
      )}
      {alerts.length > 0 && (
        <span
          className="text-amber-500 shrink-0 inline-flex"
          title={alerts.map(a => t(a.messageKey, a.values)).join('\n')}
          aria-label={alerts.map(a => t(a.messageKey, a.values)).join('；')}
        >
          <AlertTriangle size={13} />
        </span>
      )}
      {isOnline && (
        <button
          onClick={(e) => { e.stopPropagation(); onCreateInstance(device) }}
          className="flex items-center justify-center w-5 h-5 rounded text-blue-500 hover:bg-blue-100 text-lg font-bold leading-none shrink-0" title={t('host:deviceList.newInstance')}
          aria-label={t('a11y.newInstanceOn', { ip: mode === 'public' ? device.device_id : device.ip })}
        >
          +
        </button>
      )}
      {isAuthFail && (
        <button
          onClick={(e) => { e.stopPropagation(); onAuth(device) }}
          className="hidden group-hover:block text-amber-500 text-xs hover:text-amber-600 shrink-0" title={t('auth.enterPasswordShort')}
        >
          {t('auth.authenticate')}
        </button>
      )}
      {!isOnline && !isAuthFail && (
        <button
          onClick={(e) => { e.stopPropagation(); onDelete(device) }}
          className="hidden group-hover:block text-red-400 text-sm hover:text-red-500 shrink-0" title={t('host:deviceList.deleteDevice')}
          aria-label={t('host:deviceList.deleteDevice')}
        >
          &times;
        </button>
      )}
      </div>
      {/* 展开区：必须是设备行本体的**兄弟节点**（外层容器的子元素），不能放进上面的
          flex row——放进去会被 items-center 当作行内 flex item 横向挤压，展开的容器
          列表错位到分组标题区域（2026-09-27 用户截图反馈）。 */}
      {isExpanded && hasAndroids && (
        <DeviceAndroidList
          androids={androids!}
          checkedIds={checkedAndroidIds}
          onToggleAndroid={handleAndroidToggle}
          showOffline={showOffline}
          deviceChecked={isChecked}
        />
      )}
    </div>
  )
})

export default DeviceListItem