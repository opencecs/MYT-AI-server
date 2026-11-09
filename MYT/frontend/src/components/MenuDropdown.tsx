import { useState, useRef, useEffect, useLayoutEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { ChevronDown } from 'lucide-react'
import { useDropdownEnter, animateDropdownOut } from '../hooks/useModalAnimation'

export interface MenuItem {
  key: string
  label: string
  onClick: () => void
  disabled?: boolean
  /** 禁用原因（悬浮提示） */
  title?: string
  /** 危险操作（红字 + 红底 hover，如 重置/删除） */
  danger?: boolean
}

interface Props {
  /** 触发按钮文案（如「批量操作」） */
  label: string
  items: MenuItem[]
  /** 整体禁用（如未选中目标 / 批量执行中），与散按钮的 disabled 语义一致 */
  disabled?: boolean
  /** 触发按钮样式（复用调用方的按钮类名串） */
  className?: string
  title?: string
}

/**
 * 通用「按钮 + 下拉菜单」。仿 Select 的浮层实现：createPortal 固定定位跟随按钮、
 * 点击外部 / Esc 关闭、useDropdownEnter 展开动画。层级走浮层约定的「菜单」档 z-[100]。
 */
export default function MenuDropdown({ label, items, disabled, className = '', title }: Props) {
  const [open, setOpen] = useState(false)
  const closingRef = useRef(false) // 退场动画播放中：忽略重复关闭/切换
  const rootRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ top: 0, left: 0, minWidth: 0 })

  // 动画关闭（animateDropdownOut 与 useDropdownEnter 配对：上移淡出，播完才卸载面板）
  const closeMenu = useCallback(() => {
    if (closingRef.current) return
    closingRef.current = true
    animateDropdownOut(panelRef.current, () => { closingRef.current = false; setOpen(false) })
  }, [])

  // 点击外部 / Esc 关闭
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node) &&
          panelRef.current && !panelRef.current.contains(e.target as Node)) closeMenu()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeMenu()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open, closeMenu])

  // 面板定位：贴触发按钮下方展开，底部空间不足时上翻
  useLayoutEffect(() => {
    if (!open || !rootRef.current) return
    const update = () => {
      const rect = rootRef.current!.getBoundingClientRect()
      const panelH = panelRef.current?.offsetHeight ?? 240
      const spaceBelow = window.innerHeight - rect.bottom
      const top = spaceBelow < panelH + 8 ? Math.max(8, rect.top - panelH - 4) : rect.bottom + 4
      // 面板固定 132px 横向宽度（2026-09-26 用户要求：w-max 曾被撑得特别宽），
      // 仅以按钮宽为下限——按钮更宽时不缩到按钮内
      setPos({ top, left: rect.left, minWidth: rect.width })
    }
    update()
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    return () => {
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
    }
  }, [open])

  // 面板越过视口右缘时回拉
  useEffect(() => {
    if (!open || !panelRef.current) return
    const vw = document.documentElement.clientWidth
    const rect = panelRef.current.getBoundingClientRect()
    if (rect.right > vw - 8) {
      panelRef.current.style.left = `${Math.max(8, vw - 8 - rect.width)}px`
    }
  }, [open])

  useDropdownEnter(panelRef, open)

  return (
    <div ref={rootRef} className={`relative inline-flex ${disabled ? 'opacity-40 pointer-events-none' : ''}`}>
      <button
        type="button"
        onClick={() => { if (closingRef.current) return; if (open) closeMenu(); else setOpen(true) }}
        title={title}
        className={`inline-flex items-center gap-1 ${className}`}
      >
        {label}
        <ChevronDown size={12} className={`transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && createPortal(
        <div
          ref={panelRef}
          style={{ position: 'fixed', top: pos.top, left: pos.left, minWidth: pos.minWidth, zIndex: 100 }}
          className="bg-white border border-gray-200 rounded-md shadow-xl py-1 w-[132px]"
        >
          {items.map(item => (
            <button
              key={item.key}
              type="button"
              disabled={item.disabled}
              title={item.title}
              onClick={() => { if (!item.disabled) { item.onClick(); closeMenu() } }}
              className={`w-full px-3 py-1.5 text-sm text-left transition-colors whitespace-nowrap ${
                item.disabled
                  ? 'text-gray-300 cursor-not-allowed'
                  : item.danger
                    ? 'text-red-500 hover:bg-red-50'
                    : 'text-gray-600 hover:bg-gray-50'
              }`}
            >
              {item.label}
            </button>
          ))}
        </div>,
        document.body
      )}
    </div>
  )
}
