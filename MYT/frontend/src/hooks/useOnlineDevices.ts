import { useState, useEffect, useCallback } from 'react'
import { Events } from '@wailsio/runtime'
import * as DiscoverService from '../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { DeviceState } from '../types/index'

const DEVICE_ONLINE = 0

/**
 * 在线设备列表（事件驱动）。
 * 后端 httpbeat goroutine 5 秒探活一次，状态变化时 emit `devicelist:updated`（含全量设备列表）。
 * 这里监听事件更新，不再前端轮询——避免重复请求 + 不阻塞渲染。
 * 启动时先拉一次（事件可能还没注册/还没触发），保证首屏有数据。
 */
export function useOnlineDevices() {
  const [devices, setDevices] = useState<DeviceState[]>([])

  const applyList = useCallback((list: DeviceState[]) => {
    setDevices(list.filter(d => d.state === DEVICE_ONLINE))
  }, [])

  useEffect(() => {
    // 首次加载：后端事件可能还没触发，先拉一次保证首屏有数据
    DiscoverService.GetDeviceList().then(list => applyList(list as DeviceState[])).catch(() => {})
    // 后续由 devicelist:updated 事件驱动更新（后端 goroutine 探活有变化才推）
    const off = Events.On('devicelist:updated', (ev: any) => {
      const list = (ev.data ?? ev) as DeviceState[]
      if (Array.isArray(list)) applyList(list)
    })
    return () => { off() }
  }, [applyList])

  // refresh 保留供手动刷新场景（如切模式后强制重拉）
  const refresh = useCallback(() => {
    DiscoverService.GetDeviceList().then(list => applyList(list as DeviceState[])).catch(() => {})
  }, [applyList])

  return { devices, refresh }
}
