import { useMemo, useRef, useState, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Check, ChevronDown, Tag } from 'lucide-react'
import { useDropdownEnter } from '../../../hooks/useModalAnimation'
import { tagStyle } from '../../../utils/tagColor'

interface Props {
  /** 可选标签（当前视图容器 tags 去重，已排序） */
  allTags: string[]
  /** 当前选中的标签：多选为「或」语义（命中任一即显示） */
  selected: Set<string>
  /** 切换某个标签的选中态 */
  onToggle: (tag: string) => void
  /** 全选（未全选时显示）/ 清空全部选中 */
  onSelectAll: () => void
  onClear: () => void
  /** 下拉打开时回调（每次从关到开触发）：宿主借此拉一次设备端标签字典刷新选项。 */
  onOpen?: () => void
}

/**
 * 标签下拉多选筛选（2026-10-27）：搜索框右侧的按钮 → 点击弹出浮层多选勾选。
 * 取代截图模式原先的平铺 chips 行（TagFilterBar）——标签多时平铺会挤满筛选行。
 *
 * 筛选语义与 TagFilterBar 一致：多选「或」（命中任一选中标签即保留），复用宿主的
 * selectedTags 状态；数据源为「当前视图容器」的 android.tags 去重（与所见同范围）。
 */
function TagFilterDropdown({ allTags, selected, onToggle, onSelectAll, onClear, onOpen }: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ top: 0, left: 0, width: 0 })

  // 点外面关闭（面板是 portal，必须同时监听 document 才能覆盖浮层外区域）
  useEffect(() => {
    if (!open) return
    const onDocClick = (e: MouseEvent) => {
      const target = e.target as Node
      if (rootRef.current?.contains(target)) return
      if (panelRef.current?.contains(target)) return
      setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', onDocClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDocClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const updatePos = () => {
    const rect = rootRef.current?.getBoundingClientRect()
    if (!rect) return
    setPos({ top: rect.bottom + 4, left: rect.left, width: rect.width })
  }

  const toggleOpen = () => {
    if (!open) {
      updatePos()
      onOpen?.()
    }
    setOpen(o => !o)
  }

  const allSelected = allTags.length > 0 && allTags.every(tag => selected.has(tag))

  // 浮层定位在打开瞬间 + 窗口变化时校正一次（不依赖位置 state 之外的东西）
  const panelStyle = useMemo(() => ({
    position: 'fixed' as const,
    top: pos.top,
    left: pos.left,
    minWidth: Math.max(pos.width, 180),
    maxWidth: 'min(320px, calc(100vw - 16px))',
    zIndex: 9999,
  }), [pos])

  useDropdownEnter(panelRef, open)

  return (
    <div ref={rootRef} className="relative shrink-0">
      <button
        type="button"
        onClick={toggleOpen}
        className={`h-[34px] px-2.5 rounded-md border text-sm flex items-center gap-1.5 transition-colors cursor-pointer ${
          selected.size > 0
            ? 'border-blue-300 bg-blue-50 text-blue-600'
            : 'border-gray-300 bg-white text-gray-400 hover:border-gray-400'
        }`}
        title={t('host:tags.filter')}
      >
        <Tag className="size-3.5" />
        <span className="whitespace-nowrap">{t('host:tags.filter')}</span>
        {selected.size > 0 && (
          <span className="px-1.5 text-[11px] rounded-full bg-blue-500 text-white">{selected.size}</span>
        )}
        <ChevronDown size={14} className={`transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && createPortal(
        <div
          ref={panelRef}
          style={panelStyle}
          className="bg-white border border-gray-200 rounded-md shadow-xl max-h-72 overflow-y-auto scrollbar-thin"
        >
          {/* 全选 / 清空：同一行二选一显示，保证任何状态下都有一条「回到全部」的出路 */}
          <div className="sticky top-0 bg-white px-2 py-1.5 border-b border-gray-100 flex items-center justify-between">
            <button
              type="button"
              onClick={allSelected ? onClear : onSelectAll}
              className="text-[11px] text-gray-500 hover:text-blue-500 cursor-pointer"
            >
              {allSelected ? t('host:tags.filterClear') : t('host:tags.filterAll')}
            </button>
            <span className="text-[11px] text-gray-400">{t('host:tags.selectedCount', { count: selected.size })}</span>
          </div>
          {allTags.map(tag => {
            const active = selected.has(tag)
            const style = tagStyle(tag)
            return (
              <button
                key={tag}
                type="button"
                onClick={() => onToggle(tag)}
                className={`w-full px-3 py-1.5 text-sm text-left flex items-center justify-between gap-2 transition-colors cursor-pointer ${
                  active ? 'bg-blue-50' : 'hover:bg-gray-50'
                }`}
              >
                <span className={`truncate text-xs px-1.5 py-0.5 rounded border whitespace-nowrap ${style.chip}`} title={tag}>
                  {tag}
                </span>
                {active && <Check size={14} className="shrink-0 text-blue-500" />}
              </button>
            )
          })}
        </div>,
        document.body,
      )}
    </div>
  )
}

export default TagFilterDropdown
