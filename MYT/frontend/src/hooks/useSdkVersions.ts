import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { Events } from '@wailsio/runtime'
import * as SdkApiService from '../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import type { DeviceState } from '../types/index'
import { useDeployMode } from './useDeployMode'

/** SDK 版本阈值：低于此版本的设备部分 WS 功能不可用（事件推送等），列表提示升级。 */
export const MIN_SDK_VERSION = 208

/**
 * runWithConcurrency 滑动窗口批量执行：窗口满一个补一个（慢项超时不拖累后续）。
 * batchRun 收到当前窗口的一批（并发 1）并负责消化结果。
 * 千台规模全并发 = 瞬时千余拨号（客户端+设备双重冲击），批量设备查询一律限流。
 */
export async function runWithConcurrency<T>(
  items: T[], limit: number, batchRun: (batch: T[]) => Promise<void>,
): Promise<void> {
  let next = 0
  const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (next < items.length) {
      const item = items[next++]
      await batchRun([item])
    }
  })
  await Promise.all(workers)
}

/**
 * useSdkVersions 批量查询设备的 SDK 版本（currentVersion）结果按 device_id 索引，供设备列表展示 "v177"
 * 与低版本提示（< MIN_SDK_VERSION）。
 *
 * 版本的三个来源（互补）：
 * 1. **hello 实时推送**（sdkversion:updated）：设备重启（升级/回退）必经 WS 重连
 *    hello——版本变化的最权威失效信号，直达缓存（覆盖「升级完成后橙点不消失」：
 *    翻转清缓存依赖离线→在线边沿，升级时序下可能不翻转或翻转时查到旧值）
 * 2. 进页面批量查询（Promise.allSettled）：查询非认证失败（state!==2）设备——
 *    旧 SDK 设备可能被 WS 断连标离线但 HTTP 仍可达（老 SDK 无 hello，这是唯一源）
 * 3. 离线→在线翻转清缓存重查：设备重启的另一失效信号（与 1 双保险）
 *
 * 失败的设备版本记为 0（展示时不显示版本号），已查的不重复查（在飞守卫防重复批次）。
 *
 * v2 模式跳过查询：v2 的 SDK 版本是 string（"5.3.6.11"），int 化后语义失真且
 * DeviceItem/DeviceInfoBar 的版本徽标在 v2 模式一律隐藏（V2 版本只在
 * SdkVersionsModal 展示）——v2 下批量查询纯属浪费设备拨号，直接不查。
 */
