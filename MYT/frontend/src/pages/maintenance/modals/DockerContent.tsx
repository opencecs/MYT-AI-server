import { useState, useEffect } from 'react'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { showToast } from '../../../components/Toast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { useTranslation } from 'react-i18next'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'

export function DockerContent({ deviceIP, onClose }: { deviceIP: string; onClose?: () => void }) {
  const { t } = useTranslation()
  const { addTask, openPanel } = useTaskQueue()
  const [images, setImages] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [confirmAction, setConfirmAction] = useState<{ title: string; message: string; onConfirm: () => void | Promise<void> } | null>(null)

  const fetch = async () => {
    setLoading(true)
    try { const l = await SdkApiService.ListImages(deviceIP); setImages(Array.isArray(l) ? l : []) } catch { setImages([]) } finally { setLoading(false) }
  }

  useEffect(() => { fetch() }, [deviceIP])

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between text-xs">
        <span className="text-gray-500">{t('maintenance:docker.imageCount', { count: images.length })}</span>
        <div className="flex gap-2">
          <button onClick={fetch} disabled={loading} className="px-2 py-1 text-xs bg-white border border-gray-200 rounded hover:bg-blue-50 disabled:opacity-50 cursor-pointer">{t('button.refresh')}</button>
          <button onClick={() => setConfirmAction({ title: t('maintenance:docker.cleanUnusedTitle'), message: t('maintenance:docker.cleanUnusedMsg', { ip: deviceIP }), onConfirm: async () => {
            setConfirmAction(null)
            const taskId = await addTask({
              type: 'cleanImage',
              targets: [{ deviceIp: deviceIP }],
              deviceIps: [deviceIP],
              total: 1,
            })
            if (taskId) {
              showToast(t('maintenance:docker.cleanTriggered'), 'success')
              openPanel(taskId)
              onClose?.()
            }
          }})}
            disabled={images.length === 0} className="px-2 py-1 text-xs bg-orange-50 text-orange-600 border border-orange-200 rounded hover:bg-orange-100 disabled:opacity-50 cursor-pointer">
            {t('maintenance:docker.cleanUnused')}
          </button>
        </div>
      </div>
      {loading ? <div className="text-xs text-gray-400 text-center py-4">{t('status.loading')}</div> : images.length === 0 ? <div className="text-xs text-gray-400 text-center py-4">{t('maintenance:docker.noImages')}</div> : (
        <div className="divide-y divide-gray-100 max-h-[300px] overflow-y-auto scrollbar-thin">
          {images.map((img, i) => (
            <div key={i} className="px-2 py-1.5 flex items-center gap-3 text-xs">
              <span className="text-blue-500 font-mono shrink-0">{img.id?.slice(7, 19) || '-'}</span>
              <span className="truncate flex-1 text-gray-600">{img.labels?.m || img.imageUrl}</span>
              {img.labels?.v && <span className="text-gray-400">v{img.labels.v}</span>}
              <span className="text-gray-400 shrink-0 w-16 text-right">{img.size || '-'}</span>
            </div>
          ))}
        </div>
      )}
      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('maintenance:docker.confirmClean')} onConfirm={confirmAction.onConfirm} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}
