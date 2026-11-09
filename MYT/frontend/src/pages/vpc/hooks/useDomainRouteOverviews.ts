import { useState } from 'react'
import { VpcGroup, VpcNodeRule } from '../../../types/index'
import { useDomainOverview, DomainOverviewState } from '../domain/useDomainOverview'
import { useRouteOverview, RouteOverviewState } from '../domain/useRouteOverview'

/**
 * 域名/中转总览数据提升层：把 useDomainOverview / useRouteOverview 挂到
 * VpcManagement（页面级常驻组件），与 useVpcData 同层。
 *
 * 根因：主 tab 为条件渲染（切走即卸载），此前 overview 数据在各 section 的
 * hook 本地 state，切回 mount 时 useState 清零 + effect 重查 → 首帧必闪骨架。
 * 分组/规则 tab 不闪是因为数据在父组件 state 常驻，remount 首帧直接渲染。
 *
 * 提升后：切 tab 只换展示组件，hook 实例常驻不清零，mount 首帧即渲染缓存数据，
 * SWR（stale-while-revalidate）继续负责后台静默刷新。
 */

export interface UseDomainRouteOverviewsResult {
  domain: DomainOverviewState
  route: RouteOverviewState
  /** 保存/删除/复制成功后由 section 触发对应总览重拉 */
  refreshDomain: () => void
  refreshRoute: () => void
}

export function useDomainRouteOverviews(
  selectedIP: string,
  groups: VpcGroup[],
  rules: VpcNodeRule[],
): UseDomainRouteOverviewsResult {
  const [domainKey, setDomainKey] = useState(0)
  const [routeKey, setRouteKey] = useState(0)

  const domain = useDomainOverview(selectedIP, groups, rules, domainKey)
  const route = useRouteOverview(selectedIP, rules, routeKey)

  return {
    domain,
    route,
    refreshDomain: () => setDomainKey(k => k + 1),
    refreshRoute: () => setRouteKey(k => k + 1),
  }
}
