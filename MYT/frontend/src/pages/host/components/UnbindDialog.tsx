import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as PaymentService from '../../../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import { showToast } from '../../../components/Toast'
import type { DeviceState } from '../../../types/index'
import i18n from '../../../i18n'
import { useModalEnter } from '../../../hooks/useModalAnimation'

interface UnbindDialogProps {
  devices: DeviceState[]
  username: string
  onClose: () => void
  /** 解绑成功回调（宿主据此刷新设备列表，拿到新的 bind_status）。 */
  onSuccess?: () => void
}

function parseUnbindError(err: unknown): string {
  const raw = [err instanceof Error ? err.message : '', String(err), JSON.stringify(err)].filter(Boolean).join(' ')
  if (raw.includes('60016') || raw.includes('无权限')) return i18n.t('host:unbind.noPermission')
  if (raw.includes('token') || raw.includes('过期')) return i18n.t('host:unbind.loginExpired')
  const opMatch = raw.match(/操作失败:\s*(.+)/)
  if (opMatch) return opMatch[1].trim()
  try {
    const m = raw.match(/"msg"\s*:\s*"((?:[^"\\]|\\.)*)"/)
    if (m) return m[1].replace(/\\u([0-9a-fA-F]{4})/g, (_, h) => String.fromCharCode(parseInt(h, 16)))
  } catch { /* ignore */ }
  return i18n.t('host:unbind.unbindFailed')
}

function UnbindDialog({ devices, username, onClose, onSuccess }: UnbindDialogProps) {
  const { t } = useTranslation()
  const [vcode, setVcode] = useState('')
  const [vkey, setVkey] = useState('')
  const [sending, setSending] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [unbinding, setUnbinding] = useState(false)
  const [error, setError] = useState('')
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // 倒计时
  useEffect(() => {
    if (countdown <= 0) {
      if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null }
      return
    }
    if (!timerRef.current) {
      timerRef.current = setInterval(() => setCountdown(n => n - 1), 1000)
    }
    return () => { if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null } }
  }, [countdown])

  // 重置状态
  useEffect(() => {
    setVcode(''); setVkey(''); setError(''); setCountdown(0); setUnbinding(false)
  }, [devices])

  if (devices.length === 0) return null

  const handleSendCode = async () => {
    if (!username) { setError(t('host:toast.unbindNeedLogin')); return }
    if (countdown > 0 || sending) return
    setError(''); setSending(true)
    try {
      const key = await PaymentService.GetPhoneVCode(username) as string
      setVkey(key)
      setCountdown(60)
      showToast(t('host:toast.vcodeSent'), 'success')
    } catch (err: any) {
      setError(err?.message || t('host:toast.unbindGetVcodeFailed'))
    } finally {
      setSending(false)
    }
  }

  const handleUnbind = async () => {
    if (!vcode.trim()) { setError(t('host:toast.unbindNeedVcode')); return }
    if (!vkey) { setError(t('host:toast.unbindNeedVkey')); return }
    setUnbinding(true); setError('')
    try {
      const deviceIds = devices.map(d => d.device_id)
      await PaymentService.UnbindHost(deviceIds, vcode, vkey)
      showToast(t('host:toast.unbindSuccess', { count: deviceIds.length }), 'success')
      onSuccess?.()
      onClose()
    } catch (err: unknown) {
      setError(parseUnbindError(err))
    } finally {
      setUnbinding(false)
    }
  }

  const handleClose = () => {
    setError('')
    onClose()
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-2xl shadow-xl p-6 w-[360px] max-w-[calc(100vw-32px)] text-center modal-content" onClick={e => e.stopPropagation()}>
        <div className="w-12 h-12 rounded-full bg-gradient-to-br from-amber-400 to-orange-500 flex items-center justify-center mx-auto mb-4 shadow-lg shadow-amber-200/50">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M3 6h18" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" /><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
          </svg>
        </div>
        <div className="flex items-center justify-between mb-1">
          <div className="text-base font-semibold text-gray-700">{t('host:unbind.title')}</div>
          <button onClick={handleClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="text-sm font-medium text-gray-800 bg-gray-50 rounded-lg px-3 py-2 mb-3">
          {t('host:unbind.deviceCount', { ips: devices.map(d => d.ip).join(', '), count: devices.length })}
        </div>
        <p className="text-[11px] text-red-500 bg-red-50 rounded-lg px-3 py-2 mb-4 text-left leading-relaxed">
          {t('host:unbind.warning')}
        </p>
        {username && (
          <div className="flex items-center gap-2 justify-center mb-3">
            <div className="w-5 h-5 rounded-full bg-gradient-to-br from-amber-400 to-orange-500 flex items-center justify-center shrink-0">
              <span className="text-[10px] text-white font-bold">{username[0]}</span>
            </div>
            <span className="text-xs text-gray-500">{t('host:unbind.unbindAccount')} <span className="text-gray-700 font-medium">{username}</span></span>
          </div>
        )}
        <div className="space-y-3 mb-4">
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={vcode}
              onChange={e => setVcode(e.target.value)}
              placeholder={t('host:unbind.smsVcode')}
              className="flex-1 px-3 py-2 text-sm border border-gray-200 rounded-lg focus:outline-none focus:border-amber-400"
              disabled={unbinding}
            />
            <button
              onClick={handleSendCode}
              disabled={countdown > 0 || sending || unbinding}
              className="px-3 py-2 text-xs bg-amber-50 text-amber-600 border border-amber-200 rounded-lg hover:bg-amber-100 transition-colors disabled:opacity-50 whitespace-nowrap min-w-[90px]"
            >
              {countdown > 0 ? `${countdown}s` : sending ? t('host:unbind.sending') : t('host:unbind.getVcode')}
            </button>
          </div>
        </div>
        {error && (
          <div className="flex items-start gap-2 text-[11px] text-red-500 bg-red-50 rounded-lg px-3 py-2 mb-4 text-left">
            <svg className="w-3.5 h-3.5 shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>
            <span>{error}</span>
          </div>
        )}
        <div className="flex gap-3">
          <button
            onClick={handleClose}
            disabled={unbinding}
            className="flex-1 px-4 py-2 text-sm bg-gray-100 text-gray-600 rounded-lg hover:bg-gray-200 transition-colors disabled:opacity-50"
          >{t('button.cancel')}</button>
          <button
            onClick={handleUnbind}
            disabled={unbinding}
            className="flex-1 px-4 py-2 text-sm bg-gradient-to-r from-amber-500 to-orange-500 text-white rounded-lg hover:from-amber-600 hover:to-orange-600 transition-all shadow-md shadow-amber-200/50 disabled:opacity-50"
          >{unbinding ? t('host:unbind.unbinding') : t('host:unbind.confirmUnbind')}</button>
        </div>
      </div>
    </div>
  )
}

export default UnbindDialog
