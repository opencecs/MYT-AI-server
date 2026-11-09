import { useState, useEffect } from 'react'
import * as MaintenanceService from '../../../../bindings/github.com/moyunteng/myt/maintenanceservicewrapper'
import { showToast } from '../../../components/Toast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { useTranslation } from 'react-i18next'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'

interface CacheItem { key: string; label: string; sizeMB: number }

export function CacheContent({ deviceIP, androidName, onClose }: { deviceIP: string; androidName: string; onClose?: () => void }) {
  const { t } = useTranslation()
  const { addTask, openPanel } = useTaskQueue()
  const [items, setItems] = useState<CacheItem[]>([])
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [loading, setLoading] = useState(true)
  const [confirmAction, setConfirmAction] = useState<{ title: string; message: string; onConfirm: () => void | Promise<void> } | null>(null)

  const fetch = async () => {
    setLoading(true)
    try {
      const r = await MaintenanceService.GetAndroidCacheUsage(deviceIP, androidName) as any
      if (r.success && Array.isArray(r.items)) {
        setItems(r.items)
        setChecked(prev => {
          if (Object.keys(prev).length > 0) return prev
          const next: Record<string, boolean> = {}
          r.items.forEach((i: any) => { next[i.key] = true })
          return next
        })
      }
    } catch {} finally { setLoading(false) }
  }

  useEffect(() => { fetch() }, [deviceIP, androidName])

  const totalMB = items.reduce((s, i) => checked[i.key] ? s + i.sizeMB : s, 0)
  const allChecked = items.length > 0 && items.every(i => checked[i.key])

  const handleClean = () => {
    const categories = items.filter(i => checked[i.key] && i.sizeMB > 0).map(i => i.key)
    if (categories.length === 0) { showToast(t('maintenance:cache.selectItems')); return }
    setConfirmAction({ title: t('maintenance:cache.cleanTitle'), message: t('maintenance:cache.cleanMsg', { name: androidName, count: categories.length }), onConfirm: async () => {
      setConfirmAction(null)
      const taskId = await addTask({
        type: 'cleanCache',
        targets: [{ deviceIp: deviceIP, name: androidName, categories }],
        deviceIps: [deviceIP],
        total: 1,
      })
      if (taskId) {
        showToast(t('maintenance:cache.cleanTriggered'), 'success')
        openPanel(taskId)
        onClose?.()
      }
    }})
  }

  return (
    <div className="space-y-3">
      <div className="text-xs text-gray-500">{t('maintenance:cache.android', { name: androidName })}</div>
      {loading ? <div className="text-xs text-gray-400 text-center py-4">{t('status.loading')}</div> : (
        <>
          <div className="flex items-center gap-2">
            <input type="checkbox" checked={allChecked} onChange={() => {
              const next: Record<string, boolean> = {}
              items.forEach(i => { next[i.key] = !allChecked })
              setChecked(next)
            }} className="w-3 h-3 border-gray-300 text-blue-500 focus:ring-blue-400" />
            <span className="text-[10px] text-gray-500">{t('button.selectAll')}</span>
          </div>
          <div className="divide-y divide-gray-100 border border-gray-200 rounded-lg overflow-hidden">
          {items.map(item => (
            <div key={item.key} className="px-3 py-2.5 flex items-center justify-between text-xs">
              <div className="flex items-center gap-2">
                <input type="checkbox" checked={!!checked[item.key]} onChange={() => setChecked(p => ({ ...p, [item.key]: !p[item.key] }))}
                  className="w-3 h-3 border-gray-300 text-blue-500 focus:ring-blue-400" />
                <span className="text-gray-600">{item.label}</span>
              </div>
              <span className="text-gray-400">{item.sizeMB > 0 ? `${item.sizeMB.toFixed(1)}MB` : '0MB'}</span>
            </div>
          ))}
        </div>
        </>
      )}
      <div className="flex items-center justify-between pt-2">
        <span className="text-[10px] text-gray-400">{t('maintenance:cache.selectedTotal', { size: totalMB > 0 ? `${totalMB.toFixed(1)}MB` : '0' })}</span>
        <div className="flex gap-2">
          <button onClick={fetch} disabled={loading} className="px-3 py-1.5 text-xs bg-white border border-gray-200 rounded hover:bg-blue-50 disabled:opacity-50 cursor-pointer">{t('button.refresh')}</button>
          <button onClick={handleClean} disabled={totalMB === 0}
            className="px-3 py-1.5 text-xs bg-orange-500 text-white rounded hover:bg-orange-600 disabled:opacity-50 cursor-pointer">
            {t('maintenance:cache.cleanSelected')}
          </button>
        </div>
      </div>
      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('maintenance:cache.confirmClean')} onConfirm={confirmAction.onConfirm} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}
