import { useTranslation } from 'react-i18next'
import { VpcGroup, VpcNodeRule } from '../../../types/index'
import { RouteOverviewState } from './useRouteOverview'
import VpcRouteTable from './VpcRouteTable'

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  rules: VpcNodeRule[]
  /** 总览数据由父组件（VpcManagement）常驻持有，切 tab 重挂载不清零、不重查 */
  overview: RouteOverviewState
  onChanged: () => void
}

/**
 * 中转管理（重设计）：不再「先选容器才能看路由」。
 * 选中设备后自动并发聚合所有可写容器的路由成一张总览表（序号即优先级）；
 * 单条编辑/删除/上下移 = 前端内存副本改后 SetDomainRoutes 全量提交，
 * 编辑弹窗内容器为只读徽标（杜绝弹窗内切容器把 A 的规则整写到 B 的覆盖事故）。
 *
 * 本组件为纯展示：数据源 useRouteOverview 提升至 VpcManagement 层常驻，
 * 避免 tab 条件渲染卸载导致 mount 清零闪骨架。
 */
function VpcRouteSection({ selectedIP, groups, rules, overview, onChanged }: Props) {
  const { t } = useTranslation()

  return (
    <div className="space-y-3">
      {!selectedIP && <div className="text-xs text-gray-400 text-center py-12">{t('vpc:domain.selectDeviceFirst')}</div>}
      {selectedIP && (
        <VpcRouteTable
          selectedIP={selectedIP}
          groups={groups}
          rules={rules}
          overview={overview}
          onChanged={onChanged}
        />
      )}
    </div>
  )
}

export default VpcRouteSection
