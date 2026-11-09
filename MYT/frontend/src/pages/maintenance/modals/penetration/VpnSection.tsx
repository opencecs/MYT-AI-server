import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Download, Trash2, Play, Square } from 'lucide-react'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { showToast } from '../../../../components/Toast'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { btnPrimary, btnDanger, btnGhost, StatusLine, InfoCard, InfoRow, FeedbackBar } from '../maintenanceUi'

/**
 * VPN 穿透（单设备面板）：查询设备 VPN 客户端状态并提供安装/卸载/启动/停止。
 *
 * 从智能体页 DeviceToolsModal 的 VPN tab 抽取的单设备版：右键语义天然单设备，
 * 动作直接 await 执行 + toast 反馈（不走任务队列），完成后回刷状态。
 * 统一用 getDeviceId(device, mode) 取标识符（修复原面板动作传 ip / 状态查询用 ident 的不一致）。
 *
 * 模式门禁：v2 由右键菜单隐藏整个「公网穿透」入口，不会进到这里；
 * v4 走 SSH 只有 frp 可用，VPN 栏显示禁用提示。
 * SDK 门禁：local/public 模式下安装/启动/停止需要设备 SDK ≥177（卸载不需要）。
 */

interface VpnStatus {
  installed: boolean
  running: boolean
  enabled: boolean
}

type VpnAction = 'install' | 'uninstall' | 'start' | 'stop'

const LOW_SDK_VERSION = 177

export function VpnSection({ deviceIP }: { deviceIP: string }) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [status, setStatus] = useState<VpnStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [operating, setOperating] = useState<VpnAction | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [sdkVersion, setSdkVersion] = useState(0)

  const unsupported = mode === 'v4'

  const refresh = useCallback(async () => {
    try {
      const v = await SdkApiService.GetVpnStatus(deviceIP) as VpnStatus | null
      setStatus(v ? { installed: !!v.installed, running: !!v.running, enabled: !!v.enabled } : null)
    } catch {
      setStatus(null)
    } finally {
      setLoading(false)
    }
    // SDK 版本门禁仅 local/public 有意义（v2/v4 已由模式门禁覆盖）
    if (mode === 'local' || mode === 'public') {
      try {
        const info = await SdkApiService.GetAPIVersionInfo(deviceIP) as { currentVersion?: number } | null
        setSdkVersion(info?.currentVersion ?? 0)
      } catch { /* ignore */ }
    }
  }, [deviceIP, mode])

  useEffect(() => { void refresh() }, [refresh])

  const runAction = useCallback(async (action: VpnAction) => {
    setOperating(action)
    setError(null)
    const fnMap: Record<VpnAction, (ident: string) => Promise<void>> = {
      install: (id) => SdkApiService.ReinstallVpn(id),
      uninstall: (id) => SdkApiService.UninstallVpn(id),
      start: (id) => SdkApiService.StartVpn(id),
      stop: (id) => SdkApiService.StopVpn(id),
    }
    try {
      await fnMap[action](deviceIP)
      showToast(t('maintenance:penetration.vpn.toast.success', { action: t(`maintenance:penetration.vpn.action.${action}`) }), 'success')
    } catch (e) {
      const msg = t('maintenance:penetration.vpn.toast.failed', { action: t(`maintenance:penetration.vpn.action.${action}`), error: String(e) })
      setError(msg)
      showToast(msg, 'error')
    } finally {
      setOperating(null)
      void refresh()
    }
  }, [deviceIP, refresh, t])

  const needSdk = sdkVersion > 0 && sdkVersion < LOW_SDK_VERSION
  const lowSdkAction = (a: VpnAction) => a !== 'uninstall' && needSdk

  /** 与状态冲突的动作禁用：未安装→启动/停止、运行中→启动、已停止→停止（uninstall 恒可用） */
  const stateDisabled = (a: VpnAction) =>
    !status?.installed
      ? a === 'start' || a === 'stop'
      : a === 'start' ? status.running : a === 'stop' ? !status.running : false

  const actionBtn = (a: VpnAction, icon: React.ReactNode, label: string, cls: string, hint?: string) => {
    const busy = operating === a
    return (
      <button
        onClick={() => void runAction(a)}
        disabled={loading || unsupported || !!operating || lowSdkAction(a) || stateDisabled(a)}
        title={hint}
        className={cls}
      >
        {busy ? <span className="w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-pulse" /> : icon}
        {busy ? t('maintenance:penetration.vpn.executing') : label}
      </button>
    )
  }

  // 等高对齐：h-full + flex col + 内容 flex-1、按钮行 mt-auto，使右栏按钮行与左栏贴底对齐
  if (unsupported) {
    return (
      <div className="flex-1 flex flex-col space-y-3">
        <p className="text-xs text-gray-500 leading-relaxed">{t('maintenance:penetration.vpn.desc')}</p>
        <FeedbackBar tone="error">{t('maintenance:penetration.vpn.unsupportedV4')}</FeedbackBar>
        <div className="mt-auto" />
      </div>
    )
  }

  return (
    <div className="flex-1 flex flex-col justify-between space-y-3">
      {loading && <StatusLine tone="busy" label={t('maintenance:penetration.vpn.querying')} />}
      {!loading && status?.installed && (
        <StatusLine tone={status.running ? 'on' : 'off'} label={t(status.running ? 'maintenance:penetration.vpn.statusRunning' : 'maintenance:penetration.vpn.statusStopped')} />
      )}

      <p className="text-xs text-gray-500 leading-relaxed">{t('maintenance:penetration.vpn.desc')}</p>

      <InfoCard>
        <InfoRow label={t('maintenance:penetration.vpn.labelStatus')} value={t(
          !status?.installed
            ? 'maintenance:penetration.vpn.valueNotInstalled'
            : status.running
              ? 'maintenance:penetration.vpn.valueRunning'
              : 'maintenance:penetration.vpn.valueStopped'
        )} />
        <InfoRow label={t('maintenance:penetration.vpn.labelEnabled')} value={t(
          status?.installed
            ? (status.enabled ? 'maintenance:penetration.vpn.valueEnabled' : 'maintenance:penetration.vpn.valueDisabled')
            : 'maintenance:penetration.vpn.valueDash'
        )} />
      </InfoCard>

      {error && <FeedbackBar tone="error">{error}</FeedbackBar>}
      {needSdk && <FeedbackBar tone="error">{t('maintenance:penetration.vpn.lowSdk', { version: sdkVersion })}</FeedbackBar>}

      <div className="flex flex-wrap items-center gap-2 pt-0.5">
        {!status?.installed && actionBtn('install', <Download size={13} />, t('maintenance:penetration.vpn.action.install'), btnPrimary)}
        {status?.installed && actionBtn('start', <Play size={13} />, t('maintenance:penetration.vpn.action.start'), btnPrimary, status.running ? t('maintenance:penetration.vpn.tooltipRunning') : undefined)}
        {status?.installed && actionBtn('stop', <Square size={13} />, t('maintenance:penetration.vpn.action.stop'), btnGhost, !status.running ? t('maintenance:penetration.vpn.tooltipStopped') : undefined)}
        {actionBtn('uninstall', <Trash2 size={13} />, t('maintenance:penetration.vpn.action.uninstall'), btnDanger)}
      </div>
    </div>
  )
}