export function useSdkVersions(devices: DeviceState[]) {
  const { mode } = useDeployMode()
  const [sdkVersions, setSdkVersions] = useState<Record<string, number>>({})
  // 上次各设备在线态（只在「离线→在线」翻转边沿清版本缓存——见下 effect；
  // 恒在线设备永不清：不会无限重查）
  const prevOnlineRef = useRef<Set<string>>(new Set())
  // 在飞标记：devices 数组随列表/指标事件高频换引用，结果落定前 effect 重跑会
  // 对同一批设备重复发起请求（慢设备 15s 超时 × 事件节奏 = 反复拨号）——在飞
  // 集合保证每台设备同一时刻最多一次在途查询
  const fetchingRef = useRef<Set<string>>(new Set())
  // mode 的 ref 镜像：异步批量查询结果晚到时（15s 超时级）需用「当前」mode 判定，
  // effect 闭包里的 mode 是发起时快照（可能还是 'local'）——见批量并入处的注释
  const modeRef = useRef(mode)
  useEffect(() => { modeRef.current = mode }, [mode])

  const fetchOne = useCallback(async (device: DeviceState): Promise<[string, number]> => {
    try {
      const info = await SdkApiService.GetAPIVersionInfo(device.ip) as { currentVersion?: number; stringCurrentVersion?: string } | null
      // v2 模式版本号是 string：解析为 number（非数字→0，列表不显示版本）；
      // 版本显示的 string 形态由 SdkCheckUpgrade/DeviceInfoBar 分支承接
      if (info?.stringCurrentVersion) {
        const n = Number.parseInt(info.stringCurrentVersion, 10)
        return [device.device_id, Number.isFinite(n) ? n : 0]
      }
      return [device.device_id, info?.currentVersion ?? 0]
    } catch {
      return [device.device_id, 0]
    }
  }, [])

  // hello 实时版本（设备重启必经 WS 重连 hello）：直达缓存——升级/回退完成后
  // 状态缓存立即跟上（橙点→绿点 / 橙点出现），不等批量查询/翻转。幂等合并。
  // v2 模式忽略：v2 主机也走 eventws 通道，hello 会带 SDKVersionNum——放进来
  // 会让 v2 徽标复活（v2 版本号是 string，int 化失真，且徽标在 v2 一律隐藏）。
  useEffect(() => {
    if (mode === 'v2') return
    const off = Events.On('sdkversion:updated', (ev: any) => {
      const d = (ev.data ?? ev) as { deviceId?: string; version?: number }
      const id = d?.deviceId
      const v = d?.version
      if (id && typeof v === 'number' && v > 0) {
        setSdkVersions(prev => prev[id] === v ? prev : { ...prev, [id]: v })
      }
    })
    return () => { off() }
  }, [mode])

  useEffect(() => {
    // v2 模式不查（见函数注释）：版本徽标在 v2 隐藏，查询浪费设备拨号。
    // 「离线→在线」翻转清缓存逻辑对 v2 无意义（缓存恒为空），一并跳过。
    if (mode === 'v2') return
    // 「离线→在线」翻转时清该设备的版本缓存——版本可能在离线期间变了
    // （SDK 升级/回退都会重启设备=必经翻转），残留旧值会显示错误状态
    // （如 209→204 回退后仍显示 v209 绿点，应为 v204 橙点）。
    // 只清翻转边沿（prevOnlineRef 对比）防无限重查：0 值（查询失败）与
    // 成功值都清——设备重查一次即可；恒在线设备永不清（不重查）。
    const onlineNow = new Set(devices.filter(d => d.state === 0).map(d => d.device_id))
    const prevOnline = prevOnlineRef.current
    prevOnlineRef.current = onlineNow
    const flippedOnline = [...onlineNow].filter(id => !prevOnline.has(id))
    if (flippedOnline.length > 0) {
      setSdkVersions(prev => {
        let changed = false
        const next = { ...prev }
        for (const id of flippedOnline) {
          if (next[id] !== undefined) { delete next[id]; changed = true }
        }
        return changed ? next : prev
      })
    }
    const toFetch = devices.filter(d =>
      d.state !== 2 && !(d.device_id in sdkVersions) && !fetchingRef.current.has(d.device_id))
    if (toFetch.length === 0) return
    toFetch.forEach(d => fetchingRef.current.add(d.device_id))
    // 结果无条件并入（不做 cancelled 丢弃）：版本是时不变事实，功能式更新安全；
    // 丢弃旧模式下慢设备（15s 超时）遇上 devices 高频换引用会陷入取消-重查循环。
    // 滑动窗口并发上限：千台规模全并发 = 瞬时千余拨号（客户端+设备双重冲击）；
    // 完成一个补一个，慢设备超时不拖累后续设备。
    // ⚠️ 并入前复查 mode：启动时 useDeployModeState 初始 'local'、异步 GetDeployMode
    // 之后才落 'v2'——窗口期发起的 v2 设备查询（Go 走 v2 分支返回 stringCurrentVersion，
    // parseInt 后成假版本号）会在 mode 已切 'v2'、清空 effect 跑完之后晚到写入，
    // 徽标复活。慢查询（15s 超时）远晚于模式落定，必须在此拦截。
    runWithConcurrency(toFetch, 8, async (batch) => {
      const results = await Promise.allSettled(batch.map(fetchOne))
      const next: Record<string, number> = {}
      for (const r of results) {
        if (r.status === 'fulfilled') {
          next[r.value[0]] = r.value[1]
          fetchingRef.current.delete(r.value[0])
        }
      }
      if (modeRef.current === 'v2') return
      setSdkVersions(prev => ({ ...prev, ...next }))
    })
  }, [devices, sdkVersions, fetchOne, mode])

  /** 手动刷新单台设备 SDK 版本（升级后回查） */
  const refreshOne = useCallback(async (device: DeviceState) => {
    const [id, ver] = await fetchOne(device)
    setSdkVersions(prev => ({ ...prev, [id]: ver }))
  }, [fetchOne])

  /** 低版本设备 ID 集合（0 < ver < MIN_SDK_VERSION）。引用稳定（memo），可安全传入 memo 化子组件。
   *  v2 模式跳过低版本判定：v2 设备（myt-sdk-go-v2）无 WS 功能阈值语义，MIN_SDK_VERSION
   *  是 V3 SDK 的指标，v2 版本 "1" 会被误报为低版本（orange 标签）；且 v2 不查版本，
   *  sdkVersions 恒为空。 */
  const lowSdkIds = useMemo(() => {
    if (mode === 'v2') return new Set<string>()
    const s = new Set<string>()
    for (const [id, ver] of Object.entries(sdkVersions)) {
      if (ver > 0 && ver < MIN_SDK_VERSION) s.add(id)
    }
    return s
  }, [sdkVersions, mode])

  // v2 模式清空缓存：模式切换（local/public→v2）时 useState 持久残留旧查询值，
  // 徽标会继续显示；清空让「v2 恒空」假设真正成立（消费端拿空 map 不渲染徽标）。
  useEffect(() => {
    if (mode !== 'v2') return
    setSdkVersions(prev => Object.keys(prev).length === 0 ? prev : {})
  }, [mode])

  // v2 模式 sdkVersions 恒为空对象引用（不查不缓存），消费端（DeviceItem 徽标、
  // BatchOpsModal）拿到空 map 自然不渲染版本徽标——与「v2 隐藏版本徽标」一致。
  return { sdkVersions, lowSdkIds, refreshOne }
}
