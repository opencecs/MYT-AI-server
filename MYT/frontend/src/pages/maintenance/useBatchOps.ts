import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Events } from '@wailsio/runtime'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import { useDeployMode } from '../../hooks/useDeployMode'
import { getDeviceId } from '../../utils/deviceId'
import { cast } from '../../utils/cast'
import { showToast } from '../../components/Toast'
import * as SdkApiService from '../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { MIN_SDK_VERSION } from '../../hooks/useSdkVersions'
import type { DeviceState } from '../../types/index'

export interface BatchOpsRow {
  device: DeviceState
  ip: string
  current: number
  latest: number | null
  /** 仅在线设备可升级（离线/待认证提交了也执行不了）；离线/待认证可勾选，勾选后只参与「移除设备」 */
  upgradable: boolean
}

/** 勾选键：device_id 空/重复时用 ip 兜底——每行必须能独立勾选（含离线/待认证）。 */
export const rowKey = (r: BatchOpsRow) => r.device.device_id || r.ip

/** 是否进批量列表：在线/离线/待认证（与侧栏 DeviceSidebar 口径一致）。 */
const inScope = (d: DeviceState) => d.state === 0 || d.state === 1 || d.state === 2

/** 设备维度勾选键（与 rowKey 同口径，事件合并清理勾选用）。 */
const keyOf = (d: DeviceState) => d.device_id || d.ip

/**
 * 批量操作弹窗逻辑：主机列表快照 + 勾选 + 两个批量动作（升级 SDK / 移除设备）。
 * 勾选完全手动（默认一个不勾，用户明确要求），全选 = 全部行（含离线/待认证）。
 *
 * 列表来源：挂载时自拉全量设备（含离线/待认证，与侧栏口径一致）——页面级 devices
 * 只含在线+离线，用它会漏待认证设备。之后**只**订阅 `devicelist:updated`（真变化：
 * 增删/上下线）全量替换快照，让弹窗存续期间（含被 display:none 隐藏时）新添加的
 * 设备能出现（2026-09-27 修：此前只在挂载时拉一次，弹窗开着时新增设备永远不出现，
 * 用户切回来看到的还是旧快照，必须手动重开弹窗）。
 * ⚠️ 不订阅 `devicestats:updated`（心跳，每轮全量）：那会把列表按心跳反复重算，
 * 是原注释担心的「重算列表」来源；且勾选在事件到达时只做「已消失设备」清理，
 * 用户已勾选的设备一律保留（自动逻辑不得改写用户 UI 意图）。
 *
 * 移除走 DiscoverService.DeleteDevices（Go 侧广播 devicelist:updated，各列表自动刷新）。
 */
