import { useState, useRef, useEffect, useLayoutEffect } from 'react'
import { createPortal } from 'react-dom'
import { ChevronDown, Check } from 'lucide-react'
import { useDropdownEnter } from '../hooks/useModalAnimation'

/** 选项分组（optgroup 或扁平） */
export interface SelectOption {
  value: string
  label: string
  disabled?: boolean
}

export interface SelectGroup {
  label?: string
  options: SelectOption[]
}

interface Props {
  value: string
  onChange: (v: string) => void
  options: SelectOption[] | SelectGroup[]
  placeholder?: string
  className?: string
  disabled?: boolean
  buttonClassName?: string
  /** 开启后下拉面板顶部显示过滤输入框（按 label/value 模糊匹配） */
  searchable?: boolean
  searchPlaceholder?: string
}

/** 判断是否含分组结构：任一元素带 options 字段即视为分组（兼容扁平+分组的混合数组） */
function isGrouped(opt: SelectOption[] | SelectGroup[]): opt is SelectGroup[] {
  return opt.length > 0 && opt.some(o => 'options' in o && o.options !== undefined)
}

/** 归一化为纯分组结构：无 options 的扁平选项包成无名分组，避免混合数组在 flatMap/渲染时丢项 */
function toGroups(options: SelectGroup[]): SelectGroup[] {
  return options.map(o => (o.options ? o : { options: [o as unknown as SelectOption] }))
}

export default function Select({ value, onChange, options, placeholder = '请选择', className = '', disabled, buttonClassName = 'h-9', searchable, searchPlaceholder }: Props) {
  const [open, setOpen] = useState(false)
  const [kw, setKw] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ top: 0, left: 0, width: 0 })

  const groups = isGrouped(options) ? toGroups(options as SelectGroup[]) : null
  const flat: SelectOption[] = groups ? groups.flatMap(g => g.options) : (options as SelectOption[])
  const selected = flat.find(o => o.value === value)

  // 关闭时清空过滤词，下次打开从全量列表开始
  useEffect(() => { if (!open) setKw('') }, [open])

  // 搜索过滤：命中 label 或 value（不分大小写）；分组内逐组过滤、空组丢弃
  const needle = kw.trim().toLowerCase()
  const hit = (o: SelectOption) => !needle || o.label.toLowerCase().includes(needle) || o.value.toLowerCase().includes(needle)
  const filteredGroups = groups ? toGroups(groups as SelectGroup[]).map(g => ({ ...g, options: g.options.filter(hit) })).filter(g => g.options.length > 0) : null
  const filteredFlat = groups ? null : flat.filter(hit)

  // 点击外部关闭
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node) &&
          panelRef.current && !panelRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  // 计算下拉面板位置（跟随触发按钮，向下展开）
  useLayoutEffect(() => {
    if (!open || !rootRef.current) return
    const update = () => {
      const rect = rootRef.current!.getBoundingClientRect()
      const panelH = Math.min(240, 300) // max-h-60=15rem=240px
      // 底部空间不足时向上展开
      const spaceBelow = window.innerHeight - rect.bottom
      const top = spaceBelow < panelH + 8 ? rect.top - panelH - 4 : rect.bottom + 4
      setPos({ top, left: rect.left, width: rect.width })
    }
    update()
    // 滚动/resize 时更新位置
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    return () => {
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
    }
  }, [open])

  // 面板按内容自适应变宽（长选项完整可见）后，若越过视口右缘则回拉右对齐
  useEffect(() => {
    if (!open || !panelRef.current) return
    const panel = panelRef.current
    const vw = document.documentElement.clientWidth
    const rect = panel.getBoundingClientRect()
    if (rect.right > vw - 8) {
      panel.style.left = `${Math.max(8, vw - 8 - rect.width)}px`
    }
  }, [open])

  // gsap 下拉展开
  useDropdownEnter(panelRef, open)

  const handleSelect = (v: string) => {
    onChange(v)
    setOpen(false)
  }

  return (
    <div ref={rootRef} className={`relative ${disabled ? 'opacity-40 pointer-events-none' : ''} ${className}`}>
      <button
        type="button"
        onClick={() => setOpen(o => !o)}
        className={'w-full px-3 text-sm rounded-md border border-gray-200 bg-white text-gray-700 hover:border-gray-300 transition-colors flex items-center justify-between gap-2 disabled:opacity-40 ' + buttonClassName}
      >
        <span className={`truncate ${selected ? 'text-gray-700' : 'text-gray-400'}`} title={selected?.label}>
          {selected ? selected.label : placeholder}
        </span>
        <ChevronDown size={14} className={`text-gray-400 shrink-0 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && createPortal(
        <div
          ref={panelRef}
          style={{ position: 'fixed', top: pos.top, left: pos.left, minWidth: pos.width, maxWidth: 'min(560px, calc(100vw - 16px))', zIndex: 9999 }}
          className="bg-white border border-gray-200 rounded-md shadow-xl max-h-60 overflow-y-auto scrollbar-thin w-max"
        >
          {searchable && (
            <div className="sticky top-0 bg-white p-1.5 border-b border-gray-100">
              <input autoFocus value={kw} onChange={e => setKw(e.target.value)}
                placeholder={searchPlaceholder || '搜索...'}
                className="w-full text-xs px-2 py-1 border border-gray-200 rounded outline-none focus:border-blue-400" />
            </div>
          )}
          {filteredGroups
            ? filteredGroups.map((g, gi) => (
              <div key={gi}>
                {g.label && <div className="px-3 pt-2 pb-1 text-[10px] font-medium text-gray-400 uppercase tracking-wide">{g.label}</div>}
                {g.options.map(o => (
                  <OptionItem key={o.value} option={o} selected={value === o.value} onSelect={handleSelect} />
                ))}
              </div>
            ))
            : (filteredFlat as SelectOption[]).map(o => (
              <OptionItem key={o.value} option={o} selected={value === o.value} onSelect={handleSelect} />
            ))}
          {((filteredGroups && filteredGroups.length === 0) || (!filteredGroups && (filteredFlat as SelectOption[]).length === 0)) && (
            <div className="px-3 py-2 text-xs text-gray-300 text-center">{searchable ? '无匹配项' : ''}</div>
          )}
        </div>,
        document.body
      )}
    </div>
  )
}

function OptionItem({ option, selected, onSelect }: { option: SelectOption; selected: boolean; onSelect: (v: string) => void }) {
  return (
    <button
      type="button"
      disabled={option.disabled}
      onClick={() => !option.disabled && onSelect(option.value)}
      className={`w-full px-3 py-1.5 text-sm text-left flex items-center justify-between gap-2 transition-colors ${
        option.disabled
          ? 'text-gray-300 cursor-not-allowed'
          : selected
            ? 'bg-blue-50 text-blue-600'
            : 'text-gray-600 hover:bg-gray-50'
      }`}
    >
      <span className="truncate" title={option.label}>{option.label}</span>
      {selected && !option.disabled && <Check size={14} className="shrink-0 text-blue-500" />}
    </button>
  )
}
