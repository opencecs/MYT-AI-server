import { useEffect, useRef, useState } from 'react'
import * as VpcApi from '../../../api/vpcApi'
import { VpcGroup, VpcNodeRule } from '../../../types/index'
import { filterSelectableRules, parseDomainRule } from '../shared/VpcDomainShared'

export type ScopeKind = 'container' | 'global'
export type RuleKind = 'filter' | 'direct'

export interface DomainRow {
  /** 唯一键：kind|scope|containerID|index */
  key: string
  kind: RuleKind
  scope: ScopeKind
  containerID: string
  containerName: string
  containerIP: string
  rule: { prefix: string; value: string }
  raw: string
}

export interface DomainOverviewState {
  rows: DomainRow[]
  loaded: number
  total: number
  loading: boolean
  error: boolean
}

/** 逐条生成行 key（保存/删除后 index 变化不影响识别） */
function rowKey(kind: RuleKind, scope: ScopeKind, cid: string, index: number): string {
  return `${kind}|${scope}|${cid}|${index}`
}

function containerLabel(r: VpcNodeRule): string {
  return `${r.containerName} (${r.containerIP})`
}

const EMPTY: DomainOverviewState = { rows: [], loaded: 0, total: 0, loading: false, error: false }

/**
 * 域名总览聚合数据源：选中设备后自动并发拉取 全局过滤 + 每个可写容器的 filter/direct。
 * GET 只有按容器/全局查询，无聚合 API——前端分批并发（5/批）+ 进度提示。
 *
 * stale-while-revalidate：已有数据先继续渲染（不清空重查），后台刷新完成后原地更新。
 * 行 key 含 index，行集合未变时刷新结果与旧数据逐行同 key 同内容，无视觉变化 = 不闪。
 * staleRef 代际防竞态：设备切换/重载时旧结果按代际丢弃。
 *
 * rules 依赖收敛为「可写容器 ID 集合 key」：外层 rules 数组引用变化（tab 切入沿、
 * 心跳重拉、无关字段更新）不再触发全量重查，仅容器集合真实增减时才重查。
 */
