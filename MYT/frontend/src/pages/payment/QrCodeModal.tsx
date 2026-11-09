import { OrderInfo } from './types'
import { useTranslation } from 'react-i18next'
import { useRef } from 'react'
import { useModalEnter } from '../../hooks/useModalAnimation'

interface Props {
  orderInfo: OrderInfo
  polling: boolean
  onClose: () => void
}

export function QrCodeModal({ orderInfo, polling, onClose }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  // qrcode 可能是: 1) 完整 data URL 2) 裸 base64 3) http 图片 URL 4) 支付链接
  let imgSrc = orderInfo.qrcode
  if (imgSrc && !imgSrc.startsWith('data:') && !imgSrc.startsWith('http')) {
    // 裸 base64，补上 data URL 前缀
    imgSrc = `data:image/png;base64,${imgSrc}`
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl p-6 w-80 text-center modal-content">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-semibold text-gray-800">{t('payment:qr.title')}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        {imgSrc ? (
          <img src={imgSrc} alt={t('payment:qr.qrCode')} className="w-48 h-48 mx-auto mb-3" />
        ) : (
          <div className="w-48 h-48 mx-auto mb-3 flex items-center justify-center text-xs text-gray-400">{t('status.loading')}</div>
        )}
        <p className="text-xs text-gray-400 mb-2">{polling ? t('payment:qr.waiting') : t('payment:qr.incomplete')}</p>
        <button onClick={onClose}
          className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">
          {t('button.close')}
        </button>
      </div>
    </div>
  )
}
