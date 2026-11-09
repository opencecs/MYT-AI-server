import { useState, useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { ChevronLeft, ChevronRight, Grid3x3, PowerOff, Monitor, RefreshCw, Volume2, VolumeX, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import * as MytAgentService from '../../../bindings/github.com/moyunteng/myt/mytagentservice'
import type { AgentConnInfo, ScreenCastItemData } from './agentTypes'
import { PAGE_SIZE, RENEW_INTERVAL, handleStopAll } from './screenCastApi'
import { IframeLayer } from './ScreenCastIframeLayer'

interface Props {
  visible: boolean
  items: ScreenCastItemData[]
  connInfo: AgentConnInfo | null
  onClose: () => void
  onRemove: (id: string) => void
  onClear?: () => void
}

function ScreenCastPanel({ visible, items, connInfo, onClose, onRemove, onClear }: Props) {
  const { t } = useTranslation()
  const [page, setPage] = useState(0)
  const [zoomedId, setZoomedId] = useState<string | null>(null)
  const totalPages = Math.max(1, Math.ceil(items.length / PAGE_SIZE))
  const pageItems = items.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const pageItemIds = new Set(pageItems.map(i => i.id))
  const slotRefs = useRef<Record<string, HTMLDivElement | null>>({})

  useEffect(() => { if (page >= totalPages) setPage(Math.max(0, totalPages - 1)) }, [totalPages, page])
  useEffect(() => { if (!visible) setZoomedId(null) }, [visible])
  useEffect(() => () => { setZoomedId(null) }, [])

  // 自动续期
  useEffect(() => {
    if (items.length === 0 || !connInfo) return
    const doRenew = () => { MytAgentService.HttpProxy('GET', `/api/screencast/renew?token=${encodeURIComponent(connInfo.sessionToken)}`, '').catch(() => {}) }
    doRenew()
    const timer = setInterval(doRenew, RENEW_INTERVAL)
    return () => clearInterval(timer)
  }, [items.length, connInfo])

  // 同步小窗位置到 iframe 层和覆盖层
  useEffect(() => {
    const updatePositions = () => {
      for (const item of pageItems) {
        const slot = slotRefs.current[item.id]
        if (!slot || item.id === zoomedId) continue
        const rect = slot.getBoundingClientRect()
        if (rect.width === 0 || rect.height === 0) return // slot 还没渲染
        const layer = document.querySelector(`[data-sc-layer="${item.id}"]`) as HTMLElement | null
        if (layer) { layer.style.left = `${rect.left}px`; layer.style.top = `${rect.top}px`; layer.style.width = `${rect.width}px`; layer.style.height = `${rect.height}px`; layer.style.transform = 'none'; layer.style.borderRadius = '0' }
        const overlay = document.querySelector(`[data-sc-overlay="${item.id}"]`) as HTMLElement | null
        if (overlay) { overlay.style.left = `${rect.left}px`; overlay.style.top = `${rect.top}px`; overlay.style.width = `${rect.width}px`; overlay.style.height = `${rect.height}px`; overlay.style.transform = 'none' }
      }
    }
    // 延迟一帧，确保 slot DOM 已渲染
    requestAnimationFrame(updatePositions)
    // 面板展开动画结束后再同步一次（300ms transition）
    const animTimer = setTimeout(updatePositions, 350)
    const ro = new ResizeObserver(updatePositions)
    window.addEventListener('scroll', updatePositions, true)
    window.addEventListener('resize', updatePositions)
    for (const item of pageItems) { const slot = slotRefs.current[item.id]; if (slot) ro.observe(slot) }
    return () => { ro.disconnect(); window.removeEventListener('scroll', updatePositions, true); window.removeEventListener('resize', updatePositions); clearTimeout(animTimer) }
  }, [pageItems, zoomedId, page, visible])

  if (!connInfo) return null

  const slots: (ScreenCastItemData | null)[] = [...pageItems]
  while (slots.length < PAGE_SIZE) slots.push(null)
  const zoomedItem = zoomedId ? items.find(i => i.id === zoomedId) : null
  const sendNav = (id: string, cmd: string) => {
    const iframe = document.getElementById(`sc-iframe-${id}`) as HTMLIFrameElement | null
    iframe?.contentWindow?.postMessage({ type: 'myt-nav', cmd }, '*')
  }
  const getMuted = (id: string) => {
    const w = window as unknown as { __scMutedState?: Record<string, boolean> }
    return w.__scMutedState?.[id] ?? true
  }
  const setMuteGlobal = (id: string, muted: boolean) => {
    const w = window as unknown as { __scSetMute?: (id: string, muted: boolean) => void }
    w.__scSetMute?.(id, muted)
  }
  const reloadGlobal = (id: string) => {
    const w = window as unknown as { __scReload?: (id: string) => void }
    w.__scReload?.(id)
  }

  return (
    <>
      {createPortal(
        <IframeLayer items={items} info={connInfo} pageItemIds={pageItemIds} zoomedId={zoomedId} onRemove={onRemove} visible={visible} />,
        document.body
      )}

      {/* 右侧面板 */}
      <div className="flex h-full flex-col border-l transition-all duration-300"
        style={{ borderColor: c.line, background: c.surface, width: visible ? 420 : 0, opacity: visible ? 1 : 0, overflow: 'hidden', flexShrink: 0 }}>
        <div className="flex items-center gap-2 border-b px-3 py-2" style={{ borderColor: c.line }}>
          <Grid3x3 className="size-3.5" style={{ color: c.accent }} />
          <span className="text-xs font-semibold" style={{ color: c.text }}>{t('agent:screencast.title')}</span>
          <span className="text-[10px]" style={{ color: c.textMuted }}>{t('agent:screencast.count', { count: items.length })}</span>
          <div className="ml-auto flex items-center gap-1">
            {totalPages > 1 && (
              <>
                <button onClick={() => setPage(p => Math.max(0, p - 1))} disabled={page === 0} className="rounded p-1 disabled:opacity-30 hover:bg-black/5" style={{ color: c.textMuted }}><ChevronLeft className="size-3.5" /></button>
                <span className="text-[10px] tabular-nums" style={{ color: c.textMuted }}>{page + 1}/{totalPages}</span>
                <button onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))} disabled={page >= totalPages - 1} className="rounded p-1 disabled:opacity-30 hover:bg-black/5" style={{ color: c.textMuted }}><ChevronRight className="size-3.5" /></button>
              </>
            )}
            {items.length > 0 && (
              <button onClick={() => { for (const item of items) reloadGlobal(item.id) }} className="rounded px-1.5 py-0.5 text-[10px] hover:bg-black/5" style={{ color: c.textMuted }} title={t('agent:tooltip.reloadAll')}>
                <RefreshCw className="size-3" />
              </button>
            )}
            {items.length > 0 && onClear && (
              <button onClick={() => handleStopAll(connInfo, onClear)} className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] hover:bg-red-500/10" style={{ color: c.danger }} title={t('agent:tooltip.closeAll')}>
                <PowerOff className="size-3" /><span>{t('agent:screencast.closeAll')}</span>
              </button>
            )}
            <button onClick={onClose} className="rounded p-1 hover:bg-black/5" style={{ color: c.textMuted }} title={t('agent:tooltip.collapse')}><ChevronLeft className="size-3.5" /></button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto">
          {items.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
              <Monitor className="size-8" style={{ color: c.textMuted, opacity: 0.4 }} />
              <span className="text-xs" style={{ color: c.textMuted }}>{t('agent:screencast.emptyHint')}</span>
            </div>
          ) : (
            <div className="grid grid-cols-3 gap-1 p-1" style={{ minHeight: '100%' }}>
              {slots.map((item, idx) => (
                <div key={item?.id || `empty-${idx}`}
                  ref={item ? (el: HTMLDivElement | null) => { slotRefs.current[item.id] = el } : undefined}
                  className={item ? 'group relative aspect-[9/16] cursor-pointer' : 'flex aspect-[9/16] items-center justify-center'}
                  style={item ? { background: '#000', overflow: 'hidden' } : { background: c.surfaceAlt, opacity: 0.3 }}
                  onClick={item ? () => setZoomedId(item.id) : undefined}>
                  {!item && <span className="text-[10px]" style={{ color: c.textMuted }}>{t('agent:screencast.emptySlot')}</span>}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* 放大模式：遮罩 + 标题栏/导航栏浮层（不挡画面） */}
      {zoomedItem && createPortal(
        <>
          <div style={{ position: 'fixed', inset: 0, zIndex: 200, background: 'rgba(0,0,0,0.9)' }} onClick={() => setZoomedId(null)} />
          <div style={{ position: 'fixed', left: '50%', top: '50%', transform: 'translate(-50%, -50%)', height: '90vh', aspectRatio: '9 / 16', zIndex: 202, pointerEvents: 'none' }}>
            {/* 顶部标题栏 */}
            <div className="absolute left-0 right-0 top-0 flex items-center gap-2 px-3 py-2" style={{ background: 'linear-gradient(to bottom, rgba(0,0,0,0.8), transparent)' }}>
              <Monitor className="size-4 shrink-0 text-white" />
              <span className="truncate text-sm font-medium text-white">{zoomedItem.name}</span>
              <div className="ml-auto flex items-center gap-2">
                <span className="size-2 shrink-0 rounded-full bg-green-500" />
                <button onClick={() => { setMuteGlobal(zoomedItem.id, !getMuted(zoomedItem.id)) }} className="rounded p-1 hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={t('agent:tooltip.mute')}>
                  {!getMuted(zoomedItem.id) ? <Volume2 className="size-4 text-white" /> : <VolumeX className="size-4 text-white" />}
                </button>
                <button onClick={() => reloadGlobal(zoomedItem.id)} className="rounded p-1 hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={t('agent:tooltip.reload')}>
                  <RefreshCw className="size-4 text-white" />
                </button>
                <button onClick={() => setZoomedId(null)} className="rounded p-1 hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={t('agent:tooltip.close')}>
                  <X className="size-4 text-white" />
                </button>
              </div>
            </div>
            {/* 底部导航 */}
            <div className="absolute bottom-0 left-0 right-0 flex items-center justify-center gap-8 pb-2 pt-3" style={{ background: 'linear-gradient(to top, rgba(0,0,0,0.8), transparent)' }}>
              <button onClick={() => sendNav(zoomedItem.id, 'goBack')} className="flex h-10 w-10 items-center justify-center rounded-full text-white hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={t('agent:tooltip.goBack')}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M19 12H5M12 19l-7-7 7-7" /></svg>
              </button>
              <button onClick={() => sendNav(zoomedItem.id, 'goHome')} className="flex h-10 w-10 items-center justify-center rounded-full text-white hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={t('agent:tooltip.home')}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M3 12l9-9 9 9M5 10v10h14V10" /></svg>
              </button>
              <button onClick={() => sendNav(zoomedItem.id, 'goClean')} className="flex h-10 w-10 items-center justify-center rounded-full text-white hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={t('agent:tooltip.multitask')}>
                <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><rect x="4" y="4" width="16" height="16" rx="2" /><line x1="9" y1="9" x2="9" y2="15" /><line x1="15" y1="9" x2="15" y2="15" /></svg>
              </button>
            </div>
          </div>
        </>,
        document.body
      )}
    </>
  )
}

export default ScreenCastPanel
