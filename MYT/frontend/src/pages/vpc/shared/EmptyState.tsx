import type { LucideIcon } from 'lucide-react'

interface EmptyStateProps {
  icon: LucideIcon
  title: string
  subtitle?: string
  actionLabel?: string
  onAction?: () => void
}

/** VPC 页统一空态引导：图标 + 文案 + 主操作按钮 */
export default function EmptyState({ icon: Icon, title, subtitle, actionLabel, onAction }: EmptyStateProps) {
  return (
    <div className="text-center py-10">
      <Icon size={36} strokeWidth={1.5} className="text-gray-200 mx-auto mb-2" />
      <p className="text-xs text-gray-400">{title}</p>
      {subtitle && <p className="text-[11px] text-gray-300 mt-0.5">{subtitle}</p>}
      {actionLabel && onAction && (
        <button onClick={onAction}
          className="mt-3 px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 transition-colors">
          {actionLabel}
        </button>
      )}
    </div>
  )
}
