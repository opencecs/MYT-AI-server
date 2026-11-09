import { useState, useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { Monitor, Volume2, VolumeX, RefreshCw, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { AgentConnInfo, ScreenCastItemData } from './agentTypes'
import { buildPlayerSrc, handleStop } from './screenCastApi'

/** 全局 iframe 层：每个投屏一个 iframe，portal 到 body，小窗时跟随 slot 位置，放大时居中 */
export function IframeLayer({ items, info, pageItemIds, zoomedId, onRemove, visible }: {
  items: ScreenCastItemData[]
  info: AgentConnInfo
  pageItemIds: Set<string>
  zoomedId: string | null
  onRemove?: (id: string) => void
  visible: boolean
}) {
  const { t } = useTranslation()
  const mutedRef = useRef<Record<string, boolean>>({})
  const [mutedUI, setMutedUI] = useState<Record<string, boolean>>({})
  const [retryKey, setRetryKey] = useState<Record<string, number>>({})

  // 监听 iframe 重试消息
  useEffect(() => {
    const handler = (e: MessageEvent) => {
      if (e.data?.type === 'screencast_retry') {
        for (const item of items) {
          const iframe = document.getElementById(`sc-iframe-${item.id}`) as HTMLIFrameElement | null
          if (iframe && iframe.contentWindow === e.source) {
            setRetryKey(prev => ({ ...prev, [item.id]: (prev[item.id] || 0) + 1 }))
            break
          }
        }
      }
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
  }, [items])

  // 初始化静音
  useEffect(() => {
    for (const item of items) {
      if (!(item.id in mutedRef.current)) {
        mutedRef.current[item.id] = true
        setMutedUI(prev => ({ ...prev, [item.id]: true }))
      }
    }
  }, [items])

  const setMute = (id: string, muted: boolean) => {
    mutedRef.current[id] = muted
    setMutedUI(prev => ({ ...prev, [id]: muted }))
    const iframe = document.getElementById(`sc-iframe-${id}`) as HTMLIFrameElement | null
    if (iframe?.contentWindow) {
      try {
        (iframe.contentWindow as unknown as Record<string, unknown>)['__screencast_muted'] = muted
        const gaudio = iframe.contentWindow.document.getElementById('gaudio') as HTMLAudioElement | null
        if (gaudio) { gaudio.muted = muted; if (!muted) gaudio.play() }
      } catch { /* cross-origin */ }
    }
  }

  // 放大时自动取消静音
  useEffect(() => {
    if (zoomedId) {
      let retries = 0; let cancelled = false
      const tryUnmute = () => {
        if (cancelled) return
        const iframe = document.getElementById(`sc-iframe-${zoomedId}`) as HTMLIFrameElement | null
        if (iframe?.contentWindow) {
          try {
            const gaudio = iframe.contentWindow.document.getElementById('gaudio') as HTMLAudioElement | null
            if (gaudio) { gaudio.muted = false; gaudio.play(); setMute(zoomedId, false); return }
          } catch { /* */ }
        }
        if (retries++ < 10 && !cancelled) setTimeout(tryUnmute, 500)
      }
      tryUnmute()
      return () => { cancelled = true }
    } else {
      for (const item of items) setMute(item.id, true)
    }
  }, [zoomedId, items])

  // 暴露给放大模式按钮
  useEffect(() => {
    const w = window as unknown as Record<string, unknown>
    w.__scSetMute = setMute
    w.__scMutedState = mutedRef
    w.__scReload = (id: string) => setRetryKey(prev => ({ ...prev, [id]: (prev[id] || 0) + 1 }))
  }, [mutedUI])

  return createPortal(
    <div style={{ display: visible ? 'contents' : 'none' }}>
      {items.map(item => {
        const isZoomed = item.id === zoomedId
        const isOnPage = pageItemIds.has(item.id)
        return (
          <div key={item.id} data-sc-layer={item.id}
            style={{
              position: 'fixed', zIndex: isZoomed ? 201 : 50,
              transition: isZoomed ? 'all 300ms ease' : 'none',
              pointerEvents: isZoomed ? 'auto' : 'none',
              ...(isZoomed ? {
                left: '50%', top: '50%', transform: 'translate(-50%, -50%)',
                height: '90vh', aspectRatio: '9 / 16', borderRadius: '0.5rem',
                overflow: 'hidden', boxShadow: '0 25px 50px -12px rgba(0,0,0,0.5)',
              } : {
                left: 0, top: 0, width: 0, height: 0, overflow: 'hidden',
                display: !isOnPage ? 'none' : undefined,
              }),
            }}>
            <iframe id={`sc-iframe-${item.id}`} key={`${item.id}-${retryKey[item.id] || 0}`}
              src={buildPlayerSrc(item, info, true)}
              className="h-full w-full border-0" allow="autoplay; encrypted-media; picture-in-picture"
              sandbox="allow-scripts allow-same-origin allow-presentation" title={item.name} />
          </div>
        )
      })}
      {/* 小窗标题栏覆盖层（z-index 高于 iframe 层） */}
      {!zoomedId && items.filter(i => pageItemIds.has(i.id)).map(item => (
        <div key={`overlay-${item.id}`} data-sc-overlay={item.id}
          style={{ position: 'fixed', zIndex: 51, pointerEvents: 'none', left: 0, top: 0, width: 0, height: 0, overflow: 'hidden' }}>
          <div className="flex items-center gap-1 px-2 py-1" style={{ background: 'linear-gradient(to bottom, rgba(0,0,0,0.7), transparent)' }}>
            <Monitor className="size-3 shrink-0 text-white" />
            <span className="truncate text-[10px] font-medium text-white">{item.name}</span>
            <div className="ml-auto flex items-center gap-1">
              <span className="size-1.5 shrink-0 rounded-full bg-green-500" />
              <button onClick={(e) => { e.stopPropagation(); setMute(item.id, !(mutedUI[item.id] ?? true)) }} className="rounded p-0.5 hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={mutedUI[item.id] ?? true ? t('agent:tooltip.unmute') : t('agent:tooltip.mute')}>
                {mutedUI[item.id] ?? true ? <VolumeX className="size-2.5 text-white/60" /> : <Volume2 className="size-2.5 text-white" />}
              </button>
              <button onClick={(e) => { e.stopPropagation(); setRetryKey(prev => ({ ...prev, [item.id]: (prev[item.id] || 0) + 1 })) }} className="rounded p-0.5 hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={t('agent:tooltip.reload')}>
                <RefreshCw className="size-2.5 text-white/60" />
              </button>
              <button onClick={(e) => { e.stopPropagation(); handleStop(item, info, onRemove) }} className="rounded p-0.5 hover:bg-white/20" style={{ pointerEvents: 'auto' }} title={t('agent:tooltip.close')}>
                <X className="size-2.5 text-white" />
              </button>
            </div>
          </div>
        </div>
      ))}
    </div>,
    document.body
  )
}
