import { useState, useEffect, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Folder, FolderOpen, FileImage, FileVideo, FileAudio,
  FileArchive, FileText, File, Package, RotateCw, ChevronLeft,
  type LucideIcon,
} from 'lucide-react'

type IconInfo = { Icon: LucideIcon; className: string }

/** 文件类型分色：文件夹 amber、图片 violet、视频 rose、音频 sky、apk emerald、压缩包 orange、文本/普通 gray（Tailwind-500 级，深浅克制） */
export function fileIcon(isDir: boolean, name: string): IconInfo {
  if (isDir) return { Icon: Folder, className: 'text-amber-500' }
  const ext = name.split('.').pop()?.toLowerCase() || ''
  if (['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp'].includes(ext)) return { Icon: FileImage, className: 'text-violet-500' }
  if (['mp4', 'avi', 'mkv', 'mov', 'wmv', 'flv', 'webm', '3gp'].includes(ext)) return { Icon: FileVideo, className: 'text-rose-500' }
  if (['mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a'].includes(ext)) return { Icon: FileAudio, className: 'text-sky-500' }
  if (ext === 'apk') return { Icon: Package, className: 'text-emerald-500' }
  if (['zip', 'rar', '7z', 'tar', 'gz'].includes(ext)) return { Icon: FileArchive, className: 'text-orange-500' }
  if (['txt', 'log', 'md', 'json', 'xml', 'csv'].includes(ext)) return { Icon: FileText, className: 'text-gray-500' }
  return { Icon: File, className: 'text-gray-400' }
}

