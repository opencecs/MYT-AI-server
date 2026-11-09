import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import QRCode from 'qrcode'
import { useModalEnter } from '../../hooks/useModalAnimation'

/** 采集工具 APK 下载链接 */
const COLLECTION_TOOL_URL = 'https://d.moyunteng.com/download/devinfo/devinfo_v2beta_20260206.apk'

export default function CollectionToolModal({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [qrDataUrl, setQrDataUrl] = useState('')

  useEffect(() => {
    QRCode.toDataURL(COLLECTION_TOOL_URL, { width: 200, margin: 2, color: { dark: '#000000', light: '#ffffff' } })
      .then(url => setQrDataUrl(url))
      .catch(err => console.error('Failed to generate QR code:', err))
  }, [])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-xl shadow-2xl w-[380px] max-w-[calc(100vw-32px)] modal-content" onClick={e => e.stopPropagation()}>
        {/* 标题 */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-100">
          <h3 className="text-sm font-semibold text-gray-700">{t('model:collection.download')}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        {/* 内容 */}
        <div className="flex flex-col items-center py-6 px-5">
          <p className="text-sm text-gray-600 mb-4 font-medium">{t('model:collection.scanToDownload')}</p>

          {qrDataUrl ? (
            <div className="p-4 bg-gray-50 rounded-lg">
              <img src={qrDataUrl} alt={t('model:collection.qrAlt')} className="w-[200px] h-[200px]" />
            </div>
          ) : (
            <div className="w-[200px] h-[200px] flex items-center justify-center text-gray-400 text-xs">{t('model:collection.generating')}</div>
          )}

          <div className="mt-4 text-xs text-gray-400 space-y-1 text-center">
            <p>{t('model:collection.openInBrowser')}</p>
          </div>
          <div className="mt-3 text-xs text-red-500 space-y-1 text-center">
            <p>{t('model:collection.androidOnly')}</p>
            <p>{t('model:collection.grantPermissions')}</p>
          </div>
        </div>

        {/* 底部按钮 */}
        <div className="flex justify-end px-5 py-3 border-t border-gray-100">
          <button
            onClick={onClose}
            className="px-4 py-1.5 text-xs text-white bg-blue-500 rounded-lg hover:bg-blue-600"
          >
            {t('button.close')}
          </button>
        </div>
      </div>
    </div>
  )
}
