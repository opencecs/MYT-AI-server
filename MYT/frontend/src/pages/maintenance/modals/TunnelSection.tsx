import { useState, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Download, Trash2, BookOpen, AlertTriangle, ExternalLink } from 'lucide-react'
import { showToast } from '../../../components/Toast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { useModalEnter } from '../../../hooks/useModalAnimation'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'
import { isTerminal } from '../../../hooks/taskQueue/taskQueueHelpers'
import { btnPrimary, btnDanger, btnGhost, StatusLine, InfoCard, InfoRow, FeedbackBar } from './maintenanceUi'

/** 子弹窗外壳：portal 到 body —— 本组件渲染在维护弹窗的 .modal-content 内，
 *  该卡片被 useModalEnter 写过 transform，会成为 fixed 后代的包含块（同 ConfirmDialog 的坑）。 */
function PortalDialog({ children, onClose, width = 'w-[460px]' }: { children: ReactNode; onClose: () => void; width?: string }) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className={`bg-white rounded-lg shadow-xl ${width} max-w-[calc(100vw-32px)] max-h-[85vh] flex flex-col modal-content`} onClick={e => e.stopPropagation()}>
        {children}
      </div>
    </div>,
    document.body,
  )
}

const labelCls = 'block text-[11px] text-gray-500 mb-1'
const inputCls = 'w-full h-8 text-xs px-2.5 border border-gray-200 rounded outline-none focus:border-blue-400 bg-white'

/**
 * 公网穿透（frp）：在用户自己的公网服务器上部署 frps，在设备上部署 frpc，
 * 把设备 SSH(22) 与魔云互联面板(7400) 暴露到公网；两个端口由 frps 自动分配。
 *
 * 装卸走任务队列（后端 installTunnel/uninstallTunnel handler 执行）：提交后关弹窗，
 * 分阶段进度（部署服务端 → 安装客户端）与结果在任务面板；frps 分配到的公网地址由 handler
 * 写回任务详情，本组件从最近一次任务里读回来展示，因此关掉弹窗再打开地址不丢。
 */
