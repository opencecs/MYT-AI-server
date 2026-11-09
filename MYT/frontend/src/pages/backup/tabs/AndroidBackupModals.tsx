import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { getErrorMessage } from '../../../utils/error'
import { cast } from '../../../utils/cast'
import { formatSize } from '../../../utils/format'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'
import { useModalEnter } from '../../../hooks/useModalAnimation'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { File, Inbox, X, HardDrive, Cloud, RefreshCw, Download, Archive } from 'lucide-react'

interface DeviceBackup { name: string; size: string; mtimestamp: number }
interface LocalBackup { name: string; size: number }

// --- 共享 UI 片段 ---

function ModeToggle({ tabMode, onChange }: { tabMode: 'device' | 'local'; onChange: (m: 'device' | 'local') => void }) {
  const { t } = useTranslation()
  return (
    <div className="flex items-center bg-gray-100 rounded-md p-0.5">
      <button onClick={() => onChange('device')}
        className={`flex items-center gap-1 text-xs px-2.5 py-1 rounded ${tabMode === 'device' ? 'bg-white text-blue-600 shadow-sm' : 'text-gray-500'}`}>
        <HardDrive size={12} />{t('backup:android.deviceMode')}
      </button>
      <button onClick={() => onChange('local')}
        className={`flex items-center gap-1 text-xs px-2.5 py-1 rounded ${tabMode === 'local' ? 'bg-white text-blue-600 shadow-sm' : 'text-gray-500'}`}>
        <Cloud size={12} />{t('backup:android.localMode')}
      </button>
    </div>
  )
}

