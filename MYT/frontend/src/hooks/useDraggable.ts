import { useCallback, useRef } from 'react'

/**
 * 通用浮窗拖动 hook（从 IscsiWizardModal 的标题栏拖拽逻辑抽取而来）。
 *
 * 位移方式：transform translate 偏移量（不依赖面板的定位方式，居中布局/像素定位都适用）。
 * 拖动过程直接写 panel.style.transform，不触发 React 重渲染/重挂载。
 *
 * 可选 constrain：把面板夹在视口内。非浮窗场景（居中遮罩弹窗）沿用旧规则
 * 「主体至少留 MIN_VISIBLE 可见」；浮窗场景（constrainTop 有值）收紧为
 * 「手柄（标题栏）完整可见 + 顶部不得越过视口顶/自绘标题栏下缘」——否则浮窗
 * 拖到视口顶部会盖住主窗口右上角的最小化/最大化/关闭按钮，或标题栏跑到屏幕外。
 * 可选 onMove：每次移动回调当前偏移（宿主记到 ref 即可，用于重渲染时保持位置）。
 *
 * 注意：面板必须由父级条件渲染（mount = 出现），拖动状态存于 ref，
 * 组件卸载即丢弃，无跨实例残留。
 */
interface UseDraggableOptions {
  /** 浮窗面板元素（位移施加在它身上） */
  panelRef: React.RefObject<HTMLElement | null>
  /** 拖动手柄（通常是标题栏）；手柄内的 button/input/select/textarea 不触发拖动 */
  handleRef: React.RefObject<HTMLElement | null>
  /** 是否限制面板不越出视口（默认 true；居中遮罩弹窗可关掉以保持原行为） */
  constrain?: boolean
  /**
   * 拖动上界（px，视口坐标）：面板顶部（含手柄）不得越过此线。
   * 设了此值即启用浮窗规则；典型值 = 主窗口自绘标题栏高度（如 App.tsx 顶栏 h-8 → 32）。
   */
  constrainTop?: number
  /** 每次移动回调当前偏移量（可选） */
  onMove?: (x: number, y: number) => void
}

/** 面板至少保留在视口内的可见尺寸（px，旧规则用） */
const MIN_VISIBLE = 60

export function useDraggable({ panelRef, handleRef, constrain = true, constrainTop, onMove }: UseDraggableOptions) {
  // 累计偏移量与拖动会话内的临时状态，均不进 React state（避免高频 setState）
  const offsetRef = useRef({ x: 0, y: 0 })
  const dragRef = useRef<{ startX: number; startY: number; origX: number; origY: number } | null>(null)

  const apply = useCallback((x: number, y: number) => {
    const panel = panelRef.current
    if (!panel) return
    offsetRef.current = { x, y }
    panel.style.transform = `translate(${x}px, ${y}px)`
    onMove?.(x, y)
  }, [panelRef, onMove])

  /** 标题栏 onMouseDown 接此方法开始拖动 */
  const startDrag = useCallback((e: React.MouseEvent) => {
    const panel = panelRef.current
    if (!panel || !handleRef.current) return
    // 手柄内的交互元素（按钮/输入框）不触发拖动
    if ((e.target as HTMLElement).closest('button, input, select, textarea')) return
    e.preventDefault()
    // 基准位置 = 当前可视位置 - 已有偏移（getBoundingClientRect 不受书写模式等影响）
    const rect = panel.getBoundingClientRect()
    // 浮窗规则（constrainTop 有值）：手柄完整可见 + 顶部不越过上界；否则旧规则
    // （主体至少留 MIN_VISIBLE 可见）。手柄矩形在拖动中相对面板是固定的（面板
    // 尺寸不变），以「初始偏移下手柄的视口矩形」为基准计算各边界的偏移区间。
    const floatMode = constrain && constrainTop !== undefined
    const handleRect = floatMode ? handleRef.current.getBoundingClientRect() : null
    const base = {
      left: rect.left - offsetRef.current.x,
      top: rect.top - offsetRef.current.y,
      w: rect.width,
      h: rect.height,
      handleLeft: handleRect ? handleRect.left - offsetRef.current.x : 0,
      handleTop: handleRect ? handleRect.top - offsetRef.current.y : 0,
      handleW: handleRect ? handleRect.width : 0,
      handleH: handleRect ? handleRect.height : 0,
    }
    dragRef.current = { startX: e.clientX, startY: e.clientY, origX: offsetRef.current.x, origY: offsetRef.current.y }
    const onMoveDoc = (ev: MouseEvent) => {
      const drag = dragRef.current
      if (!drag) return
      let x = drag.origX + ev.clientX - drag.startX
      let y = drag.origY + ev.clientY - drag.startY
      if (floatMode) {
        // 顶部不越过上界（自绘标题栏下缘）；底部/左右保证手柄完整在视口内。
        // 按手柄而不是面板整体夹紧：面板可以高于视口，手柄始终可抓。
        y = Math.min(window.innerHeight - base.handleTop - base.handleH, Math.max(constrainTop - base.handleTop, y))
        x = Math.min(window.innerWidth - base.handleW - base.handleLeft, Math.max(-base.handleLeft, x))
      } else if (constrain) {
        // 视口夹紧：面板主体至少留 MIN_VISIBLE 可见
        x = Math.min(window.innerWidth - MIN_VISIBLE - base.left, Math.max(MIN_VISIBLE - base.w - base.left, x))
        y = Math.min(window.innerHeight - MIN_VISIBLE - base.top, Math.max(MIN_VISIBLE - base.h - base.top, y))
      }
      apply(x, y)
    }
    const onUp = () => {
      dragRef.current = null
      document.removeEventListener('mousemove', onMoveDoc)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mousemove', onMoveDoc)
    document.addEventListener('mouseup', onUp)
  }, [panelRef, handleRef, constrain, constrainTop, apply])

  /** 重置位移（回到布局原位） */
  const reset = useCallback(() => apply(0, 0), [apply])

  return { startDrag, reset, offsetRef }
}
