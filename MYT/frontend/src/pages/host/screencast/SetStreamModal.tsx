import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Browser } from '@wailsio/runtime'
import QRCode from 'qrcode'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as FileService from '../../../../bindings/github.com/moyunteng/myt/fileservice'
import { AndroidInfo } from '../../../types/index'
import { useModalEnter } from '../../../hooks/useModalAnimation'
import Select from '../../../components/Select'

type StreamType = '' | 'image' | 'video' | 'app'

interface Props {
  deviceIP: string
  android: AndroidInfo
  onClose: () => void
}

/** APP 下载链接 */
const APP_DOWNLOAD_URL = 'http://d.moyunteng.com/download/mytcloudphone_v1.3_20260302.apk'

export default function SetStreamModal({ deviceIP, android, onClose }: Props) {
  const { t } = useTranslation()
  const [streamType, setStreamType] = useState<StreamType>('')
  const [filePath, setFilePath] = useState('')
  const [loading, setLoading] = useState(false)
  const [qrCodeUrl, setQrCodeUrl] = useState('')
  const [qrLoading, setQrLoading] = useState(false)

  // 打开弹窗时先停止摄像头
  useEffect(() => {
    SdkApiService.StopCamera(deviceIP, android.name).catch(() => {})
  }, [deviceIP, android.name])

  // 切换类型时生成二维码
  useEffect(() => {
    setFilePath('')
    if (streamType === 'app') {
      generateAppQRCode()
    } else {
      setQrCodeUrl('')
    }
  }, [streamType]) // eslint-disable-line react-hooks/exhaustive-deps

  const generateAppQRCode = async () => {
    setQrLoading(true)
    try {
      const info = await SdkApiService.GetStreamConnectionInfo(deviceIP, android.name)
      if (!info) return
      // 拼接参数字符串
      const rawStr = `n=${info.name}&ip=${info.ip}&t=${info.portVideo}&u=${info.portControl}&i=${info.portMYTOS}&ct=${info.portCamTCP}&cu=${info.portCamUDP}`
      // Base64 编码
      const base64Str = btoa(unescape(encodeURIComponent(rawStr)))
      // 生成二维码
      const qrDataUrl = await QRCode.toDataURL(base64Str, { width: 200, margin: 2 })
      setQrCodeUrl(qrDataUrl)
    } catch (err) {
      console.error('QR code generation failed:', err)
    } finally {
      setQrLoading(false)
    }
  }

  const handleSelectFile = async () => {
    try {
      if (streamType === 'image') {
        const result = await FileService.SelectImageFile('')
        if (result) setFilePath(result)
      } else if (streamType === 'video') {
        const result = await FileService.SelectVideoFile('')
        if (result) setFilePath(result)
      }
    } catch {
      // 用户取消选择，忽略
    }
  }

  const handleConfirm = async () => {
    if (!streamType) return

    // APP扫码模式：直接关闭弹窗
    if (streamType === 'app') {
      onClose()
      return
    }

    // 图片/视频模式
    if (!filePath) return

    setLoading(true)
    try {
      await SdkApiService.UploadAndSetStream(deviceIP, android.name, filePath, streamType)
      onClose()
    } catch (err: any) {
      console.error('Set stream failed:', err)
      alert(t('host:screencast.setStream.setStreamFailed', { error: err?.message || String(err) }))
    } finally {
      setLoading(false)
    }
  }

  const handleClose = () => {
    setStreamType('')
    setFilePath('')
    onClose()
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 modal-overlay" ref={rootRef} onClick={handleClose}>
      <div className="bg-white rounded-xl shadow-2xl w-[480px] max-h-[90vh] overflow-y-auto modal-content" onClick={e => e.stopPropagation()}>
        {/* 标题 */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-100">
          <h3 className="text-sm font-semibold text-gray-700">{t('host:screencast.setStream.title')}</h3>
          <button onClick={handleClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        {/* 内容 */}
        <div className="px-5 py-4 space-y-4">
          {/* 推流类型选择 */}
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">{t('host:screencast.setStream.streamType')}</label>
            <Select
              value={streamType}
              onChange={v => setStreamType(v as StreamType)}
              options={[
                { value: '', label: t('host:screencast.setStream.selectType') },
                { value: 'image', label: t('host:screencast.setStream.image') },
                { value: 'video', label: t('host:screencast.setStream.video') },
                { value: 'app', label: t('host:screencast.setStream.app') },
              ]}
            />
          </div>

          {/* 图片/视频：文件选择 */}
          {(streamType === 'image' || streamType === 'video') && (
            <div>
              <label className="block text-xs font-medium text-gray-500 mb-1.5">{t('host:screencast.setStream.filePath')}</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={filePath}
                  readOnly
                  placeholder={t('host:screencast.setStream.selectFile')}
                  className="flex-1 border border-gray-200 rounded-lg px-3 py-2 text-sm bg-gray-50 focus:outline-none"
                />
                <button
                  onClick={handleSelectFile}
                  className="px-3 py-2 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 whitespace-nowrap"
                >
                  {t('host:screencast.setStream.selectFileBtn')}
                </button>
              </div>
              <p className="mt-2 text-xs text-red-500">{streamType === 'image' ? t('host:screencast.setStream.imageAutoPush') : t('host:screencast.setStream.videoAutoPush')}</p>
            </div>
          )}

          {/* APP 扫码 */}
          {streamType === 'app' && (
            <div className="flex flex-col items-center py-2">
              <h4 className="text-sm font-medium text-gray-600 mb-3">{t('host:screencast.setStream.qrConnect')}</h4>

              {qrLoading ? (
                <div className="w-[200px] h-[200px] flex items-center justify-center text-gray-400 text-xs">{t('host:screencast.setStream.qrGenerating')}</div>
              ) : qrCodeUrl ? (
                <img src={qrCodeUrl} alt={t('host:screencast.setStream.qrAlt')} className="w-[200px] h-[200px]" />
              ) : (
                <div className="w-[200px] h-[200px] flex items-center justify-center text-gray-400 text-xs">{t('host:screencast.setStream.qrFailed')}</div>
              )}

              {/* APP 下载 */}
              <div className="mt-3">
                <span
                  className="text-xs text-blue-500 cursor-pointer hover:underline"
                  onClick={() => Browser.OpenURL(APP_DOWNLOAD_URL)}
                >{t('host:screencast.setStream.appDownload')}</span>
              </div>

              {/* 注意事项 */}
              <div className="mt-4 text-xs text-red-500 space-y-1">
                <p>{t('host:screencast.setStream.networkNotice')}</p>
                <p>{t('host:screencast.setStream.usageMethod')}</p>
                <p>{t('host:screencast.setStream.usageSteps')}</p>
              </div>
            </div>
          )}
        </div>

        {/* 底部按钮 */}
        <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-100">
          <button
            onClick={handleClose}
            className="px-4 py-1.5 text-xs text-gray-500 border border-gray-200 rounded-lg hover:bg-gray-50"
          >
            {t('host:screencast.setStream.cancel')}
          </button>
          {streamType !== 'app' && (
            <button
              onClick={handleConfirm}
              disabled={!streamType || (streamType !== 'app' && !filePath) || loading}
              className="px-4 py-1.5 text-xs text-white bg-blue-500 rounded-lg hover:bg-blue-600 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {loading ? t('host:screencast.setStream.processing') : t('host:screencast.setStream.confirm')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
