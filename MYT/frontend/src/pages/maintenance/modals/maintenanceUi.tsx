import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Copy, ExternalLink } from 'lucide-react'

/**
 * 维护弹窗内容区的统一视觉语言（魔云互联 / 公网穿透共用）。
 *
 * 半径沿用本仓既有规则：容器 rounded-lg、控件与信息块内元素 rounded。
 * 强调色锁 blue-500（全仓唯一强调色），状态色仅用语义色（绿=正常 / 琥珀=进行中 / 灰=未启用）。
 */

const btnBase = 'inline-flex items-center gap-1.5 h-7 px-3 text-xs rounded border whitespace-nowrap transition-colors disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer'

/** 主操作：实心蓝（blue-600 起，白字对比度 ≈5.2:1，达 WCAG AA；blue-500 仅 3.7:1） */
export const btnPrimary = `${btnBase} bg-blue-600 text-white border-blue-600 hover:bg-blue-700`
/** 危险操作：白底红描边 */
export const btnDanger = `${btnBase} bg-white text-red-600 border-red-200 hover:bg-red-50`
/** 次要/辅助操作：白底灰描边 */
export const btnGhost = `${btnBase} bg-white text-gray-600 border-gray-200 hover:bg-gray-50`

/** 状态行：语义色圆点 + 文案（进行中带脉冲） */
export function StatusLine({ tone, label }: { tone: 'on' | 'off' | 'busy'; label: string }) {
  const dot = tone === 'on' ? 'bg-green-500' : tone === 'busy' ? 'bg-amber-500 animate-pulse' : 'bg-gray-300'
  const text = tone === 'on' ? 'text-gray-700' : tone === 'busy' ? 'text-amber-700' : 'text-gray-500'
  return (
    <span className="inline-flex items-center gap-1.5 text-xs">
      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${dot}`} />
      <span className={text}>{label}</span>
    </span>
  )
}

/** 信息卡：圆角描边容器 + 行分隔（内部不放按钮，操作统一走底部操作行） */
export function InfoCard({ children }: { children: ReactNode }) {
  return <div className="rounded-lg border border-gray-200 divide-y divide-gray-100">{children}</div>
}

/** 信息行：label / value（等宽）/ 可选尾部动作（打开、复制） */
export function InfoRow({ label, value, onOpen, onCopy }: {
  label: string
  value: string
  onOpen?: () => void
  onCopy?: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex items-center gap-3 px-3 py-2">
      <span className="w-16 shrink-0 text-[11px] text-gray-500">{label}</span>
      <span
        className={`flex-1 min-w-0 truncate text-xs font-mono ${onOpen ? 'text-blue-600 cursor-pointer hover:underline' : 'text-gray-700'}`}
        onClick={onOpen}
        title={value}
      >
        {value}
      </span>
      {onOpen && (
        <button type="button" onClick={onOpen} title={t('maintenance:ui.open')} className="shrink-0 text-gray-500 hover:text-blue-600 cursor-pointer">
          <ExternalLink size={13} />
        </button>
      )}
      {onCopy && (
        <button type="button" onClick={onCopy} title={t('maintenance:ui.copy')} className="shrink-0 text-gray-500 hover:text-blue-600 cursor-pointer">
          <Copy size={13} />
        </button>
      )}
    </div>
  )
}

/** 反馈条：成功/失败的整行提示（图标 + 可换行文案） */
export function FeedbackBar({ tone, children }: { tone: 'success' | 'error'; children: ReactNode }) {
  const cls = tone === 'success'
    ? 'border-green-200 bg-green-50 text-green-700'
    : 'border-red-200 bg-red-50 text-red-600'
  return (
    <div className={`rounded-lg border px-3 py-2 text-[11px] leading-relaxed whitespace-pre-line ${cls}`}>
      {children}
    </div>
  )
}
