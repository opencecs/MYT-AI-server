import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { ImageInfo } from '../../../bindings/github.com/moyunteng/myt/internal/sdkapi/models'
import { useOnlineDevices } from '../../hooks/useOnlineDevices'
import ConfirmDialog from '../../components/ConfirmDialog'

interface Props {
  devices: ReturnType<typeof useOnlineDevices>['devices']
}

function DeviceImageSection({ devices }: Props) {
  const { t } = useTranslation()
  const [selectedIP, setSelectedIP] = useState('')
  const [images, setImages] = useState<ImageInfo[]>([])
  const [expanded, setExpanded] = useState(false)
  const [loading, setLoading] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const selectedDevice = devices.find(d => d.ip === selectedIP)

  const loadImages = async (ip: string) => {
    if (!ip) return
    setLoading(true)
    try {
      const result = await SdkApiService.ListImages(ip)
      setImages(result as ImageInfo[])
    } catch (err) {
      console.error('Failed to list device images:', err)
      setImages([])
    } finally {
      setLoading(false)
    }
  }

  const handleToggle = () => {
    const next = !expanded
    setExpanded(next)
    if (next && selectedIP) {
      loadImages(selectedIP)
    }
  }

  const handleDeviceChange = (ip: string) => {
    setSelectedIP(ip)
    if (expanded) loadImages(ip)
  }

  const handleDelete = async () => {
    if (!selectedIP || !deleteTarget) return
    try {
      await SdkApiService.DeleteImage(selectedIP, deleteTarget)
      setDeleteTarget(null)
      await loadImages(selectedIP)
    } catch (err) {
      console.error('Failed to delete image:', err)
      setDeleteTarget(null)
    }
  }

  return (
    <div className="mb-4">
      <div className="flex items-center gap-2 mb-2">
        <button
          onClick={handleToggle}
          className="px-3 py-1 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700"
        >
          {expanded ? t('mirror:deviceImage.collapse') : t('mirror:deviceImage.expand')}
        </button>
        {expanded && (
          <select
            value={selectedIP}
            onChange={(e) => handleDeviceChange(e.target.value)}
            className="bg-gray-50 text-sm px-2 py-1 rounded border border-gray-200"
          >
            <option value="">{t('mirror:deviceImage.selectDevice')}</option>
            {devices.map(d => (
              <option key={d.device_id} value={d.ip}>{d.ip} - {d.device_type.toUpperCase()}</option>
            ))}
          </select>
        )}
        {expanded && selectedIP && (
          <button
            onClick={() => loadImages(selectedIP)}
            disabled={loading}
            className="px-2 py-1 text-xs bg-blue-500 rounded hover:bg-blue-600 disabled:opacity-50"
          >
            {loading ? t('status.loading') : t('button.refresh')}
          </button>
        )}
      </div>

      {expanded && selectedIP && (
        <div className="bg-white border border-gray-200 shadow-sm rounded p-3">
          {images.length === 0 && !loading && (
            <div className="text-xs text-gray-500">{t('mirror:deviceImage.empty')}</div>
          )}
          {images.map(img => (
            <div key={img.id} className="flex items-center justify-between py-1 border-b border-gray-100 last:border-0">
              <div className="text-sm">
                <span className="text-gray-800">{img.id}</span>
                {img.size && <span className="text-xs text-gray-500 ml-2">{img.size}</span>}
              </div>
              <button
                onClick={() => setDeleteTarget(img.id)}
                className="px-2 py-0.5 text-xs bg-red-500 rounded hover:bg-red-600"
              >
                {t('button.delete')}
              </button>
            </div>
          ))}
        </div>
      )}

      {deleteTarget && selectedDevice && (
        <ConfirmDialog
          title={t('confirm.deleteTitle')}
          message={t('mirror:deviceImage.confirmDelete', { ip: selectedDevice.ip, image: deleteTarget })}
          confirmLabel={t('button.delete')}
          onConfirm={handleDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
    </div>
  )
}

export default DeviceImageSection