export function TunnelSection({ deviceIP, onClose }: { deviceIP: string; onClose?: () => void }) {
  const { t } = useTranslation()
  const { tasks, addTask, openPanel } = useTaskQueue()
  const [showConfig, setShowConfig] = useState(false)
  const [showGuide, setShowGuide] = useState(false)
  const [confirmAction, setConfirmAction] = useState<{ title: string; message: string; onConfirm: () => void | Promise<void> } | null>(null)

  // 服务器配置表单（默认值与旧客户端一致；仅内存，不进任务落档的密码字段由后端抹除）
  const [serverAddr, setServerAddr] = useState('')
  const [serverSSHPort, setServerSSHPort] = useState('22')
  const [serverSSHUser, setServerSSHUser] = useState('root')
  const [serverSSHPassword, setServerSSHPassword] = useState('')
  const [serverPort, setServerPort] = useState('7000')

  // 该设备上的公网穿透任务：最近一次决定展示内容，进行中的用于禁用按钮
  const tunnelTasks = tasks.filter(tk => (tk.type === 'installTunnel' || tk.type === 'uninstallTunnel') && tk.deviceIps.includes(deviceIP))
  const latest = tunnelTasks[0]
  const activeTask = tunnelTasks.find(tk => !isTerminal(tk.status))
  const busy = !!activeTask
  const installed = !busy && latest?.type === 'installTunnel' && latest?.status === 'completed'
  const failed = !busy && latest?.status === 'failed'

  const addrOf = (v: unknown) => (typeof v === 'string' && v ? v : undefined)
  const serverAddrOut = installed ? addrOf(latest?.serverAddr) : undefined
  const remoteAddress = installed ? addrOf(latest?.remoteAddress) : undefined
  const webAddress = installed ? addrOf(latest?.webAddress) : undefined

  const submitInstall = async () => {
    const addr = serverAddr.trim()
    if (!addr) { showToast(t('maintenance:tunnel.needServerAddr'), 'error'); return }
    if (!serverSSHPassword) { showToast(t('maintenance:tunnel.needSSHPassword'), 'error'); return }
    setShowConfig(false)
    const taskId = await addTask({
      type: 'installTunnel',
      targets: [{
        deviceIp: deviceIP,
        serverAddr: addr,
        serverSSHUser: serverSSHUser || 'user',
        serverSSHPassword: serverSSHPassword || 'myt',
        serverSSHPort: Number(serverSSHPort) || 22,
        bindPort: Number(serverPort) || 7000,
      }],
      deviceIps: [deviceIP],
      total: 1,
    })
    if (taskId) {
      showToast(t('maintenance:tunnel.installQueued'), 'success')
      openPanel(taskId)
      onClose?.()
    }
  }

  const submitUninstall = async () => {
    const hasServer = !!serverAddr.trim() && !!serverSSHPassword
    const taskId = await addTask({
      type: 'uninstallTunnel',
      targets: [{
        deviceIp: deviceIP,
        serverAddr: hasServer ? serverAddr.trim() : '',
        serverSSHUser: serverSSHUser || 'root',
        serverSSHPassword: hasServer ? serverSSHPassword : '',
        serverSSHPort: Number(serverSSHPort) || 22,
      }],
      deviceIps: [deviceIP],
      total: 1,
    })
    if (taskId) {
      showToast(t('maintenance:tunnel.uninstallQueued'), 'success')
      openPanel(taskId)
      onClose?.()
    }
  }

  const copyText = (text: string) => {
    navigator.clipboard.writeText(text).then(() => showToast(t('maintenance:ui.copied'), 'success')).catch(() => {})
  }

  return (
    <div className="flex-1 flex flex-col space-y-3">
      {busy && <StatusLine tone="busy" label={t('maintenance:tunnel.installing')} />}
      {!busy && installed && <StatusLine tone="on" label={t('maintenance:tunnel.connected')} />}

      <p className="text-xs text-gray-500 leading-relaxed">{t('maintenance:tunnel.desc')}</p>

      <InfoCard>
        <InfoRow label={t('maintenance:tunnel.labelExpose')} value={t('maintenance:tunnel.valueExpose')} />
        {serverAddrOut && (
          <InfoRow
            label={t('maintenance:tunnel.resultServerAddr')}
            value={`${serverAddrOut}:7500`}
            onOpen={() => window.open(`http://${serverAddrOut}:7500`, '_blank')}
          />
        )}
        {remoteAddress && (
          <InfoRow label={t('maintenance:tunnel.labelSshAddr')} value={remoteAddress} onCopy={() => copyText(remoteAddress)} />
        )}
        {webAddress && (
          <InfoRow label={t('maintenance:tunnel.labelPanelAddr')} value={webAddress} onOpen={() => window.open(webAddress, '_blank')} />
        )}
      </InfoCard>

      {!installed && !busy && !latest && (
        <p className="text-[11px] text-gray-500">{t('maintenance:tunnel.exposeHint')}</p>
      )}
      {busy && <p className="text-[11px] text-blue-600">{t('maintenance:tunnel.installingHint')}</p>}
      {failed && <FeedbackBar tone="error">{latest?.error || t('maintenance:tunnel.installFailed')}</FeedbackBar>}

      {/* 主操作随状态切换：未安装→一键安装；已安装→打开面板（不保留与状态矛盾的"安装"） */}
      <div className="mt-auto flex items-center gap-2 pt-0.5">
        {installed && webAddress ? (
          <button onClick={() => window.open(webAddress, '_blank')} disabled={busy} className={btnPrimary}>
            <ExternalLink size={13} />
            {t('maintenance:tunnel.webPanel')}
          </button>
        ) : (
          <button onClick={() => setShowConfig(true)} disabled={busy} className={btnPrimary}>
            <Download size={13} />
            {busy ? t('maintenance:tunnel.installing') : t('maintenance:tunnel.installNow')}
          </button>
        )}
        <button
          onClick={() => setConfirmAction({
            title: t('maintenance:tunnel.uninstallTitle'),
            message: serverAddr.trim() && serverSSHPassword
              ? t('maintenance:tunnel.uninstallMsg')
              : t('maintenance:tunnel.uninstallMsgClientOnly'),
            onConfirm: () => { setConfirmAction(null); submitUninstall() },
          })}
          disabled={busy}
          className={btnDanger}
        >
          <Trash2 size={13} />
          {t('button.uninstall')}
        </button>
        <div className="flex-1" />
        <button onClick={() => setShowGuide(true)} className={btnGhost}>
          <BookOpen size={13} />
          {t('maintenance:tunnel.usageGuide')}
        </button>
      </div>

      {/* 安装配置弹窗 */}
      {showConfig && (
        <PortalDialog onClose={() => setShowConfig(false)}>
          <div className="flex items-center justify-between px-4 py-3 border-b border-gray-100">
            <h4 className="text-sm font-medium text-gray-700">{t('maintenance:tunnel.dialogTitle')}</h4>
            <button onClick={() => setShowConfig(false)} className="text-gray-500 hover:text-gray-700 text-lg leading-none cursor-pointer">&times;</button>
          </div>
          <div className="flex-1 overflow-y-auto p-4 space-y-3">
            <div className="flex items-start gap-2 text-[11px] text-blue-600 bg-blue-50 border border-blue-100 rounded px-2.5 py-2 leading-relaxed">
              <AlertTriangle size={13} className="mt-0.5 shrink-0" />
              <span>{t('maintenance:tunnel.dialogAlert')}</span>
            </div>
            <div>
              <label className={labelCls}>{t('maintenance:tunnel.serverAddr')}</label>
              <input value={serverAddr} onChange={e => setServerAddr(e.target.value)} placeholder={t('maintenance:tunnel.serverAddrPlaceholder')} className={inputCls} />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className={labelCls}>{t('maintenance:tunnel.serverSSHPort')}</label>
                <input type="number" value={serverSSHPort} onChange={e => setServerSSHPort(e.target.value)} className={inputCls} />
              </div>
              <div>
                <label className={labelCls}>{t('maintenance:tunnel.frpsPort')}</label>
                <input type="number" value={serverPort} onChange={e => setServerPort(e.target.value)} className={inputCls} />
              </div>
            </div>
            <div>
              <label className={labelCls}>{t('maintenance:tunnel.sshUser')}</label>
              <input value={serverSSHUser} onChange={e => setServerSSHUser(e.target.value)} className={inputCls} />
            </div>
            <div>
              <label className={labelCls}>{t('maintenance:tunnel.sshPassword')}</label>
              <input type="password" value={serverSSHPassword} onChange={e => setServerSSHPassword(e.target.value)} className={inputCls} />
            </div>
          </div>
          <div className="flex justify-end gap-2 px-4 py-3 border-t border-gray-100">
            <button onClick={() => setShowConfig(false)} className={btnGhost}>{t('button.cancel')}</button>
            <button onClick={submitInstall} className={btnPrimary}>{t('maintenance:tunnel.installNow')}</button>
          </div>
        </PortalDialog>
      )}

      {/* 使用说明弹窗 */}
      {showGuide && (
        <PortalDialog onClose={() => setShowGuide(false)} width="w-[520px]">
          <div className="flex items-center justify-between px-4 py-3 border-b border-gray-100">
            <h4 className="text-sm font-medium text-gray-700">{t('maintenance:tunnel.title')} · {t('maintenance:tunnel.usageGuide')}</h4>
            <button onClick={() => setShowGuide(false)} className="text-gray-500 hover:text-gray-700 text-lg leading-none cursor-pointer">&times;</button>
          </div>
          <div className="flex-1 overflow-y-auto p-4 text-xs text-gray-600 space-y-2 leading-relaxed">
            <p className="font-medium text-gray-700">{t('maintenance:tunnel.guideWhat')}</p>
            <p className="text-gray-500">{t('maintenance:tunnel.guideWhatDesc')}</p>
            <div className="border-t border-gray-100 pt-2">
              <p className="font-medium text-gray-700 mb-1">{t('maintenance:tunnel.guideSteps')}</p>
              <p>1. {t('maintenance:tunnel.guideStep1')}</p>
              <p>2. {t('maintenance:tunnel.guideStep2')}</p>
              <p>3. {t('maintenance:tunnel.guideStep3')}</p>
              <p>4. {t('maintenance:tunnel.guideStep4')}</p>
              <p>5. {t('maintenance:tunnel.guideStep5')}</p>
            </div>
            <div className="border-t border-gray-100 pt-2">
              <p className="font-medium text-gray-700 mb-1">{t('maintenance:tunnel.guideLoginTitle')}</p>
              <p>{t('maintenance:tunnel.guideAccount')}: <span className="font-mono text-gray-800">admin</span></p>
              <p>{t('maintenance:tunnel.guidePassword')}: <span className="font-mono text-gray-800">admin</span></p>
            </div>
            <div className="border-t border-gray-100 pt-2">
              <p className="font-medium text-gray-700 mb-1">{t('maintenance:tunnel.guideParamTitle')}</p>
              <p>{t('maintenance:tunnel.guideParamServer')}</p>
              <p>{t('maintenance:tunnel.guideParamSSHPort')}</p>
              <p>{t('maintenance:tunnel.guideParamFrpsPort')}</p>
            </div>
            <p className="text-blue-600 cursor-pointer hover:underline" onClick={() => window.open('https://gofrp.org/zh-cn/docs/', '_blank')}>
              {t('maintenance:tunnel.guideFrpDocs')}
            </p>
          </div>
        </PortalDialog>
      )}

      {confirmAction && (
        <ConfirmDialog
          title={confirmAction.title}
          message={confirmAction.message}
          confirmLabel={t('maintenance:tunnel.uninstallConfirm')}
          onConfirm={confirmAction.onConfirm}
          onCancel={() => setConfirmAction(null)}
        />
      )}
    </div>
  )
}
