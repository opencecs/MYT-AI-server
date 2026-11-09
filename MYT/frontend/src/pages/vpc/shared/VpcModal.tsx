import { useRef, type ReactNode } from 'react'
import { useModalEnter } from '../../../hooks/useModalAnimation'

interface VpcModalProps {
  title: string
  subtitle?: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  width?: string
}

/**
 * VPC 页共享弹窗：遮罩 + 标题栏 + 关闭按钮 + 可选 footer。
 * 统一入口动画（useModalEnter）与视觉规格（rounded-2xl / 头部下边框 / 底部按钮区）。
 */
export default function VpcModal({ title, subtitle, onClose, children, footer, width = 'w-[440px]' }: VpcModalProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef}>
      <div className={`bg-white rounded-2xl shadow-xl ${width} max-h-[85vh] flex flex-col modal-content`}>
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-100">
          <div>
            <h4 className="text-sm font-semibold text-gray-800">{title}</h4>
            {subtitle && <p className="text-[10px] text-gray-400 mt-0.5">{subtitle}</p>}
          </div>
          <button onClick={onClose}
            className="text-gray-400 hover:text-gray-600 text-lg leading-none w-7 h-7 flex items-center justify-center rounded-lg hover:bg-gray-100">&times;</button>
        </div>
        <div className="flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer && (
          <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-100">{footer}</div>
        )}
      </div>
    </div>
  )
}
