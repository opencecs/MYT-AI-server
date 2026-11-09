import { memo } from 'react'
import { DeviceState } from '../types/index'
import { MIN_SDK_VERSION } from '../hooks/useSdkVersions'
import { SeriesBadge, DiskPill } from './DeviceRowBits'
import { bindLabel, type BindActions } from '../utils/bindLabel'
import { AlertTriangle } from 'lucide-react'
import { getDeviceAlerts } from '../utils/deviceAlerts'

interface DeviceItemProps {
  device: DeviceState
  selectedIP: string
  onSelect: (ip: string) => void
  sdkVersions?: Record<string, number>
  notes: Record<string, string>
  setAuthTarget: (d: DeviceState) => void
  idOf: (d: DeviceState) => string
  t: (key: string, opts?: any) => string
  /** 新建云机（不传则不显示 + 入口）：仅在线设备可创建，与云机页设备行同款 */
  onCreateInstance?: (device: DeviceState) => void
  /**
   * 绑定状态徽章的动作与登录态（第二行右端小 tag）。宿主未传则不渲染徽章
   * （其余复用本侧栏的页面不受影响），弹窗挂在宿主层（MaintenancePage）。
   */
  bindActions?: BindActions
}

// 行内显示名：优先备注，否则显示 IP——不能用 idOf（v2 返回序列号，对用户无意义）；
// 本地模式 device.ip 即标识，显示不变
function displayNameOf(device: DeviceState): string {
  return device.ip || device.device_id
}

// memo 生效前提：device 引用稳定（mergeDeviceList 复用旧对象）+ idOf 用
// useCallback——见 DeviceSidebar 注释。无 memo 时侧栏每次心跳全量重渲染。
const DeviceItem = memo(function DeviceItem({ device, selectedIP, onSelect, sdkVersions,
  notes, setAuthTarget, idOf, t, onCreateInstance, bindActions }: DeviceItemProps) {
  const isOnline = device.state === 0
  const isAuthFail = device.state === 2
  const note = notes[device.device_id]
  const isSelected = idOf(device) === selectedIP
  const lowSdk = (sdkVersions?.[device.device_id] ?? 0) > 0 && (sdkVersions![device.device_id]) < MIN_SDK_VERSION
  // 绑定状态徽章（第二行右端）：未登录→跳授权页；已绑定→解绑弹窗；未绑定→绑定弹窗；
  // 被其他账号绑定→不可点。绑定状态与在线无关，离线设备同样显示。
  const bindBadge = bindActions ? bindLabel(device.bind_status, bindActions.loggedIn, t) : null
  // 设备级告警（未购买实例 / 被其他账号绑定 / 磁盘快满）→「+」左侧 ⚠️，
  // hover title 逐行显示所有告警文案；登录态与绑定徽章同源（未登录不判定 bind_status）
  const alerts = getDeviceAlerts(device, { loggedIn: bindActions?.loggedIn })

  return (
    <div onClick={() => !isAuthFail && onSelect(idOf(device))}
      role="button"
      aria-label={t('a11y.deviceRow', { ip: displayNameOf(device) })}
      className={`p-3 cursor-pointer border-b border-gray-50 hover:bg-gray-50 transition-colors ${isSelected ? 'bg-blue-50 border-l-2 border-l-blue-500' : ''}`}>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 min-w-0">
          <SeriesBadge deviceType={device.device_type} state={device.state} />
          <span
            className="text-sm text-gray-800 truncate transition-colors"
            title={note ? displayNameOf(device) + ' - ' + note : displayNameOf(device)}
          >
            {note || displayNameOf(device)}
          </span>
          {sdkVersions && (sdkVersions[device.device_id] ?? 0) > 0 && (
            <span className={`text-[10px] px-1 rounded shrink-0 ${lowSdk ? 'text-orange-600 bg-orange-50' : 'text-gray-400 bg-gray-100'}`} title={lowSdk ? t('host:deviceList.lowSdkHint') : undefined}>v{sdkVersions[device.device_id]}</span>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {alerts.length > 0 && (
            <span
              className="text-amber-500 shrink-0 inline-flex"
              title={alerts.map(a => t(a.messageKey, a.values)).join('\n')}
              aria-label={alerts.map(a => t(a.messageKey, a.values)).join('；')}
            >
              <AlertTriangle size={13} />
            </span>
          )}
          {isOnline && onCreateInstance && (
            <button
              onClick={(e) => { e.stopPropagation(); onCreateInstance(device) }}
              className="text-blue-500 text-lg font-bold hover:text-blue-600"
              title={t('host:deviceList.newInstance')}
              aria-label={t('a11y.newInstanceOn', { ip: displayNameOf(device) })}
            >
              +
            </button>
          )}
          {isAuthFail && (
            <button onClick={(e) => { e.stopPropagation(); setAuthTarget(device) }} className="text-amber-500 text-xs hover:text-amber-600" title={t('auth.enterPasswordShort')}>
              {t('auth.authenticate')}
            </button>
          )}
        </div>
      </div>
      {/* 第二行：磁盘条（在线且有容量时）+ 右端绑定徽章；离线设备左侧为 1px 空占位 */}
      <div className="mt-2.5 flex items-center gap-1.5">
        {isOnline && device.disk_total != null && device.disk_total > 0
          ? <DiskPill used={device.disk_used} total={device.disk_total} />
          : <div className="h-1" />}
        {bindBadge && bindActions && (
          <span
            className={'ml-auto shrink-0 text-[9px] px-1.5 py-0.5 rounded font-medium whitespace-nowrap transition-colors ' + bindBadge.cls}
            title={bindBadge.text}
            onClick={bindBadge.clickable ? (e) => {
              e.stopPropagation()
              if (bindBadge.isLogin) bindActions.onNavigateToPayment()
              else if (bindBadge.isUnbind) bindActions.onUnbind(device)
              else bindActions.onBind(device)
            } : undefined}
          >
            {bindBadge.text}
          </span>
        )}
      </div>
    </div>
  )
})

export default DeviceItem
export { DeviceItem }
