import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2, X, Info, FileArchive } from 'lucide-react'
import { AndroidInfo } from '../../../../types/index'
import * as FileService from '../../../../../bindings/github.com/moyunteng/myt/fileservice'
import { UploadFileEntry } from '../../../../../bindings/github.com/moyunteng/myt/models'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import { registerTaskAbort, unregisterTaskAbort } from '../../../../hooks/taskQueue/useTaskQueue'
import type { DeviceProgress } from '../../../../hooks/taskQueue/types'
import { showToast } from '../../../../components/Toast'
import { getErrorMessage } from '../../../../utils/error'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import { Events } from '@wailsio/runtime'

interface Props {
  selectedAndroids: AndroidInfo[]
  notes: Record<string, string>
  onClose: () => void
}

/** 窗口文件拖拽事件载荷（Go 端 registerFileDropHandler emit） */
interface DroppedFilesPayload {
  targetId: string
  entries: UploadFileEntry[]
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)}MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)}GB`
}

function BatchUploadModal({ selectedAndroids, notes, onClose }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [files, setFiles] = useState<UploadFileEntry[]>([])
  const [picking, setPicking] = useState(false)
  const [autoGrant, setAutoGrant] = useState(false)
  const { addTask, updateTask } = useTaskQueue()

  // 「自动授予运行时权限」仅对 .apk 生效（.apks/.xapk 走 /installapks 不支持 -g，置灰）
  const hasApk = files.some(f => f.ext?.toLowerCase() === '.apk')
  const hasNonApkInstallable = files.some(f => ['.apks', '.xapk'].includes(f.ext?.toLowerCase() || ''))

  // 监听窗口文件拖拽：落点命中本弹窗 dropzone 时合并文件（按 path 去重）
  // Wails 仅在带 data-file-drop-target 的元素上触发，未标记区域拖拽照常走 HTML5 onDrop
  useEffect(() => {
    const off = Events.On('files-dropped', (ev: unknown) => {
      const payload = ((ev as { data?: DroppedFilesPayload })?.data ?? ev) as DroppedFilesPayload
      if (payload.targetId !== 'batch-upload-dropzone' || !payload.entries?.length) return
      setFiles(prev => {
        const seen = new Set(prev.map(f => f.path))
        const merged = [...prev]
        for (const nf of payload.entries) {
          if (!seen.has(nf.path)) { seen.add(nf.path); merged.push(nf) }
        }
        return merged
      })
    })
    return () => { off() }
  }, [])

  const handlePickFiles = async () => {
    setPicking(true)
    try {
      const list = await FileService.SelectUploadFiles('')
      if (Array.isArray(list) && list.length > 0) {
        setFiles(prev => {
          const seen = new Set(prev.map(f => f.path))
          const merged = [...prev]
          for (const nf of list) {
            if (!seen.has(nf.path)) {
              seen.add(nf.path)
              merged.push(nf)
            }
          }
          return merged
        })
      }
    } catch {
      // 用户取消或失败，静默处理
    } finally {
      setPicking(false)
    }
  }

  const handleRemove = (path: string) => {
    setFiles(prev => prev.filter(f => f.path !== path))
  }

  const handleClear = () => setFiles([])

  const handleUpload = async () => {
    if (files.length === 0 || selectedAndroids.length === 0) return

    const tasks: { android: AndroidInfo; file: UploadFileEntry }[] = []
    for (const a of selectedAndroids) {
      for (const f of files) {
        tasks.push({ android: a, file: f })
      }
    }

    const total = tasks.length
    const fileCount = files.length
    const machineCount = selectedAndroids.length
    const deviceIps = [...new Set(selectedAndroids.map(a => a.ip))]
    // 初始化每设备 total（进度条分母）：该设备的任务数 = 云机数 × 文件数
    // （同一物理设备多坑位时云机数 >1，只按文件数会低估分母 → completed>=total 提前成立 → 进度条假 100%）
    const dpInit: Record<string, { total: number; completed: number; failed: number }> = {}
    for (const ip of deviceIps) dpInit[ip] = { total: tasks.filter(t => t.android.ip === ip).length, completed: 0, failed: 0 }
    // deviceProgress 预填随任务创建进后端：消除 CreateSingle 的 task:updated 快照(dp=null)
    // 晚于首个 updateTask 到达时把 dpInit 覆盖掉的竞态（事件处理器 fallback total=1 会致 done 提前判定）
    const taskId = await addTask({ type: 'uploadFile', targets: tasks.map(t => ({ deviceIp: t.android.ip, machines: [{ name: t.android.name }], filePath: t.file.path })), deviceIps, total, fileCount, machineCount, deviceProgress: dpInit })
    // 取消支持：任务面板取消按钮 → cancelTask → ac.abort()，编排在组边界停止（在途单组继续传完）
    const ac = new AbortController()
    registerTaskAbort(taskId, ac)
    updateTask(taskId, { status: 'running', startTime: Date.now(), deviceProgress: dpInit })

    onClose()
    showToast(t('host:toast.uploadTaskAdded', { fileCount, machineCount }), 'success')

    // 按（设备IP, 文件）分组：同组一次网络上传到共享目录，其余容器直接复制/安装（零传输）
    type UploadGroup = { ip: string; file: UploadFileEntry; androids: AndroidInfo[] }
    const groupMap = new Map<string, UploadGroup>()
    for (const t of tasks) {
      const key = `${t.android.ip}|${t.file.path}`
      const g = groupMap.get(key)
      if (g) g.androids.push(t.android)
      else groupMap.set(key, { ip: t.android.ip, file: t.file, androids: [t.android] })
    }
    // 按设备聚合：同设备组串行（其余容器依赖首容器传完共享目录），跨设备并行
    const byDevice = new Map<string, UploadGroup[]>()
    for (const g of groupMap.values()) {
      const arr = byDevice.get(g.ip)
      if (arr) arr.push(g)
      else byDevice.set(g.ip, [g])
    }

    setTimeout(async () => {
      let completed = 0, failed = 0
      // 设备维度进度副本：每次 updateTask 都随 patch 下发。
      // 后端 task:updated 快照会整体覆盖本地任务——若不带最新 deviceProgress，
      // 事件涨上去的计数会被后端旧值（completed=0）回滚，最终定格「0/N 已完成」。
      // 组完成时补 currentProgress/stage，避免快照覆盖抹掉进度条与文案造成闪烁。
      const dp: Record<string, DeviceProgress> = {}
      for (const [ip, init] of Object.entries(dpInit)) dp[ip] = { ...init }
      // 失败明细全量维护：applyPatch 对 failedTargets 是整体替换，必须每次带全量（跨设备并行更新）
      const failedTargets: Array<{ deviceIp: string; machineName?: string; error: string; filePath?: string }> = []

      await Promise.all([...byDevice.values()].map(async devGroups => {
        for (const g of devGroups) {
          if (ac.signal.aborted) break
          try {
            const r = await FileService.UploadToCloudBatch(g.ip, g.androids.map(a => a.name), g.file.path, autoGrant)
            const ok = r?.installed?.length ?? 0
            const bad = r?.failed?.length ?? 0
            completed += ok
            failed += bad
            const d = dp[g.ip] || { total: 0, completed: 0, failed: 0 }
            // 组完成进度按计数折算（多文件设备不能置 100，否则下一组开始前进度条假满）；
            // 不带 stage：渲染层 done/hasError 由计数驱动，组间窗口带 'done' 会闪裸文案 "done…"
            const nc = d.completed + ok, nf = d.failed + bad
            dp[g.ip] = { ...d, completed: nc, failed: nf, currentProgress: Math.round((nc + nf) / Math.max(d.total, 1) * 100) }
            for (const x of r?.failed || []) {
              failedTargets.push({ deviceIp: g.ip, machineName: x.androidName, error: x.error, filePath: g.file.path })
            }
          } catch (err) {
            // 首容器上传失败等组级错误：整组计失败
            failed += g.androids.length
            const d = dp[g.ip] || { total: 0, completed: 0, failed: 0 }
            const nc = d.completed, nf = d.failed + g.androids.length
            dp[g.ip] = { ...d, completed: nc, failed: nf, currentProgress: Math.round((nc + nf) / Math.max(d.total, 1) * 100) }
            const msg = getErrorMessage(err)
            for (const a of g.androids) {
              failedTargets.push({ deviceIp: g.ip, machineName: a.name, error: msg, filePath: g.file.path })
            }
          }
          // 进行中的字节级进度仍由 file:upload-progress 事件维护，这里推进计数
          const pct = Math.round((completed + failed) / total * 100)
          updateTask(taskId, { progress: pct, completed, failed, status: 'running', deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
        }
      }))

      if (ac.signal.aborted) {
        updateTask(taskId, { status: 'canceled', endTime: Date.now(), deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
      } else {
        updateTask(taskId, { status: failed === 0 ? 'completed' : 'failed', progress: 100, completed, failed, endTime: Date.now(), deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
      }
      unregisterTaskAbort(taskId)
    }, 0)
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-xl shadow-2xl w-[640px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        {/* 标题栏 */}
        <div className="px-6 py-4 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-base font-semibold text-gray-800">{t('host:batchUpload.title')}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-md p-1 transition-colors">
            <X size={18} />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto scrollbar-thin p-6 space-y-5">
          {/* 所选云机 */}
          <div>
            <p className="text-xs font-medium text-gray-500 mb-2">{t('host:batchUpload.selectedMachines', { count: selectedAndroids.length })}</p>
            <div className="flex flex-wrap gap-1.5">
              {selectedAndroids.map(a => (
                <span key={a.id} className="text-xs bg-white text-gray-700 border border-gray-200 px-2 py-1 rounded-md">{notes[a.id] || a.name}</span>
              ))}
            </div>
          </div>

          {/* 已选文件 */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <p className="text-xs font-medium text-gray-500">{t('host:batchUpload.selectedFilesCount', { count: files.length })}</p>
              {files.length > 0 && (
                <button onClick={handleClear} className="inline-flex items-center gap-1 h-6 px-2 text-xs rounded-md text-gray-500 hover:text-red-500 hover:bg-red-50 transition-colors cursor-pointer">
                  <Trash2 size={11} />
                  {t('button.clearAll')}
                </button>
              )}
            </div>
            <div id="batch-upload-dropzone" data-file-drop-target>
            {files.length > 0 ? (
              <div className="border border-gray-100 rounded-lg divide-y divide-gray-50 max-h-[260px] overflow-y-auto scrollbar-thin">
                {files.map(f => (
                  <div key={f.path} className="flex items-center gap-2.5 px-3 py-2 group hover:bg-gray-50 transition-colors">
                    <span className="text-[10px] font-medium text-gray-400 shrink-0 w-10 text-center bg-gray-100 rounded px-1 py-0.5">{f.ext.replace('.', '').toUpperCase() || 'FILE'}</span>
                    <span className="text-xs text-gray-700 truncate flex-1" title={f.name}>{f.name}</span>
                    <span className="text-xs text-gray-400 shrink-0">{formatSize(f.size)}</span>
                    <button onClick={() => handleRemove(f.path)} className="shrink-0 text-gray-300 hover:text-red-500 transition-colors cursor-pointer opacity-0 group-hover:opacity-100">
                      <X size={14} />
                    </button>
                  </div>
                ))}
              </div>
            ) : (
              <button onClick={handlePickFiles} disabled={picking} className="w-full border-2 border-dashed border-gray-200 rounded-lg p-8 text-center hover:border-blue-300 hover:bg-blue-50/30 transition-colors cursor-pointer disabled:opacity-50">
                <FileArchive size={28} className="mx-auto text-gray-300 mb-2" />
                <p className="text-xs text-gray-500 mb-1">{picking ? t('host:batchUpload.picking') : t('host:batchUpload.clickToSelect')}</p>
                <p className="text-[11px] text-gray-400">{t('host:batchUpload.supportFormats')}</p>
              </button>
            )}
            </div>
            {files.length > 0 && (
              <button onClick={handlePickFiles} disabled={picking} className="mt-2 inline-flex items-center gap-1 h-7 px-3 text-xs rounded-md border border-gray-200 bg-white text-gray-600 hover:bg-gray-50 hover:border-gray-300 transition-colors cursor-pointer disabled:opacity-40">
                <Plus size={12} />
                {t('host:batchUpload.addMore')}
              </button>
            )}
          </div>

          {/* APK 运行时权限自动授予（仅 .apk 生效；.apks/.xapk 不支持置灰） */}
          <label className={`flex items-center gap-2 text-xs ${hasApk ? 'text-gray-600 cursor-pointer' : 'text-gray-300 cursor-not-allowed'}`}>
            <input type="checkbox" checked={autoGrant && hasApk} disabled={!hasApk}
              onChange={e => setAutoGrant(e.target.checked)} className="size-3.5 accent-blue-500" />
            <span>
              {t('host:batchUpload.autoGrant')}
              {hasNonApkInstallable && <span className="text-gray-400 ml-1">{t('host:batchUpload.autoGrantOnlyApk')}</span>}
            </span>
          </label>

          {/* 提示 */}
          <div className="flex items-start gap-2 text-xs text-gray-500 bg-gray-50 border border-gray-100 rounded-lg p-3">
            <Info size={14} className="text-gray-400 shrink-0 mt-0.5" />
            <div className="space-y-0.5">
              <p><span className="font-medium text-gray-600">.apk</span> {t('host:batchUpload.uploadHint2')} ｜ <span className="font-medium text-gray-600">.apks / .xapk</span> {t('host:batchUpload.uploadHint3')}</p>
              <p>{t('host:batchUpload.uploadHint4')}</p>
            </div>
          </div>
        </div>

        {/* 底部按钮 */}
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="h-8 px-4 text-sm rounded-md border border-gray-200 bg-white text-gray-600 hover:bg-gray-50 hover:border-gray-300 transition-colors">{t('button.cancel')}</button>
          <button onClick={handleUpload} disabled={files.length === 0}
            className="h-8 px-4 text-sm rounded-md bg-blue-500 text-white hover:bg-blue-600 transition-colors disabled:opacity-40 disabled:hover:bg-blue-500"
          >{t('button.upload')}</button>
        </div>
      </div>
    </div>
  )
}

export default BatchUploadModal
