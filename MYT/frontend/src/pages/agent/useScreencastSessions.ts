import { useCallback, Dispatch, SetStateAction, MutableRefObject } from 'react'
import * as MytAgentService from '../../../bindings/github.com/moyunteng/myt/mytagentservice'
import type { AgentConnInfo, ScreenCastItemData } from './agentTypes'

/** 处理投屏事件（add/remove/clear） */
export function useHandleScreencast(
  setScreencastItems: Dispatch<SetStateAction<ScreenCastItemData[]>>,
  connInfoRef: MutableRefObject<AgentConnInfo | null>,
) {
  return useCallback((payload: Record<string, unknown>) => {
    const action = (payload.screencast_action as string) || 'add'
    const raw = payload.screencast_items
    const items = (Array.isArray(raw) ? raw : [raw]).filter((i): i is ScreenCastItemData => i != null)
    if (items.length === 0) return

    if (action === 'clear') {
      setScreencastItems([])
    } else if (action === 'remove') {
      setScreencastItems(prev => prev.filter(i => !items.some(r => r.id === i.id || (r.device_id === i.device_id && r.container === i.container))))
    } else {
      // add：先加入列表，再调 /start 获取 signal_port/media_port
      setScreencastItems(prev => {
        const existing = new Set(prev.map(i => i.id))
        const newItems = items.filter(i => !existing.has(i.id))
        return [...prev, ...newItems]
      })
      const info = connInfoRef.current
      if (!info) return
      for (const item of items) {
        const containerParam = item.container ? `&container=${encodeURIComponent(item.container)}` : ''
        const signalParam = item.signal_port ? `&signal_port=${item.signal_port}` : ''
        const mediaParam = item.media_port ? `&media_port=${item.media_port}` : ''
        MytAgentService.HttpProxy('GET', `/api/screencast/start?device_id=${encodeURIComponent(item.device_id)}${containerParam}${signalParam}${mediaParam}`, '')
          .then(res => JSON.parse(res))
          .then((data: { ok?: boolean; signal_port?: number; udp_port?: number }) => {
            if (data.ok && data.signal_port) {
              setScreencastItems(prev => prev.map(i => i.id === item.id ? { ...i, signal_port: data.signal_port!, media_port: data.udp_port || i.media_port } : i))
            }
          })
          .catch(() => {})
      }
    }
  }, [])
}

/** 同步已有投屏会话（连接时） */
export function useSyncScreencastSessions(
  setScreencastItems: Dispatch<SetStateAction<ScreenCastItemData[]>>,
) {
  return useCallback(async () => {
    try {
      const res = await MytAgentService.HttpProxy('GET', '/api/screencast/list', '')
      const data = JSON.parse(res)
      const sessions: Array<{ device_id: string; container: string; device_name?: string }> = data.sessions || []
      if (sessions.length === 0) return
      const results = await Promise.all(sessions.map(async (s) => {
        try {
          const r = await MytAgentService.HttpProxy('GET', `/api/screencast/start?device_id=${encodeURIComponent(s.device_id)}&container=${encodeURIComponent(s.container || '')}`, '')
          return JSON.parse(r)
        } catch { return null }
      }))
      const items: ScreenCastItemData[] = []
      for (let i = 0; i < sessions.length; i++) {
        const s = sessions[i]
        const d = results[i]
        const name = s.device_name ? `${s.device_name} / ${s.container}` : (s.container || s.device_id.slice(0, 8))
        items.push({
          id: `sc_${s.device_id}_${s.container}`,
          name, device_id: s.device_id, device_name: s.device_name,
          container: s.container, index: 0, player_url: '',
          signal_port: d?.signal_port || 0, media_port: d?.udp_port || 0, status: 'active',
        })
      }
      setScreencastItems(items)
    } catch { /* ignore */ }
  }, [])
}
