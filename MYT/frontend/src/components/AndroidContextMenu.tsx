import { useEffect, useRef, useState } from 'react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'

gsap.registerPlugin(useGSAP)

export interface MenuItemConfig {
  icon: React.ComponentType<{ size?: number; className?: string }>
  label: string
  disabled?: boolean
  onClick: () => void
}
export interface MenuGroup {
  items: MenuItemConfig[]
  danger?: boolean
}

interface Props {
  x: number
  y: number
  groups: MenuGroup[]
  /** 顶部快捷按钮区（Win11 风格：图标在上/文字在下，五项等宽铺满）；复用 MenuItemConfig，
   *  关闭逻辑沿用现有模式——onClick 里自带关闭，组件不代关。空数组不渲染。 */
  quickActions?: MenuItemConfig[]
  /** 菜单宽度（px），默认 200；带 quickActions 的调用方传更宽值让按钮间距舒展 */
  width?: number
  onClose: () => void
}

/**
 * 安卓云机右键/操作菜单：带边界检测的定位 + gsap 入场动画 + 分组渲染。
 * 截图模式右键、坑位模式操作按钮共用。
 */
function AndroidContextMenu({ x, y, groups, quickActions, width = 200, onClose }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ x, y })

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const h = el.offsetHeight
    const w = el.offsetWidth
    const maxX = window.innerWidth - w - 4
    const nx = Math.max(4, Math.min(x, maxX))
    const margin = 4
    const vh = window.innerHeight
    let ny = y
    if (y + h + margin > vh) {
      ny = vh - h - margin
    }
    if (ny < margin) ny = margin
    setPos({ x: nx, y: ny })
  }, [x, y])

  useGSAP(() => {
    if (!ref.current) return
    gsap.fromTo(ref.current, { scale: 0.9, autoAlpha: 0 }, { scale: 1, autoAlpha: 1, duration: 0.18, ease: 'power3.out' })
  }, [])

  return (
    <div className="fixed inset-0 z-50" onClick={onClose} onContextMenu={e => { e.preventDefault(); onClose() }}>
      <div ref={ref} role="menu" className="fixed top-0 left-0 bg-white border border-gray-200 rounded-lg shadow-lg py-1 max-h-[calc(100vh-8px)] overflow-y-auto scrollbar-thin" style={{ left: pos.x, top: pos.y, width }} onClick={e => e.stopPropagation()}>
        {/* 顶部快捷按钮区（Win11 风格）：图标在上/文字在下，五项 flex-1 等宽铺满整行；
            观感与下方列表项一致——图标中性灰、文字深灰、hover 三态同列表项，默认透明底 */}
        {quickActions && quickActions.length > 0 && (
          <div className="flex px-1 py-1 border-b border-gray-100 mb-1">
            {quickActions.map((item, i) => {
              const Icon = item.icon
              const color = item.disabled
                ? 'text-gray-300 cursor-not-allowed'
                : 'text-gray-700 hover:bg-blue-50 hover:text-blue-600'
              return (
                <button
                  key={i}
                  role="menuitem"
                  disabled={item.disabled}
                  onClick={item.disabled ? undefined : item.onClick}
                  className={`flex flex-1 flex-col items-center justify-center gap-1 px-1 py-2 rounded-md cursor-pointer ${color}`}
                >
                  <Icon size={20} className={`shrink-0 ${item.disabled ? '' : 'text-gray-500'}`} />
                  <span className={`text-[11px] leading-none whitespace-nowrap ${item.disabled ? 'text-gray-300' : 'text-gray-700'}`}>{item.label}</span>
                </button>
              )
            })}
          </div>
        )}
        {/* 空组不渲染（v2 模式砍掉部分功能后组可能为空，避免残留分隔线） */}
        {groups.filter(g => g.items.length > 0).map((group, gi) => (
          <div key={gi}>
            {gi > 0 && <div className="border-t border-gray-100 my-1" />}
            {group.items.map((item, ii) => {
              const Icon = item.icon
              const base = 'w-full text-left flex items-center gap-2 px-3 py-1.5 text-xs cursor-pointer'
              const color = item.disabled
                ? 'text-gray-300 cursor-not-allowed'
                : group.danger
                  ? 'text-red-600 hover:bg-red-50'
                  : 'text-gray-700 hover:bg-blue-50 hover:text-blue-600'
              return (
                <button
                  key={ii}
                  role="menuitem"
                  disabled={item.disabled}
                  onClick={item.disabled ? undefined : item.onClick}
                  className={`${base} ${color}`}
                >
                  <Icon size={14} className="shrink-0" />
                  <span className="truncate">{item.label}</span>
                </button>
              )
            })}
          </div>
        ))}
      </div>
    </div>
  )
}

export default AndroidContextMenu
