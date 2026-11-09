import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useDeployMode } from './useDeployMode'
import { getDeviceId } from '../utils/deviceId'
import { useTaskQueue } from './taskQueue/useTaskQueue'
import { useSdkUpgradeConfirm } from './useSdkUpgradeConfirm'
import * as MaintenanceService from '../../bindings/github.com/moyunteng/myt/maintenanceservicewrapper'
import { showToast } from '../components/Toast'
import ConfirmDialog from '../components/ConfirmDialog'
import RebootDeviceDialog from '../components/RebootDeviceDialog'
import { MaintenanceModal } from '../pages/maintenance/MaintenanceModal'
import type { DeviceState } from '../types/index'
import type { ModalType } from '../pages/maintenance/maintenanceTypes'

type OpsModalType = Exclude<ModalType, 'cache' | null>

/**
 * 设备右键「维护类」操作统一状态：重启 / 魔云互联 / 镜像清理 / 日志清理 / 格式化 / SDK 升级 / tebox 安装。
 * 调用方把返回的 callbacks 传给 DeviceContextMenu，dialogs 挂在页面末尾。
 */
export function useDeviceOpsMenu() {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { openPanel } = useTaskQueue()
  const { requestUpgrade, upgradeDialog } = useSdkUpgradeConfirm()
  const [rebootTarget, setRebootTarget] = useState<DeviceState | null>(null)
  const [ops, setOps] = useState<{ device: DeviceState; type: OpsModalType } | null>(null)
  const [teboxTarget, setTeboxTarget] = useState<DeviceState | null>(null)
  const [teboxBusy, setTeboxBusy] = useState(false)

  const open = (type: OpsModalType) => (device: DeviceState) => setOps({ device, type })

  // SDK 升级：公共 hook 统一处理 SdkCheckUpgrade → 确认弹窗（含版本说明）→
  // 提交（v2 走 SubmitSdkInstallTask，local/public 走 addTask sdkUpgrade）
  const onSdkUpgrade = (device: DeviceState) => { void requestUpgrade(device.ip) }

  // tebox SDK 安装：确认弹窗 → 后端 SubmitTeboxInstallTask（任务队列执行，
  // 进度/结果在任务面板）。防重入由后端锁兜底，前端 busy 仅防双击。
  const submitTeboxInstall = async () => {
    const device = teboxTarget
    if (!device) return
    setTeboxBusy(true)
    try {
      const ip = getDeviceId(device, mode) || device.ip
      const taskId = await MaintenanceService.SubmitTeboxInstallTask(ip)
      if (taskId) {
        showToast(t('maintenance:teboxInstall.queued'), 'success')
        openPanel(taskId)
      }
      setTeboxTarget(null)
    } catch (e) {
      showToast(t('maintenance:teboxInstall.submitFailed', { message: String(e) }), 'error')
    } finally {
      setTeboxBusy(false)
    }
  }

  const teboxDialog: ReactNode = teboxTarget ? (
    <ConfirmDialog
      title={t('maintenance:teboxInstall.confirmTitle')}
      message={t('maintenance:teboxInstall.confirmMsg', {
        ip: getDeviceId(teboxTarget, mode) || teboxTarget.ip,
      })}
      confirmLabel={t('maintenance:teboxInstall.confirmBtn')}
      onConfirm={submitTeboxInstall}
      onCancel={() => { if (!teboxBusy) setTeboxTarget(null) }}
    />
  ) : null

  const dialogs: ReactNode = (
    <>
      <RebootDeviceDialog device={rebootTarget} onClose={() => setRebootTarget(null)} />
      {ops && (
        <MaintenanceModal
          type={ops.type}
          deviceIP={getDeviceId(ops.device, mode) || ops.device.ip}
          androidName=""
          logMode="device"
          onClose={() => setOps(null)}
        />
      )}
      {teboxDialog}
      {upgradeDialog}
    </>
  )

  return {
    onRebootDevice: setRebootTarget,
    onMytPanel: open('mytPanel'),
    onTunnel: open('penetration'),
    onDockerCleanup: open('docker'),
    onLogCleanup: open('log'),
    onFormatDisk: open('formatDisk'),
    onSdkUpgrade,
    sdkUpgradeV2: mode === 'v2',
    onTeboxInstall: setTeboxTarget,
    dialogs,
  }
}
