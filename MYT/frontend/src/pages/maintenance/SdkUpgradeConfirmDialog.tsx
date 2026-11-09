import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { useModalEnter } from '../../hooks/useModalAnimation'

interface Props {
  deviceIp: string
  currentVersion: string
  latestVersion: string
  description: string
  submitting: boolean
  onConfirm: () => void
  onCancel: () => void
}

/**
 * SDK 升级确认弹窗（结构对齐 AboutSection 的客户端更新弹窗）：
 * 头部设备/版本信息 + 更新日志独立滚动区 + 底部取消/确认按钮。
 * portal 到 body —— 避免被宿主弹窗的 GSAP transform 变成 fixed 包含块（同 ConfirmDialog）。
 */
function SdkUpgradeConfirmDialog({ deviceIp, currentVersion, latestVersion, description, submitting, onConfirm, onCancel }: Props) {
  const { t } = useTranslation()
  const tp = (k: string) => t(`maintenance:sdkUpgradeConfirm.${k}`)
  const [showNotes, setShowNotes] = useState(true)
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const onCancelRef = useRef(onCancel)
  onCancelRef.current = onCancel

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancelRef.current()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [])

  // 版本显示：空值显示「未知」（v2 是 string 版本、V3 是 int 版本，hook 已归一成 string）
  const v = (ver: string) => ver || t('maintenance:devicePwd.unknown')

  return createPortal(
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-[60] modal-overlay" ref={rootRef} role="dialog" aria-modal="true" aria-labelledby="sdk-upgrade-title">
      <div className="bg-white rounded-lg shadow-xl w-80 max-w-[calc(100vw-32px)] overflow-hidden modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-5 pt-5 pb-4">
          <div className="flex items-center justify-between mb-3">
            <h3 id="sdk-upgrade-title" className="text-sm font-semibold text-gray-800">{tp('title')}</h3>
            <button onClick={onCancel} disabled={submitting} className="text-gray-400 hover:text-gray-600 text-lg leading-none disabled:opacity-50 cursor-pointer">&times;</button>
          </div>
          <div className="space-y-2 mb-3">
            <div className="flex items-center text-xs"><span className="text-gray-400 w-16 shrink-0">{tp('device')}</span><span className="text-gray-600">{deviceIp}</span></div>
            <div className="flex items-center text-xs"><span className="text-gray-400 w-16 shrink-0">{tp('currentVersion')}</span><span className="text-gray-600">{v(currentVersion)}</span></div>
            <div className="flex items-center text-xs"><span className="text-gray-400 w-16 shrink-0">{tp('latestVersion')}</span><span className="text-green-600 font-medium">{v(latestVersion)}</span></div>
          </div>
          <div className="border border-gray-200 rounded overflow-hidden">
            <button onClick={() => setShowNotes(!showNotes)} className="w-full flex items-center justify-between px-3 py-2 text-xs text-gray-700 hover:bg-gray-50 cursor-pointer">
              <span>{tp('description')}</span>
              <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" className={`transition-transform ${showNotes ? 'rotate-180' : ''}`}><polyline points="2,4 5,7 8,4" /></svg>
            </button>
            {showNotes && (
              <div className="max-h-36 overflow-y-auto px-3 py-2 text-[11px] text-gray-600 bg-gray-50 border-t border-gray-200 leading-relaxed" style={{ whiteSpace: 'pre-wrap' }}>
                {description || tp('noUpdateNotes')}
              </div>
            )}
          </div>
        </div>
        <div className="flex justify-end gap-2 px-5 pb-4">
          <button onClick={onCancel} disabled={submitting} className="px-3 py-1 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700 disabled:opacity-50">{t('button.cancel')}</button>
          <button onClick={onConfirm} disabled={submitting} className="px-3 py-1 text-xs bg-red-500 rounded hover:bg-red-600 text-white disabled:opacity-50">{tp('confirmBtn')}</button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

export default SdkUpgradeConfirmDialog
