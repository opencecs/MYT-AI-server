import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { RefreshCw, Upload, Download, Trash2, Archive } from 'lucide-react'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as ScreenshotService from '../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import { DeviceState, AndroidInfo } from '../../../types/index'
import { getDeviceId } from '../../../utils/deviceId'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { showToast } from '../../../components/Toast'
import { getErrorMessage } from '../../../utils/error'
import { cast } from '../../../utils/cast'
import { emitSlotStatus } from '../../../utils/wailsEvent'
import { useModalEnter } from '../../../hooks/useModalAnimation'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'
import { useDeviceAndroids } from '../../../hooks/useDeviceAndroids'

interface Props {
  device: DeviceState
}

interface BackupItem { name: string }

// 备份机型 Tab：云机卡片网格，每张卡片有导出/导入机型按钮
export function PhoneModelBackupTab({ device }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const { mode } = useDeployMode()
  const deviceId = getDeviceId(device, mode)
  // 云机列表 + 刷新机制与主机 tab 对齐（useDeviceAndroids）
  const androids = useDeviceAndroids(device.device_id)
  const [backups, setBackups] = useState<BackupItem[]>([])
  const [exportTarget, setExportTarget] = useState<AndroidInfo | null>(null)
  const [importTarget, setImportTarget] = useState<AndroidInfo | null>(null)
  const [busy, setBusy] = useState('')
  const [listOpen, setListOpen] = useState(false)
  const { addTask, updateTask } = useTaskQueue()

  // 拉取备份列表
  const loadBackups = useCallback(async () => {
    try {
      const raw = await SdkApiService.ListPhoneModelBackups(deviceId)
      setBackups(cast<BackupItem[]>(Array.isArray(raw) ? raw : ((raw as any)?.list ?? [])))
    } catch {}
  }, [deviceId])

  // 页面加载时拉取备份列表
  useEffect(() => { loadBackups() }, [loadBackups])

  // 删除备份
  const handleDeleteBackup = async (name: string) => {
    try {
      await SdkApiService.DeletePhoneModelBackup(deviceId, name)
      showToast(t('backup:phoneModel.deleteSuccess'), 'success')
      loadBackups()
    } catch (err) { showToast(getErrorMessage(err), 'error') }
  }

  // 导出机型（从云机备份）
  const handleExport = async (android: AndroidInfo, suffix: string) => {
    setBusy(android.name)
    try {
      await SdkApiService.SavePhoneModelBackup(deviceId, android.name, suffix)
      showToast(t('backup:phoneModel.exportSuccess'), 'success')
      setExportTarget(null)
      loadBackups()
    } catch (err) { showToast(getErrorMessage(err), 'error') }
    finally { setBusy('') }
  }

  // 导入机型（切换备份到云机）
  const handleImport = async (android: AndroidInfo, backupName: string) => {
    const taskId = await addTask({ type: 'switchModel', targets: [{ deviceIp: deviceId, name: android.name }], deviceIps: [deviceId], total: 1 })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    setBusy(android.name)
    try {
      await SdkApiService.SwitchModel(deviceId, android.name, '', '', backupName, 0, 0, '')
      ScreenshotService.InvalidateSlot(deviceId, android.indexNum).catch(() => {})
      emitSlotStatus(`${deviceId}_${android.indexNum}`, 'starting')
      updateTask(taskId, { status: 'completed', progress: 100, completed: 1, endTime: Date.now() })
      showToast(t('backup:phoneModel.importSuccess'), 'success')
      setImportTarget(null)
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
      showToast(getErrorMessage(err), 'error')
    } finally { setBusy('') }
  }

  const fmtDevice = () => {
    const note = device.alias || device.device_id
    return mode === 'public' ? note : `${note} (${device.ip})`
  }

  return (
    <div className="flex flex-col h-full">
      {/* 顶部 */}
      <div className="flex items-center justify-between px-5 py-3 border-b border-gray-200">
        <div className="text-sm text-gray-600 font-medium">{fmtDevice()}</div>
        <button onClick={() => { loadBackups(); setListOpen(true) }}
          className="flex items-center gap-1 text-xs px-2.5 py-1.5 rounded-md text-gray-500 hover:bg-gray-100">
          <Archive size={13} /> {t('backup:phoneModel.backupList')} ({backups.length})
        </button>
      </div>

      {/* 卡片网格 */}
      <div className="flex-1 overflow-auto p-4">
        {androids.length === 0 ? (
          <div className="flex items-center justify-center h-full text-gray-400 text-sm">{t('backup:phoneModel.noContainer')}</div>
        ) : (
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3">
            {androids.map(a => (
              <div key={a.id} className="border border-gray-200 rounded-lg p-3 flex flex-col gap-2 hover:shadow-sm transition-shadow">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium text-gray-700 truncate">{notes[a.id] || a.name}</span>
                  <span className="text-xs text-gray-400">#{a.indexNum}</span>
                </div>
                <div>
                  <span className={`text-xs px-1.5 py-0.5 rounded ${a.status === 'running' ? 'bg-green-50 text-green-600' : 'bg-gray-100 text-gray-400'}`}>{a.status}</span>
                </div>
                <div className="flex gap-1.5 pt-1">
                  <button onClick={() => setExportTarget(a)} disabled={busy === a.name}
                    className="flex-1 flex items-center justify-center gap-1 text-xs px-2 py-1.5 rounded-md bg-gray-50 text-gray-600 hover:bg-gray-100 disabled:opacity-50">
                    {busy === a.name ? <RefreshCw size={12} className="animate-spin" /> : <Upload size={12} />}
                    {t('backup:phoneModel.exportModel')}
                  </button>
                  <button onClick={() => { loadBackups(); setImportTarget(a) }} disabled={busy === a.name || a.status !== 'running'}
                    className="flex-1 flex items-center justify-center gap-1 text-xs px-2 py-1.5 rounded-md bg-blue-50 text-blue-600 hover:bg-blue-100 disabled:opacity-50 disabled:cursor-not-allowed">
                    <Download size={12} />
                    {t('backup:phoneModel.importModel')}
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {listOpen && (
        <BackupListModal backups={backups} onDelete={handleDeleteBackup} onClose={() => setListOpen(false)} />
      )}
      {exportTarget && (
        <ExportModal android={exportTarget} onConfirm={(s) => handleExport(exportTarget, s)} onClose={() => setExportTarget(null)} />
      )}
      {importTarget && (
        <ImportModal android={importTarget} backups={backups}
          onConfirm={(b) => handleImport(importTarget, b)} onClose={() => setImportTarget(null)} />
      )}
    </div>
  )
}

// 导出机型弹窗（输入后缀名）
function ExportModal({ android, onConfirm, onClose }: { android: AndroidInfo; onConfirm: (suffix: string) => void; onClose: () => void }) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const [suffix, setSuffix] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[340px] modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 py-3 border-b border-gray-100">
          <h2 className="text-base font-semibold text-gray-800">{t('backup:phoneModel.exportTitle')}</h2>
          <p className="text-xs text-gray-400 mt-0.5">{notes[android.id] || android.name}</p>
        </div>
        <div className="p-5">
          <input autoFocus value={suffix} onChange={e => setSuffix(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && suffix.trim()) onConfirm(suffix.trim()) }}
            placeholder={t('backup:phoneModel.suffixPlaceholder')}
            className="w-full px-3 py-2 text-sm border border-gray-300 rounded-lg focus:outline-none focus:border-blue-400" />
        </div>
        <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={() => suffix.trim() && onConfirm(suffix.trim())} disabled={!suffix.trim()}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">{t('button.confirm')}</button>
        </div>
      </div>
    </div>
  )
}

// 导入机型弹窗（选备份）
function ImportModal({ android, backups, onConfirm, onClose }: {
  android: AndroidInfo; backups: BackupItem[]; onConfirm: (name: string) => void; onClose: () => void
}) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const [selected, setSelected] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const itemCls = 'w-full px-3 py-2 text-left text-sm hover:bg-blue-50 rounded-lg border border-gray-100'
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[380px] max-h-[70vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 py-3 border-b border-gray-100">
          <h2 className="text-base font-semibold text-gray-800">{t('backup:phoneModel.importTitle')}</h2>
          <p className="text-xs text-gray-400 mt-0.5">{notes[android.id] || android.name}</p>
        </div>
        <div className="flex-1 overflow-y-auto p-4 space-y-1.5">
          {backups.length === 0 ? (
            <div className="text-center text-xs text-gray-400 py-8">{t('backup:phoneModel.empty')}</div>
          ) : backups.map(b => (
            <button key={b.name} onClick={() => setSelected(b.name)}
              className={`${itemCls} ${selected === b.name ? 'bg-blue-50 border-blue-300' : ''}`}>
              {b.name}
            </button>
          ))}
        </div>
        <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={() => selected && onConfirm(selected)} disabled={!selected}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">{t('button.confirm')}</button>
        </div>
      </div>
    </div>
  )
}

// 备份列表弹窗（查看 + 删除）
function BackupListModal({ backups, onDelete, onClose }: { backups: BackupItem[]; onDelete: (name: string) => void; onClose: () => void }) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[380px] max-h-[70vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 py-3 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-base font-semibold text-gray-800">{t('backup:phoneModel.backupList')}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="flex-1 overflow-y-auto p-4 space-y-1.5">
          {backups.length === 0 ? (
            <div className="text-center text-xs text-gray-400 py-8">{t('backup:phoneModel.empty')}</div>
          ) : backups.map(b => (
            <div key={b.name} className="flex items-center justify-between px-3 py-2 border border-gray-100 rounded-lg hover:bg-gray-50">
              <span className="text-sm text-gray-700 truncate">{b.name}</span>
              <button onClick={() => onDelete(b.name)} className="p-1 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded" title={t('backup:phoneModel.delete')}>
                <Trash2 size={14} />
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
