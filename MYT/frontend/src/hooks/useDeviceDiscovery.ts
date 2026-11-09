import { useState, useEffect, useCallback, useMemo } from 'react'
import { Events } from '@wailsio/runtime'
import * as DiscoverService from '../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import type { DeviceState } from '../types/index'
import { cast } from '../utils/cast'
import { USE_MOCK } from '../utils/mockData'
import { showToast } from '../components/Toast'
import { useDeployMode, type DeployMode } from './useDeployMode'

/**
 * pending 列表按部署模式兜底过滤（数据层，UI 不感知）：
 * 后端已按 mode 分文件隔离，这里防旧数据/异常来源串扰——
 * v2 入口只显示 v2 来源条目（version=v2），v3（local）入口只显示 v3；public 不扫描不过滤。
 * v4：设备数据里目前没有 V4 版本标识（Version 仍可能是 UDP 回复的 v3/v2/v0），
 * 若沿用版本过滤会把合法的 V4 设备全过滤掉，因此 v4 暂不做版本过滤
 * （后端已按 mode 分文件隔离，不会串台）。
 */
function filterPendingByMode(list: DeviceState[], mode: DeployMode): DeviceState[] {
  if (mode === 'v2') return list.filter(d => d.version === 'v2')
  if (mode === 'local') return list.filter(d => d.version === 'v3')
  // TODO(v4): V4 设备版本标识 / /health 字段确认后，改为按 version 过滤
  // （如 return list.filter(d => d.version === 'v4')），与 local/v2 的过滤策略对齐。
  return list
}

/**
 * 设备发现 + 待添加设备列表 共享 hook。
 * 两处消费方共用一份实现（消除 useHostManagement 与 DeviceSidebar 的重复）：
 * - 云机 tab（useHostManagement）：刷新按钮 + 添加设备弹窗
 * - 主机 tab（DeviceSidebar）：同上（仅主机页显示按钮，但 hook 常驻订阅）
 *
 * - pendingDevices：待添加列表，初始 GetPendingDevices + pendinglist:updated 事件流式更新
 * - addPendingDevices / addAllPendingDevices / clearPendingDevices：待添加列表操作
 * - refreshDevices + scanning：设备发现/刷新（RefreshDevices 增量合并，同步阻塞约 2s）
 */
export function useDeviceDiscovery() {
  const [rawPendingDevices, setPendingDevices] = useState<DeviceState[]>([])
  const [scanning, setScanning] = useState(false)
  const { mode } = useDeployMode()

  // 待添加设备：初始拉取 + 事件流式更新（按当前模式过滤后暴露给消费方）
  useEffect(() => {
    DiscoverService.GetPendingDevices().then(list => {
      setPendingDevices(cast<DeviceState[]>(list))
    }).catch(() => {})
    const off = Events.On('pendinglist:updated', (ev: any) => {
      const list = cast<DeviceState[]>(ev.data ?? ev)
      if (Array.isArray(list)) setPendingDevices(list)
    })
    // 公网拉取失败的错误事件（后端 60s 节流）：轮询路径的失败没有同步返回值，
    // 不 toast 的话网关故障时用户只看到空列表无任何提示
    const offErr = Events.On('devicelist:error', (ev: any) => {
      const msg = ev?.data ?? ev
      if (typeof msg === 'string' && msg) showToast(`公网设备拉取失败：${msg}`, 'error')
    })
    return () => { off(); offErr() }
  }, [])

  const pendingDevices = useMemo(
    () => filterPendingByMode(rawPendingDevices, mode),
    [rawPendingDevices, mode],
  )

  const addPendingDevices = useCallback(async (deviceIDs: string[]) => {
    await DiscoverService.AddPendingDevices(JSON.stringify(deviceIDs))
  }, [])

  const addAllPendingDevices = useCallback(async () => {
    await DiscoverService.AddAllPendingDevices()
  }, [])

  const clearPendingDevices = useCallback(async () => {
    await DiscoverService.ClearPendingDevices()
  }, [])

  // 设备发现：走 RefreshDevices（discoverAndUpdate 增量合并）——
  // 已存在设备同步 IP 漂移/型号，新设备进待添加列表；同步阻塞约 2s，
  // scanning 态覆盖真实扫描窗口（此前调 Discover() 是纯扫描，修不了 IP 漂移）。
  // 失败原因（public 网关登录/拉取失败等）经 toast 提示——后端已把错误作为
  // 返回值回传（Promise reject），不再静默吞掉。
  const refreshDevices = useCallback(async () => {
    if (USE_MOCK) return
    setScanning(true)
    try {
      await DiscoverService.RefreshDevices()
    } catch (err: any) {
      const reason = err?.message || String(err)
      console.error('Device discovery failed:', err)
      showToast(`刷新失败：${reason}`, 'error')
    } finally {
      setScanning(false)
    }
  }, [])

  return { pendingDevices, scanning, refreshDevices, addPendingDevices, addAllPendingDevices, clearPendingDevices }
}
