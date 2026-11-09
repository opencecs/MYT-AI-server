import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Events } from '@wailsio/runtime'
import * as FileService from '../../../../bindings/github.com/moyunteng/myt/fileservice'
import { CloudFileEntry } from '../../../../bindings/github.com/moyunteng/myt/internal/mytos/models'
import { LocalFileEntry } from '../../../../bindings/github.com/moyunteng/myt/models'
import { showToast } from '../../../components/Toast'
import { getErrorMessage } from '../../../utils/error'
import { isMac } from '../../../utils/platform'
import { useTaskQueue, registerTaskAbort, unregisterTaskAbort } from '../../../hooks/taskQueue/useTaskQueue'
import type { DeviceProgress } from '../../../hooks/taskQueue/types'
import type { ClipboardItem, Side } from './FileManagerTypes'

/** 窗口文件拖拽事件载荷（Go 端 registerFileDropHandler emit，见 setup_filedrop.go） */
interface DroppedFilesPayload {
  targetId: string
  entries: { name: string; path: string }[]
}

/** 云机栏原生拖放落点 id（Go 端 WindowFilesDropped 按 ElementID 回传 targetId） */
export const CLOUD_PANE_DROP_ID = 'file-manager-cloud-pane'

/** FileList 行 HTML5 拖拽载荷（buildDragData 的 fallback 行条目形状） */
export interface FileItemLike {
  name: string
  path?: string
  isDir?: boolean
}

interface UseFileManagerTransferParams {
  deviceIP: string
  androidName: string
  /** 「安装后自动授予运行时权限」勾选（仅对 .apk 生效） */
  autoGrant: boolean
  cloudPath: string
  localPath: string
  cloudFiles: CloudFileEntry[]
  localFiles: LocalFileEntry[]
  cloudSelected: Set<number>
  localSelected: Set<number>
  setCloudSelected: (s: Set<number>) => void
  setLocalSelected: (s: Set<number>) => void
  /** 传输完成后刷新对应栏（上传刷云机栏 / 下载刷本地栏） */
  loadCloudFiles: (dir: string) => void
  loadLocalFiles: (dir: string) => void
}

/**
 * 文件管理器传输编排 hook（从 FileManagerModal 拆出，纯逻辑无 UI）：
 * - 上传/下载任务编排（任务队列：addTask + abort 注册 + 逐文件循环 + 进度推送）
 * - 剪贴板（跨栏复制/粘贴统一载荷）与统一粘贴分派
 * - 键盘快捷键 Ctrl+A/C/V（面包屑输入框聚焦时不拦截）
 * - 窗口系统文件拖入（files-dropped 事件 → 上传）
 * - 栏间 HTML5 拖拽（dragstart/dragover/drop）
 */
