import { useState, useMemo,  useRef} from 'react'
import { useTranslation } from 'react-i18next'
import * as ImageService from '../../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import { MirrorItem, SavedMirror } from '../../types/index'
import { useOnlineDevices } from '../../hooks/useOnlineDevices'
import { useDeviceNotes } from '../../hooks/useNoteSync'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import { showToast } from '../../components/Toast'
import { useModalEnter } from '../../hooks/useModalAnimation'

interface Props {
  mirror: MirrorItem | SavedMirror
  onClose: () => void
}

function formatDisk(used?: number, total?: number): string {
  if (!total || total <= 0) return '-'
  const toGB = (b: number) => (b / 1024 / 1024 / 1024).toFixed(1)
  return used != null && used >= 0 ? `${toGB(used)} / ${toGB(total)} GB` : `${toGB(total)} GB`
}

function diskUsagePercent(used?: number, total?: number): number {
  if (!used || !total || total <= 0) return 0
  return Math.round((used / total) * 100)
}

function diskBarColor(pct: number): string {
  if (pct > 90) return 'bg-red-400'
  if (pct > 70) return 'bg-amber-400'
  return 'bg-green-400'
}

function PushMirrorModal({ mirror, onClose }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const { t } = useTranslation()
  const { devices } = useOnlineDevices()
  const { addTask } = useTaskQueue()
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [search, setSearch] = useState('')
  const { notes: deviceNotes } = useDeviceNotes()

  const filteredDevices = useMemo(() => {
    if (!search.trim()) return devices
    const q = search.toLowerCase()
    return devices.filter(d => {
      const note = deviceNotes[d.device_id] || ''
      return d.ip.toLowerCase().includes(q) ||
        d.device_name.toLowerCase().includes(q) ||
        d.device_type.toLowerCase().includes(q) ||
        note.toLowerCase().includes(q)
    })
  }, [devices, deviceNotes, search])

  const allFilteredSelected = filteredDevices.length > 0 && filteredDevices.every(d => selected.has(d.ip))

  const toggleDevice = (ip: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(ip)) next.delete(ip)
      else next.add(ip)
      return next
    })
  }

  const toggleAll = () => {
    if (allFilteredSelected) {
      setSelected(prev => {
        const next = new Set(prev)
        filteredDevices.forEach(d => next.delete(d.ip))
        return next
      })
    } else {
      setSelected(prev => {
        const next = new Set(prev)
        filteredDevices.forEach(d => next.add(d.ip))
        return next
      })
    }
  }

  const handlePush = async () => {
    if (selected.size === 0) return
    const deviceIPs = devices.filter(d => selected.has(d.ip)).map(d => d.ip)
    addTask({ type: 'uploadImage', id: mirror.id, targets: deviceIPs.map(ip => ({ deviceIP: ip })), deviceIps: deviceIPs, total: deviceIPs.length, imageName: mirror.name, startTime: Date.now() })
    onClose()
    showToast(t('mirror:toast.pushAdded', { count: deviceIPs.length }), 'success')
    try {
      await ImageService.PushToDevices(mirror.id, deviceIPs)
    } catch {
      // error handled in task queue
    }
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg w-[620px] max-h-[80vh] flex flex-col shadow-xl modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 pt-5 pb-3 border-b border-gray-100">
          <div className="flex items-center justify-between">
            <h2 className="text-base font-semibold text-gray-800">{t('mirror:push.title')}</h2>
            <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
          </div>
          <p className="text-sm text-gray-500 mt-1">{mirror.name}</p>
        </div>

        <div className="flex-1 flex flex-col min-h-0">
          <div className="px-5 py-3 flex items-center gap-2">
            <div className="flex-1 relative">
              <input
                type="text"
                placeholder={t('mirror:push.searchPlaceholder')}
                value={search}
                onChange={e => setSearch(e.target.value)}
                className="w-full pl-8 pr-3 py-1.5 text-sm border border-gray-200 rounded-lg bg-gray-50 focus:outline-none focus:border-blue-400"
              />
              <svg className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
            </div>
            <button onClick={toggleAll} className="text-xs text-blue-500 hover:text-blue-600 shrink-0">
              {allFilteredSelected ? t('button.deselectAll') : t('button.selectAll')}
            </button>
            <span className="text-xs text-gray-400 shrink-0">{t('mirror:push.selected', { count: selected.size })}</span>
          </div>

          <div className="px-5 grid grid-cols-[auto_1fr_80px_120px_120px] gap-2 text-xs text-gray-400 border-b border-gray-100 pb-1.5">
            <span className="w-5" />
            <span>{t('mirror:push.ipAddress')}</span>
            <span>{t('table.type')}</span>
            <span>{t('table.note')}</span>
            <span>{t('mirror:push.diskSpace')}</span>
          </div>

          <div className="flex-1 overflow-auto px-5 py-1">
            {filteredDevices.length === 0 ? (
              <div className="text-center text-gray-300 py-8 text-sm">
                {devices.length === 0 ? t('empty.noOnlineDevices') : t('empty.noMatch')}
              </div>
            ) : (
              filteredDevices.map(d => {
                const pct = diskUsagePercent(d.disk_used, d.disk_total)
                return (
                  <label key={d.ip} className="grid grid-cols-[auto_1fr_80px_120px_120px] gap-2 items-center py-2 hover:bg-gray-50 rounded cursor-pointer border-b border-gray-50 last:border-0">
                    <input type="checkbox" checked={selected.has(d.ip)} onChange={() => toggleDevice(d.ip)} className="rounded" />
                    <span className="text-sm text-gray-800 font-mono">{d.ip}</span>
                    <span className="text-xs text-gray-500 truncate">{d.device_type || '-'}</span>
                    <span className="text-xs text-gray-600 truncate">{deviceNotes[d.device_id] || ''}</span>
                    <div className="flex items-center gap-1.5">
                      <div className="flex-1 h-1.5 bg-gray-200 rounded overflow-hidden min-w-0">
                        <div className={`h-full rounded ${diskBarColor(pct)}`} style={{ width: `${pct}%` }} />
                      </div>
                      <span className="text-[10px] text-gray-500 shrink-0 whitespace-nowrap">{formatDisk(d.disk_used, d.disk_total)}</span>
                    </div>
                  </label>
                )
              })
            )}
          </div>
        </div>

        <div className="px-5 py-3 border-t border-gray-100 flex justify-end gap-2">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 rounded-lg hover:bg-gray-200 text-gray-700">{t('button.cancel')}</button>
          <button onClick={handlePush} disabled={selected.size === 0}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">
            {t('mirror:push.pushToDevices', { count: selected.size })}
          </button>
        </div>
      </div>
    </div>
  )
}

export default PushMirrorModal
