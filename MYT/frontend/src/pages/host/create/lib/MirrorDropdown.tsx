import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { formatOsVer, type DisplayMirror } from './CreateInstanceConstants'
import { useDropdownEnter } from '../../../../hooks/useModalAnimation'

// 镜像下拉选择器（更新云机用；创建云机用 CreateInstanceModal 内的增强版）。
// allowCustom：列表底部追加「自定义镜像」项（哨兵值 '__custom__'），选中后在下方展开路径输入框。
export function MirrorDropdown({ value, onChange, mirrors, hasLoadError, allowCustom, customUrl, onCustomUrlChange }: {
  value: string
  onChange: (url: string) => void
  mirrors: DisplayMirror[]
  hasLoadError?: boolean
  allowCustom?: boolean
  customUrl?: string
  onCustomUrlChange?: (v: string) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const panelRef = useRef<HTMLDivElement>(null)
  useDropdownEnter(panelRef, open)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false) }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const selected = mirrors.find(m => m.url === value)
  const isCustom = value === '__custom__'

  return (
    <div ref={ref} className="relative">
      <button type="button" onClick={() => setOpen(!open)}
        className={`w-full flex items-center justify-between bg-gray-50 text-sm px-3 py-1.5 rounded-lg border text-left ${open ? 'border-blue-400' : 'border-gray-200 hover:border-gray-300'}`}>
        <span className={selected || isCustom ? 'text-gray-700' : 'text-gray-400'}>
          {isCustom
            ? t('host:create.customImage')
            : selected ? <>{selected.name} <span className="text-gray-400">({formatOsVer(selected.os_ver)})</span></> : t('host:create.selectImage')}
        </span>
        <svg className={`w-3 h-3 text-gray-400 transition-transform ${open ? 'rotate-180' : ''}`} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" /></svg>
      </button>
      {open && (
        <div ref={panelRef} className="absolute z-50 mt-1 w-full bg-white border border-gray-200 rounded-lg shadow-lg max-h-60 overflow-y-auto">
          {mirrors.map(m => (
            <button key={m.id} type="button"
              onClick={() => { onChange(m.url); setOpen(false) }}
              className={`w-full px-3 py-2 text-left text-sm hover:bg-blue-50 flex items-center gap-2 ${m.url === value ? 'bg-blue-50' : ''}`}>
              <span className="truncate flex-1">{m.name} <span className="text-gray-400">({formatOsVer(m.os_ver)})</span></span>
              <span className={`shrink-0 px-1.5 py-px rounded text-[10px] font-medium ${m.source === 'local' ? 'bg-green-50 text-green-600' : m.source === 'special' ? 'bg-purple-50 text-purple-600' : 'bg-gray-100 text-gray-500'}`}>
                {m.source === 'local' ? t('host:create.imageLocal') : m.source === 'special' ? t('host:create.imageSpecial') : t('host:create.imageOnline')}
              </span>
            </button>
          ))}
          {mirrors.length === 0 && hasLoadError && <div className="px-3 py-2 text-xs text-gray-400">{t('host:create.imageLoadFailed')}</div>}
          {mirrors.length === 0 && !hasLoadError && <div className="px-3 py-2 text-xs text-gray-400">{t('host:create.noCompatibleImage')}</div>}
          {allowCustom && (
            <button type="button"
              onClick={() => { onChange('__custom__'); setOpen(false) }}
              className={`w-full px-3 py-1.5 text-left text-sm border-t border-gray-100 hover:bg-blue-50 ${isCustom ? 'bg-blue-50 text-blue-600' : 'text-gray-500'}`}>
              {t('host:create.customImage')}
            </button>
          )}
        </div>
      )}
      {allowCustom && isCustom && (
        <input type="text" value={customUrl || ''} onChange={e => onCustomUrlChange?.(e.target.value)}
          placeholder={t('host:create.customImagePlaceholder')}
          className="mt-1 w-full text-sm px-3 py-1.5 rounded-lg border border-gray-200 outline-none focus:border-blue-400 bg-white" />
      )}
    </div>
  )
}
