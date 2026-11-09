import { useState, useEffect,  useRef} from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { showToast } from '../../../../components/Toast'
import { formatSize } from '../../../../utils/format'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import { useModalEnter } from '../../../../hooks/useModalAnimation'

interface Props {
  deviceIP: string
  slotIndex: number
  onClose: () => void
}

interface BackupItem {
  name: string
  size: number
  path: string
}

function ImportModal({ deviceIP, slotIndex, onClose }: Props) {
  const { t } = useTranslation()
  const [stage, setStage] = useState<'loading' | 'select'>('loading')
  const [backups, setBackups] = useState<BackupItem[]>([])
  const [selected, setSelected] = useState('')
  const { addTask, updateTask, openPanel } = useTaskQueue()

  useEffect(() => {
    ;(async () => {
      try {
        const list = await SdkApiService.ListLocalBackupMachines()
        setBackups((list as BackupItem[]) || [])
        setStage('select')
      } catch {
        setBackups([])
        setStage('select')
      }
    })()
  }, [])

  const handleOpenDir = async () => {
    try {
      await SdkApiService.OpenBackupDirectory()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : t('host:toast.openDirFailed'), 'error')
    }
  }

  const startImport = async () => {
    if (!selected.trim()) {
      showToast(t('host:toast.importSelectBackup'), 'error')
      return
    }
    const taskId = await addTask({
      type: 'importAndroid',
      deviceIps: [deviceIP],
      total: 1,
      machineName: selected,
      slotIndex,
    })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    onClose()
    openPanel(taskId)
    try {
      await SdkApiService.ImportAndroidFromFile(deviceIP, selected, slotIndex, '', false, taskId)
      updateTask(taskId, { status: 'completed', completed: 1, progress: 100 })
      showToast(t('host:toast.importComplete'), 'success')
    } catch (err) {
      updateTask(taskId, {
        status: 'failed',
        failed: 1,
        failedTargets: [{ deviceIp: deviceIP, machineName: selected, slot: slotIndex, error: String(err) }],
      })
      showToast(t('host:toast.importFailed'), 'error')
    }
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[440px] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <h3 className="text-sm font-medium text-gray-700">{t('host:import.title', { slot: slotIndex })}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="p-6 space-y-4">
          {stage === 'loading' && (
            <div className="flex items-center gap-2 text-sm text-blue-600">
              <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
              {t('host:import.loadingBackups')}
            </div>
          )}
          {stage === 'select' && (
            <div className="space-y-3">
              {backups.length === 0 ? (
                <div className="text-sm text-gray-400">{t('host:import.noBackups')}</div>
              ) : (
                <>
                  <label className="block text-xs text-gray-500">{t('host:import.selectBackup')}</label>
                  <div className="max-h-48 overflow-y-auto space-y-1 border border-gray-200 rounded-lg p-2">
                    {backups.map(b => (
                      <label key={b.name} className={`flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer hover:bg-gray-50 ${selected === b.name ? 'bg-blue-50' : ''}`}>
                        <input
                          type="radio"
                          name="backup"
                          checked={selected === b.name}
                          onChange={() => setSelected(b.name)}
                          className="accent-blue-500"
                        />
                        <span className="text-sm text-gray-700 truncate flex-1">{b.name}</span>
                        <span className="text-xs text-gray-400">{formatSize(b.size)}</span>
                      </label>
                    ))}
                  </div>
                  <p className="text-xs text-gray-400">{t('host:import.importHint', { slot: slotIndex })}</p>
                </>
              )}
            </div>
          )}
        </div>
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          {stage === 'select' && (
            <button onClick={handleOpenDir} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.openDirectory')}</button>
          )}
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          {stage === 'select' && backups.length > 0 && (
            <button
              onClick={startImport}
              disabled={!selected}
              className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 disabled:cursor-not-allowed"
            >{t('host:import.import')}</button>
          )}
        </div>
      </div>
    </div>
  )
}

export default ImportModal