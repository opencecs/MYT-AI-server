import ConfirmDialog from './ConfirmDialog'
import { showToast } from './Toast'
import * as MaintenanceService from '../../bindings/github.com/moyunteng/myt/maintenanceservicewrapper'
import * as V2Service from '../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import { useDeployMode } from '../hooks/useDeployMode'
import { getDeviceId } from '../utils/deviceId'
import { useTranslation } from 'react-i18next'
import type { DeviceState } from '../types/index'

interface Props {
  device: DeviceState | null
  onClose: () => void
}

/** ARM 设备重启确认：主机页/群控页/共用侧栏右键「重启设备」共用。
 *  v2 模式走 81 /reboot_host/{ip}（壳子同款），local/public 走 8000 /server/device/reboot。 */
export default function RebootDeviceDialog({ device, onClose }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  if (!device) return null
  const ip = getDeviceId(device, mode) || device.ip

  return (
    <ConfirmDialog
      title={t('maintenance:rebootTitle')}
      message={t('maintenance:rebootMsg', { ip })}
      onConfirm={async () => {
        onClose()
        try {
          if (mode === 'v2') {
            await V2Service.RebootV2Host(device.device_id)
            showToast(t('maintenance:rebootSuccess'), 'success')
            return
          }
          const r = await MaintenanceService.RebootDevice(ip) as { success?: boolean; message?: string }
          r.success
            ? showToast(r.message || t('maintenance:rebootSuccess'), 'success')
            : showToast(t('maintenance:rebootFailed', { message: r.message ?? '' }), 'error')
        } catch (e) {
          showToast(t('maintenance:rebootFailed', { message: String(e) }), 'error')
        }
      }}
      onCancel={onClose}
    />
  )
}
