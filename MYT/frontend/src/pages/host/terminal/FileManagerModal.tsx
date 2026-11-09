import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Cloud, Monitor } from 'lucide-react'
import * as FileService from '../../../../bindings/github.com/moyunteng/myt/fileservice'
import { CloudFileEntry } from '../../../../bindings/github.com/moyunteng/myt/internal/mytos/models'
import { LocalFileEntry } from '../../../../bindings/github.com/moyunteng/myt/models'
import { showToast } from '../../../components/Toast'
import { isMac } from '../../../utils/platform'
import { BreadcrumbBar, FileList } from './FileManagerComponents'
import FileManagerProgress from './FileManagerProgress'
import { useModalEnter } from '../../../hooks/useModalAnimation'
import { CLOUD_PANE_DROP_ID, useFileManagerTransfer } from './useFileManagerTransfer'

interface Props {
  deviceIP: string
  androidName: string
  displayName?: string
  onClose: () => void
}

function FileManagerModal({ deviceIP, androidName, displayName, onClose }: Props) {
  const { t } = useTranslation()
  // 云机侧
  const [cloudPath, setCloudPath] = useState('/sdcard')
  const [cloudFiles, setCloudFiles] = useState<CloudFileEntry[]>([])
  const [cloudLoading, setCloudLoading] = useState(false)

  // 本地侧
  const [localPath, setLocalPath] = useState('')
  const [localFiles, setLocalFiles] = useState<LocalFileEntry[]>([])
  const [localLoading, setLocalLoading] = useState(false)

  // 选中集（索引）：提升到 Modal 层，供 Ctrl+C / 拖拽使用
  const [cloudSelected, setCloudSelected] = useState<Set<number>>(new Set())
  const [localSelected, setLocalSelected] = useState<Set<number>>(new Set())
  // 「安装后自动授予运行时权限」勾选（仅对 .apk 生效）
  const [autoGrant, setAutoGrant] = useState(false)

  const cloudPaneRef = useRef<HTMLDivElement>(null)
  const localPaneRef = useRef<HTMLDivElement>(null)

  // 加载云机文件列表
  const loadCloudFiles = useCallback(async (dir: string) => {
    setCloudLoading(true)
    try {
      const files = await FileService.ListCloudFiles(deviceIP, androidName, dir)
      setCloudFiles(files || [])
    } catch (err) {
      showToast(t('host:terminal.fileManager.loadingCloudFailed', { error: String(err) }), 'error')
      setCloudFiles([])
    } finally {
      setCloudLoading(false)
    }
  }, [deviceIP, androidName, t])

  // 加载本地文件列表
  const loadLocalFiles = useCallback(async (dir: string) => {
    setLocalLoading(true)
    try {
      const files = await FileService.ListLocalDir(dir)
      setLocalFiles(files || [])
    } catch (err) {
      showToast(t('host:terminal.fileManager.loadingLocalFailed', { error: String(err) }), 'error')
      setLocalFiles([])
    } finally {
      setLocalLoading(false)
    }
  }, [t])

  // 初始化
  useEffect(() => {
    loadCloudFiles(cloudPath)
    FileService.GetDefaultLocalDir().then(dir => {
      setLocalPath(dir)
      loadLocalFiles(dir)
    }).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 路径变化清空对应栏选中集（避免跨目录残留索引指向错误条目）
  useEffect(() => { setCloudSelected(new Set()) }, [cloudPath])
  useEffect(() => { setLocalSelected(new Set()) }, [localPath])

  // 传输编排（上传/下载/剪贴板/键盘/拖拽）抽 hook：纯逻辑无 UI，见 useFileManagerTransfer.ts
  const transfer = useFileManagerTransfer({
    deviceIP, androidName, autoGrant, cloudPath, localPath,
    cloudFiles, localFiles, cloudSelected, localSelected,
    setCloudSelected, setLocalSelected,
    loadCloudFiles, loadLocalFiles,
  })

  // 本地选择目录
  const handlePickFolder = async () => {
    try {
      const dir = await FileService.SelectDirectory(localPath)
      if (dir) {
        setLocalPath(dir)
        loadLocalFiles(dir)
      }
    } catch (err) {
      // 用户取消不报错
    }
  }

  // 导航云机目录
  const navigateCloud = (dir: string) => {
    setCloudPath(dir)
    loadCloudFiles(dir)
  }

  // 导航本地目录
  const navigateLocal = (dir: string) => {
    setLocalPath(dir)
    loadLocalFiles(dir)
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  // 栏间拖放高亮（fm-drop-active）由各栏 onDragEnter/Leave/End 维护——拖拽数据处理在 hook
  const dropActive = (ref: React.RefObject<HTMLDivElement | null>, on: boolean) => {
    ref.current?.classList[on ? 'add' : 'remove']('fm-drop-active')
  }
  const paneDragEnter = (ref: React.RefObject<HTMLDivElement | null>) => (e: React.DragEvent) => {
    if (e.dataTransfer.types.includes('application/x-fm-items')) dropActive(ref, true)
  }
  const paneDragLeave = (ref: React.RefObject<HTMLDivElement | null>) => (e: React.DragEvent) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node)) dropActive(ref, false)
  }
  const paneDragEnd = (ref: React.RefObject<HTMLDivElement | null>) => () => dropActive(ref, false)

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onKeyDown={transfer.onKeyDown} tabIndex={-1}>
      <div className="bg-white rounded-xl shadow-2xl w-[860px] h-[70vh] flex flex-col overflow-hidden modal-content" onClick={e => e.stopPropagation()}>
        {/* 标题栏：加粗主标题（i18n title 含设备名），关闭按钮补 hover 底色 */}
        <div className="px-4 py-3 border-b border-gray-100 flex items-center justify-between gap-3">
          <div className="min-w-0">
            <h3 className="text-sm font-semibold text-gray-800">{t('host:terminal.fileManager.title', { name: displayName || androidName })}</h3>
          </div>
          <button onClick={onClose} className="shrink-0 size-7 flex items-center justify-center rounded text-gray-400 hover:text-gray-600 hover:bg-gray-100 transition-colors" aria-label={t('host:terminal.fileManager.close')}>×</button>
        </div>

        {/* 双栏内容：左=本机，右=云机（WinSCP 范式） */}
        <div className="flex-1 flex min-h-0">
          {/* 左侧：本机 */}
          <div className="flex-1 min-w-0 flex flex-col border-r border-gray-100" data-fm-side="local"
            ref={localPaneRef}
            onDragOver={transfer.onLocalPaneDragOver} onDrop={transfer.onLocalPaneDrop}
            onDragEnter={paneDragEnter(localPaneRef)}
            onDragLeave={paneDragLeave(localPaneRef)}
            onDragEnd={paneDragEnd(localPaneRef)}>
            <div className="px-3 py-2 bg-gray-50 border-b border-gray-100">
              <span className="flex items-center gap-1.5 text-xs font-semibold text-gray-700">
                <Monitor className="size-3.5 text-gray-400" />
                {t('host:terminal.fileManager.localMachine', { os: isMac ? 'macOS' : 'Windows' })}
              </span>
            </div>
            <div className="px-2 py-1.5 border-b border-gray-50">
              <BreadcrumbBar path={localPath} onNavigate={navigateLocal} onRefresh={() => loadLocalFiles(localPath)} onPickFolder={handlePickFolder} />
            </div>
            <FileList items={localFiles} loading={localLoading}
              onOpen={(f) => f.isDir && navigateLocal(f.path!)}
              selected={localSelected} onSelectionChange={setLocalSelected}
              buildDragData={transfer.localDragData} />
          </div>

          {/* 右侧：云机（带 data-file-drop-target：系统文件拖入上传） */}
          <div className="flex-1 min-w-0 flex flex-col" data-fm-side="cloud"
            id={CLOUD_PANE_DROP_ID} data-file-drop-target
            ref={cloudPaneRef}
            onDragOver={transfer.onCloudPaneDragOver} onDrop={transfer.onCloudPaneDrop}
            onDragEnter={paneDragEnter(cloudPaneRef)}
            onDragLeave={paneDragLeave(cloudPaneRef)}
            onDragEnd={paneDragEnd(cloudPaneRef)}>
            <div className="px-3 py-2 bg-gray-50 border-b border-gray-100">
              <span className="flex items-center gap-1.5 text-xs font-semibold text-gray-700">
                <Cloud className="size-3.5 text-indigo-500" />
                {t('host:terminal.fileManager.cloudMachine')}
              </span>
            </div>
            <div className="px-2 py-1.5 border-b border-gray-50">
              <BreadcrumbBar path={cloudPath} onNavigate={navigateCloud} onRefresh={() => loadCloudFiles(cloudPath)} />
            </div>
            <FileList items={cloudFiles} loading={cloudLoading}
              onOpen={(f) => f.flag && navigateCloud(f.file!)}
              selected={cloudSelected} onSelectionChange={setCloudSelected}
              buildDragData={transfer.cloudDragData} />
          </div>
        </div>

        {/* 传输进度区：进行中进度条 + 终态汇总/失败明细（数据源：任务队列） */}
        <FileManagerProgress deviceIP={deviceIP} />

        {/* 底部状态栏：autoGrant 开关 + 提示文字 + 关闭（右对齐） */}
        <div className="flex items-center gap-3 px-4 py-2 bg-gray-50 border-t border-gray-100">
          <label className={`flex items-center gap-1.5 text-xs shrink-0 cursor-pointer transition-colors ${autoGrant ? 'text-gray-700' : 'text-gray-500'}`}
            title={t('host:terminal.fileManager.autoGrantTip')}>
            <input type="checkbox" checked={autoGrant} onChange={e => setAutoGrant(e.target.checked)}
              className="size-3.5 accent-indigo-500" />
            {t('host:terminal.fileManager.autoGrant')}
          </label>
          <span className="text-[11px] text-gray-400 truncate flex-1 text-right">{t('host:terminal.fileManager.transferHint')}</span>
          <button onClick={onClose}
            className="px-4 py-1.5 text-xs font-medium bg-white text-gray-600 border border-gray-200 rounded-md hover:bg-gray-100 hover:text-gray-800 transition-colors shrink-0">
            {t('host:terminal.fileManager.close')}
          </button>
        </div>
      </div>
    </div>
  )
}

export default FileManagerModal
