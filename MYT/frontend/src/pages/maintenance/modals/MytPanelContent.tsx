import { useState, useEffect, useCallback } from 'react'
import * as MaintenanceService from '../../../../bindings/github.com/moyunteng/myt/maintenanceservicewrapper'
import { showToast } from '../../../components/Toast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { useTranslation } from 'react-i18next'
import { Download, Trash2, ExternalLink, Play } from 'lucide-react'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'
import { isTerminal } from '../../../hooks/taskQueue/taskQueueHelpers'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { btnPrimary, btnDanger, StatusLine, InfoCard, InfoRow } from './maintenanceUi'

/**
 * 魔云互联（设备端面板）：状态行 + 说明 + 访问地址信息卡 + 操作行。
 * 装卸走任务队列（后端 installMytPanel/uninstallMytPanel handler 执行）：提交后关弹窗，
 * 进度与结果在任务面板；本组件的「进行中」状态从任务队列派生，不依赖本地 state。
 * 视觉语言与公网穿透共用 maintenanceUi。
 */
export function MytPanelContent({ deviceIP, onClose }: { deviceIP: string; onClose?: () => void }) {
  const { t } = useTranslation()
  const { mode: deployMode } = useDeployMode()
  const { tasks, addTask, openPanel } = useTaskQueue()
  const [installed, setInstalled] = useState(false)
  const [panelURL, setPanelURL] = useState('')
  // v2 专属回显（CheckMytPanelInstalled 的 v2 分支返回）：版本/运行态/需更新
  const [panelVer, setPanelVer] = useState('')
  const [panelRunning, setPanelRunning] = useState(false)
  const [needUpdate, setNeedUpdate] = useState(false)
  const [confirmAction, setConfirmAction] = useState<{ title: string; message: string; onConfirm: () => void | Promise<void> } | null>(null)

  const check = useCallback(async () => {
    try {
      const r = await MaintenanceService.CheckMytPanelInstalled(deviceIP) as any
      setInstalled(!!r.installed)
      // v2 模式面板监听 8181（myt-panel-v2 全参数），v3/其他模式 8081；r.url 优先
      setPanelURL(r.url || `http://${deviceIP}:${r.v2 ? 8181 : 8081}`)
      setPanelVer(typeof r.version === 'string' ? r.version : '')
      setPanelRunning(!!r.running)
      setNeedUpdate(!!r.needUpdate)
    } catch {
      setInstalled(false)
      // v2 模式面板监听 8181（CheckMytPanelInstalled 异常时按部署模式 fallback）
      setPanelURL(`http://${deviceIP}:${deployMode === 'v2' ? 8181 : 8081}`)
      setPanelVer(''); setPanelRunning(false); setNeedUpdate(false)
    }
  }, [deviceIP, deployMode])

  useEffect(() => { check() }, [check])

  // 该设备上正在跑的装/卸任务：用于状态显示与按钮禁用（关掉弹窗再打开也能反映）
  const activeTask = tasks.find(tk =>
    (tk.type === 'installMytPanel' || tk.type === 'uninstallMytPanel') &&
    !isTerminal(tk.status) && tk.deviceIps.includes(deviceIP))

  // 任务结束后回查一次安装状态（如刚装完，状态应变为已安装）
  const activeId = activeTask?.id
  useEffect(() => { if (!activeId) check() }, [activeId, check])

  const busy = !!activeTask

  const submit = async (type: 'installMytPanel' | 'uninstallMytPanel') => {
    const taskId = await addTask({ type, targets: [{ deviceIp: deviceIP }], deviceIps: [deviceIP], total: 1 })
    if (taskId) {
      showToast(type === 'installMytPanel' ? t('maintenance:panel.installQueued') : t('maintenance:panel.uninstallQueued'), 'success')
      openPanel(taskId)
      onClose?.()
    }
  }

  const copyURL = () => {
    navigator.clipboard.writeText(panelURL).then(() => showToast(t('maintenance:ui.copied'), 'success')).catch(() => {})
  }

  // 手动启动（「已安装未运行」：设备重启后 nohup 进程丢失）。直调
  // StartMytPanel（轻量 SSH 操作，不经任务队列）；v2 与 v3 同走 SSH 路径。
  const [starting, setStarting] = useState(false)
  const startPanel = async () => {
    setStarting(true)
    try {
      const r = await MaintenanceService.StartMytPanel(deviceIP) as any
      if (r.success) showToast(r.message || t('maintenance:panel.startSuccess'), 'success')
      else showToast(t('maintenance:panel.startFailed', { message: r.message }), 'error')
    } catch (e) {
      showToast(t('maintenance:panel.startFailed', { message: String(e) }), 'error')
    } finally {
      setStarting(false)
      check()
    }
  }

  const statusLabel = activeTask
    ? (activeTask.type === 'installMytPanel' ? t('maintenance:panel.installing') : t('maintenance:panel.uninstalling'))
    : !installed
      ? t('maintenance:panel.notInstalled')
      : panelRunning === false && !panelVer
        ? t('maintenance:panel.installedStopped')
        : t('maintenance:panel.installed')

  return (
    <div className="space-y-3">
      <StatusLine tone={busy ? 'busy' : !installed ? 'off' : panelRunning === false && !panelVer ? 'busy' : 'on'} label={statusLabel} />

      <p className="text-xs text-gray-500 leading-relaxed">{t('maintenance:panel.desc')}</p>

      {/* v2 容器插件回显：版本 + 运行态 + 需更新提示（local 无这些字段，不渲染） */}
      {installed && panelVer && (
        <InfoCard>
          <InfoRow label={t('maintenance:panel.version')} value={`v${panelVer}${panelRunning ? '' : ' · ' + t('maintenance:panel.stopped')}`} />
          {needUpdate && (
            <InfoRow label={t('maintenance:panel.updateHint')} value={t('maintenance:panel.updateAvailable')} />
          )}
        </InfoCard>
      )}

      {installed && (
        <InfoCard>
          <InfoRow
            label={t('maintenance:panel.url')}
            value={panelURL}
            onOpen={() => window.open(panelURL, '_blank')}
            onCopy={copyURL}
          />
        </InfoCard>
      )}

      {/* 主操作随状态切换：未安装→安装；已安装未运行（SSH 路径）→启动；已安装运行中→访问面板 + 卸载 */}
      <div className="flex items-center gap-2 pt-0.5">
        {installed ? (
          panelRunning === false && !panelVer ? (
            <button onClick={startPanel} disabled={busy || starting} className={btnPrimary}>
              <Play size={13} />
              {starting ? t('maintenance:panel.starting') : t('maintenance:panel.start')}
            </button>
          ) : (
            <>
              <button onClick={() => window.open(panelURL, '_blank')} disabled={busy} className={btnPrimary}>
                <ExternalLink size={13} />
                {t('maintenance:panel.visitPanel')}
              </button>
              <button
                onClick={() => setConfirmAction({
                  title: t('maintenance:panel.uninstallTitle'),
                  message: t('maintenance:panel.uninstallMsg'),
                  onConfirm: () => { setConfirmAction(null); submit('uninstallMytPanel') },
                })}
                disabled={busy}
                className={btnDanger}
              >
                <Trash2 size={13} />
                {t('button.uninstall')}
              </button>
            </>
          )
        ) : (
          <button onClick={() => submit('installMytPanel')} disabled={busy} className={btnPrimary}>
            <Download size={13} />
            {busy ? t('maintenance:panel.installing') : t('button.install')}
          </button>
        )}
      </div>

      {confirmAction && (
        <ConfirmDialog
          title={confirmAction.title}
          message={confirmAction.message}
          confirmLabel={t('button.confirm')}
          onConfirm={confirmAction.onConfirm}
          onCancel={() => setConfirmAction(null)}
        />
      )}
    </div>
  )
}
