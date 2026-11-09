import { useCallback, useEffect, useMemo, useState } from 'react'
import * as V4Service from '../../../bindings/github.com/moyunteng/myt/v4servicewrapper'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { Events } from '@wailsio/runtime'
import { cast } from '../../utils/cast'
import { DeviceState } from '../../types/index'
import type { RomVariantRow } from './romShared'

/** 归一为纯 IP（V4 缓存 DeviceID/IP 字段都用纯 IP，与 Go v4HostIP 同口径）。 */
function v4HostIP(ident: string): string {
  const i = ident.lastIndexOf(':')
  return i > 0 ? ident.slice(0, i) : ident
}

/**
 * ROM 页数据 hook（从 RomPage 抽出，保持页面文件 ≤300 行）。
 *
 * 设备列表：与云机页同源——DiscoverService.GetDeviceList()（V4 模式下即 deviceListV4，
 * 8088 主机）+ devicelist:updated 事件驱动在线状态刷新。ListRomHosts 与它同源
 * （Go v4HostsOf(configManager.GetDeviceList())），故不再单独调 ListRomHosts。
 *
 * 选中语义：单选一台主机——host 由选中设备派生（ip || device_id 归一为纯 IP），
 * 所有 ROM 操作（list/pull/upload/delete）按它执行；未选中时 variants 为空。
 * 选中主机被删（设备列表刷新后不存在）→ 自动清空选中。
 */
export function useRomPage() {
  const [devices, setDevices] = useState<DeviceState[]>([])
  const [selectedDevice, setSelectedDevice] = useState<DeviceState | null>(null)
  const [root, setRoot] = useState('')
  const [variants, setVariants] = useState<RomVariantRow[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [searchKw, setSearchKw] = useState('')
  // 操作中的变体名（按钮禁用）
  const [busyVariant, setBusyVariant] = useState('')
  // 弹窗
  const [pullTarget, setPullTarget] = useState<RomVariantRow | null>(null)
  const [uploadOpen, setUploadOpen] = useState(false)
  // 上传弹窗预填的变体名（行内上传带入；工具栏/空态打开时为空 = 新建变体）
  const [uploadVariant, setUploadVariant] = useState('')

  // 设备列表：初始拉一次 + devicelist:updated 事件驱动（在线状态由后端探活推送）
  useEffect(() => {
    const apply = (list: DeviceState[]) => {
      const next = Array.isArray(list) ? list : []
      setDevices(next)
      // 选中设备被删 → 清空选中（host 随之清空，右侧显示引导空态）
      setSelectedDevice(prev =>
        prev && next.some(d => d.device_id === prev.device_id) ? prev : null,
      )
    }
    DiscoverService.GetDeviceList().then(list => apply(cast<DeviceState[]>(list))).catch(() => {})
    const off = Events.On('devicelist:updated', (ev: unknown) => {
      const anyEv = ev as { data?: unknown }
      const list = cast<DeviceState[]>((anyEv.data ?? ev) as DeviceState[])
      if (Array.isArray(list)) apply(list)
    })
    return () => { off() }
  }, [])

  // 选中主机：由选中设备派生（与 Go ListRomHosts 的 v4HostIP 归一口径一致）
  const host = useMemo(() => {
    if (!selectedDevice) return ''
    const ident = selectedDevice.ip || selectedDevice.device_id
    return ident ? v4HostIP(ident) : ''
  }, [selectedDevice])

  const load = useCallback(async (targetHost: string, silent = false) => {
    if (!targetHost) {
      setVariants([])
      setLoading(false)
      return
    }
    if (!silent) setLoading(true)
    setError('')
    try {
      const res = await V4Service.ListRomImages(targetHost)
      if (!res) {
        setVariants([])
        return
      }
      setRoot(res.root ?? '')
      const rows: RomVariantRow[] = (res.variants ?? []).map(v => ({
        ...v,
        missing: v.missing ?? [],
        files: v.files ?? [],
        totalSize: v.totalSize ?? 0,
      }))
      setVariants(rows)
    } catch (e) {
      setError(String(e))
      setVariants([])
    } finally {
      setLoading(false)
    }
  }, [])

  // 主机变化即拉列表
  useEffect(() => { load(host) }, [host, load])

  const refresh = useCallback(() => load(host, true), [host, load])

  const visible = useMemo(() => {
    const kw = searchKw.trim().toLowerCase()
    if (!kw) return variants
    return variants.filter(v => v.variant.toLowerCase().includes(kw))
  }, [variants, searchKw])

  return {
    devices, selectedDevice, setSelectedDevice,
    host, root, variants, visible, loading, error,
    searchKw, setSearchKw,
    busyVariant, setBusyVariant,
    pullTarget, setPullTarget,
    uploadOpen, setUploadOpen, uploadVariant, setUploadVariant,
    refresh,
  }
}
