import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { useModalEnter } from '../hooks/useModalAnimation'

/** 算术确认挑战：答对（trim 后一致）才能点确认按钮 */
export interface ConfirmChallenge {
  question: string
  expectedAnswer: string
}

interface Props {
  title: string
  message: string
  confirmLabel?: string
  onConfirm: () => void | Promise<void>
  onCancel: () => void
  /** 可选算术确认：提供时正文下方渲染输入框，答案不匹配则确认按钮置灰 */
  challenge?: ConfirmChallenge
}

function ConfirmDialog({ title, message, confirmLabel, onConfirm, onCancel, challenge }: Props) {
  const { t } = useTranslation()
  const confirmText = confirmLabel ?? t('button.confirm')
  const [answer, setAnswer] = useState('')
  const confirmRef = useRef<HTMLButtonElement>(null)
  const onCancelRef = useRef(onCancel)
  onCancelRef.current = onCancel
  // 一次性确认保护：onConfirm 触发后（弹窗卸载的渲染 flush 前）双击的第二次点击
  // 仍会命中同一按钮——不拦截会导致批量删除等操作整轮重发
  const confirmedRef = useRef(false)
  // 但本组件可能被「同实例换内容」复用（不卸载，只换 title/message，如多阶段确认）：
  // 那种情况下 guard 必须随新内容复位，否则新内容带着上一轮的 disabled 渲染进
  // DOM → 确认按钮点不动。只能在 render 期同步比对：放 useEffect 无效——effect
  // 在 commit 之后才跑，disabled 已按旧值写进 DOM 且不会再有渲染纠正它。
  const actionKey = `${title}\u0000${message}\u0000${confirmLabel ?? ''}\u0000${challenge?.question ?? ''}\u0000${challenge?.expectedAnswer ?? ''}`
  const actionKeyRef = useRef(actionKey)
  if (actionKeyRef.current !== actionKey) {
    actionKeyRef.current = actionKey
    confirmedRef.current = false
    // 同实例换内容（如重新生成算术题）时输入也复位，避免旧答案碰巧命中新题
    setAnswer('')
  }

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancelRef.current()
    }
    document.addEventListener('keydown', handleKey)
    confirmRef.current?.focus()
    return () => document.removeEventListener('keydown', handleKey)
  }, [])

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  // 渲染到 body（而非就地渲染）：本组件常被放进弹窗内容里（维护弹窗的 log/cache/
  // format 确认等），而弹窗外壳的卡片被 useModalEnter 的 GSAP 动画写入了 transform
  // —— 有 transform 的祖先会成为 position:fixed 后代的包含块，遮罩只会覆盖那张卡片、
  // 缩在弹窗里而不是全屏。portal 到 body 脱离该子树后，fixed inset-0 才真正相对视口
  // （与 Select.tsx 下拉面板同做法）。层级仍保持 z-[60]：现有使用点要么在页面层、
  // 要么在 z-50 的弹窗外壳内，60 都够用；也不改动与标题栏(z-[70])、Toast(z-[100])
  // 的既有先后关系。
  return createPortal(
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-[60] modal-overlay" ref={rootRef} role="dialog" aria-modal="true" aria-labelledby="confirm-title">
      <div className="bg-white rounded-lg shadow-xl p-6 w-80 max-w-[calc(100vw-32px)] modal-content" onClick={e => e.stopPropagation()}>
        <h3 id="confirm-title" className="text-sm font-semibold text-gray-800 mb-2">{title}</h3>
        <p className="text-xs text-gray-500 mb-4 whitespace-pre-line leading-relaxed">{message}</p>
        {challenge && (
          <div className="mb-4">
            <label className="block text-xs text-gray-600 mb-1.5">
              {t('confirm.challengeHint')} <span className="font-mono font-semibold text-gray-800">{challenge.question}</span>
            </label>
            <input
              type="text"
              autoFocus
              value={answer}
              onChange={e => setAnswer(e.target.value)}
              placeholder={t('confirm.challengePlaceholder')}
              className="w-full px-2.5 py-1.5 text-xs border border-gray-300 rounded focus:outline-none focus:ring-1 focus:ring-blue-400"
            />
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button onClick={onCancel} className="px-3 py-1 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700">{t('button.cancel')}</button>
          <button
            ref={confirmRef}
            disabled={confirmedRef.current || (challenge != null && answer.trim() !== challenge.expectedAnswer.trim())}
            onClick={() => {
              if (confirmedRef.current) return
              confirmedRef.current = true
              onConfirm()
            }}
            className="px-3 py-1 text-xs bg-red-500 rounded hover:bg-red-600 text-white disabled:opacity-50"
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

export default ConfirmDialog
