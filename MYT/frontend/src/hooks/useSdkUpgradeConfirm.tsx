import { useCallback, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { useDeployMode } from './useDeployMode'
import { useTaskQueue } from './taskQueue/useTaskQueue'
import SdkUpgradeConfirmDialog from '../pages/maintenance/SdkUpgradeConfirmDialog'
import { showToast } from '../components/Toast'

/** 升级确认弹窗数据：SdkCheckUpgrade 结果里弹窗要展示的字段 */
interface UpgradeConfirmInfo {
  deviceIp: string
  currentVersion: string
  latestVersion: string
  description: string
}

/**
 * SDK 升级确认流程（右键菜单 / 维护页信息簇共用）：
 * SdkCheckUpgrade → 无新版 toast 提示 / 有新版弹确认（展示当前→最新版本 + 版本说明）
 * → 确认后按部署模式提交升级（v2 走 SubmitSdkInstallTask 指定版本安装，
 * local/public 走设备自升级任务 sdkUpgrade）。
 *
 * 用法：调用 requestUpgrade(device) 启动流程；把返回的 dialog 挂到页面末尾。
 */
export function useSdkUpgradeConfirm() {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { addTask, openPanel } = useTaskQueue()
  const [info, setInfo] = useState<UpgradeConfirmInfo | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const tp = (k: string, opts?: Record<string, unknown>) => t(`maintenance:sdkUpgradeConfirm.${k}`, opts)

  /** 版本显示：空值显示「未知」（弹窗内处理，这里不再需要） */

  const submit = async () => {
    if (!info || submitting) return
    setSubmitting(true)
    try {
      if (mode === 'v2') {
        await SdkApiService.SubmitSdkInstallTask([info.deviceIp], info.latestVersion)
      } else {
        await addTask({ type: 'sdkUpgrade', targets: [{ deviceIp: info.deviceIp }], deviceIps: [info.deviceIp], total: 1 })
      }
      showToast(tp('queued', { ip: info.deviceIp }), 'success')
      openPanel()
      setInfo(null)
    } catch (err) {
      console.error('SDK upgrade submit failed:', err)
      showToast(t('maintenance:devicePwd.upgradeFailed'), 'error')
    } finally {
      setSubmitting(false)
    }
  }

  const requestUpgrade = useCallback(async (deviceIp: string) => {
    try {
      const raw = await SdkApiService.SdkCheckUpgrade(deviceIp)
      const i = raw as {
        currentVersion?: number
        latestVersion?: number
        stringCurrentVersion?: string
        stringLatestVersion?: string
        description?: string
      } | null
      if (!i) { showToast(tp('checkFailed'), 'error'); return }
      // 版本判定与 DeviceInfoBar 一致：v2 版本号是 string（int 字段为空），
      // V3 走 int。解析不出（如 v2 版本串非数字）按未知处理，不弹窗
      const cur = i.stringCurrentVersion || (typeof i.currentVersion === 'number' && i.currentVersion > 0 ? String(i.currentVersion) : '')
      const latest = i.stringLatestVersion || (typeof i.latestVersion === 'number' && i.latestVersion > 0 ? String(i.latestVersion) : '')
      if (!cur || !latest) { showToast(tp('checkFailed'), 'error'); return }
      if (parseFloat(latest) <= parseFloat(cur)) { showToast(tp('noUpgrade'), 'success'); return }
      setInfo({ deviceIp, currentVersion: cur, latestVersion: latest, description: (i.description ?? '').trim() })
    } catch (err) {
      console.error('SdkCheckUpgrade failed:', err)
      showToast(tp('checkFailed'), 'error')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode])

  const dialog: ReactNode = info ? (
    <SdkUpgradeConfirmDialog
      deviceIp={info.deviceIp}
      currentVersion={info.currentVersion}
      latestVersion={info.latestVersion}
      description={info.description}
      submitting={submitting}
      onConfirm={submit}
      onCancel={() => { if (!submitting) setInfo(null) }}
    />
  ) : null

  return { requestUpgrade, upgradeDialog: dialog }
}