function SearchBox({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { t } = useTranslation()
  return (
    <div className="relative">
      <svg className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
      <input type="text" value={value} onChange={e => onChange(e.target.value)} placeholder={t('backup:android.searchPlaceholder')}
        className="w-full text-xs pl-7 pr-3 py-1.5 bg-gray-50 border border-gray-200 rounded-lg focus:outline-none focus:border-blue-400 placeholder:text-gray-300" />
    </div>
  )
}

function BackupRow({ name, size, selected, onSelect }: { name: string; size: string; selected: boolean; onSelect: () => void }) {
  return (
    <button onClick={onSelect}
      className={`w-full flex items-center gap-2.5 px-3 py-2.5 rounded-xl text-left transition-all border-2 ${selected ? 'border-blue-400 bg-blue-50' : 'border-transparent hover:bg-gray-50'}`}>
      <File size={16} className={selected ? 'text-blue-500' : 'text-gray-300'} />
      <span className={`text-sm truncate flex-1 ${selected ? 'text-blue-700' : 'text-gray-700'}`}>{name}</span>
      <span className="text-xs text-gray-400 shrink-0">{size}</span>
    </button>
  )
}

function EmptyState() {
  const { t } = useTranslation()
  return <div className="flex flex-col items-center justify-center py-8 text-gray-300 gap-2"><Inbox size={28} /><span className="text-xs">{t('backup:android.empty')}</span></div>
}

function useDeviceBackups(deviceId: string) {
  const [backups, setBackups] = useState<DeviceBackup[]>([])
  const load = async () => {
    try { setBackups(cast<DeviceBackup[]>(await SdkApiService.ListBackups(deviceId) || [])) } catch {}
  }
  useEffect(() => { load() }, [deviceId])
  return { backups, load }
}

function useLocalBackups() {
  const [backups, setBackups] = useState<LocalBackup[]>([])
  const load = async () => {
    try { setBackups(cast<LocalBackup[]>(await SdkApiService.ListLocalBackupMachines() || [])) } catch {}
  }
  useEffect(() => { load() }, [])
  return { backups, load }
}

const fmtBackupSize = (b: DeviceBackup | LocalBackup) => formatSize(typeof b.size === 'string' ? parseInt(b.size) || 0 : b.size)

// --- 导出确认弹窗（内部有设备/本地切换） ---
export function ExportConfirmModal({ android, onConfirm, onClose }: {
  android: AndroidInfo
  onConfirm: (mode: 'device' | 'local', exportName: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const [tabMode, setTabMode] = useState<'device' | 'local'>('device')
  const [exportName, setExportName] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const confirm = () => {
    // 设备端约定 exportName 不含 .tar.gz 后缀，用户误带后缀时剔除
    onConfirm(tabMode, exportName.trim().replace(/\.tar\.gz$/i, ''))
  }
  return (
    <div className="fixed inset-0 bg-black/30 backdrop-blur-sm flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-2xl shadow-xl w-[360px] modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 py-4 border-b border-gray-100"><h2 className="text-base font-semibold text-gray-800">{t('backup:android.exportTitle')}</h2></div>
        <div className="p-5 space-y-3">
          <div className="flex items-center gap-2 text-sm text-gray-700"><File size={15} className="text-gray-400 shrink-0" /><span className="truncate">{notes[android.id] || android.name}</span></div>
          <ModeToggle tabMode={tabMode} onChange={setTabMode} />
          <div>
            <label className="text-xs text-gray-500">{t('backup:android.exportNameLabel')}</label>
            <input type="text" value={exportName} onChange={e => setExportName(e.target.value)}
              placeholder={t('backup:android.exportNamePlaceholder')}
              className="w-full text-xs px-3 py-1.5 bg-gray-50 border border-gray-200 rounded-lg mt-1 focus:outline-none focus:border-blue-400 placeholder:text-gray-300" />
          </div>
          <p className="text-xs text-gray-400 leading-relaxed">{tabMode === 'local' ? t('backup:android.exportLocalHint') : t('backup:android.exportDeviceHint')}</p>
        </div>
        <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-50">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-600 bg-gray-50 rounded-full hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
          <button onClick={confirm} className="px-4 py-2 text-sm text-white bg-blue-500 rounded-full hover:bg-blue-600 transition-colors">{t('button.confirm')}</button>
        </div>
      </div>
    </div>
  )
}

// --- 导入弹窗（内部有设备/本地切换 + 搜索 + 参数） ---
export function ImportModal({ slot, deviceId, onConfirmDevice, onConfirmLocal, onClose }: {
  slot: number; deviceId: string
  onConfirmDevice: (name: string, newName: string, start: boolean, keepMac: boolean) => void
  onConfirmLocal: (name: string, newName: string, keepMac: boolean) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [tabMode, setTabMode] = useState<'device' | 'local'>('device')
  const { backups: deviceBackups, load: loadDevice } = useDeviceBackups(deviceId)
  const { backups: localBackups, load: loadLocal } = useLocalBackups()
  const [selected, setSelected] = useState('')
  const [search, setSearch] = useState('')
  const [newName, setNewName] = useState('')
  const [start, setStart] = useState(true)
  const [keepMac, setKeepMac] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  const backups = tabMode === 'device' ? deviceBackups : localBackups
  const kw = search.trim().toLowerCase()
  const filtered = kw ? backups.filter(b => b.name.toLowerCase().includes(kw)) : backups

  const switchMode = (m: 'device' | 'local') => { setTabMode(m); setSelected('') }

  return (
    <div className="fixed inset-0 bg-black/30 backdrop-blur-sm flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-2xl shadow-xl w-[420px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
          <h2 className="text-base font-semibold text-gray-800">{t('backup:android.importTitle', { n: slot })}</h2>
          <button onClick={onClose} className="text-gray-300 hover:text-gray-500 transition-colors"><X size={18} /></button>
        </div>
        <div className="px-4 pt-3 pb-2"><ModeToggle tabMode={tabMode} onChange={switchMode} /></div>
        <div className="px-4 pb-2"><SearchBox value={search} onChange={setSearch} /></div>
        <div className="flex-1 overflow-y-auto px-4 pb-2 space-y-1.5 min-h-[120px]">
          {filtered.length === 0 ? <EmptyState /> : filtered.map(b => (
            <BackupRow key={b.name} name={b.name} size={fmtBackupSize(b)} selected={selected === b.name} onSelect={() => setSelected(b.name)} />
          ))}
        </div>
        {selected && (
          <div className="px-4 py-3 border-t border-gray-100 bg-gray-50/50 space-y-2.5">
            <div>
              <label className="block text-xs text-gray-500 mb-1">{t('backup:android.newName')}</label>
              <input type="text" value={newName} onChange={e => setNewName(e.target.value)} placeholder={t('backup:android.newNamePlaceholder')}
                className="w-full text-xs px-2.5 py-1.5 bg-white border border-gray-200 rounded-lg focus:outline-none focus:border-blue-400" />
            </div>
            {tabMode === 'device' && (
              <label className="flex items-center gap-1.5 text-xs text-gray-600 cursor-pointer">
                <input type="checkbox" checked={start} onChange={e => setStart(e.target.checked)} className="w-3.5 h-3.5 rounded accent-blue-500" />
                {t('backup:android.startAfterImport')}
              </label>
            )}
            <label className="flex items-center gap-1.5 text-xs text-gray-600 cursor-pointer">
              <input type="checkbox" checked={keepMac} onChange={e => setKeepMac(e.target.checked)} className="w-3.5 h-3.5 rounded accent-blue-500" />
              {t('backup:android.keepMac')}
            </label>
          </div>
        )}
        <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-50">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-600 bg-gray-50 rounded-full hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
          <button onClick={() => { if (!selected) return; tabMode === 'device' ? onConfirmDevice(selected, newName.trim(), start, keepMac) : onConfirmLocal(selected, newName.trim(), keepMac) }}
            disabled={!selected} className="px-4 py-2 text-sm text-white bg-blue-500 rounded-full hover:bg-blue-600 transition-colors disabled:opacity-40 disabled:cursor-not-allowed">{t('backup:android.import')}</button>
        </div>
      </div>
    </div>
  )
}

// --- 切换备份弹窗（内部有设备/本地切换 + 搜索 + 参数，start=true 固定） ---
export function SwitchBackupModal({ slot, deviceId, onConfirmDevice, onConfirmLocal, onClose }: {
  slot: number; deviceId: string
  onConfirmDevice: (name: string, newName: string, keepMac: boolean) => void
  onConfirmLocal: (name: string, newName: string, keepMac: boolean) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [tabMode, setTabMode] = useState<'device' | 'local'>('device')
  const { backups: deviceBackups } = useDeviceBackups(deviceId)
  const { backups: localBackups } = useLocalBackups()
  const [selected, setSelected] = useState('')
  const [search, setSearch] = useState('')
  const [newName, setNewName] = useState('')
  const [keepMac, setKeepMac] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  const backups = tabMode === 'device' ? deviceBackups : localBackups
  const kw = search.trim().toLowerCase()
  const filtered = kw ? backups.filter(b => b.name.toLowerCase().includes(kw)) : backups
  const switchMode = (m: 'device' | 'local') => { setTabMode(m); setSelected('') }

  return (
    <div className="fixed inset-0 bg-black/30 backdrop-blur-sm flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-2xl shadow-xl w-[420px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
          <h2 className="text-base font-semibold text-gray-800">{t('backup:android.switchBackupTitle', { n: slot })}</h2>
          <button onClick={onClose} className="text-gray-300 hover:text-gray-500 transition-colors"><X size={18} /></button>
        </div>
        <div className="px-4 pt-3 pb-2"><ModeToggle tabMode={tabMode} onChange={switchMode} /></div>
        <div className="px-4 pb-2"><SearchBox value={search} onChange={setSearch} /></div>
        <div className="flex-1 overflow-y-auto px-4 pb-2 space-y-1.5 min-h-[120px]">
          {filtered.length === 0 ? <EmptyState /> : filtered.map(b => (
            <BackupRow key={b.name} name={b.name} size={fmtBackupSize(b)} selected={selected === b.name} onSelect={() => setSelected(b.name)} />
          ))}
        </div>
        {selected && (
          <div className="px-4 py-3 border-t border-gray-100 bg-gray-50/50 space-y-2.5">
            <div>
              <label className="block text-xs text-gray-500 mb-1">{t('backup:android.newName')}</label>
              <input type="text" value={newName} onChange={e => setNewName(e.target.value)} placeholder={t('backup:android.newNamePlaceholder')}
                className="w-full text-xs px-2.5 py-1.5 bg-white border border-gray-200 rounded-lg focus:outline-none focus:border-blue-400" />
            </div>
            <label className="flex items-center gap-1.5 text-xs text-gray-600 cursor-pointer">
              <input type="checkbox" checked={keepMac} onChange={e => setKeepMac(e.target.checked)} className="w-3.5 h-3.5 rounded accent-blue-500" />
              {t('backup:android.keepMac')}
            </label>
          </div>
        )}
        <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-50">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-600 bg-gray-50 rounded-full hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
          <button onClick={() => { if (!selected) return; tabMode === 'device' ? onConfirmDevice(selected, newName.trim(), keepMac) : onConfirmLocal(selected, newName.trim(), keepMac) }}
            disabled={!selected} className="px-4 py-2 text-sm text-white bg-blue-500 rounded-full hover:bg-blue-600 transition-colors disabled:opacity-40 disabled:cursor-not-allowed">{t('backup:android.switchBackup')}</button>
        </div>
      </div>
    </div>
  )
}

// --- 备份列表弹窗（内部有设备/本地切换） ---
export function BackupListModal({ deviceId, onClose }: { deviceId: string; onClose: () => void }) {
  const { t } = useTranslation()
  const [tabMode, setTabMode] = useState<'device' | 'local'>('device')
  const { backups: deviceBackups, load: loadDevice } = useDeviceBackups(deviceId)
  const { backups: localBackups, load: loadLocal } = useLocalBackups()
  const [downloadTarget, setDownloadTarget] = useState<DeviceBackup | null>(null)
  const [downloading, setDownloading] = useState('')
  // 删除二次确认目标（备份文件名）：删除不可恢复，与项目删除类操作惯例一致
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const { addTask, updateTask } = useTaskQueue()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  const handleDownload = async (backup: DeviceBackup, deleteAfter: boolean) => {
    const taskId = await addTask({ type: 'exportAndroid', deviceIps: [deviceId], total: 1, machineName: backup.name })
    updateTask(taskId, { status: 'running', startTime: Date.now(), exportStage: 'downloading' })
    setDownloading(backup.name); setDownloadTarget(null)
    try {
      const localPath = await SdkApiService.DownloadBackup(deviceId, backup.name, deleteAfter, taskId)
      updateTask(taskId, { status: 'completed', progress: 100, completed: 1, exportStage: 'done', savedPath: localPath || '' })
      showToast(t('backup:android.downloadSuccess'), 'success'); loadDevice()
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
      showToast(getErrorMessage(err), 'error')
    } finally { setDownloading('') }
  }

  const handleDelete = async (name: string) => {
    try { await SdkApiService.DeleteBackup(deviceId, name); showToast(t('backup:android.deleteSuccess'), 'success'); loadDevice() }
    catch (err) { showToast(getErrorMessage(err), 'error') }
  }

  return (
    <div className="fixed inset-0 bg-black/30 backdrop-blur-sm flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-2xl shadow-xl w-[460px] max-h-[70vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
          <h2 className="text-base font-semibold text-gray-800">{t('backup:android.backupList')}</h2>
          <button onClick={onClose} className="text-gray-300 hover:text-gray-500 transition-colors"><X size={18} /></button>
        </div>
        <div className="px-4 pt-3 pb-2"><ModeToggle tabMode={tabMode} onChange={setTabMode} /></div>
        <div className="flex-1 overflow-y-auto p-4 space-y-1.5">
          {tabMode === 'device' ? (
            deviceBackups.length === 0 ? <EmptyState /> : deviceBackups.map(b => (
              <div key={b.name} className="flex items-center gap-2.5 px-3 py-2.5 rounded-xl hover:bg-gray-50 transition-colors group">
                <File size={16} className="text-gray-300 shrink-0" />
                <div className="min-w-0 flex-1">
                  <span className="text-sm text-gray-700 truncate block">{b.name}</span>
                  <span className="text-xs text-gray-400">{fmtBackupSize(b)}{b.mtimestamp > 0 && ' · ' + new Date(b.mtimestamp * 1000).toLocaleString()}</span>
                </div>
                <div className="flex items-center gap-1 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button onClick={() => setDownloadTarget(b)} disabled={downloading === b.name}
                    className="w-7 h-7 flex items-center justify-center text-gray-400 hover:text-blue-500 hover:bg-blue-50 rounded-full transition-colors disabled:opacity-50" title={t('backup:android.download')}>
                    {downloading === b.name ? <RefreshCw size={14} className="animate-spin" /> : <Download size={14} />}
                  </button>
                  <button onClick={() => setDeleteTarget(b.name)}
                    className="w-7 h-7 flex items-center justify-center text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-full transition-colors" title={t('backup:android.delete')}>
                    <Archive size={14} />
                  </button>
                </div>
              </div>
            ))
          ) : (
            localBackups.length === 0 ? <EmptyState /> : localBackups.map(b => (
              <div key={b.name} className="flex items-center gap-2.5 px-3 py-2.5 rounded-xl hover:bg-gray-50 transition-colors">
                <File size={16} className="text-gray-300 shrink-0" />
                <span className="text-sm text-gray-700 truncate flex-1">{b.name}</span>
                <span className="text-xs text-gray-400 shrink-0">{fmtBackupSize(b)}</span>
              </div>
            ))
          )}
        </div>
        {downloadTarget && (
          <DownloadConfirmModal name={downloadTarget.name} onConfirm={(del) => handleDownload(downloadTarget, del)} onClose={() => setDownloadTarget(null)} />
        )}
        {/* 删除二次确认：与批量移除设备/删除云机同款 ConfirmDialog（portal 到 body，z-[60]） */}
        {deleteTarget && (
          <ConfirmDialog
            title={t('backup:android.deleteConfirmTitle')}
            message={t('backup:android.deleteConfirmMessage', { name: deleteTarget })}
            confirmLabel={t('backup:android.delete')}
            onConfirm={() => { handleDelete(deleteTarget); setDeleteTarget(null) }}
            onCancel={() => setDeleteTarget(null)}
          />
        )}
      </div>
    </div>
  )
}

// --- 下载确认弹窗 ---
function DownloadConfirmModal({ name, onConfirm, onClose }: { name: string; onConfirm: (deleteAfter: boolean) => void; onClose: () => void }) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 backdrop-blur-sm flex items-center justify-center z-[60] modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-2xl shadow-xl w-[360px] modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 py-4 border-b border-gray-100"><h2 className="text-base font-semibold text-gray-800">{t('backup:android.downloadTitle')}</h2></div>
        <div className="p-5 space-y-3">
          <div className="flex items-center gap-2 text-sm text-gray-700"><File size={15} className="text-gray-400 shrink-0" /><span className="truncate">{name}</span></div>
          <p className="text-xs text-gray-400 leading-relaxed">{t('backup:android.deleteAfterDownloadHint')}</p>
        </div>
        <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-50">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-600 bg-gray-50 rounded-full hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
          <button onClick={() => onConfirm(true)} className="px-4 py-2 text-sm text-white bg-red-500 rounded-full hover:bg-red-600 transition-colors">{t('backup:android.downloadAndDelete')}</button>
          <button onClick={() => onConfirm(false)} className="px-4 py-2 text-sm text-white bg-blue-500 rounded-full hover:bg-blue-600 transition-colors">{t('backup:android.downloadOnly')}</button>
        </div>
      </div>
    </div>
  )
}