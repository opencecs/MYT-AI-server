import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo } from '../../../../types/index'
import { getErrorMessage } from '../../../../utils/error'
import { showToast } from '../../../../components/Toast'
import { cast } from '../../../../utils/cast'
import { formatSize } from '../../../../utils/format'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { useAndroidNotes } from '../../../../hooks/useNoteSync'
import { getDeviceId } from '../../../../utils/deviceId'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import { File, Inbox, X, HardDrive, Cloud } from 'lucide-react'

interface Props {
  android: AndroidInfo
  onClose: () => void
  onSwitched?: () => void
}

interface DeviceBackup { name: string; size: string; mtimestamp: number }
interface LocalBackup { name: string; size: number }

// 切换备份：选设备/本地备份，导入到该坑位并开机
function SwitchBackupModal({ android, onClose, onSwitched }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const { mode } = useDeployMode()
  const deviceId = getDeviceId(android, mode)
  const [tabMode, setTabMode] = useState<'device' | 'local'>('device')
  const [deviceBackups, setDeviceBackups] = useState<DeviceBackup[]>([])
  const [localBackups, setLocalBackups] = useState<LocalBackup[]>([])
  const [selected, setSelected] = useState('')
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(false)
  const [keepMac, setKeepMac] = useState(false)
  const [newName, setNewName] = useState('')
  const { addTask, updateTask } = useTaskQueue()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  useEffect(() => {
    const loadDevice = async () => {
      try { setDeviceBackups(cast<DeviceBackup[]>(await SdkApiService.ListBackups(deviceId) || [])) }
      catch {}
    }
    const loadLocal = async () => {
      try { setLocalBackups(cast<LocalBackup[]>(await SdkApiService.ListLocalBackupMachines() || [])) }
      catch {}
    }
    loadDevice()
    loadLocal()
  }, [deviceId])

  const handleSwitch = async () => {
    if (!selected) return
    setLoading(true)
    const taskId = await addTask({ type: 'importAndroid', deviceIps: [deviceId], total: 1, machineName: selected, slotIndex: android.indexNum })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    try {
      if (tabMode === 'device') {
        await SdkApiService.ImportAndroid(deviceId, selected, android.indexNum, newName.trim(), true, keepMac, taskId)
      } else {
        await SdkApiService.ImportAndroidFromFile(deviceId, selected, android.indexNum, newName.trim(), keepMac, taskId)
      }
      updateTask(taskId, { status: 'completed', progress: 100, completed: 1 })
      showToast(t('host:toast.switchBackupSuccess'), 'success')
      onSwitched?.()
      onClose()
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
      showToast(getErrorMessage(err), 'error')
    } finally {
      setLoading(false)
    }
  }

  const backups = tabMode === 'device' ? deviceBackups : localBackups
  const kw = search.trim().toLowerCase()
  const filtered = kw ? backups.filter(b => b.name.toLowerCase().includes(kw)) : backups
  const fmtSize = (b: DeviceBackup | LocalBackup) => {
    const s = typeof b.size === 'string' ? parseInt(b.size) || 0 : b.size
    return formatSize(s)
  }

  return (
    <div className="fixed inset-0 bg-black/30 backdrop-blur-sm flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-2xl shadow-xl w-[420px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
          <div>
            <h2 className="text-base font-semibold text-gray-800">{t('host:switchBackup.title')}</h2>
            <p className="text-xs text-gray-400 mt-0.5">{notes[android.id] || android.name}</p>
          </div>
          <button onClick={onClose} className="text-gray-300 hover:text-gray-500 transition-colors"><X size={18} /></button>
        </div>
        {/* 模式切换 */}
        <div className="px-4 pt-3 pb-2 flex items-center gap-2">
          <div className="flex items-center bg-gray-100 rounded-md p-0.5">
            <button onClick={() => { setTabMode('device'); setSelected('') }}
              className={`flex items-center gap-1 text-xs px-2.5 py-1 rounded ${tabMode === 'device' ? 'bg-white text-blue-600 shadow-sm' : 'text-gray-500'}`}>
              <HardDrive size={12} />{t('backup:android.deviceMode')}
            </button>
            <button onClick={() => { setTabMode('local'); setSelected('') }}
              className={`flex items-center gap-1 text-xs px-2.5 py-1 rounded ${tabMode === 'local' ? 'bg-white text-blue-600 shadow-sm' : 'text-gray-500'}`}>
              <Cloud size={12} />{t('backup:android.localMode')}
            </button>
          </div>
        </div>
        {/* 搜索框 */}
        <div className="px-4 pb-2">
          <div className="relative">
            <svg className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input type="text" value={search} onChange={e => setSearch(e.target.value)} placeholder={t('backup:android.searchPlaceholder')}
              className="w-full text-xs pl-7 pr-3 py-1.5 bg-gray-50 border border-gray-200 rounded-lg focus:outline-none focus:border-blue-400 placeholder:text-gray-300" />
          </div>
        </div>
        {/* 备份列表 */}
        <div className="flex-1 overflow-y-auto px-4 pb-2 space-y-1.5 min-h-[120px]">
          {filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-8 text-gray-300 gap-2"><Inbox size={28} /><span className="text-xs">{t('backup:android.empty')}</span></div>
          ) : filtered.map(b => (
            <button key={b.name} onClick={() => setSelected(b.name)}
              className={`w-full flex items-center gap-2.5 px-3 py-2.5 rounded-xl text-left transition-all border-2 ${selected === b.name ? 'border-blue-400 bg-blue-50' : 'border-transparent hover:bg-gray-50'}`}>
              <File size={16} className={selected === b.name ? 'text-blue-500' : 'text-gray-300'} />
              <span className={`text-sm truncate flex-1 ${selected === b.name ? 'text-blue-700' : 'text-gray-700'}`}>{b.name}</span>
              <span className="text-xs text-gray-400 shrink-0">{fmtSize(b)}</span>
            </button>
          ))}
        </div>
        {/* 参数 */}
        {selected && (
          <div className="px-4 py-3 border-t border-gray-100 bg-gray-50/50 space-y-2.5">
            {tabMode === 'device' && (
              <div>
                <label className="block text-xs text-gray-500 mb-1">{t('backup:android.newName')}</label>
                <input type="text" value={newName} onChange={e => setNewName(e.target.value)} placeholder={t('backup:android.newNamePlaceholder')}
                  className="w-full text-xs px-2.5 py-1.5 bg-white border border-gray-200 rounded-lg focus:outline-none focus:border-blue-400" />
              </div>
            )}
            <label className="flex items-center gap-1.5 text-xs text-gray-600 cursor-pointer">
              <input type="checkbox" checked={keepMac} onChange={e => setKeepMac(e.target.checked)} className="w-3.5 h-3.5 rounded accent-blue-500" />
              {t('backup:android.keepMac')}
            </label>
          </div>
        )}
        {/* footer */}
        {filtered.length > 0 && (
          <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-50">
            <button onClick={onClose} className="px-4 py-2 text-sm text-gray-600 bg-gray-50 rounded-full hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
            <button onClick={handleSwitch} disabled={!selected || loading}
              className="px-4 py-2 text-sm text-white bg-blue-500 rounded-full hover:bg-blue-600 transition-colors disabled:opacity-40 disabled:cursor-not-allowed">
              {loading ? t('host:switchBackup.switching') : t('host:switchBackup.confirmSwitch')}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

export default SwitchBackupModal