export function useDomainOverview(selectedIP: string, groups: VpcGroup[], rules: VpcNodeRule[], refreshKey = 0): DomainOverviewState {
  const [state, setState] = useState<DomainOverviewState>(EMPTY)
  const staleRef = useRef(0)
  const selectedIPRef = useRef(selectedIP)
  selectedIPRef.current = selectedIP
  // hadDataRef 记录「上一次 rows 是否已有数据」：SWR 后台刷新沿用旧数据渲染。
  // 不用 setState updater 捕获局部变量——hadData 的判定与异步回调里多次
  // setState 之间存在竞态（updater 执行时机不确定），ref 同步更新无此问题。
  const hadDataRef = useRef(false)

  // 容器集合 key：外层 rules 抖动（引用变化但内容不变）不应触发重查
  const containerKey = filterSelectableRules(rules).map(r => r.containerID).join(',')

  useEffect(() => {
    if (!selectedIP) {
      hadDataRef.current = false
      setState(EMPTY)
      return
    }
    const seq = ++staleRef.current
    const containers = filterSelectableRules(rules)
    const total = containers.length + 1 // + 全局
    // SWR：无旧数据才亮 loading 骨架（首查/切设备）；有旧数据后台静默刷新，
    // 刷新全程保留旧 rows（部分 collect 不顶掉旧行，避免未重查到的容器行先消失再出现）
    const hadData = hadDataRef.current
    setState(s => (hadData ? { ...s, total, loading: true } : { ...EMPTY, total, loading: true }))

    const collect: { rows: DomainRow[] } = { rows: [] }
    const applyGlobal = (domains: string[]) => {
      domains.forEach((d, i) => collect.rows.push({
        key: rowKey('filter', 'global', 'global', i),
        kind: 'filter', scope: 'global', containerID: 'global', containerName: '', containerIP: '',
        rule: parseDomainRule(d), raw: d,
      }))
    }
    const applyContainer = (r: VpcNodeRule, kind: RuleKind, domains: string[]) => {
      domains.forEach((d, i) => collect.rows.push({
        key: rowKey(kind, 'container', r.containerID, i),
        kind, scope: 'container', containerID: r.containerID,
        containerName: containerLabel(r), containerIP: r.containerIP,
        rule: parseDomainRule(d), raw: d,
      }))
    }

    const runBatch = async (batch: Promise<void>[]) => {
      await Promise.all(batch.map(p => p.catch(() => { /* 单个容器失败不阻断整体 */ })))
    }

    const load = async () => {
      // 1) 全局过滤
      try {
        const g = await VpcApi.GetGlobalDomainFilters(selectedIP)
        if (seq !== staleRef.current) return
        applyGlobal(g?.domains || [])
      } catch { /* 全局查询失败不阻断（老固件可能不支持） */ }
      setState(s => (s.total === total ? { ...s, loaded: 1, rows: hadData ? s.rows : [...collect.rows] } : s))
      hadDataRef.current = hadData || collect.rows.length > 0

      // 2) 各容器 filter/direct 分批并发（5/批）
      for (let i = 0; i < containers.length; i += 5) {
        if (seq !== staleRef.current) return
        const batch = containers.slice(i, i + 5)
        await runBatch(batch.map(r => (async () => {
          const [f, d] = await Promise.all([
            VpcApi.GetDomainFilters(selectedIPRef.current, r.containerID),
            VpcApi.GetDomainDirects(selectedIPRef.current, r.containerID),
          ])
          if (seq !== staleRef.current) return
          applyContainer(r, 'filter', f?.domains || [])
          applyContainer(r, 'direct', d?.domains || [])
        })()))
        if (seq !== staleRef.current) return
        setState(s => (s.total === total ? { ...s, loaded: Math.min(s.loaded + batch.length, total), rows: hadData ? s.rows : [...collect.rows] } : s))
        hadDataRef.current = hadData || collect.rows.length > 0
      }
      if (seq !== staleRef.current) return
      setState({ rows: collect.rows, loaded: total, total, loading: false, error: false })
      hadDataRef.current = collect.rows.length > 0
    }

    load()
    return () => { staleRef.current = seq + 1 } // 卸载/切设备：作废本次
    // rules 只经 containerKey 参与触发（行构建用捕获的 containers），故不列入依赖
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIP, containerKey, refreshKey])

  // groups 仅用于表头统计展示（分组名），不参与数据行构建
  void groups

  return state
}

/** 组装一份容器维度的现有规则（行内编辑回显用） */
export function rulesForContainer(rows: DomainRow[], kind: RuleKind, containerID: string, scope: ScopeKind): string[] {
  return rows.filter(r => r.kind === kind && r.scope === scope && r.containerID === containerID).map(r => r.raw)
}

/** 冲突检测：同 raw 值同时出现在 过滤+直连，或 全局+容器 两种作用域 */
export function computeConflictKeys(rows: DomainRow[]): Set<string> {
  const byKind = new Map<string, Set<string>>()
  const byScope = new Map<string, Set<string>>()
  for (const r of rows) {
    const kSet = byKind.get(r.raw) || new Set<string>()
    kSet.add(r.kind)
    byKind.set(r.raw, kSet)
    const sSet = byScope.get(r.raw) || new Set<string>()
    sSet.add(r.scope)
    byScope.set(r.raw, sSet)
  }
  const conflicted = new Set<string>()
  for (const [raw, kinds] of byKind) {
    if (kinds.has('filter') && kinds.has('direct')) conflicted.add(raw)
  }
  for (const [raw, scopes] of byScope) {
    if (scopes.has('global') && scopes.has('container')) conflicted.add(raw)
  }
  return conflicted
}
