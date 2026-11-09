import type { DeviceState } from '../types/index'

/** 磁盘占用告警阈值（百分比）：超过即提示「磁盘快满」。 */
export const DISK_ALERT_THRESHOLD = 90

export interface DeviceAlert {
  key: string
  /** i18n key（host 命名空间 deviceList.*），渲染方用 t() 翻译 */
  messageKey: string
  /** 插值参数（如磁盘百分比 pct） */
  values?: Record<string, unknown>
}

export interface DeviceAlertOptions {
  /** 磁盘告警阈值（百分比），默认 DISK_ALERT_THRESHOLD */
  diskThreshold?: number
  /**
   * 当前是否已登录（授权账号）。未登录时 bind_status 未知（omitempty 缺失），
   * 绑定类告警没有意义——与 PaymentPage / bindLabel 的口径一致。
   */
  loggedIn?: boolean
}

/**
 * 设备级告警计算（纯函数，不依赖 React/i18n 实例，单测友好）。
 *
 * 有数据支撑的告警项（数据源 DeviceState，见 types/index.ts）：
 * - bind_status === 0：未购买实例（未绑定）
 * - bind_status === 2：被其他账号绑定（影响购买/续费）
 * - 磁盘占用 > 阈值（默认 90%）
 *
 * 不在此重复提示（组件已有独立标识）：离线/认证失败（状态点 + 认证按钮）、
 * SDK 过低（lowSdk 橙色徽标）、授权到期（数据在坑位级 GetSlotExpiry，
 * 不在设备对象上，由坑位卡展示）。
 */
export function getDeviceAlerts(
  device: DeviceState,
  opts: DeviceAlertOptions = {},
): DeviceAlert[] {
  const { diskThreshold = DISK_ALERT_THRESHOLD, loggedIn } = opts
  const alerts: DeviceAlert[] = []
  if (loggedIn !== false) {
    if (device.bind_status === 0) {
      alerts.push({ key: 'unbound', messageKey: 'host:deviceList.alertUnbound' })
    }
    if (device.bind_status === 2) {
      alerts.push({ key: 'boundByOther', messageKey: 'host:deviceList.alertBoundByOther' })
    }
  }
  if (device.disk_total && device.disk_total > 0) {
    const pct = Math.round(((device.disk_used ?? 0) / device.disk_total) * 100)
    if (pct > diskThreshold) {
      alerts.push({
        key: 'disk',
        messageKey: 'host:deviceList.alertDiskFull',
        values: { pct },
      })
    }
  }
  return alerts
}
