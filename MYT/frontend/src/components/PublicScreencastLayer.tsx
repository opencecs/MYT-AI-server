import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { onEvent } from '../utils/wailsEvent'
import { useDeployMode } from '../hooks/useDeployMode'

interface ScreencastWindow {
  id: string
  deviceId: string
  indexNum: number
  playerUrl: string
  title: string
}

/**
 * 公网模式投屏层：监听 Go 端事件，用 iframe 加载 myt-agent player URL。
 * 与 myt-agent 网页端的 ScreenCastPanel 方式一致——同源 iframe 加载 play.html。
 */
export function PublicScreencastLayer() {
  const { mode } = useDeployMode()
  const { t } = useTranslation()
  const [windows, setWindows] = useState<ScreencastWindow[]>([])

  useEffect(() => {
    if (mode !== 'public') return

    const offStart = onEvent('public-screencast:start', (data: any) => {
      if (!data || !data.deviceId) return
      const id = `${data.deviceId}_${data.indexNum}`
      setWindows(prev => {
        if (prev.find(w => w.id === id)) return prev
        return [...prev, {
          id,
          deviceId: data.deviceId,
          indexNum: data.indexNum,
          playerUrl: data.playerUrl,
          title: data.title || id,
        }]
      })
    })

    const offStop = onEvent('public-screencast:stop', (data: any) => {
      if (!data || !data.deviceId) return
      const id = `${data.deviceId}_${data.indexNum}`
      setWindows(prev => prev.filter(w => w.id !== id))
    })

    const offStopAll = onEvent('public-screencast:stop-all', () => {
      setWindows([])
    })

    return () => { offStart(); offStop(); offStopAll() }
  }, [mode])

  const closeWindow = useCallback((id: string) => {
    setWindows(prev => prev.filter(w => w.id !== id))
  }, [])

  if (mode !== 'public' || windows.length === 0) return null

  // 计算网格布局
  const cols = Math.min(windows.length, 3)
  const rows = Math.ceil(windows.length / cols)

  return (
    <div className="fixed inset-0 z-[100] bg-black/80 flex flex-col">
      <div className="flex items-center justify-between px-4 py-2 bg-black/90">
        <span className="text-sm text-white font-medium">
          {t('stream:screencast.title', { count: windows.length })}
        </span>
        <button
          onClick={() => setWindows([])}
          className="px-3 py-1 text-xs text-white bg-red-500 hover:bg-red-600 rounded transition-colors"
        >
          {t('stream:screencast.closeAll')}
        </button>
      </div>
      <div
        className="flex-1 grid gap-1 p-1"
        style={{ gridTemplateColumns: `repeat(${cols}, 1fr)`, gridTemplateRows: `repeat(${rows}, 1fr)` }}
      >
        {windows.map(w => (
          <div key={w.id} className="relative bg-black rounded overflow-hidden flex flex-col">
            <div className="flex items-center justify-between px-2 py-1 bg-black/60">
              <span className="text-xs text-white truncate">{w.title}</span>
              <button
                onClick={() => closeWindow(w.id)}
                className="text-white/60 hover:text-white text-xs px-1"
              >
                ✕
              </button>
            </div>
            <iframe
              src={w.playerUrl}
              className="flex-1 w-full border-0"
              allow="autoplay; encrypted-media; picture-in-picture"
              sandbox="allow-scripts allow-same-origin allow-presentation"
              title={w.title}
            />
          </div>
        ))}
      </div>
    </div>
  )
}

export default PublicScreencastLayer
