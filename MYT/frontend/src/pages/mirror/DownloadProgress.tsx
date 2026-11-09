import { memo } from 'react'
import { useTranslation } from 'react-i18next'

interface Props {
  progress: number
  stage: string
  onCancel: () => void
}

export const DownloadProgress = memo(function DownloadProgress({ progress, stage, onCancel }: Props) {
  const { t } = useTranslation()
  const label = stage === 'extracting' ? t('mirror:download.extract') : t('mirror:download.download')
  const isUnknownSize = progress < 0
  const displayText = isUnknownSize
    ? `${label} ${Math.abs(progress).toFixed(1)}MB`
    : `${label} ${Math.round(progress)}%`
  const barWidth = isUnknownSize ? 100 : progress

  return (
    <div
      className="group/dl relative w-24 h-6 bg-gray-200 rounded overflow-hidden cursor-pointer select-none"
      onClick={onCancel}
      title={t('mirror:download.clickToCancel')}
    >
      {!isUnknownSize && (
        <div className="absolute inset-y-0 left-0 bg-green-400 group-hover/dl:opacity-0" style={{ width: `${barWidth}%` }} />
      )}
      {isUnknownSize && (
        <div className="absolute inset-y-0 left-0 bg-green-400 group-hover/dl:opacity-0 animate-pulse" style={{ width: '100%' }} />
      )}
      <span className="absolute inset-0 flex items-center justify-center text-[10px] text-gray-700 font-medium group-hover/dl:invisible">{displayText}</span>
      <div className="absolute inset-0 bg-red-500 flex items-center justify-center rounded opacity-0 group-hover/dl:opacity-100 transition-opacity">
        <span className="text-[10px] text-white font-medium pointer-events-none">{t('button.cancel')}</span>
      </div>
    </div>
  )
})
