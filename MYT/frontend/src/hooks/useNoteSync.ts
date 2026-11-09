import { useState, useEffect, useCallback } from 'react'
import { Events } from '@wailsio/runtime'
import * as DiscoverService from '../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { cleanRecord } from '../utils/cleanRecord'

type NoteMap = Record<string, string>

/**
 * 设备备注同步 hook：加载设备备注 + 监听 devicenote:updated 事件实时刷新。
 * 所有需要设备备注的页面统一使用此 hook，修改后自动同步到所有 TAB。
 */
export function useDeviceNotes(): { notes: NoteMap; refresh: () => void; setNoteLocal: (id: string, value: string) => void } {
  const [notes, setNotes] = useState<NoteMap>({})

  const refresh = useCallback(() => {
    DiscoverService.GetAllDeviceNotes().then((result: Record<string, string | undefined>) => {
      setNotes(cleanRecord(result))
    }).catch(() => {})
  }, [])

  useEffect(() => {
    refresh()
    const off = Events.On('devicenote:updated', (evt: unknown) => {
      const data = (evt as Record<string, unknown>)?.data ?? evt
      const id = (data as Record<string, unknown>)?.deviceID as string
      const note = (data as Record<string, unknown>)?.note as string
      if (id) {
        setNotes(prev => {
          const next = { ...prev }
          if (note) { next[id] = note } else { delete next[id] }
          return next
        })
      } else {
        // 全量刷新
        refresh()
      }
    })
    return () => { off() }
  }, [refresh])

  const setNoteLocal = useCallback((id: string, value: string) => {
    setNotes(prev => {
      const next = { ...prev }
      if (value) { next[id] = value } else { delete next[id] }
      return next
    })
  }, [])

  return { notes, refresh, setNoteLocal }
}

/**
 * 安卓备注同步 hook：加载安卓备注 + 监听 androidnote:updated 事件实时刷新。
 * 所有需要安卓备注的页面统一使用此 hook，修改后自动同步到所有 TAB。
 */
export function useAndroidNotes(): { notes: NoteMap; refresh: () => void; setNoteLocal: (id: string, value: string) => void } {
  const [notes, setNotes] = useState<NoteMap>({})

  const refresh = useCallback(() => {
    DiscoverService.GetAllAndroidNotes().then((result: Record<string, string | undefined>) => {
      setNotes(cleanRecord(result))
    }).catch(() => {})
  }, [])

  useEffect(() => {
    refresh()
    const off = Events.On('androidnote:updated', (evt: unknown) => {
      const data = (evt as Record<string, unknown>)?.data ?? evt
      const id = (data as Record<string, unknown>)?.androidID as string
      const note = (data as Record<string, unknown>)?.note as string
      if (id) {
        setNotes(prev => {
          const next = { ...prev }
          if (note) { next[id] = note } else { delete next[id] }
          return next
        })
      } else {
        refresh()
      }
    })
    return () => { off() }
  }, [refresh])

  const setNoteLocal = useCallback((id: string, value: string) => {
    setNotes(prev => {
      const next = { ...prev }
      if (value) { next[id] = value } else { delete next[id] }
      return next
    })
  }, [])

  return { notes, refresh, setNoteLocal }
}
