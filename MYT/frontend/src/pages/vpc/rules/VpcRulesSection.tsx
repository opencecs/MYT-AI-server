import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link2, Ban } from 'lucide-react'
import * as VpcApi from '../../../api/vpcApi'
import { VpcGroup, VpcNodeRule, AndroidInfo } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import TooltipCell from '../../../components/TooltipCell'
import { useConfirm } from '../../../hooks/useConfirm'
import ConfirmDialog from '../../../components/ConfirmDialog'
import SectionCard from '../shared/SectionCard'
import EmptyState from '../shared/EmptyState'
import AssignVpcModal from './AssignVpcModal'

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  rules: VpcNodeRule[]
  onRefresh: () => void
}

function VpcRulesSection({ selectedIP, groups, rules, onRefresh }: Props) {
  const { t } = useTranslation()
  const [selectedRules, setSelectedRules] = useState<VpcNodeRule[]>([])
  const [showAssign, setShowAssign] = useState(false)
  const [assignLoading, setAssignLoading] = useState(false)
  const { confirmAction, setConfirmAction, ask } = useConfirm()

  const openAssign = () => setShowAssign(true)

  // 三步弹窗的提交动作（容器/分组/节点状态都在弹窗内部，此处只做 API 调用）。
  // 返回 false 时弹窗保持打开（随机分配部分失败等场景）。
  const handleAssign = async (
    containers: AndroidInfo[], groupId: number | null, nodeId: number | null, mode: 'specified' | 'random',
  ): Promise<boolean> => {
    if (!selectedIP) return false
    setAssignLoading(true)
    try {
      if (mode === 'random') {
        const group = groups.find(g => g.id === groupId)
        if (!group?.vpcs?.list?.length) { showToast(t('vpc:toast.noAvailableNodes'), 'error'); return false }
        const nodes = group.vpcs.list
        let failCount = 0
        for (const c of containers) {
          try {
            const node = nodes[Math.floor(Math.random() * nodes.length)]
            await VpcApi.AddVpcRule(selectedIP, c.name, node.id)
          } catch { failCount++ }
        }
        if (failCount > 0) {
          showToast(t('vpc:toast.containerAssignFailed', { count: failCount }), 'error')
          onRefresh()
          return false
        }
      } else {
        if (!nodeId) { showToast(t('vpc:toast.selectNode'), 'error'); return false }
        await VpcApi.AddVpcRuleBatch(selectedIP, containers.map(c => c.name), nodeId)
      }
      showToast(t('vpc:toast.assignSuccess'))
      onRefresh()
      return true
    } catch { showToast(t('vpc:toast.assignFailed'), 'error'); return false }
    finally { setAssignLoading(false) }
  }

  const handleClearRule = async (name: string) => {
    if (!selectedIP) return
    try {
      await VpcApi.DeleteVpcRule(selectedIP, name)
      showToast(t('vpc:toast.clearSuccess'))
      onRefresh()
    } catch { showToast(t('vpc:toast.clearFailed'), 'error') }
  }

  const handleBatchClear = async () => {
    if (!selectedIP || selectedRules.length === 0) return
    try {
      await VpcApi.DeleteVpcRuleBatch(selectedIP, selectedRules.map(r => r.containerName))
      setSelectedRules([])
      showToast(t('vpc:toast.batchClearSuccess'))
      onRefresh()
    } catch { showToast(t('vpc:toast.batchClearFailed'), 'error') }
  }

  // 白名单 DNS 开关。API（POST /mytVpc/whiteListDns）语义是全量提交 whiteListDns 数组，
  // enable 时必须回传设备端当前值（GetContainerRules 回显的 rule.whiteListDns）——
  // 传 [] 会把设备端已有白名单条目清空；disable 时传 [] 才是清空语义。
  const handleToggleDns = async (rule: VpcNodeRule) => {
    if (!selectedIP) return
    const enable = !rule.whiteListDns?.length
    try {
      await VpcApi.EnableWhiteListDNS(selectedIP, rule.id, enable, enable ? (rule.whiteListDns ?? []) : [])
      onRefresh()
    } catch { showToast(t('vpc:toast.operationFailed'), 'error') }
  }

  // 启用/停用绑定（绑定关系保留）：停用后流量直连，老固件缺省按启用处理
  const handleToggleEnable = async (rule: VpcNodeRule) => {
    if (!selectedIP) return
    const enable = rule.enabled === false
    try {
      await VpcApi.EnableVpcRule(selectedIP, rule.id, enable)
      onRefresh()
    } catch { showToast(t('vpc:toast.operationFailed'), 'error') }
  }

  const toggleRule = (rule: VpcNodeRule) => {
    setSelectedRules(prev =>
      prev.some(r => r.id === rule.id) ? prev.filter(r => r.id !== rule.id) : [...prev, rule]
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <button onClick={openAssign} disabled={!selectedIP}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
          <Link2 size={13} />
          {t('vpc:rule.assignVpcNode')}
        </button>
        {selectedRules.length > 0 && (
          <>
            <button onClick={() => ask(t('vpc:rule.batchClearTitle'), t('vpc:rule.confirmBatchClear', { count: selectedRules.length }), handleBatchClear)}
              className="flex items-center gap-1 px-2.5 py-1.5 text-xs text-red-500 bg-red-50 rounded-lg hover:bg-red-100 transition-colors">
              <Ban size={13} />
              {t('vpc:rule.batchClear')}
            </button>
            <span className="text-[11px] text-blue-600 bg-blue-50 px-2 py-0.5 rounded-full">{t('vpc:rule.selected', { count: selectedRules.length })}</span>
          </>
        )}
      </div>

      {rules.length === 0 ? (
        <SectionCard>
          <EmptyState icon={Link2} title={selectedIP ? t('vpc:rule.noRules') : t('vpc:rule.selectDevice')}
            subtitle={selectedIP ? t('vpc:rule.noRulesHint') : undefined}
            actionLabel={selectedIP ? t('vpc:rule.assignVpcNode') : undefined}
            onAction={selectedIP ? openAssign : undefined} />
        </SectionCard>
      ) : (
        <SectionCard className="overflow-hidden">
          {/* 表头 */}
          <div className="grid grid-cols-[30px_1fr_110px_1fr_1fr_230px] gap-x-3 px-4 py-2 text-[11px] text-gray-400 bg-gray-50/70 font-medium text-center border-b border-gray-100">
            <span></span><span>{t('vpc:rule.colMachineName')}</span><span>{t('vpc:rule.colMachineIP')}</span><span>{t('vpc:rule.colSourceGroup')}</span><span>{t('vpc:rule.colNodeAlias')}</span><span>{t('table.action')}</span>
          </div>
          {rules.map(r => (
            <div key={r.id} className={`grid grid-cols-[30px_1fr_110px_1fr_1fr_230px] gap-x-3 px-4 py-2 border-t border-gray-50 hover:bg-gray-50/60 transition-colors items-center text-center ${r.enabled === false ? 'opacity-60' : ''}`}>
              <input type="checkbox" checked={selectedRules.some(s => s.id === r.id)} onChange={() => toggleRule(r)} className="w-3 h-3" />
              <div className="flex items-center justify-center gap-1.5 min-w-0">
                <TooltipCell text={r.containerName} className="text-xs text-gray-700" />
                {r.enabled === false && (
                  <span className="text-[10px] px-1.5 py-px rounded-full bg-gray-100 text-gray-400 shrink-0">{t('vpc:rule.disabledBadge')}</span>
                )}
              </div>
              <span className="text-xs text-gray-500 font-mono">{r.containerIP}</span>
              <TooltipCell text={r.groupName} className="text-xs text-gray-500" />
              <TooltipCell text={r.vpcRemarks} className="text-xs text-gray-500" />
              <div className="flex gap-1 justify-center">
                <button onClick={() => handleToggleEnable(r)}
                  className={`px-2 py-1 text-[11px] rounded-lg transition-colors ${r.enabled === false
                    ? 'text-blue-600 bg-blue-50 hover:bg-blue-100'
                    : 'text-gray-500 bg-gray-100 hover:bg-gray-200'}`}>
                  {r.enabled === false ? t('vpc:rule.enableOn') : t('vpc:rule.enableOff')}
                </button>
                <button onClick={() => handleToggleDns(r)}
                  className={`px-2 py-1 text-[11px] rounded-lg transition-colors ${r.whiteListDns?.length
                    ? 'text-emerald-600 bg-emerald-50 hover:bg-emerald-100'
                    : 'text-amber-600 bg-amber-50 hover:bg-amber-100'}`}>
                  {r.whiteListDns?.length ? t('vpc:rule.dnsOff') : t('vpc:rule.dnsOn')}
                </button>
                <button onClick={() => ask(t('vpc:rule.clearRuleTitle'), t('vpc:rule.confirmClearRule'), () => handleClearRule(r.containerName))}
                  className="px-2 py-1 text-[11px] rounded-lg text-red-500 bg-red-50 hover:bg-red-100 transition-colors">
                  {t('vpc:rule.clear')}
                </button>
              </div>
            </div>
          ))}
        </SectionCard>
      )}

      {showAssign && (
        <AssignVpcModal selectedIP={selectedIP} groups={groups} rules={rules}
          onClose={() => setShowAssign(false)} onAssign={handleAssign} submitting={assignLoading} />
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={async () => { await confirmAction.onConfirm(); setConfirmAction(null) }} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

export default VpcRulesSection