export function useFileManagerTransfer(p: UseFileManagerTransferParams) {
  const { t } = useTranslation()
  const { addTask, updateTask } = useTaskQueue()
  const { deviceIP, androidName, autoGrant, cloudPath, localPath } = p

  // 剪贴板：来源栏 + 条目集（由 Modal 持有状态，本 hook 通过回调写入）
  const [clipboard, setClipboard] = useState<{ side: Side; items: ClipboardItem[] } | null>(null)

  // 上传编排：本地文件集 → 云机当前目录
  // 复用 BatchUploadModal 的任务队列模式：addTask 建任务 + registerTaskAbort 取消支持 + 循环 UploadToCloud
  const startUpload = useCallback(async (files: { name: string; path: string }[]) => {
    if (files.length === 0) return
    const total = files.length
    const dpInit: Record<string, DeviceProgress> = { [deviceIP]: { total, completed: 0, failed: 0 } }
    const taskId = await addTask({
      type: 'uploadFile',
      targets: files.map(f => ({ deviceIp: deviceIP, machines: [{ name: androidName }], filePath: f.path })),
      deviceIps: [deviceIP],
      total,
      fileCount: total,
      machineCount: 1,
      deviceProgress: dpInit,
    })
    const ac = new AbortController()
    registerTaskAbort(taskId, ac)
    updateTask(taskId, { status: 'running', startTime: Date.now(), deviceProgress: dpInit })
    showToast(t('host:terminal.fileManager.taskAdded', { count: total }), 'success')

    let completed = 0, failed = 0
    const failedTargets: Array<{ deviceIp: string; machineName?: string; error: string; filePath?: string }> = []
    const dp = { ...dpInit }
    for (const f of files) {
      if (ac.signal.aborted) break
      try {
        const ext = f.name.split('.').pop()?.toLowerCase() || ''
        const isApk = ext === 'apk'
        await FileService.UploadToCloud(deviceIP, androidName, f.path, autoGrant && isApk, cloudPath)
        completed++
      } catch (err) {
        failed++
        failedTargets.push({ deviceIp: deviceIP, machineName: androidName, error: getErrorMessage(err), filePath: f.path })
      }
      dp[deviceIP] = { ...dp[deviceIP], total, completed, failed }
      const pct = Math.round((completed + failed) / total * 100)
      updateTask(taskId, { progress: pct, completed, failed, status: 'running', deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
    }
    if (ac.signal.aborted) {
      updateTask(taskId, { status: 'canceled', endTime: Date.now(), deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
    } else {
      updateTask(taskId, { status: failed === 0 ? 'completed' : 'failed', progress: 100, completed, failed, endTime: Date.now(), deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
    }
    unregisterTaskAbort(taskId)
    p.loadCloudFiles(cloudPath)
  }, [deviceIP, androidName, autoGrant, cloudPath, addTask, updateTask, p.loadCloudFiles, t])

  // 下载编排：云机条目集 → 本地当前目录（无字节级进度，按文件计数）
  const startDownload = useCallback(async (items: ClipboardItem[]) => {
    if (items.length === 0) return
    const total = items.length
    const dpInit: Record<string, DeviceProgress> = { [deviceIP]: { total, completed: 0, failed: 0 } }
    const taskId = await addTask({
      type: 'downloadImage',
      targets: items.map(it => ({ deviceIp: deviceIP, filePath: it.path })),
      deviceIps: [deviceIP],
      total,
      deviceProgress: dpInit,
      fileManagerDownload: true,
    })
    const ac = new AbortController()
    registerTaskAbort(taskId, ac)
    updateTask(taskId, { status: 'running', startTime: Date.now(), deviceProgress: dpInit })
    showToast(t('host:terminal.fileManager.taskAdded', { count: total }), 'success')

    let completed = 0, failed = 0
    const failedTargets: Array<{ deviceIp: string; machineName?: string; error: string; filePath?: string }> = []
    const dp = { ...dpInit }
    for (const it of items) {
      if (ac.signal.aborted) break
      try {
        if (it.isDir) {
          await FileService.DownloadCloudFolder(deviceIP, androidName, it.path, localPath)
        } else {
          await FileService.DownloadCloudFile(deviceIP, androidName, it.path, localPath)
        }
        completed++
      } catch (err) {
        failed++
        failedTargets.push({ deviceIp: deviceIP, machineName: androidName, error: getErrorMessage(err), filePath: it.path })
      }
      dp[deviceIP] = { ...dp[deviceIP], total, completed, failed }
      const pct = Math.round((completed + failed) / total * 100)
      updateTask(taskId, { progress: pct, completed, failed, status: 'running', deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
    }
    if (ac.signal.aborted) {
      updateTask(taskId, { status: 'canceled', endTime: Date.now(), deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
    } else {
      updateTask(taskId, { status: failed === 0 ? 'completed' : 'failed', progress: 100, completed, failed, endTime: Date.now(), deviceProgress: { ...dp }, failedTargets: [...failedTargets] })
    }
    unregisterTaskAbort(taskId)
    p.loadLocalFiles(localPath)
  }, [deviceIP, androidName, localPath, addTask, updateTask, p.loadLocalFiles, t])

  // 本机条目 → 云机：文件夹不支持上传（UploadToCloud 只收文件路径），过滤并提示
  const pasteLocalToCloud = useCallback(async (items: ClipboardItem[]) => {
    const dirs = items.filter(it => it.isDir)
    const files = items.filter(it => !it.isDir)
    if (dirs.length > 0) {
      showToast(t('host:terminal.fileManager.folderUploadSkipped', { count: dirs.length }), 'warning')
    }
    await startUpload(files)
  }, [startUpload, t])

  // 云机条目 → 本机：文件夹走 DownloadCloudFolder 递归下载
  const pasteCloudToLocal = useCallback(async (items: ClipboardItem[]) => {
    await startDownload(items)
  }, [startDownload])

  // 统一粘贴入口：按剪贴板来源分派
  const doPaste = useCallback((clip: { side: Side; items: ClipboardItem[] }) => {
    if (clip.items.length === 0) return
    if (clip.side === 'cloud') pasteCloudToLocal(clip.items)
    else pasteLocalToCloud(clip.items)
  }, [pasteCloudToLocal, pasteLocalToCloud])

  // ---- 键盘：Ctrl+A / Ctrl+C / Ctrl+V（面包屑输入框聚焦时不拦截）----
  // 从某栏选中集组装 ClipboardItem 列表（CloudFileEntry 与 LocalFileEntry 字段不同：file/flag vs path/isDir）
  const collectItems = useCallback((s: Side): ClipboardItem[] => {
    const sel = s === 'cloud' ? p.cloudSelected : p.localSelected
    const files: (CloudFileEntry | LocalFileEntry)[] = s === 'cloud' ? p.cloudFiles : p.localFiles
    return [...sel].filter(i => i < files.length).map(i => {
      const f = files[i]
      if (s === 'cloud') {
        const c = f as CloudFileEntry
        return { name: c.name, path: c.file, isDir: !!c.flag }
      }
      const l = f as LocalFileEntry
      return { name: l.name, path: l.path, isDir: !!l.isDir }
    })
  }, [p.cloudSelected, p.localSelected, p.cloudFiles, p.localFiles])

  const onKeyDown = useCallback((e: React.KeyboardEvent) => {
    const target = e.target as HTMLElement
    if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
    const mod = isMac ? e.metaKey : e.ctrlKey
    if (!mod) return
    const key = e.key.toLowerCase()
    // 事件源所在栏（FileList 行 / 栏容器上都标了 data-fm-side，冒泡可取）
    const paneSide = (e.target as HTMLElement).closest('[data-fm-side]')?.getAttribute('data-fm-side') as Side | null
    if (key === 'a') {
      e.preventDefault()
      if (paneSide === 'cloud') p.setCloudSelected(new Set(p.cloudFiles.map((_, i) => i)))
      else if (paneSide === 'local') p.setLocalSelected(new Set(p.localFiles.map((_, i) => i)))
      else if (p.localSelected.size > 0 || p.cloudSelected.size === 0) p.setLocalSelected(new Set(p.localFiles.map((_, i) => i)))
      else p.setCloudSelected(new Set(p.cloudFiles.map((_, i) => i)))
    } else if (key === 'c') {
      // 复制：事件源栏优先；无明确来源时取有选中集的栏
      e.preventDefault()
      const copy = (s: Side) => {
        const items = collectItems(s)
        if (items.length === 0) return
        setClipboard({ side: s, items })
        showToast(t('host:terminal.fileManager.copied', { count: items.length }), 'success')
      }
      if (paneSide === 'cloud') copy('cloud')
      else if (paneSide === 'local') copy('local')
      else if (p.localSelected.size > 0) copy('local')
      else copy('cloud')
    } else if (key === 'v') {
      e.preventDefault()
      if (!clipboard) return
      // 粘贴到对侧：剪贴板来源为 cloud → 下载到本地；来源为 local → 上传到云机
      doPaste(clipboard)
      setClipboard(null)
    }
  }, [p, clipboard, doPaste, collectItems, t])

  // ---- 窗口文件拖入云机栏（Go 端 WindowFilesDropped → files-dropped 事件）----
  useEffect(() => {
    const off = Events.On('files-dropped', (ev: unknown) => {
      const payload = ((ev as { data?: DroppedFilesPayload })?.data ?? ev) as DroppedFilesPayload
      if (payload.targetId !== CLOUD_PANE_DROP_ID || !payload.entries?.length) return
      startUpload(payload.entries.map(en => ({ name: en.name, path: en.path })))
    })
    return () => { off() }
  }, [startUpload])

  // ---- 栏间 HTML5 拖拽：dragstart 记录来源栏与选中集，drop 到对侧触发传输 ----
  const onCloudPaneDragOver = useCallback((e: React.DragEvent) => {
    // 系统文件拖入由 Wails data-file-drop-target 处理（无 dataTransfer types Files 之外的判断需要）；
    // 栏间拖拽带自定义 MIME
    if (e.dataTransfer.types.includes('application/x-fm-items')) {
      e.preventDefault()
      e.dataTransfer.dropEffect = 'copy'
    }
  }, [])

  const onLocalPaneDragOver = useCallback((e: React.DragEvent) => {
    if (e.dataTransfer.types.includes('application/x-fm-items')) {
      e.preventDefault()
      e.dataTransfer.dropEffect = 'copy'
    }
  }, [])

  const onCloudPaneDrop = useCallback((e: React.DragEvent) => {
    const raw = e.dataTransfer.getData('application/x-fm-items')
    if (!raw) return // 系统文件拖入走 files-dropped 事件，这里不处理
    e.preventDefault()
    try {
      const clip = JSON.parse(raw) as { side: Side; items: ClipboardItem[] }
      if (clip.side === 'local') pasteLocalToCloud(clip.items)
      else if (clip.side === 'cloud') {
        // 云→云：同栏拖放忽略
        showToast(t('host:terminal.fileManager.samePaneDrop'), 'warning')
      }
    } catch { /* 非本组件的拖拽数据，忽略 */ }
  }, [pasteLocalToCloud, t])

  const onLocalPaneDrop = useCallback((e: React.DragEvent) => {
    const raw = e.dataTransfer.getData('application/x-fm-items')
    if (!raw) return
    e.preventDefault()
    try {
      const clip = JSON.parse(raw) as { side: Side; items: ClipboardItem[] }
      if (clip.side === 'cloud') pasteCloudToLocal(clip.items)
      else showToast(t('host:terminal.fileManager.samePaneDrop'), 'warning')
    } catch { /* 忽略 */ }
  }, [pasteCloudToLocal, t])

  // 栏间 HTML5 拖拽源数据：dragstart 由 FileList 行触发，按当前选中集组装载荷；
  // fallback 传行条目时以该行为唯一载荷（拖未选中行也能传输）
  const buildDragData = useCallback((side: Side, fallback?: FileItemLike) => {
    const items = fallback
      ? [{ name: fallback.name, path: fallback.path!, isDir: !!fallback.isDir }]
      : collectItems(side)
    return JSON.stringify({ side, items })
  }, [collectItems])

  const cloudDragData = useMemo(() => (item?: FileItemLike) => buildDragData('cloud', item), [buildDragData])
  const localDragData = useMemo(() => (item?: FileItemLike) => buildDragData('local', item), [buildDragData])

  return {
    onKeyDown,
    onCloudPaneDragOver, onLocalPaneDragOver, onCloudPaneDrop, onLocalPaneDrop,
    cloudDragData, localDragData,
  }
}
