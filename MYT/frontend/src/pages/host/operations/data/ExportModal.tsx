import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Events } from '@wailsio/runtime'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { showToast } from '../../../../components/Toast'
import { formatElapsed } from './crossDeviceMove'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import { eventData } from '../../../../hooks/taskQueue/taskQueueHelpers'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import { HardDrive, Cloud } from 'lucide-react'

interface Props {
  deviceIP: string
  androidName: string
  onClose: () => void
}

type ExportStage = 'idle' | 'exporting' | 'downloading' | 'done' | 'error'

function useLiveTimer(active: boolean): string {
  const [text, setText] = useState('')
  const startRef = useRef(0)
  useEffect(() => {
    if (!active) { setText(''); return }
    startRef.current = Date.now()
    const id = setInterval(() => setText(formatElapsed(Date.now() - startRef.current)), 1000)
    return () => clearInterval(id)
  }, [active])
  return text
}

function ExportModal({ deviceIP, androidName, onClose }: Props) {
  const { t } = useTranslation()
  const [stage, setStage] = useState<ExportStage>('idle')
  const [error, setError] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [progress, setProgress] = useState(0)
  const [progressText, setProgressText] = useState('')
  const [exportTime, setExportTime] = useState('')
  const [downloadTime, setDownloadTime] = useState('')
  const [savedPath, setSavedPath] = useState('')
  // 与备份 tab 的 ExportConfirmModal 一致：设备=只导出存设备；本地=导出+下载到 PC（默认保持原行为）
  const [tabMode, setTabMode] = useState<'device' | 'local'>('local')
  const [exportName, setExportName] = useState('')
  const exportTimer = useLiveTimer(stage === 'exporting')
  const downloadTimer = useLiveTimer(stage === 'downloading')
  const { addTask, updateTask, openPanel } = useTaskQueue()

  // 进度来源：Go 侧 android:export-progress 事件（下载在 Go 后端完成，前端不再重复 fetch 同一 URL）
  useEffect(() => {
    if (stage !== 'downloading') return
    const off = Events.On('android:export-progress', (evt: unknown) => {
      const d = eventData<{ deviceIP: string; stage: string; progress?: number; downloaded?: number; total?: number }>(evt)
      if (d.deviceIP !== deviceIP || d.stage !== 'downloading') return
      if (typeof d.progress === 'number') setProgress(Math.round(d.progress))
      if (typeof d.downloaded === 'number') {
        setProgressText(typeof d.total === 'number' && d.total > 0 ? `${d.downloaded} / ${d.total}` : `${d.downloaded}`)
      }
    })
    return () => { off() }
  }, [stage, deviceIP])

  const startExport = async () => {
    const taskId = await addTask({
      type: 'exportAndroid',
      deviceIps: [deviceIP],
      total: 1,
      machineName: androidName,
    })
    updateTask(taskId, { status: 'running', startTime: Date.now(), exportStage: 'exporting' })
    onClose()
    openPanel(taskId)

    setStage('exporting')
    setError('')
    setProgress(0)
    const t0 = Date.now()
    try {
      // 1. 导出 → 返回备份文件名（设备端约定 customName 不含 .tar.gz 后缀，用户误带后缀时剔除）
      const customName = exportName.trim().replace(/\.tar\.gz$/i, '')
      const exportedName = await SdkApiService.ExportAndroid(deviceIP, androidName, taskId, customName)
      setExportTime(formatElapsed(Date.now() - t0))
      if (tabMode === 'device') {
        updateTask(taskId, { status: 'completed', progress: 100, completed: 1, exportStage: 'done' })
        showToast(t('host:toast.exportDeviceSuccess'), 'success')
        setStage('done')
        return
      }
      if (!exportedName) {
        updateTask(taskId, { status: 'completed', progress: 100, completed: 1 })
        showToast(t('host:toast.exportCompleteNoName'), 'success')
        setStage('done')
        return
      }

      // 2. 下载：Go 侧保存到 cloudMachineBackup 目录（进度经 android:export-progress 事件回推）
      setStage('downloading')
      updateTask(taskId, { exportStage: 'downloading' })
      const t1 = Date.now()

      const localPath = await SdkApiService.DownloadBackup(deviceIP, exportedName, false, taskId)
      if (localPath) setSavedPath(localPath)

      setDownloadTime(formatElapsed(Date.now() - t1))
      setStage('done')
      updateTask(taskId, { status: 'completed', progress: 100, completed: 1, exportStage: 'done', savedPath: localPath || '' })
      showToast(t('host:toast.exportComplete'), 'success')
    } catch (err) {
      setStage('error')
      setError(`${err}`)
      updateTask(taskId, {
        status: 'failed',
        failed: 1,
        failedTargets: [{ deviceIp: deviceIP, machineName: androidName, error: String(err) }],
      })
    }
  }

  const canClose = stage === 'idle' || stage === 'done' || stage === 'error'

  const handleOpenDir = async () => {
    try {
      await SdkApiService.OpenBackupDirectory()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : t('host:toast.openDirFailed'), 'error')
    }
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[400px] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <h3 className="text-sm font-medium text-gray-700">{t('host:export.title', { name: androidName })}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="p-6 space-y-4">
          {stage === 'idle' && (
            <div className="space-y-3">
              <p className="text-sm text-gray-600">{t('host:export.confirmMessage')}</p>
              <div className="flex items-center bg-gray-100 rounded-md p-0.5 w-fit">
                <button onClick={() => setTabMode('device')}
                  className={`flex items-center gap-1 text-xs px-2.5 py-1 rounded ${tabMode === 'device' ? 'bg-white text-blue-600 shadow-sm' : 'text-gray-500'}`}>
                  <HardDrive size={12} />{t('host:export.deviceMode')}
                </button>
                <button onClick={() => setTabMode('local')}
                  className={`flex items-center gap-1 text-xs px-2.5 py-1 rounded ${tabMode === 'local' ? 'bg-white text-blue-600 shadow-sm' : 'text-gray-500'}`}>
                  <Cloud size={12} />{t('host:export.localMode')}
                </button>
              </div>
              <div>
                <label className="text-xs text-gray-500">{t('host:export.exportNameLabel')}</label>
                <input type="text" value={exportName} onChange={e => setExportName(e.target.value)}
                  placeholder={t('host:export.exportNamePlaceholder')}
                  className="w-full text-xs px-3 py-1.5 bg-gray-50 border border-gray-200 rounded-lg mt-1 focus:outline-none focus:border-blue-400 placeholder:text-gray-300" />
              </div>
              <p className="text-xs text-gray-400 leading-relaxed">{tabMode === 'local' ? t('host:export.localHint') : t('host:export.deviceHint')}</p>
            </div>
          )}
          {stage === 'exporting' && (
            <div className="flex items-center gap-2 text-sm text-blue-600">
              <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
              {t('host:export.exporting')} {exportTimer}
            </div>
          )}
          {stage === 'downloading' && (
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm text-blue-600">
                <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
                {t('host:export.downloading')} {progressText} {downloadTimer}
              </div>
              <div className="w-full bg-gray-200 rounded-full h-2">
                <div className="bg-blue-500 h-2 rounded-full transition-all" style={{ width: `${progress}%` }} />
              </div>
            </div>
          )}
          {stage === 'done' && (
            <div className="space-y-1">
              <div className="flex items-center gap-2 text-sm text-green-600">
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12" /></svg>
                {t('host:export.done')}
              </div>
              {(exportTime || downloadTime) && (
                <div className="text-xs text-gray-400 ml-6">
                  {exportTime && t('host:export.exportTime', { time: exportTime })}{exportTime && downloadTime && ', '}{downloadTime && t('host:export.downloadTime', { time: downloadTime })}
                </div>
              )}
              {savedPath && (
                <div className="text-xs text-gray-500 ml-6 mt-1 break-all">
                  {t('host:export.savedTo')}: {savedPath}
                </div>
              )}
            </div>
          )}
          {stage === 'error' && (
            <div className="text-sm text-red-500 bg-red-50 p-2 rounded-lg">{error}</div>
          )}
        </div>
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          {stage === 'done' && savedPath && (
            <button onClick={handleOpenDir} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.openDirectory')}</button>
          )}
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200" disabled={!canClose}>
            {stage === 'done' ? t('button.close') : t('button.cancel')}
          </button>
          {stage === 'idle' && (
            <button onClick={startExport} className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600">{t('host:actionBar.export')}</button>
          )}
        </div>
      </div>
    </div>
  )
}

export default ExportModal