export function useBatchOps(
  sdkVersions: Record<string, number>,
  onClose: () => void,
) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { addTask } = useTaskQueue()

  const [loading, setLoading] = useState(true)
  /** 设备快照（挂载自拉 + devicelist:updated 全量替换） */
  const [devices, setDevices] = useState<DeviceState[]>([])
  /** 云端最新版本：一次全局查询（Go 侧 10 分钟缓存），失败/未知时 latest=null 只按 <MIN_SDK_VERSION 判定 */
  const [latest, setLatest] = useState<number | null>(null)
  const [checked, setChecked] = useState<Set<string>>(new Set())
  const [submitting, setSubmitting] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const [confirmFormatDisk, setConfirmFormatDisk] = useState(false)
  // 事件已到达标记：初次快照与 devicelist:updated 竞态时，慢的那份不覆盖新的
  // （否则弹窗刚打开就收到事件、随后初次快照返回，会把新设备又盖回旧列表）
  const sawEventRef = useRef(false)

  useEffect(() => {
    let cancelled = false
    const run = async () => {
      // 打开瞬间自拉全量快照（含离线/待认证，与侧栏口径一致）
      let list: DeviceState[] = []
      try {
        const raw = await DiscoverService.GetDeviceList() as DeviceState[]
        if (Array.isArray(raw)) list = raw.filter(inScope)
      } catch { /* 拉不到列表就出空态 */ }
      let cloudLatest: number | null = null
      try {
        const v = await SdkApiService.GetLatestSdkVersion() as number
        if (v > 0) cloudLatest = v
      } catch { /* 云端不可知，降级 */ }
      if (cancelled) return
      // 初次快照期间若已收到 devicelist:updated（更新），不再用旧快照覆盖
      if (!sawEventRef.current) {
        setDevices(list)
        // 默认一个都不勾（用户明确要求手动勾选）：勾选完全由用户决定，
        // 离线/待认证行同样可单独勾（用于移除），全选按钮一键全勾
        setChecked(new Set())
      }
      setLatest(cloudLatest)
      setLoading(false)
    }
    run()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 快照语义：只跑一次
  }, [])

  // 设备列表真变化（增删设备/上下线）：全量替换快照 → rows 重算，新设备立刻出现。
  // 勾选只清掉「已消失设备」的键，其余原样保留——不重置用户意图（项目铁律）。
  // 不订阅 devicestats:updated：心跳每轮全量，订阅会让列表反复重算且勾选被冲掉。
  useEffect(() => {
    const off = Events.On('devicelist:updated', (ev: any) => {
      const list = cast<DeviceState[]>(ev.data ?? ev)
      if (!Array.isArray(list)) return
      sawEventRef.current = true
      const next = list.filter(inScope)
      setDevices(next)
      setChecked(prev => {
        const alive = new Set(next.map(keyOf))
        const kept = new Set([...prev].filter(k => alive.has(k)))
        return kept.size === prev.size ? prev : kept
      })
    })
    return () => { off() }
  }, [])

  // 行由设备快照派生（在线在上，其余在下）：sdkVersions/latest 变化时也重算可升级态，
  // 但 checked 独立保存 —— 重算不会动用户勾选
  const rows = useMemo<BatchOpsRow[]>(() => {
    const sorted = [...devices].sort((a, b) => Number(a.state !== 0) - Number(b.state !== 0))
    return sorted.map(device => {
      const current = sdkVersions[device.device_id] ?? 0
      const upgradable = device.state === 0
        && ((current > 0 && current < MIN_SDK_VERSION) || (latest != null && latest > current))
      return { device, ip: getDeviceId(device, mode) || device.ip, current, latest, upgradable }
    })
  }, [devices, sdkVersions, latest, mode])

  const toggle = (id: string) => {
    setChecked(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleAll = () => {
    setChecked(prev => (prev.size === rows.length ? new Set() : new Set(rows.map(rowKey))))
  }

  // 一键选中/取消所有离线设备（toggle：该类全勾时再点取消，否则补勾；不影响其他勾选）
  const selectOffline = () => {
    setChecked(prev => {
      const next = new Set(prev)
      const keys = rows.filter(r => r.device.state === 1).map(rowKey)
      const allOn = keys.length > 0 && keys.every(k => next.has(k))
      if (allOn) keys.forEach(k => next.delete(k))
      else keys.forEach(k => next.add(k))
      return next
    })
  }

  // 一键选中/取消所有认证中的设备（同上，state === 2）
  const selectAuthPending = () => {
    setChecked(prev => {
      const next = new Set(prev)
      const keys = rows.filter(r => r.device.state === 2).map(rowKey)
      const allOn = keys.length > 0 && keys.every(k => next.has(k))
      if (allOn) keys.forEach(k => next.delete(k))
      else keys.forEach(k => next.add(k))
      return next
    })
  }

  // 升级只作用于「勾选 ∩ 可升级（在线）」；移除作用于全部勾选（含离线/待认证）
  const upgradableChecked = rows.filter(r => checked.has(rowKey(r)) && r.upgradable).length

  const submitUpgrade = async () => {
    const targets = rows.filter(r => checked.has(rowKey(r)) && r.upgradable)
    if (targets.length === 0 || submitting) return
    setSubmitting(true)
    try {
      await addTask({
        type: 'sdkUpgrade',
        targets: targets.map(r => ({ deviceIp: r.ip, name: r.device.device_id })),
        deviceIps: targets.map(r => r.ip),
        total: targets.length,
      })
      showToast(t('maintenance:batchOps.upgradeQueued'), 'success')
      onClose()
    } catch {
      showToast(t('maintenance:batchOps.upgradeFailed'), 'error')
    } finally {
      setSubmitting(false)
    }
  }

  const submitRemove = async () => {
    // 勾选键是 rowKey（可能回退到 ip），删除要 device_id，从 rows 反查
    const ids = rows.filter(r => checked.has(rowKey(r))).map(r => r.device.device_id).filter(Boolean)
    if (ids.length === 0 || submitting) return
    setSubmitting(true)
    setConfirmRemove(false)
    try {
      await DiscoverService.DeleteDevices(JSON.stringify(ids))
      showToast(t('maintenance:batchOps.removeSuccess', { count: ids.length }), 'success')
      onClose()
    } catch {
      showToast(t('maintenance:batchOps.removeFailed'), 'error')
    } finally {
      setSubmitting(false)
    }
  }

  /** 勾选中且在线（state===0）的行数：格式化只作用于在线设备（与单台口径一致） */
  const onlineCheckedCount = rows.filter(r => checked.has(rowKey(r)) && r.device.state === 0).length

  const submitFormatDisk = async () => {
    const targets = rows.filter(r => checked.has(rowKey(r)) && r.device.state === 0)
    if (targets.length === 0 || submitting) return
    setSubmitting(true)
    setConfirmFormatDisk(false)
    try {
      await addTask({
        type: 'formatDisk',
        targets: targets.map(r => ({ deviceIp: r.ip })),
        deviceIps: targets.map(r => r.ip),
        total: targets.length,
      })
      showToast(t('maintenance:batchOps.formatQueued', { count: targets.length }), 'success')
      onClose()
    } catch {
      showToast(t('maintenance:batchOps.formatFailed'), 'error')
    } finally {
      setSubmitting(false)
    }
  }

  return {
    loading, rows, checked, submitting, confirmRemove, confirmFormatDisk,
    setConfirmRemove, setConfirmFormatDisk, toggle, toggleAll, selectOffline, selectAuthPending, upgradableChecked, onlineCheckedCount, submitUpgrade, submitRemove, submitFormatDisk,
  }
}