/** 文件大小格式化（字节 → B/KB/MB/GB） */
export function formatSize(bytes: number): string {
  if (bytes <= 0) return '—'
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)}MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)}GB`
}

/** 计算上级路径 */
export function parentPath(p: string): string {
  const isWindows = /^[A-Za-z]:/.test(p)
  if (isWindows) {
    const parts = p.split('\\').filter(Boolean)
    if (parts.length <= 1) return parts[0] + '\\'
    const result = parts.slice(0, -1).join('\\')
    return /^[A-Za-z]:$/.test(result) ? result + '\\' : result
  }
  const parts = p.split('/').filter(Boolean)
  if (parts.length === 0) return '/'
  return '/' + parts.slice(0, -1).join('/')
}

/** 根据面包屑段构建路径 */
export function buildPath(segments: string[], upTo: number, isWindows: boolean): string {
  if (isWindows) return segments.slice(0, upTo + 1).join('\\') + (upTo === 0 ? '\\' : '')
  return '/' + segments.slice(0, upTo + 1).join('/')
}

/** 图标按钮：统一尺寸 + hover 态，替代裸 emoji/字符按钮 */
function IconButton({ onClick, title, children }: { onClick: () => void; title: string; children: ReactNode }) {
  return (
    <button onClick={onClick} title={title}
      className="shrink-0 size-7 flex items-center justify-center rounded text-gray-400 hover:text-gray-700 hover:bg-gray-100 transition-colors cursor-pointer">
      {children}
    </button>
  )
}

/** 路径导航栏 */
export function BreadcrumbBar({ path, onNavigate, onRefresh, onPickFolder }: {
  path: string
  onNavigate: (p: string) => void
  onRefresh: () => void
  onPickFolder?: () => void
}) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [editPath, setEditPath] = useState(path)

  useEffect(() => { setEditPath(path) }, [path])

  const segments = path.split(/[/\\]/).filter(Boolean)
  const isWindows = /^[A-Za-z]:/.test(path)
  const sep = isWindows ? '\\' : '/'

  if (editing) {
    return (
      <div className="flex items-center gap-1">
        <input type="text" value={editPath} onChange={e => setEditPath(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') { setEditing(false); onNavigate(editPath) } if (e.key === 'Escape') setEditing(false) }}
          onBlur={() => { setEditing(false) }}
          className="flex-1 text-xs px-1.5 py-1 border border-blue-300 rounded outline-none focus:border-blue-500 bg-white font-mono" autoFocus />
      </div>
    )
  }

  return (
    <div className="flex items-center gap-1 min-w-0">
      <IconButton onClick={() => onNavigate(parentPath(path))} title={t('host:terminal.fileManager.goUp')}>
        <ChevronLeft className="size-4" />
      </IconButton>
      <div className="flex items-center gap-0.5 min-w-0 overflow-x-auto scrollbar-hide text-xs" onClick={() => setEditing(true)} title={t('host:terminal.fileManager.clickToEdit')}>
        {segments.map((seg, i) => (
          <span key={i} className="flex items-center shrink-0">
            {i > 0 && <span className="text-gray-300 mx-0.5">{sep}</span>}
            <button onClick={e => { e.stopPropagation(); onNavigate(buildPath(segments, i, isWindows)) }}
              className="text-gray-600 hover:text-blue-600 hover:underline truncate max-w-[80px]">{seg}</button>
          </span>
        ))}
      </div>
      <div className="flex items-center gap-0.5 shrink-0 ml-auto">
        <IconButton onClick={onRefresh} title={t('host:terminal.fileManager.refresh')}>
          <RotateCw className="size-4" />
        </IconButton>
        {onPickFolder && (
          <IconButton onClick={onPickFolder} title={t('host:terminal.fileManager.selectDir')}>
            <FolderOpen className="size-4" />
          </IconButton>
        )}
      </div>
    </div>
  )
}

/** 统一文件条目：合并 LocalFileEntry / CloudFileEntry 的字段访问 */
export interface FileItem {
  name: string
  isDir?: boolean
  flag?: boolean
  size?: number
  length?: number
  /** 本地条目路径 */
  path?: string
  /** 云机条目路径 */
  file?: string
}

/** 文件列表组件：多选（点击单选/Ctrl 追加切换/Shift 范围）+ draggable 供栏间拖拽传输 */
export function FileList<T extends FileItem>({
  items, loading, onOpen, selected, onSelectionChange, buildDragData
}: {
  items: T[]
  loading: boolean
  onOpen: (item: T) => void
  /** 选中索引集（提升到 Modal 层，供 Ctrl+C / 拖拽组装传输载荷） */
  selected: Set<number>
  onSelectionChange: (next: Set<number>) => void
  /** 拖拽载荷构建：Modal 层按当前选中集组装；行不在选中集时以该行为唯一载荷 */
  buildDragData: (fallback?: T) => string
}) {
  const { t } = useTranslation()
  const lastClickedRef = useRef(-1)

  if (loading) return (
    <div className="flex-1 flex flex-col items-center justify-center gap-1.5 py-8">
      <RotateCw className="size-5 text-gray-300 animate-spin" />
      <span className="text-xs text-gray-400">{t('host:terminal.fileManager.loading')}</span>
    </div>
  )
  if (items.length === 0) return (
    <div className="flex-1 flex flex-col items-center justify-center gap-1.5 py-8">
      <FolderOpen className="size-7 text-gray-200" />
      <span className="text-xs text-gray-300">{t('host:terminal.fileManager.emptyDir')}</span>
    </div>
  )

  const handleClick = (e: React.MouseEvent, i: number, isDir: boolean) => {
    if (e.ctrlKey || e.metaKey) {
      // Ctrl+click：追加切换
      const next = new Set(selected)
      if (next.has(i)) next.delete(i)
      else next.add(i)
      onSelectionChange(next)
      lastClickedRef.current = i
      return
    }
    if (e.shiftKey && lastClickedRef.current >= 0) {
      // Shift+click：范围选择
      const [a, b] = [Math.min(lastClickedRef.current, i), Math.max(lastClickedRef.current, i)]
      const next = new Set<number>()
      for (let j = a; j <= b; j++) next.add(j)
      onSelectionChange(next)
      return
    }
    // 单击：单选；目录单击保持导航语义
    onSelectionChange(new Set([i]))
    lastClickedRef.current = i
    if (isDir) onOpen(items[i])
  }

  return (
    <div className="flex-1 overflow-y-auto scrollbar-thin">
      {items.map((item, i) => {
        const isDir = item.flag ?? item.isDir ?? false
        const isSelected = selected.has(i)
        const bytes = item.size ?? item.length ?? 0
        const { Icon, className } = fileIcon(!!isDir, item.name)
        return (
          <div key={i}
            className={`flex items-center gap-2 h-8 px-2.5 cursor-pointer transition-colors ${
              isSelected
                ? 'bg-indigo-50/80 ring-1 ring-inset ring-indigo-200 rounded-sm'
                : 'hover:bg-gray-50'
            }`}
            onClick={e => handleClick(e, i, !!isDir)}
            draggable
            onDragStart={e => {
              // 拖拽载荷：来源栏 + 选中集；该行未选中时以该行为唯一载荷（fallback）
              const payload = selected.has(i) ? buildDragData() : buildDragData(items[i])
              e.dataTransfer.setData('application/x-fm-items', payload)
              e.dataTransfer.effectAllowed = 'copy'
            }}>
            <Icon className={`size-4 shrink-0 ${className}`} />
            <span className={`text-[13px] truncate flex-1 ${isDir ? 'font-medium text-gray-700' : 'text-gray-600'}`} title={item.name}>{item.name}</span>
            <span className="text-[11px] text-gray-400 shrink-0 tabular-nums w-16 text-right">{isDir ? '—' : formatSize(bytes)}</span>
          </div>
        )
      })}
    </div>
  )
}
