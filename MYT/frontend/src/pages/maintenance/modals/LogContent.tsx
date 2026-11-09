import { useState, useEffect } from 'react'
import * as MaintenanceService from '../../../../bindings/github.com/moyunteng/myt/maintenanceservicewrapper'
import { showToast } from '../../../components/Toast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { useTranslation } from 'react-i18next'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'

interface LogItem {
  key: string; label: string; sizeMB: number; risk: string; riskTip?: string; group: string
}

export function LogContent({ deviceIP, androidName, onClose, mode }: { deviceIP: string; androidName: string; onClose?: () => void; mode: 'device' | 'android' }) {
  const { t } = useTranslation()
  const { addTask, openPanel } = useTaskQueue()
  const [items, setItems] = useState<LogItem[]>([])
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [confirmAction, setConfirmAction] = useState<{ title: string; message: string; onConfirm: () => void | Promise<void> } | null>(null)

  const fetch = async () => {
    setLoading(true); setError('')
    try {
      const r = await MaintenanceService.GetLogUsage(deviceIP, androidName) as any
      if (r && r.success && Array.isArray(r.items)) {
        setItems(r.items)
        const defaultChecked: Record<string, boolean> = {}
        r.items.forEach((i: any) => { defaultChecked[i.key] = i.risk === 'safe' })
        setChecked(defaultChecked)
      } else {
        setError(r?.message || t('maintenance:log.fetchFailed'))
        setItems([])
      }
    } catch (e) { setError(String(e)); setItems([]) } finally { setLoading(false) }
  }

  useEffect(() => { fetch() }, [deviceIP, androidName])

  const handleClean = () => {
    const categories = items.filter(i => checked[i.key] && i.sizeMB > 0).map(i => i.key)
    if (categories.length === 0) { showToast(t('maintenance:log.selectItems')); return }
    const labels = items.filter(i => checked[i.key] && i.sizeMB > 0).map(i => i.label)
    const riskItems = items.filter(i => checked[i.key] && i.risk === 'medium')
    let msg = t('maintenance:log.cleanConfirm', { labels: labels.join(', ') })
    if (riskItems.length > 0) {
      msg = t('maintenance:log.riskWarning', { items: riskItems.map(i => `• ${i.label}: ${i.riskTip}`).join('\n') })
    }
    setConfirmAction({ title: t('maintenance:log.cleanTitle'), message: msg, onConfirm: async () => {
      setConfirmAction(null)
      // 提交到任务队列（单设备任务），由后端执行，任务面板展示进度
      const taskId = await addTask({
        type: 'cleanLog',
        targets: [{ deviceIp: deviceIP, name: androidName, categories }],
        deviceIps: [deviceIP],
        total: 1,
      })
      if (taskId) {
        showToast(t('maintenance:log.cleanTriggered'), 'success')
        openPanel(taskId)
        onClose?.()
      }
    }})
  }

  const deviceItems = items.filter(i => i.group === 'device')
  const androidItems = items.filter(i => i.group === 'android')
  // 按 mode 过滤：device 只显示设备日志，android 只显示云机日志
  const showDevice = mode === 'device'
  const visibleItems = showDevice ? deviceItems : androidItems
  const totalMB = visibleItems.reduce((s, i) => checked[i.key] ? s + i.sizeMB : s, 0)
  const hasMediumRisk = visibleItems.some(i => checked[i.key] && i.risk === 'medium')

  return (
    <div className="space-y-3">
      {loading ? <div className="text-xs text-gray-400 text-center py-4">{t('status.loading')}</div> : error ? (
        <div className="text-xs text-red-500 text-center py-4">{error}</div>
      ) : items.length === 0 ? (
        <div className="text-xs text-gray-400 text-center py-4">{t('maintenance:log.noLogs')}</div>
      ) : (
        <div className="border border-gray-200 rounded-lg overflow-hidden">
          {showDevice
            ? <LogGroup groupKey="device" items={deviceItems} checked={checked} onCheck={setChecked} label={t('maintenance:log.deviceLogs')} />
            : <LogGroup groupKey="android" items={androidItems} checked={checked} onCheck={setChecked} label={t('maintenance:log.androidLogs')} showEmpty />}
        </div>
      )}
      {hasMediumRisk && <div className="text-[10px] text-amber-600">{t('maintenance:log.riskTip')}</div>}
      <div className="flex items-center justify-between pt-2">
        <span className="text-[10px] text-gray-400">{t('maintenance:log.selectedTotal', { size: totalMB > 0 ? `${totalMB.toFixed(1)}MB` : '0' })}</span>
        <div className="flex gap-2">
          <button onClick={fetch} disabled={loading} className="px-3 py-1.5 text-xs bg-white border border-gray-200 rounded hover:bg-blue-50 disabled:opacity-50 cursor-pointer">{t('button.refresh')}</button>
          <button onClick={handleClean} disabled={totalMB === 0}
            className="px-3 py-1.5 text-xs bg-orange-500 text-white rounded hover:bg-orange-600 disabled:opacity-50 cursor-pointer">
            {t('maintenance:log.cleanSelected')}
          </button>
        </div>
      </div>
      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('maintenance:log.confirmClean')} onConfirm={confirmAction.onConfirm} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

function LogGroup({ groupKey, items, checked, onCheck, label, showEmpty }: {
  groupKey: string; items: LogItem[]; checked: Record<string, boolean>;
  onCheck: (u: (prev: Record<string, boolean>) => Record<string, boolean>) => void; label: string; showEmpty?: boolean
}) {
  const { t } = useTranslation()
  if (items.length === 0 && showEmpty) {
    return (
      <div>
        <div className="flex items-center gap-2 px-3 py-1.5 bg-gray-50 border-b border-gray-100">
          <span className="text-[11px] font-medium text-gray-500">{label}</span>
        </div>
        <div className="px-3 py-2.5 text-xs text-gray-400 border-b border-gray-50">{t('maintenance:log.selectAndroid')}</div>
      </div>
    )
  }
  if (items.length === 0) return null
  const allChecked = items.every(i => checked[i.key])
  const totalMB = items.reduce((s, i) => checked[i.key] ? s + i.sizeMB : s, 0)
  return (
    <div>
      <div className="flex items-center gap-2 px-3 py-1.5 bg-gray-50 border-b border-gray-100">
        <input type="checkbox" checked={allChecked} onChange={() => {
          onCheck(prev => {
            const next = { ...prev }
            items.forEach(i => { next[i.key] = !allChecked })
            return next
          })
        }} className="w-3 h-3 border-gray-300 text-blue-500 focus:ring-blue-400" />
        <span className="text-[11px] font-medium text-gray-500">{label}</span>
        <span className="text-[10px] text-gray-400">{totalMB.toFixed(1)}MB</span>
      </div>
      {items.map(item => (
        <div key={item.key} className={`px-3 py-2 flex items-center justify-between text-xs border-b border-gray-50 ${item.risk === 'medium' ? 'bg-amber-50/50' : ''}`}>
          <div className="flex items-center gap-2">
            <input type="checkbox" checked={!!checked[item.key]} onChange={() => onCheck(p => ({ ...p, [item.key]: !p[item.key] }))}
              className="w-3 h-3 border-gray-300 text-blue-500 focus:ring-blue-400" />
            <span className={item.risk === 'medium' ? 'text-amber-700' : 'text-gray-600'}>{item.label}</span>
            {item.risk === 'medium' && <span className="text-[10px] px-1 py-0.5 rounded bg-amber-100 text-amber-600">⚠</span>}
          </div>
          <span className={item.risk === 'medium' ? 'text-amber-500' : 'text-gray-400'}>{item.sizeMB > 0 ? `${item.sizeMB.toFixed(1)}MB` : '0MB'}</span>
        </div>
      ))}
    </div>
  )
}
