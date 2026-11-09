import { ReactNode } from 'react'

interface TooltipCellProps {
  text: string | undefined | null
  className?: string
  children?: ReactNode
}

const tipCls = 'absolute bottom-full left-0 mb-1 px-2 py-1 bg-gray-800 text-white text-[10px] rounded shadow-lg whitespace-normal max-w-[320px] z-50 opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none'

function TooltipCell({ text, className = '', children }: TooltipCellProps) {
  if (!text) return <span className={`truncate ${className}`}>&nbsp;</span>
  return (
    <span className={`relative group ${className}`}>
      <span className="truncate block">{children ?? text}</span>
      <span className={tipCls}>{text}</span>
    </span>
  )
}

export default TooltipCell
