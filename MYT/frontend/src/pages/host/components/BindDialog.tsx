import { useState, useEffect,  useRef} from 'react'
import { useTranslation } from 'react-i18next'
import * as PaymentService from '../../../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import type { DeviceState } from '../../../types/index'
import i18n from '../../../i18n'
import { useModalEnter } from '../../../hooks/useModalAnimation'

interface BindDialogProps {
  device: DeviceState | null
  username: string
  onClose: () => void
  /** 绑定成功回调（宿主据此刷新设备列表，拿到新的 bind_status）。 */
  onSuccess?: () => void
}

function parseBindError(err: unknown): string {
  const raw = [err instanceof Error ? err.message : '', String(err), JSON.stringify(err)].filter(Boolean).join(' ')
  if (raw.includes('60016') || raw.includes('\\u65e0\\u6743') || raw.includes('无权限')) return i18n.t('host:bind.noPermission')
  if (raw.includes('token') || raw.includes('过期') || raw.includes('expired')) return i18n.t('host:bind.loginExpired')
  // 后端 checkResponse 返回的 "操作失败: xxx" 格式
  const opMatch = raw.match(/操作失败:\s*(.+)/)
  if (opMatch) return opMatch[1].trim()
  try {
    const m = raw.match(/"msg"\s*:\s*"((?:[^"\\]|\\.)*)"/)
    if (m) return m[1].replace(/\\u([0-9a-fA-F]{4})/g, (_, h) => String.fromCharCode(parseInt(h, 16)))
  } catch { /* ignore */ }
  return i18n.t('host:bind.bindFailed')
}

function BindDialog({ device, username, onClose, onSuccess }: BindDialogProps) {
  const { t } = useTranslation()
  const [binding, setBinding] = useState(false)
  const [error, setError] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  useEffect(() => {
    setError('')
    setBinding(false)
  }, [device])

  if (!device) return null

  const handleBind = async () => {
    setBinding(true)
    setError('')
    try {
      await PaymentService.BindHost(device.device_id)
      onSuccess?.()
      onClose()
    } catch (err: unknown) {
      setError(parseBindError(err))
    } finally {
      setBinding(false)
    }
  }

  const handleClose = () => {
    setError('')
    onClose()
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-2xl shadow-xl p-6 w-[300px] max-w-[calc(100vw-32px)] text-center modal-content" onClick={e => e.stopPropagation()}>
        <div className="w-12 h-12 rounded-full bg-gradient-to-br from-blue-400 to-indigo-500 flex items-center justify-center mx-auto mb-4 shadow-lg shadow-blue-200/50">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" /><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" /></svg>
        </div>
        <div className="flex items-center justify-between mb-1">
          <div className="text-base font-semibold text-gray-700">{t('host:bind.title')}</div>
          <button onClick={handleClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="text-sm font-medium text-gray-800 bg-gray-50 rounded-lg px-3 py-2 mb-3">
          {device.ip}
        </div>
        {username && (
          <div className="flex items-center gap-2 justify-center mb-3">
            <div className="w-5 h-5 rounded-full bg-gradient-to-br from-blue-400 to-indigo-500 flex items-center justify-center shrink-0">
              <span className="text-[10px] text-white font-bold">{username[0]}</span>
            </div>
            <span className="text-xs text-gray-500">{t('host:bind.bindToAccount')} <span className="text-gray-700 font-medium">{username}</span></span>
          </div>
        )}
        <p className="text-[11px] text-amber-500 bg-amber-50 rounded-lg px-3 py-2 mb-5 text-left leading-relaxed">
          {t('host:bind.description')}
        </p>
        {error && (
          <div className="flex items-start gap-2 text-[11px] text-red-500 bg-red-50 rounded-lg px-3 py-2 mb-4 text-left">
            <svg className="w-3.5 h-3.5 shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>
            <span>{error}</span>
          </div>
        )}
        <div className="flex gap-3">
          <button
            onClick={handleClose}
            disabled={binding}
            className="flex-1 px-4 py-2 text-sm bg-gray-100 text-gray-600 rounded-lg hover:bg-gray-200 transition-colors disabled:opacity-50"
          >{t('button.cancel')}</button>
          <button
            onClick={handleBind}
            disabled={binding}
            className="flex-1 px-4 py-2 text-sm bg-gradient-to-r from-blue-500 to-indigo-500 text-white rounded-lg hover:from-blue-600 hover:to-indigo-600 transition-all shadow-md shadow-blue-200/50 disabled:opacity-50"
          >{binding ? t('host:bind.binding') : t('host:bind.confirmBind')}</button>
        </div>
      </div>
    </div>
  )
}

export default BindDialog
