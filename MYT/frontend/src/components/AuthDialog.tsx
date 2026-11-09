import { useState, useRef} from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { useModalEnter } from '../hooks/useModalAnimation'

interface Props {
  deviceIP: string
  onConfirm: (password: string) => void
  onCancel: () => void
}

function AuthDialog({ deviceIP, onConfirm, onCancel }: Props) {
  const { t } = useTranslation()
  const [pwd, setPwd] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async () => {
    if (!pwd.trim()) return
    setLoading(true)
    try {
      await SdkApiService.UpdateDevicePasswordForIP(deviceIP, pwd.trim())
      onConfirm(pwd.trim())
    } catch {
      onConfirm(pwd.trim())
    } finally {
      setLoading(false)
    }
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-[100] modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[360px] max-w-[calc(100vw-32px)] p-5 modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-1">
          <h3 className="text-sm font-semibold text-gray-800">{t('auth.title')}</h3>
          <button onClick={onCancel} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <p className="text-xs text-gray-400 mb-3">{t('auth.needPassword', { ip: deviceIP })}</p>
        <input type="password" value={pwd} onChange={e => setPwd(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleSubmit()}
          placeholder={t('auth.enterPassword')} autoFocus
          className="w-full bg-gray-50 text-sm px-3 py-2 rounded-lg border border-gray-200 focus:border-blue-400 focus:outline-none mb-3" />
        <div className="flex justify-end gap-2">
          <button onClick={onCancel} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={handleSubmit} disabled={loading || !pwd.trim()}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">
            {loading ? t('status.verifying') : t('button.confirm')}
          </button>
        </div>
      </div>
    </div>
  )
}

export default AuthDialog
