import { useState, useRef } from 'react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Pencil, Trash2 } from 'lucide-react'
import { useDropdownEnter } from '../../../hooks/useModalAnimation'

interface GroupTreeSectionProps {
  title: string
  count: number
  isExpanded: boolean
  isUngrouped?: boolean
  isDragOver: boolean
  onToggleExpand: () => void
  onDragOver: (e: React.DragEvent) => void
  onDragLeave: () => void
  onDrop: (e: React.DragEvent) => void
  onRename?: (name: string) => void
  onDelete?: () => void
  /** 三态勾选（云机 tab 分组全选）：仅当 onToggleSelectGroup 回调传入时才渲染 checkbox */
  checked?: boolean
  indeterminate?: boolean
  onToggleSelectGroup?: () => void
  children?: ReactNode
}

/**
 * 分组树的分组标题行：展开/折叠箭头、分组名、设备数，以及删除/重命名按钮。
 * 整个区块是拖拽放置目标（拖入分组 = 加入，拖入未分组 = 移出）。
 */
function GroupTreeSection({
  title, count, isExpanded, isUngrouped, isDragOver,
  onToggleExpand, onDragOver, onDragLeave, onDrop, onRename, onDelete,
  checked, indeterminate, onToggleSelectGroup, children,
}: GroupTreeSectionProps) {
  const { t } = useTranslation()
  const [renaming, setRenaming] = useState(false)
  const [draft, setDraft] = useState('')
  const panelRef = useRef<HTMLDivElement>(null)
  useDropdownEnter(panelRef, isExpanded)

  const startRename = () => { setDraft(title); setRenaming(true) }

  const saveRename = () => {
    const name = draft.trim()
    setRenaming(false)
    if (name && name !== title && onRename) onRename(name)
  }

  const cancelRename = () => setRenaming(false)

  return (
    <div onDragOver={onDragOver} onDragLeave={onDragLeave} onDrop={onDrop}>
      <div
        className={`group relative flex items-center gap-1.5 px-3 py-2.5 cursor-pointer select-none transition-colors border-b ${isUngrouped ? 'border-gray-200' : 'border-gray-100'} bg-gray-50/70 ${
          isDragOver ? 'bg-blue-50 ring-1 ring-blue-300 ring-inset' : 'hover:bg-gray-100/60'
        }`}
        onClick={onToggleExpand}
        role="button"
        aria-expanded={isExpanded}
      >
        <svg
          className={`w-3 h-3 text-gray-400 transition-transform shrink-0 ${isExpanded ? 'rotate-90' : ''}`}
          viewBox="0 0 12 12" fill="currentColor"
        >
          <path d="M4 2l4 4-4 4z"/>
        </svg>
        {onToggleSelectGroup !== undefined && checked !== undefined && indeterminate !== undefined && (
          <input
            type="checkbox"
            checked={checked}
            // 三态半选：受控 checked 之外由 ref 设 indeterminate（DOM 属性无对应 React prop），
            // 写法复用 DeviceListItem 的设备行勾选框
            ref={(el) => { if (el) el.indeterminate = indeterminate }}
            readOnly
            aria-checked={indeterminate ? 'mixed' : checked}
            aria-label={t('a11y.selectGroup', { name: title })}
            className="w-3 h-3 rounded border-gray-300 text-blue-500 focus:ring-blue-400 shrink-0"
            // 阻断冒泡：标题行 onClick 是展开/折叠，勾选操作不能触发折叠
            onClick={(e) => e.stopPropagation()}
            onChange={() => onToggleSelectGroup?.()}
          />
        )}
        {renaming ? (
          <input
            type="text"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={saveRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
              if (e.key === 'Escape') cancelRename()
            }}
            onClick={(e) => e.stopPropagation()}
            autoFocus
            className="flex-1 min-w-0 text-xs px-1 py-0.5 border border-blue-300 rounded outline-none focus:border-blue-500 bg-white"
          />
        ) : (
          <span className={`text-xs font-semibold truncate flex-1 ${isUngrouped ? 'text-gray-500' : 'text-gray-600'}`}>
            {title}
          </span>
        )}
        {/* hover 时隐藏徽章保占位：右侧绝对定位的编辑/删除按钮正好落在此处，
            不隐藏会互相叠压；改名态同理（保占位 → 布局零跳动） */}
        <span className="text-[10px] text-gray-500 bg-gray-200 px-1.5 rounded-full shrink-0 group-hover:opacity-0 transition-opacity">{count}</span>
        {/* 改名输入态隐藏按钮容器：absolute 定位会覆盖在输入框上；renaming 时不渲染（退出编辑态恢复） */}
        {!isUngrouped && !renaming && (
          <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
            <button
              className="text-gray-400 hover:text-blue-500 p-0.5"
              onClick={(e) => { e.stopPropagation(); startRename() }}
              title={t('groupcontrol:renameGroup')}
              aria-label={t('groupcontrol:renameGroup')}
            >
              <Pencil size={12} />
            </button>
            <button
              className="text-gray-400 hover:text-red-500 p-0.5"
              onClick={(e) => { e.stopPropagation(); onDelete?.() }}
              title={t('groupcontrol:deleteGroup')}
              aria-label={t('groupcontrol:deleteGroup')}
            >
              <Trash2 size={12} />
            </button>
          </div>
        )}
      </div>
      {isExpanded && <div ref={panelRef}>{children}</div>}
    </div>
  )
}

export default GroupTreeSection
