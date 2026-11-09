import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import * as PaymentService from '../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import { showToast } from './Toast'
import { useModalEnter } from '../hooks/useModalAnimation'

interface CancelAccountDialogProps {
  username: string
  onClose: () => void
  /** 注销成功回调（登录态清理由后端 auth:status 事件驱动，这里只做 UI 收尾）。 */
  onSuccess?: () => void
}

/** 解析注销失败原因：验证码错误 / 登录过期 / 服务端原 msg。 */
function parseCancelError(err: unknown, t: (k: string) => string): string {
  const raw = [err instanceof Error ? err.message : '', String(err), JSON.stringify(err)].filter(Boolean).join(' ')
  if (raw.includes('60016') || raw.includes('无权限')) return t('cancelAccount.noPermission')
  if (raw.includes('token') || raw.includes('过期') || raw.includes('expired')) return t('cancelAccount.loginExpired')
  const opMatch = raw.match(/操作失败:\s*(.+)/)
  if (opMatch) return opMatch[1].trim()
  // 「注销失败[code]: msg」格式：优先透传服务端 msg（如验证码错误），msg 为空才取 code 兜底
  const bracketMatch = raw.match(/注销失败\[(\d+)\]:\s*(.+)/)
  if (bracketMatch) return bracketMatch[2].trim() || `注销失败(code=${bracketMatch[1]})`
  try {
    const m = raw.match(/"msg"\s*:\s*"((?:[^"\\]|\\.)*)"/)
    if (m) return m[1].replace(/\\u([0-9a-fA-F]{4})/g, (_, h) => String.fromCharCode(parseInt(h, 16)))
  } catch { /* ignore */ }
  // 非 JSON 结构的纯文本错误（后端 fmt.Errorf 直接返回的服务端 msg）原样展示，避免落兜底文案
  const line = raw.split(/[\n\r]/).map(s => s.trim()).filter(Boolean)[0] || ''
  if (line && !/^[{}\[\]]/.test(line) && !/^\d{3}\s/.test(line) && line.length < 120) return line
  return t('cancelAccount.cancelFailed')
}

/**
 * 注销账号弹窗（不可逆操作）：
 * 红色警示文案 + 手机验证码（GetPhoneVCode 发送 + 60s 倒计时）+ 确认按钮。
 * 确认按钮在输入验证码前禁用；成功后 toast 并收尾（登录态由后端事件统一清理）。
 */
export function CancelAccountDialog({ username, onClose, onSuccess }: CancelAccountDialogProps) {
  const { t } = useTranslation()
  const [vcode, setVcode] = useState('')
  const [vkey, setVkey] = useState('')
  const [sending, setSending] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const [canceling, setCanceling] = useState(false)
  const [error, setError] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  useModalEnter(rootRef)

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

  const handleSendCode = async () => {
    if (!username) { setError(t('cancelAccount.needLogin')); return }
    if (countdown > 0 || sending) return
    setError(''); setSending(true)
    try {
      const key = await PaymentService.GetPhoneVCode(username) as string
      setVkey(key)
      setCountdown(60)
      showToast(t('cancelAccount.vcodeSent'), 'success')
    } catch (err: unknown) {
      setError((err instanceof Error ? err.message : String(err)) || t('cancelAccount.getVcodeFailed'))
    } finally {
      setSending(false)
    }
  }

  const handleCancel = async () => {
    if (!vcode.trim()) { setError(t('cancelAccount.needVcode')); return }
    if (!vkey) { setError(t('cancelAccount.needVkey')); return }
    setCanceling(true); setError('')
    try {
      await PaymentService.CancelUser(vcode, vkey)
      showToast(t('cancelAccount.cancelSuccess'), 'success')
      onSuccess?.()
      onClose()
    } catch (err: unknown) {
      setError(parseCancelError(err, t))
    } finally {
      setCanceling(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-xl shadow-2xl w-[360px] max-w-[calc(100vw-32px)] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        {/* 标题区：小警示图标 + 标题 + 关闭按钮（与 SdkVersionsModal 等弹窗头统一） */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
          <h4 className="flex items-center gap-2 text-sm font-semibold text-gray-800">
            <span className="w-7 h-7 rounded-lg bg-red-50 flex items-center justify-center shrink-0">
              <AlertTriangle className="w-4 h-4 text-red-500" />
            </span>
            {t('cancelAccount.title')}
          </h4>
          <button
            onClick={onClose}
            disabled={canceling}
            className="w-6 h-6 flex items-center justify-center text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-md text-lg leading-none transition-colors"
          >&times;</button>
        </div>
        <div className="px-5 py-4 space-y-3">
          {/* 警示框：浅红底 + 左侧红色竖条 + 柔和文字层次 */}
          <div className="flex items-stretch gap-2.5 bg-red-50/70 border border-red-100/80 rounded-lg px-3 py-2.5">
            <span className="w-0.5 rounded-full bg-red-400/80 shrink-0" />
            <p className="text-[11px] text-red-600/90 leading-relaxed text-left">
              {t('cancelAccount.warning')}
            </p>
          </div>
          {username && (
            <div className="flex items-center justify-center gap-1.5 bg-gray-50 rounded-lg px-3 py-2">
              <div className="w-5 h-5 rounded-full bg-blue-500 flex items-center justify-center shrink-0">
                <span className="text-[10px] text-white font-bold">{username[0]}</span>
              </div>
              <span className="text-xs text-gray-500">{t('cancelAccount.cancelAccountLabel')} <span className="text-gray-700 font-medium">{username}</span></span>
            </div>
          )}
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={vcode}
              onChange={e => setVcode(e.target.value)}
              placeholder={t('cancelAccount.smsVcode')}
              className="flex-1 bg-gray-50 px-3 py-2 text-sm border border-gray-200 rounded-lg focus:outline-none focus:border-red-400 focus:bg-white transition-colors disabled:opacity-60"
              disabled={canceling}
            />
            <button
              onClick={handleSendCode}
              disabled={countdown > 0 || sending || canceling}
              className="px-3 py-2 text-xs bg-gray-100 text-gray-600 rounded-lg hover:bg-gray-200 transition-colors disabled:opacity-50 whitespace-nowrap min-w-[92px]"
            >
              {countdown > 0 ? `${countdown}s` : sending ? t('cancelAccount.sending') : t('cancelAccount.getVcode')}
            </button>
          </div>
          {error && (
            <div className="flex items-start gap-2 text-[11px] text-red-500 bg-red-50 rounded-lg px-3 py-2 text-left">
              <svg className="w-3.5 h-3.5 shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>
              <span>{error}</span>
            </div>
          )}
        </div>
        {/* 底部按钮区：右对齐、常规尺寸，主按钮红底白字（危险操作但不夸张） */}
        <div className="px-5 py-3 border-t border-gray-100 flex justify-end gap-2">
          <button
            onClick={onClose}
            disabled={canceling}
            className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200 transition-colors disabled:opacity-50"
          >{t('button.cancel')}</button>
          <button
            onClick={handleCancel}
            disabled={canceling || !vcode.trim()}
            className="px-4 py-1.5 text-sm bg-red-500 text-white rounded-lg hover:bg-red-600 shadow-sm shadow-red-200/60 transition-colors disabled:opacity-50"
          >{canceling ? t('cancelAccount.canceling') : t('cancelAccount.confirmCancel')}</button>
        </div>
      </div>
    </div>
  )
}
