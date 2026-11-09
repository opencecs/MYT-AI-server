import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as VpcApi from '../../../api/vpcApi'
import { DomainRouteEntry, VpcGroup, VpcNodeRule } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import VpcModal from '../shared/VpcModal'
import TargetPickerList from '../shared/TargetPickerList'
import DomainRuleEditor from '../shared/DomainRuleEditor'
import { DomainRule, buildDomains, filterSelectableRules } from '../shared/VpcDomainShared'

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  allRules: VpcNodeRule[]
  onClose: () => void
  onSaved: () => void
}

const EMPTY_RULES: DomainRule[] = [{ prefix: 'domain:', value: '' }]

/**
 * 中转路由添加弹窗：与 AddRuleModal 同款左右布局（左列容器单选、右列编辑器）。
 * 目标容器必选后才填目标 VPC 节点；保存 = SetDomainRoutes 追加一条（数组顺序即优先级）。
 * 编辑已有路由仍走表格内 VpcModal（容器只读），避免弹窗内切容器造成 A 的规则写到 B。
 */
export default function AddRouteModal({ selectedIP, groups, allRules, onClose, onSaved }: Props) {
  const { t } = useTranslation()
  const containers = useMemo(() => filterSelectableRules(allRules), [allRules])
  const [target, setTarget] = useState('')
  const [domainRules, setDomainRules] = useState<DomainRule[]>(EMPTY_RULES)
  const [vpcID, setVpcID] = useState('')
  const [saving, setSaving] = useState(false)

  /** 保存 = 该容器现有 routes 末尾追加一条后 SetDomainRoutes 全量提交 */
  const handleSave = async () => {
    if (!selectedIP || !target) return
    const domains = buildDomains(domainRules)
    if (!domains.length || !vpcID) { showToast(t('vpc:toast.fillDomainRule'), 'error'); return }
    setSaving(true)
    try {
      const current = await VpcApi.GetDomainRoutes(selectedIP, target)
      const base: DomainRouteEntry[] = current?.routes || []
      await VpcApi.SetDomainRoutes(selectedIP, target, [...base, { domains, vpcID: Number(vpcID) }])
      showToast(t('vpc:toast.setSuccess'))
      onSaved()
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setSaving(false) }
  }

  return (
    <VpcModal title={t('vpc:route.addRouteTitle')} subtitle={t('vpc:route.dialogSubtitle')} onClose={onClose} width="w-[720px]"
      footer={<>
        <button onClick={onClose} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
        <button onClick={handleSave} disabled={saving || !target}
          className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
          {saving ? t('vpc:route.saving') : t('vpc:domainShared.saveApply')}
        </button>
      </>}>
      <div className="grid grid-cols-[200px_1fr] gap-4 h-[52vh]">
        {/* 左列：容器单选 */}
        <div className="border-r border-gray-100 pr-3 flex flex-col min-h-0">
          <p className="text-[10px] text-gray-400 mb-1.5 shrink-0">{t('vpc:domainShared.selectTarget')}</p>
          <TargetPickerList containers={containers} value={target} onChange={setTarget} />
        </div>
        {/* 右列：域名规则 + 目标节点（选中容器后出现） */}
        <div className="flex flex-col min-h-0 overflow-y-auto">
          {!target ? (
            <p className="text-[11px] text-gray-400 py-8 text-center">{t('vpc:domainShared.selectTargetFirst')}</p>
          ) : (
            <div className="space-y-3">
              <div>
                <label className="text-[10px] text-gray-400 block mb-1.5">{t('vpc:route.domainRules')}</label>
                <DomainRuleEditor rules={domainRules} onChange={setDomainRules} />
              </div>
              <div>
                <label className="text-[10px] text-gray-400 block mb-1.5">{t('vpc:route.targetVpcNode')}</label>
                <select value={vpcID} onChange={e => setVpcID(e.target.value)}
                  className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded-lg outline-none focus:border-blue-400 bg-white">
                  <option value="">{t('vpc:domainShared.selectVpcNode')}</option>
                  {groups.map(g => g.vpcs?.list?.map(n => (
                    <option key={n.id} value={String(n.id)}>{g.alias} / {n.remarks || n.protocol}</option>
                  )))}
                </select>
              </div>
              <p className="text-[10px] text-gray-400">{t('vpc:route.appendHint')}</p>
            </div>
          )}
        </div>
      </div>
    </VpcModal>
  )
}
