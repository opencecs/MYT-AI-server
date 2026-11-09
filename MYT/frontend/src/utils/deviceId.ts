import type { DeployMode } from '../hooks/useDeployMode'

/** 设备标识符来源：local 用 IP 直连，public 用 device_id 走线上透传 */
type DeviceIdent = { ip: string; device_id?: string; deviceId?: string }

/**
 * 返回设备的操作标识符：local 模式用 IP 直连，public 模式用 device_id 走线上透传。
 * v2 模式优先 device_id（序列号，resolveDevice 按它查），旧数据序列号为空时回退 IP
 * （Go 侧 GetDeviceByIP 兜底）。
 *
 * 用法：const { mode } = useDeployMode(); const id = getDeviceId(device, mode)
 */
export function getDeviceId(device: DeviceIdent, mode: DeployMode): string {
  if (mode === 'public') {
    return device.device_id ?? device.deviceId ?? ''
  }
  if (mode === 'v2') {
    return device.device_id ?? device.deviceId ?? device.ip
  }
  return device.ip
}
