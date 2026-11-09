import { useEffect, useRef, useState } from 'react'
import * as VpcApi from '../../../api/vpcApi'
import { DomainRouteEntry, VpcNodeRule } from '../../../types/index'
import { filterSelectableRules } from '../shared/VpcDomainShared'

export interface RouteRow {
  containerID: string
  containerName: string
  containerIP: string
  routes: DomainRouteEntry[]
}

export interface RouteOverviewState {
  rows: RouteRow[]
  loaded: number
  total: number
  loading: boolean
}

const EMPTY: RouteOverviewState = { rows: [], loaded: 0, total: 0, loading: false }

/**
 * 中转总览聚合数据源：选中设备后自动并发拉取每个可写容器的 GetDomainRoutes。
 * 分批并发（5/批）+ 进度；staleRef 防竞态。
 *
 * stale-while-revalidate：已有数据先继续渲染（不清空重查），后台刷新完成后原地更新。
 * rows 按 containerID 排序、数据不变时刷新结果与旧数据相同，无视觉变化 = 不闪。
 *
 * rules 依赖收敛为「可写容器 ID 集合 key」：外层 rules 数组引用变化（tab 切入沿、
 * 心跳重拉、无关字段更新）不再触发全量重查，仅容器集合真实增减时才重查。
 */
export function useRouteOverview(selectedIP: string, rules: VpcNodeRule[], refreshKey = 0): RouteOverviewState {
  const [state, setState] = useState<RouteOverviewState>(EMPTY)
  const staleRef = useRef(0)

  // 容器集合 key：外层 rules 抖动（引用变化但内容不变）不应触发重查
  const containerKey = filterSelectableRules(rules).map(r => r.containerID).join(',')

  useEffect(() => {
    if (!selectedIP) {
      setState(EMPTY)
      return
    }
    const seq = ++staleRef.current
    const containers = filterSelectableRules(rules)
    const total = containers.length
    // SWR：无旧数据才亮 loading 骨架（首查/切设备）；有旧数据后台静默刷新，
    // 刷新全程保留旧 rows（部分 collect 不顶掉旧行）
    let hadData = false
    setState(s => {
      hadData = s.rows.length > 0
      return hadData ? { ...s, total, loading: true } : { ...EMPTY, total, loading: true }
    })

    const collect: RouteRow[] = []
    let loadedCount = 0
    const load = async () => {
      for (let i = 0; i < containers.length; i += 5) {
        if (seq !== staleRef.current) return
        const batch = containers.slice(i, i + 5)
        await Promise.all(batch.map(async r => {
          try {
            const result = await VpcApi.GetDomainRoutes(selectedIP, r.containerID)
            if (seq !== staleRef.current) return
            const routes = result?.routes || []
            if (routes.length > 0) {
              collect.push({ containerID: r.containerID, containerName: `${r.containerName} (${r.containerIP})`, containerIP: r.containerIP, routes })
            }
          } catch { /* 单容器失败不阻断 */ }
        }))
        if (seq !== staleRef.current) return
        loadedCount = Math.min(loadedCount + batch.length, total)
        setState(s => (hadData ? { ...s, loaded: loadedCount, total, loading: true } : { rows: [...collect], loaded: loadedCount, total, loading: true }))
      }
      if (seq !== staleRef.current) return
      // 按 containerID 排序：并发批次返回顺序不稳定，排序后数据不变时行 key 不变（避免无谓重排闪烁）
      collect.sort((a, b) => a.containerID.localeCompare(b.containerID))
      setState({ rows: collect, loaded: total, total, loading: false })
    }

    load()
    return () => { staleRef.current = seq + 1 }
    // rules 只经 containerKey 参与触发（行构建用捕获的 containers），故不列入依赖
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIP, containerKey, refreshKey])

  return state
}
