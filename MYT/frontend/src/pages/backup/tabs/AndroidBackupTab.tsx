import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { RefreshCw, Upload, Download, Archive, Repeat, FolderOpen, Layers } from 'lucide-react'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as SlotService from '../../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import { DeviceState, AndroidInfo } from '../../../types/index'
import { getDeviceId } from '../../../utils/deviceId'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { showToast } from '../../../components/Toast'
import { getErrorMessage } from '../../../utils/error'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'
import { useDeviceAndroids } from '../../../hooks/useDeviceAndroids'
import { ExportConfirmModal, ImportModal, SwitchBackupModal, BackupListModal } from './AndroidBackupModals'
import { SlotMachinesModal } from './SlotMachinesModal'

interface Props {
  device: DeviceState
}

interface DeviceBackup { name: string; size: string; mtimestamp: number }
interface LocalBackup { name: string; size: number }

// 备份云机 Tab：坑位卡片网格（坑位数与主机 tab 一致，来自 SlotService.GetSlotCount）
export function AndroidBackupTab({ device }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const { mode } = useDeployMode()
  const deviceId = getDeviceId(device, mode)
  // 云机列表 + 刷新机制与主机 tab 对齐（useDeviceAndroids）
  const androids = useDeviceAndroids(device.device_id)
  // 坑位数：与 SlotView 相同，来自设备真实坑位（不再硬编码 8）
  const [slotCount, setSlotCount] = useState(12)
  useEffect(() => {
    SlotService.GetSlotCount(device.device_id).then((count: number) => {
      if (count > 0) setSlotCount(count)
    }).catch(() => {})
  }, [device.device_id])
  const [exportTarget, setExportTarget] = useState<AndroidInfo | null>(null)
  const [importTarget, setImportTarget] = useState<number | null>(null)
  const [switchTarget, setSwitchTarget] = useState<number | null>(null)
  const [listOpen, setListOpen] = useState(false)
  const [busy, setBusy] = useState('')
  // 坑位下所有云机弹窗（一坑位可挂多台，仅多台时卡片显示入口）
  const [machinesSlot, setMachinesSlot] = useState<number | null>(null)
  const { addTask, updateTask } = useTaskQueue()

  // 导出云机（exportName 自定义导出名，空 = 设备自动生成）
  const handleExport = async (android: AndroidInfo, tabMode: 'device' | 'local', customName: string) => {
    const taskId = await addTask({ type: 'exportAndroid', deviceIps: [deviceId], total: 1, machineName: android.name })
    updateTask(taskId, { status: 'running', startTime: Date.now(), exportStage: 'exporting' })
    setBusy(android.name)
    setExportTarget(null)
    try {
      const exportName = await SdkApiService.ExportAndroid(deviceId, android.name, taskId, customName)
      if (tabMode === 'local' && exportName) {
        updateTask(taskId, { exportStage: 'downloading' })
        const localPath = await SdkApiService.DownloadBackup(deviceId, exportName, false, taskId)
        updateTask(taskId, { status: 'completed', progress: 100, completed: 1, exportStage: 'done', savedPath: localPath || '' })
        showToast(t('backup:android.exportLocalSuccess'), 'success')
      } else {
        updateTask(taskId, { status: 'completed', progress: 100, completed: 1 })
        showToast(t('backup:android.exportDeviceSuccess'), 'success')
      }
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
      showToast(getErrorMessage(err), 'error')
    } finally { setBusy('') }
  }

  // 导入云机（设备模式）
  const handleImportDevice = async (indexNum: number, backupName: string, newName: string, start: boolean, keepMac: boolean) => {
    const taskId = await addTask({ type: 'importAndroid', deviceIps: [deviceId], total: 1, machineName: backupName, slotIndex: indexNum })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    setBusy('import_' + indexNum)
    setImportTarget(null)
    try {
      await SdkApiService.ImportAndroid(deviceId, backupName, indexNum, newName, start, keepMac, taskId)
      updateTask(taskId, { status: 'completed', progress: 100, completed: 1 })
      showToast(t('backup:android.importSuccess'), 'success')
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
      showToast(getErrorMessage(err), 'error')
    } finally { setBusy('') }
  }

  // 导入云机（本地模式）
  const handleImportLocal = async (indexNum: number, machineName: string, newName: string, keepMac: boolean) => {
    const taskId = await addTask({ type: 'importAndroid', deviceIps: [deviceId], total: 1, machineName, slotIndex: indexNum })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    setBusy('import_' + indexNum)
    setImportTarget(null)
    try {
      await SdkApiService.ImportAndroidFromFile(deviceId, machineName, indexNum, newName, keepMac, taskId)
      updateTask(taskId, { status: 'completed', progress: 100, completed: 1 })
      showToast(t('backup:android.importSuccess'), 'success')
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
      showToast(getErrorMessage(err), 'error')
    } finally { setBusy('') }
  }

  const fmtDevice = () => {
    const note = device.alias || device.device_id
    return mode === 'public' ? note : `${note} (${device.ip})`
  }

  // 打开本地备份目录（cloudMachineBackup，云机备份 .tar.gz 存放处）
  const handleOpenLocalDir = async () => {
    try {
      await SdkApiService.OpenBackupDirectory()
    } catch (e: unknown) {
      showToast(e instanceof Error ? e.message : t('backup:android.openLocalDirFailed'), 'error')
    }
  }

  // 完整 1..N 坑位网格；一个坑位可挂多台云机，卡片默认展示运行中的，没运行就顺序第一台
  const slots = Array.from({ length: slotCount }, (_, i) => {
    const idx = i + 1
    const grouped = androids.filter(a => a.indexNum === idx)
    const android = grouped.length > 0 ? (grouped.find(a => a.status === 'running') ?? grouped[0]) : undefined
    return { idx, grouped, android }
  })

  const iconBtn = 'w-8 h-8 flex items-center justify-center rounded-full transition-all'
  const iconEnabled = 'text-gray-400 hover:bg-blue-50 hover:text-blue-600'
  const iconDisabled = 'text-gray-200 cursor-not-allowed'

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-5 py-3 border-b border-gray-200">
        <div className="text-sm text-gray-600 font-medium">{fmtDevice()}</div>
        <div className="flex items-center gap-1">
          <button onClick={() => setListOpen(true)}
            className="flex items-center gap-1 text-xs px-2.5 py-1.5 rounded-md text-gray-500 hover:bg-gray-100">
            <Archive size={13} />{t('backup:android.backupList')}
          </button>
          <button onClick={handleOpenLocalDir}
            className="flex items-center gap-1 text-xs px-2.5 py-1.5 rounded-md text-gray-500 hover:bg-gray-100"
            title={t('backup:android.openLocalDir')}>
            <FolderOpen size={13} />{t('backup:android.openLocalDir')}
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-auto p-4">
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3">
          {slots.map(({ idx, grouped, android }) => {
            const hasAndroid = !!android
            const isRunning = android?.status === 'running'
            const isExporting = busy === android?.name
            const isImporting = busy === 'import_' + idx
            return hasAndroid ? (
              <div key={idx} className="bg-white rounded-xl border border-gray-200/80 overflow-hidden hover:shadow-md hover:-translate-y-0.5 transition-all duration-200 flex flex-col">
                <div className={`h-1 ${isRunning ? 'bg-green-400' : 'bg-gray-200'}`} />
                <div className="p-3 flex flex-col flex-1 min-h-[110px]">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs text-gray-400">#{idx}</span>
                    <span className={`text-[10px] px-1.5 py-0.5 rounded-full ${isRunning ? 'bg-green-50 text-green-600' : 'bg-gray-100 text-gray-400'}`}>{android!.status}</span>
                  </div>
                  <div className="flex-1 flex items-center justify-center py-2">
                    <span className="text-sm font-medium text-gray-700 text-center truncate w-full" title={android!.name}>{notes[android!.id] || android!.name}</span>
                  </div>
                  <div className="flex items-center justify-center gap-2 pt-2 border-t border-gray-50">
                    <button onClick={() => setExportTarget(android!)} disabled={isExporting}
                      className={`${iconBtn} ${!isExporting ? iconEnabled : 'text-blue-500'}`} title={t('backup:android.export')}>
                      {isExporting ? <RefreshCw size={15} className="animate-spin" /> : <Upload size={15} />}
                    </button>
                    <button onClick={() => setSwitchTarget(idx)} disabled={isImporting}
                      className={`${iconBtn} ${!isImporting ? iconEnabled : 'text-blue-500'}`} title={t('backup:android.switchBackup')}>
                      <Repeat size={15} />
                    </button>
                    <button onClick={() => setImportTarget(idx)} disabled={isImporting}
                      className={`${iconBtn} ${!isImporting ? iconEnabled : 'text-blue-500'}`} title={t('backup:android.import')}>
                      {isImporting ? <RefreshCw size={15} className="animate-spin" /> : <Download size={15} />}
                    </button>
                    {grouped.length > 1 && (
                      <button onClick={() => setMachinesSlot(idx)}
                        className={`${iconBtn} ${iconEnabled}`} title={t('backup:android.allMachines')}>
                        <Layers size={15} />
                      </button>
                    )}
                  </div>
                </div>
              </div>
            ) : (
              <div key={idx} className="rounded-xl border-2 border-dashed border-gray-200 flex flex-col items-center justify-center min-h-[120px] gap-2 hover:border-blue-300 hover:bg-blue-50/30 transition-colors">
                <span className="text-xs text-gray-400">#{idx}</span>
                <span className="text-xs text-gray-300">{t('backup:android.idle')}</span>
                <button onClick={() => setImportTarget(idx)} disabled={isImporting}
                  className={`${iconBtn} ${!isImporting ? iconEnabled : 'text-blue-500'}`} title={t('backup:android.import')}>
                  {isImporting ? <RefreshCw size={15} className="animate-spin" /> : <Download size={15} />}
                </button>
              </div>
            )
          })}
        </div>
      </div>

      {switchTarget !== null && (
        <SwitchBackupModal slot={switchTarget} deviceId={deviceId}
          onConfirmDevice={(name, newName, keepMac) => handleImportDevice(switchTarget, name, newName, true, keepMac)}
          onConfirmLocal={(name, newName, keepMac) => handleImportLocal(switchTarget, name, newName, keepMac)}
          onClose={() => setSwitchTarget(null)} />
      )}
      {exportTarget && (
        <ExportConfirmModal android={exportTarget}
          onConfirm={(m, n) => handleExport(exportTarget, m, n)} onClose={() => setExportTarget(null)} />
      )}
      {importTarget !== null && (
        <ImportModal slot={importTarget} deviceId={deviceId}
          onConfirmDevice={(name, newName, start, keepMac) => handleImportDevice(importTarget, name, newName, start, keepMac)}
          onConfirmLocal={(name, newName, keepMac) => handleImportLocal(importTarget, name, newName, keepMac)}
          onClose={() => setImportTarget(null)} />
      )}
      {listOpen && (
        <BackupListModal deviceId={deviceId} onClose={() => setListOpen(false)} />
      )}
      {machinesSlot !== null && (
        <SlotMachinesModal slot={machinesSlot}
          machines={androids.filter(a => a.indexNum === machinesSlot)}
          onExport={a => { setMachinesSlot(null); setExportTarget(a) }}
          onClose={() => setMachinesSlot(null)} />
      )}
    </div>
  )
}