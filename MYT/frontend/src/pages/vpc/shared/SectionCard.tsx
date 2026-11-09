import type { ReactNode } from 'react'

interface SectionCardProps {
  children: ReactNode
  className?: string
}

/** VPC 页统一内容卡片：rounded-xl + 边框 + hover 阴影（与 RouteSection 设计语言对齐） */
export default function SectionCard({ children, className = '' }: SectionCardProps) {
  return (
    <div className={`bg-white border border-gray-100 rounded-xl shadow-sm hover:shadow-md transition-shadow ${className}`}>
      {children}
    </div>
  )
}
