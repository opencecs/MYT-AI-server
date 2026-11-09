import { useState, useEffect,  useRef} from 'react'
import { useTranslation } from 'react-i18next'
import { Events } from '@wailsio/runtime'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { showToast } from '../../../../components/Toast'
import { useModalEnter } from '../../../../hooks/useModalAnimation'

interface Props {
  deviceIP: string
  onClose: () => void
}

type UpgradeStage = 'checking' | 'available' | 'latest' | 'upgrading' | 'done' | 'error'

interface ProgressData {
  progress: number
  current: number
  total: number
}

function UpgradeModal({ deviceIP, onClose }: Props) {
  const { t } = useTranslation()
  const [stage, setStage] = useState<UpgradeStage>('checking')
  const [currentVer, setCurrentVer] = useState(0)
  const [latestVer, setLatestVer] = useState(0)
  const [pct, setPct] = useState(0)
  const [currentSize, setCurrentSize] = useState(0)
  const [totalSize, setTotalSize] = useState(0)
  const [error, setError] = useState('')

  // 监听升级进度事件
  useEffect(() => {
    const off = Events.On('upgrade:progress', (data: any) => {
      const d = data as ProgressData
      if (d && d.total > 0) {
        setPct(Math.round(d.progress * 100))
        setCurrentSize(d.current)
        setTotalSize(d.total)
        setStage('upgrading')
      }
    })
    return () => { off() }
  }, [])

  // 首次打开：先查版本信息，不直接触发升级
  useEffect(() => {
    ;(async () => {
      try {
        const info = await SdkApiService.GetAPIVersionInfo(deviceIP)
        if (!info) {
          setStage('error')
          setError(t('host:upgrade.getVersionFailed'))
          return
        }
        setCurrentVer(info.currentVersion ?? 0)
        setLatestVer(info.latestVersion ?? 0)
        if (info.latestVersion > info.currentVersion) {
          setStage('available')
        } else {
          setStage('latest')
        }
      } catch (err) {
        setStage('error')
        setError(`${err}`)
        showToast(t('host:toast.upgradeCheckFailed'), 'error')
      }
    })()
  }, [])

  // 执行升级
  const startUpgrade = async () => {
    setStage('upgrading')
    setPct(0)
    setError('')
    try {
      await SdkApiService.CheckUpgrade(deviceIP)
      setStage('done')
      showToast(t('host:toast.upgradeComplete'), 'success')
    } catch (err) {
      setStage('error')
      setError(`${err}`)
      showToast(t('host:toast.upgradeFailed'), 'error')
    }
  }

  const fmtBytes = (n: number) => {
    if (n < 1024) return `${n} B`
    if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / 1048576).toFixed(1)} MB`
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[420px] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <h3 className="text-sm font-medium text-gray-700">{t('host:upgrade.title')}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="p-6 space-y-4">
          {stage === 'checking' && (
            <div className="flex items-center gap-2 text-sm text-blue-600">
              <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
              {t('host:upgrade.checking')}
            </div>
          )}
          {stage === 'available' && (
            <div className="space-y-3">
              <div className="flex items-center gap-2 text-sm text-yellow-600">
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" /><line x1="12" y1="16" x2="12.01" y2="16" /></svg>
                {t('host:upgrade.newVersion')}
              </div>
              <div className="flex gap-4 text-sm">
                <span className="text-gray-500">{t('host:upgrade.currentVersion')}<span className="text-gray-800 font-medium">{currentVer}</span></span>
                <span className="text-gray-500">{t('host:upgrade.latestVersion')}<span className="text-green-600 font-medium">{latestVer}</span></span>
              </div>
              <p className="text-xs text-gray-400">{t('host:upgrade.upgradeHint')}</p>
            </div>
          )}
          {stage === 'upgrading' && (
            <div className="space-y-2">
              <div className="flex items-center justify-between text-sm text-blue-600">
                <span>{t('host:upgrade.downloading')}</span>
                <span>{pct}%</span>
              </div>
              <div className="w-full bg-gray-200 rounded-full h-2">
                <div className="bg-blue-500 h-2 rounded-full transition-all duration-300" style={{ width: `${pct}%` }} />
              </div>
              {totalSize > 0 && (
                <p className="text-xs text-gray-400">{fmtBytes(currentSize)} / {fmtBytes(totalSize)}</p>
              )}
            </div>
          )}
          {stage === 'done' && (
            <div className="flex items-center gap-2 text-sm text-green-600">
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12" /></svg>
              {t('host:upgrade.done')}
            </div>
          )}
          {stage === 'latest' && (
            <div className="flex items-center gap-2 text-sm text-green-600">
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12" /></svg>
              {t('host:upgrade.latest', { version: currentVer })}
            </div>
          )}
          {stage === 'error' && (
            <div className="text-sm text-red-500 bg-red-50 p-2 rounded-lg">{error}</div>
          )}
        </div>
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">
            {stage === 'done' ? t('button.close') : t('button.cancel')}
          </button>
          {stage === 'available' && (
            <button onClick={startUpgrade} className="px-4 py-1.5 text-sm bg-yellow-500 text-white rounded-lg hover:bg-yellow-600">{t('host:upgrade.confirmUpgrade')}</button>
          )}
        </div>
      </div>
    </div>
  )
}

export default UpgradeModal
