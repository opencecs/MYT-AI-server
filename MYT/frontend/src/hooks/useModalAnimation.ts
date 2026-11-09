import { useGSAP } from '@gsap/react'
import { gsap } from 'gsap'

gsap.registerPlugin(useGSAP)

/**
 * 弹窗入场动画 hook：在组件作用域内查找 `.modal-overlay` 和 `.modal-content`，
 * 自动播放遮罩淡入 + 内容 scale+淡入。
 *
 * 用法：弹窗根容器用 `useGSAP` scope 包裹（或直接在组件内调用本 hook，传 rootRef），
 * 遮罩层加 className "modal-overlay"，内容卡片加 className "modal-content"。
 *
 * 示例：
 *   <div className="fixed inset-0 ... modal-overlay" ref={rootRef}>
 *     <div className="bg-white ... modal-content">...</div>
 *   </div>
 *   useModalEnter(rootRef)  // rootRef 指向遮罩
 *
 * 挂载即播放（弹窗 open 时才挂载，mount = 入场）。
 */
export function useModalEnter(rootRef: React.RefObject<HTMLElement | null>) {
  useGSAP(
    () => {
      const root = rootRef.current
      if (!root) return
      // root 本身是遮罩
      gsap.fromTo(root, { autoAlpha: 0 }, { autoAlpha: 1, duration: 0.2, ease: 'power2.out' })
      // 内容卡片：scope 内 .modal-content
      const content = root.querySelector('.modal-content')
      if (content) {
        gsap.fromTo(content, { scale: 0.96, opacity: 0 }, { scale: 1, opacity: 1, duration: 0.28, ease: 'power3.out', delay: 0.02 })
      }
    },
    { scope: rootRef },
  )
}

/**
 * 下拉面板展开动画 hook：open=true 时播放 autoAlpha+y+scale，transformOrigin top center。
 * 用法：下拉面板加 ref，传 useDropdownEnter(panelRef, open)。
 */
export function useDropdownEnter(panelRef: React.RefObject<HTMLElement | null>, open: boolean) {
  useGSAP(
    () => {
      if (!panelRef.current) return
      if (open) {
        gsap.fromTo(panelRef.current,
          { autoAlpha: 0, y: -6, scale: 0.98 },
          { autoAlpha: 1, y: 0, scale: 1, duration: 0.18, ease: 'power2.out', transformOrigin: 'top center' })
      }
    },
    { dependencies: [open] },
  )
}

/**
 * 弹窗退场动画（与 useModalEnter 配对）：内容缩放淡出 + 遮罩淡出，
 * 播完调用 onClosed（通常是置空 state 卸载组件）。root 缺失时直接回调。
 */
export function animateModalOut(root: HTMLElement | null, onClosed: () => void) {
  if (!root) { onClosed(); return }
  const content = root.querySelector('.modal-content')
  const tl = gsap.timeline({ onComplete: onClosed })
  if (content) tl.to(content, { scale: 0.97, opacity: 0, duration: 0.14, ease: 'power2.in' }, 0)
  tl.to(root, { autoAlpha: 0, duration: 0.16, ease: 'power2.in' }, 0.01)
}

/**
 * 下拉面板退场动画（与 useDropdownEnter 配对）：上移淡出，播完调用 onClosed。
 */
export function animateDropdownOut(panel: HTMLElement | null, onClosed: () => void) {
  if (!panel) { onClosed(); return }
  gsap.to(panel, {
    autoAlpha: 0, y: -4, scale: 0.985,
    duration: 0.13, ease: 'power2.in', transformOrigin: 'top center',
    onComplete: onClosed,
  })
}
