import { useState, useRef} from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo } from '../../../../types/index'
import { getErrorMessage } from '../../../../utils/error'
import { showToast } from '../../../../components/Toast'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../utils/deviceId'
import { useModalEnter } from '../../../../hooks/useModalAnimation'

interface Props {
  android: AndroidInfo
  onClose: () => void
}

const labelCls = 'text-xs text-gray-500 mb-1 block'

// 上传 Google 证书（选择文件后 base64 编码上传）
function GoogleCertModal({ android, onClose }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [fileName, setFileName] = useState('')
  const [base64, setBase64] = useState('')
  const [loading, setLoading] = useState(false)

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setFileName(file.name)
    const reader = new FileReader()
    reader.onload = () => {
      // reader.result 形如 "data:application/...;base64,XXXX"，取逗号后部分
      const result = String(reader.result || '')
      const idx = result.indexOf(',')
      setBase64(idx >= 0 ? result.slice(idx + 1) : result)
    }
    reader.readAsDataURL(file)
  }

  const handleUpload = async () => {
    if (!base64) { showToast(t('host:toast.googleCertSelectFile'), 'error'); return }
    setLoading(true)
    try {
      await SdkApiService.UploadGoogleCert(getDeviceId(android, mode), android.name, base64)
      showToast(t('host:toast.googleCertUploadSuccess'), 'success')
      onClose()
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally {
      setLoading(false)
    }
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[420px] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-base font-semibold text-gray-800">{t('host:googleCert.title')}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="px-6 py-4 space-y-3">
          <div>
            <label className={labelCls}>{t('host:googleCert.certFile')}</label>
            <input type="file" onChange={handleFile} className="block w-full text-xs text-gray-500 file:mr-3 file:py-1.5 file:px-3 file:rounded-lg file:border-0 file:bg-blue-50 file:text-blue-600 hover:file:bg-blue-100" />
          </div>
          {fileName && <p className="text-xs text-gray-400">{t('host:googleCert.selected', { fileName })}</p>}
        </div>
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={handleUpload} disabled={loading || !base64}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">{loading ? t('host:googleCert.uploading') : t('host:googleCert.upload')}</button>
        </div>
      </div>
    </div>
  )
}

export default GoogleCertModal
