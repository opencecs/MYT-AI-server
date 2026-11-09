import { useTranslation } from 'react-i18next'

interface UploadState {
  progress: number
  done: boolean
  success: boolean
}

interface Props {
  mirrorId: string
  uploads: Record<string, Record<string, UploadState>>
  cancelDevice: (mirrorId: string, ip: string) => void
  cancelAll: (mirrorId: string) => void
}

export function UploadPopover({ mirrorId, uploads, cancelDevice, cancelAll }: Props) {
  const { t } = useTranslation()
  const devices = uploads[mirrorId]
  if (!devices) return null
  const allDone = Object.values(devices).every(d => d.done)

  return (
    <div className="absolute z-50 bg-white border border-gray-200 rounded-lg shadow-lg p-3 w-64 -top-2 left-0"
      style={{ transform: 'translateY(-100%)' }}>
      <div className="text-xs font-medium text-gray-700 mb-2">{t('mirror:upload.progress')}</div>
      {Object.entries(devices).map(([ip, state]) => (
        <div key={ip} className="flex items-center gap-2 mb-1.5">
          <span className="text-[10px] text-gray-600 w-24 truncate">{ip}</span>
          <div className="flex-1 h-2 bg-gray-200 rounded overflow-hidden">
            <div className={`h-full rounded transition-all ${state.done ? (state.success ? 'bg-green-500' : 'bg-red-500') : 'bg-blue-500'}`}
              style={{ width: `${state.progress}%` }} />
          </div>
          <span className="text-[10px] text-gray-500 w-6 text-right">{Math.round(state.progress)}%</span>
          {!state.done && (
            <button className="text-[10px] text-red-400 hover:text-red-600" onClick={(e) => { e.stopPropagation(); cancelDevice(mirrorId, ip) }}>✕</button>
          )}
          {state.done && (
            <span className={`text-[10px] ${state.success ? 'text-green-500' : 'text-red-500'}`}>{state.success ? '✓' : '✗'}</span>
          )}
        </div>
      ))}
      {!allDone && (
        <button className="mt-1 text-[10px] text-red-500 hover:text-red-600" onClick={(e) => { e.stopPropagation(); cancelAll(mirrorId) }}>{t('button.cancelAll')}</button>
      )}
    </div>
  )
}
