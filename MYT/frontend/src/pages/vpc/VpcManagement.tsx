import { useState, memo } from 'react'
import { useTranslation } from 'react-i18next'
import { FolderTree, Network, Globe, ArrowLeftRight, EthernetPort, RefreshCw } from 'lucide-react'
import DeviceSidebar from '../../components/DeviceSidebar'
import { useVpcData } from './hooks/useVpcData'
import { useDomainRouteOverviews } from './hooks/useDomainRouteOverviews'
import VpcGroupSection from './group/VpcGroupSection'
import VpcRulesSection from './rules/VpcRulesSection'
import VpcDomainSection from './domain/VpcDomainSection'
import VpcRouteSection from './domain/VpcRouteSection'
import VpcNicSection from './nic/VpcNicSection'


type TabKey = 'groups' | 'rules' | 'domains' | 'route' | 'nic'

const tabs: { key: TabKey; label: string; icon: typeof FolderTree }[] = [
  { key: 'groups', label: 'tab.groups', icon: FolderTree },
  { key: 'rules', label: 'tab.rules', icon: Network },
  { key: 'domains', label: 'tab.domains', icon: Globe },
  { key: 'route', label: 'tab.route', icon: ArrowLeftRight },
  { key: 'nic', label: 'tab.nic', icon: EthernetPort },
]

function VpcManagement() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<TabKey>('groups')
  const { selectedIP, setSelectedIP, groups, rules, loading, refresh } = useVpcData()
  // 域名/中转总览数据提升到本层常驻（主 tab 为条件渲染，section 卸载重建时
  // 数据不清零、不重查首帧即渲染；与 groups/rules 同待遇），消除切 tab 闪骨架
  const { domain: domainOverview, route: routeOverview, refreshDomain, refreshRoute } = useDomainRouteOverviews(selectedIP, groups, rules)

  // 统计徽章：分组数 / 节点数（全部分组节点总和）/ 绑定数（容器规则数）
  const nodeCount = groups.reduce((acc, g) => acc + (g.vpcs?.list?.length || 0), 0)
  const stats = [
    { label: t('vpc:header.statGroups'), value: groups.length },
    { label: t('vpc:header.statNodes'), value: nodeCount },
    { label: t('vpc:header.statRules'), value: rules.length },
  ]

  const ActiveSection = {
    groups: <VpcGroupSection selectedIP={selectedIP} groups={groups} onRefresh={refresh} />,
    rules: <VpcRulesSection selectedIP={selectedIP} groups={groups} rules={rules} onRefresh={refresh} />,
    domains: <VpcDomainSection selectedIP={selectedIP} rules={rules} overview={domainOverview} onChanged={refreshDomain} />,
    route: <VpcRouteSection selectedIP={selectedIP} groups={groups} rules={rules} overview={routeOverview} onChanged={refreshRoute} />,
    nic: <VpcNicSection selectedIP={selectedIP} />,
  }[activeTab]

  return (
    <div className="h-full flex">
      <DeviceSidebar selectedIP={selectedIP} onSelect={setSelectedIP} />
      <div className="flex-1 flex flex-col min-w-0">
        {/* 顶部工具栏：标题 + 统计徽章 + 刷新 */}
        <div className="flex items-center justify-between px-5 py-3 bg-white border-b border-gray-100">
          <div className="flex items-center gap-3">
            <h2 className="text-sm font-semibold text-gray-800">{t('vpc:header.title')}</h2>
            <div className="flex items-center gap-1.5">
              {stats.map(s => (
                <span key={s.label}
                  className="inline-flex items-center gap-1 text-[11px] px-2 py-0.5 rounded-full bg-gray-100 text-gray-500">
                  {s.label}
                  <span className="font-semibold text-gray-700">{s.value}</span>
                </span>
              ))}
            </div>
          </div>
          <button onClick={refresh} disabled={loading}
            className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs text-gray-500 bg-white border border-gray-200 rounded-lg hover:bg-gray-50 hover:text-gray-700 disabled:opacity-50 transition-colors">
            <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
            {t('button.refresh')}
          </button>
        </div>

        {/* 主 tab：图标 + 下划线式 */}
        <div className="flex border-b border-gray-100 px-5 gap-1">
          {tabs.map(tab => {
            const Icon = tab.icon
            const active = activeTab === tab.key
            return (
              <button key={tab.key} onClick={() => setActiveTab(tab.key)}
                className={`flex items-center gap-1.5 px-4 py-2.5 text-xs border-b-2 -mb-px transition-colors ${
                  active
                    ? 'border-blue-500 text-blue-600 font-medium'
                    : 'border-transparent text-gray-500 hover:text-gray-700'
                }`}>
                <Icon size={14} />
                {t('vpc:' + tab.label)}
              </button>
            )
          })}
        </div>

        <div className="flex-1 overflow-y-auto p-5 bg-gray-50/50">
          {ActiveSection}
        </div>
      </div>
    </div>
  )
}

export default memo(VpcManagement)
