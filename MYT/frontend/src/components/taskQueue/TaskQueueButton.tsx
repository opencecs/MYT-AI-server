import { useRef, useState, useEffect, useLayoutEffect, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import TaskQueuePanel from './TaskQueuePanel'
import { ClockGlyph } from './TaskQueueGlyphs'
import { useDropdownEnter, animateDropdownOut } from '../../hooks/useModalAnimation'

export default function TaskQueueButton() {
  const { t } = useTranslation()
  const { activeCount, failedCount } = useTaskQueue()
  const [open, setOpen] = useState(false)
  const closingRef = useRef(false) // 退场动画播放中：忽略重复关闭/切换
  const panelRef = useRef<HTMLDivElement>(null)
  useDropdownEnter(panelRef, open)
  const containerRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ top: 0, right: 0 })

  const closePanel = useCallback(() => {
    if (closingRef.current) return
    closingRef.current = true
    animateDropdownOut(panelRef.current, () => { closingRef.current = false; setOpen(false) })
  }, [])

  // 计算面板定位（贴按钮右下，fixed 到 body，避免被顶部栏层叠上下文裁剪）
  useLayoutEffect(() => {
    if (!open || !containerRef.current) return
    const update = () => {
      const r = containerRef.current!.getBoundingClientRect()
      setPos({ top: r.bottom + 4, right: window.innerWidth - r.right })
    }
    update()
    window.addEventListener('resize', update)
    return () => window.removeEventListener('resize', update)
  }, [open])

  // 点击外部关闭（弹窗层内的交互除外：点任务详情/确认弹窗的任何位置都不打扰面板）
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      const target = e.target as Node
      if (containerRef.current?.contains(target) || panelRef.current?.contains(target)) return
      if ((target as Element).closest?.('.modal-overlay, [role="dialog"]')) return
      closePanel()
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open, closePanel])

  const toggle = useCallback(() => {
    if (closingRef.current) return
    if (open) closePanel()
    else setOpen(true)
  }, [open, closePanel])

  const badge = activeCount > 0 ? activeCount : failedCount > 0 ? failedCount : 0
  const badgeType = activeCount > 0 ? 'bg-[#2f6fd6]' : 'bg-[#d4483f]'

  return (
    <div className="relative" ref={containerRef}>
      <button
        onClick={toggle}
        className={`h-6 px-2 flex items-center text-[11px] rounded-lg transition-colors cursor-pointer gap-1.5 ${open ? 'bg-[#e9f1fc] text-[#2f6fd6]' : 'text-gray-500 hover:text-blue-500 hover:bg-gray-200'}`}
        title={t('taskQueue.title')}
      >
        {/* 时钟图标 */}
        <ClockGlyph size={13} />
        <span>{t('taskQueue.title')}</span>
        {badge > 0 && (
          <span className={`min-w-[17px] h-[17px] px-1 flex items-center justify-center rounded-full text-[9.5px] font-bold text-white ${badgeType}`}>
            {badge > 99 ? '99+' : badge}
          </span>
        )}
      </button>

      {open && createPortal(
        // 层级约定：面板压在一切弹窗（z-50 起）之下、普通内容之上——
        // 打开弹窗时任务队列让位（用户决策 2026-09）；勿改回 9999。
        <div ref={panelRef} style={{ position: 'fixed', top: pos.top, right: pos.right, zIndex: 45 }}>
          <TaskQueuePanel />
        </div>,
        document.body
      )}
    </div>
  )
}
