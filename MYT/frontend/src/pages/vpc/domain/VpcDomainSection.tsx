import { useTranslation } from 'react-i18next'
import { VpcNodeRule } from '../../../types/index'
import { DomainOverviewState } from './useDomainOverview'
import VpcDomainTable from './VpcDomainTable'

interface Props {
  selectedIP: string
  rules: VpcNodeRule[]
  /** 总览数据由父组件（VpcManagement）常驻持有，切 tab 重挂载不清零、不重查 */
  overview: DomainOverviewState
  onChanged: () => void
}

/**
 * 域名管理（重设计）：不再「先选容器才能看规则」。
 * 选中设备后自动并发聚合所有可写容器的 过滤/直连/全局 规则成一张总览表；
 * 编辑/删除/复制等操作全部在总览表上完成（单条操作 = 前端内存副本改后全量提交，
 * 设备端四组写入 API 均为按容器全量替换语义，见 VpcDomainTable 的保存实现）。
 *
 * 本组件为纯展示：数据源 useDomainOverview 提升至 VpcManagement 层常驻，
 * 避免 tab 条件渲染卸载导致 mount 清零闪骨架。
 */
function VpcDomainSection({ selectedIP, rules, overview, onChanged }: Props) {
  const { t } = useTranslation()

  return (
    <div className="space-y-3">
      {!selectedIP && <div className="text-xs text-gray-400 text-center py-12">{t('vpc:domain.selectDeviceFirst')}</div>}
      {selectedIP && (
        <VpcDomainTable
          selectedIP={selectedIP}
          rules={rules}
          overview={overview}
          onChanged={onChanged}
        />
      )}
    </div>
  )
}

export default VpcDomainSection
