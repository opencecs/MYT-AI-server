import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as DiscoverService from '../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as AndroidListService from '../../bindings/github.com/moyunteng/myt/androidlistservicewrapper'
import { AndroidInfo } from '../types/index'
import { onSlotStatus } from '../utils/wailsEvent'
import { resolveStatus } from '../utils/statusIntent'
import { cast } from '../utils/cast'

function filterAndSort(list: unknown, deviceId: string): AndroidInfo[] {
  const arr = cast<AndroidInfo[]>(Array.isArray(list) ? list : [])
  return arr
    .map(a => ({ ...a, status: resolveStatus(a) }))
    .filter(a => a.deviceId === deviceId).sort((a, b) => a.indexNum - b.indexNum)
}

/**
 * 指定设备的云机列表（全部坑位含已关机），刷新机制与主机 tab（useHostEvents）对齐：
 * 1. 初始拉取 GetAndroidList；选中设备经 SetSelectedDevices 进入后端查询范围（立即查一次）
 * 2. androidlist:updated 事件直接用事件携带的全量数据（不重新拉取）
 * 3. screenshot:slot-status 事件实时更新容器 status（云机操作后即时生效，不等后端轮询）
 *
 * 备份页与主机 tab（坑位表）共用。
 */
export function useDeviceAndroids(deviceId: string): AndroidInfo[] {
  const [androids, setAndroids] = useState<AndroidInfo[]>([])

  // 选中设备进入后端查询范围（与主机 tab 勾选行为一致）
  useEffect(() => {
    if (deviceId) AndroidListService.SetSelectedDevices([deviceId]).catch(() => {})
  }, [deviceId])

  // 初始拉取
  useEffect(() => {
    DiscoverService.GetAndroidList().then(al => {
      setAndroids(filterAndSort(al, deviceId))
    }).catch(() => {})
  }, [deviceId])

  // 安卓列表：事件驱动（事件自带数据）
  useEffect(() => {
    const off = Events.On('androidlist:updated', (ev: any) => {
      const list = ev?.data ?? ev
      if (Array.isArray(list)) setAndroids(filterAndSort(list, deviceId))
    })
    return () => { off() }
  }, [deviceId])

  // 云机操作后即时更新 status（匹配逻辑与 useHostEvents 一致：ip 或 deviceId + indexNum）
  useEffect(() => {
    const off = onSlotStatus(({ key, status }) => {
      const [id, idxStr] = key.split('_')
      const indexNum = Number(idxStr)
      if (!id || isNaN(indexNum)) return
      setAndroids(prev => {
        const idx = prev.findIndex(a => (a.ip === id || a.deviceId === id) && a.indexNum === indexNum)
        if (idx < 0 || prev[idx].status === status) return prev
        const next = [...prev]
        next[idx] = { ...next[idx], status }
        return next
      })
    })
    return () => { off() }
  }, [deviceId])

  return androids
}
