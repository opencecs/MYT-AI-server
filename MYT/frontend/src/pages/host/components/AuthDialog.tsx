import { useState, useEffect,  useRef} from 'react'
import { useTranslation } from 'react-i18next'
import * as DeviceDetailService from '../../../../bindings/github.com/moyunteng/myt/devicedetailservicewrapper'
import type { DeviceState } from '../../../types/index'
import { useModalEnter } from '../../../hooks/useModalAnimation'

interface AuthDialogProps {
  device: DeviceState | null
  onClose: () => void
}

function AuthDialog({ device, onClose }: AuthDialogProps) {
  const { t } = useTranslation()
  const [pwd, setPwd] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  useEffect(() => {
    setPwd('')
    setError('')
    setLoading(false)
  }, [device])

  if (!device) return null

  const handleVerify = async () => {
    if (!pwd.trim()) return
    setLoading(true)
    setError('')
    try {
      const ok = await DeviceDetailService.VerifyDeviceAuth(
        device.device_id, device.ip, pwd.trim()
      )
      if (ok) {
        onClose()
      } else {
        setError(t('auth.authFailed'))
      }
    } catch {
      setError(t('auth.requestFailed'))
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => {
    setPwd('')
    setError('')
    onClose()
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-2xl shadow-xl p-6 w-[320px] text-center modal-content" onClick={e => e.stopPropagation()}>
        <div className="w-12 h-12 rounded-full bg-gradient-to-br from-amber-400 to-orange-500 flex items-center justify-center mx-auto mb-4 shadow-lg shadow-amber-200/50">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
        </div>
        <div className="flex items-center justify-between mb-1">
          <div className="text-base font-semibold text-gray-700">{t('auth.title')}</div>
          <button onClick={handleClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="text-sm font-medium text-gray-800 bg-gray-50 rounded-lg px-3 py-2 mb-3">
          {device.ip}
        </div>
        {error && (
          <div className="text-[11px] text-red-500 bg-red-50 rounded-lg px-3 py-2 mb-3">{error}</div>
        )}
        <input
          type="password"
          value={pwd}
          onChange={e => setPwd(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') handleVerify() }}
          placeholder={t('auth.enterPassword')}
          autoFocus
          className="w-full bg-gray-50 text-sm px-3 py-2 rounded-lg border border-gray-200 focus:border-amber-400 focus:outline-none mb-4"
        />
        <div className="flex gap-3">
          <button
            onClick={handleClose}
            disabled={loading}
            className="flex-1 px-4 py-2 text-sm bg-gray-100 text-gray-600 rounded-lg hover:bg-gray-200 transition-colors disabled:opacity-50"
          >{t('button.cancel')}</button>
          <button
            onClick={handleVerify}
            disabled={loading || !pwd.trim()}
            className="flex-1 px-4 py-2 text-sm bg-gradient-to-r from-amber-500 to-orange-500 text-white rounded-lg hover:from-amber-600 hover:to-orange-600 transition-all shadow-md shadow-amber-200/50 disabled:opacity-50"
          >{loading ? t('status.verifying') : t('auth.authenticate')}</button>
        </div>
      </div>
    </div>
  )
}

export default AuthDialog
