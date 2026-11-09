import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Search } from 'lucide-react'
import * as VpcApi from '../../api/vpcApi'
import * as SdkApiService from '../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { VpcGroup, VpcNodeRule, AndroidInfo } from '../../types/index'
import { showToast } from '../../components/Toast'
import TooltipCell from '../../components/TooltipCell'
import { useConfirm } from '../../hooks/useConfirm'
import ConfirmDialog from '../../components/ConfirmDialog'
import { cast } from '../../utils/cast'

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  rules: VpcNodeRule[]
  onRefresh: () => void
}

type AssignStep = 0 | 1 | 2

function VpcRulesSection({ selectedIP, groups, rules, onRefresh }: Props) {
  const { t } = useTranslation()
  const [selectedRules, setSelectedRules] = useState<VpcNodeRule[]>([])
  const [showAssign, setShowAssign] = useState(false)
  const [assignStep, setAssignStep] = useState<AssignStep>(0)
  const [assignContainers, setAssignContainers] = useState<AndroidInfo[]>([])
  const [assignSelectedContainers, setAssignSelectedContainers] = useState<AndroidInfo[]>([])
  const [assignGroupId, setAssignGroupId] = useState<number | null>(null)
  const [assignNodeId, setAssignNodeId] = useState<number | null>(null)
  const [assignMode, setAssignMode] = useState<'specified' | 'random'>('specified')
  const [assignLoading, setAssignLoading] = useState(false)
  // 分组/节点搜索（弹窗内按名称过滤，只影响显示不影响已选项）
  const [groupSearch, setGroupSearch] = useState('')
  const [nodeSearch, setNodeSearch] = useState('')
  const { confirmAction, setConfirmAction, ask } = useConfirm()

  const loadContainers = async () => {
    if (!selectedIP) return
    try {
      const list = cast<AndroidInfo[]>(await SdkApiService.ListAndroids(selectedIP))
      // 排除桥接模式（macvlan直连：networkName 含 myt）和已分配VPC节点的云机
      const assignedNames = new Set(rules.map(r => r.containerName))
      setAssignContainers((list || []).filter(a => {
        const isMacvlan = a.networkName?.toLowerCase().includes('myt')
        return !isMacvlan && !assignedNames.has(a.name)
      }))
    } catch { /* ignore */ }
  }

  useEffect(() => { if (showAssign) loadContainers() }, [showAssign, selectedIP])

  const nextStep = () => {
    if (assignStep === 0 && assignSelectedContainers.length === 0) return
    setAssignStep((assignStep + 1) as AssignStep)
  }

  const handleAssign = async () => {
    if (!selectedIP) return
    setAssignLoading(true)
    try {
      if (assignMode === 'random') {
        const group = groups.find(g => g.id === assignGroupId)
        if (!group?.vpcs?.list?.length) { showToast(t('vpc:toast.noAvailableNodes'), 'error'); return }
        const nodes = group.vpcs.list
        let failCount = 0
        for (const c of assignSelectedContainers) {
          try {
            const node = nodes[Math.floor(Math.random() * nodes.length)]
            await VpcApi.AddVpcRule(selectedIP, c.name, node.id)
          } catch { failCount++ }
        }
        if (failCount > 0) { showToast(t('vpc:toast.containerAssignFailed', { count: failCount }), 'error'); onRefresh(); return }
      } else {
        if (!assignNodeId) { showToast(t('vpc:toast.selectNode'), 'error'); return }
        await VpcApi.AddVpcRuleBatch(selectedIP, assignSelectedContainers.map(c => c.name), assignNodeId)
      }
      showToast(t('vpc:toast.assignSuccess'))
      setShowAssign(false)
      onRefresh()
    } catch { showToast(t('vpc:toast.assignFailed'), 'error') }
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

  const handleToggleDns = async (rule: VpcNodeRule) => {
    if (!selectedIP) return
    const enable = !rule.whiteListDns?.length
    try {
      await VpcApi.EnableWhiteListDNS(selectedIP, rule.id, enable, enable ? [] : [])
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

  const selectedGroup = groups.find(g => g.id === assignGroupId)
  // 搜索过滤：分组按别名、节点按备注，大小写不敏感；空词显示全部
  const filteredGroups = useMemo(() => {
    const kw = groupSearch.trim().toLowerCase()
    return kw ? groups.filter(g => g.alias.toLowerCase().includes(kw)) : groups
  }, [groups, groupSearch])
  const filteredNodes = useMemo(() => {
    const nodes = selectedGroup?.vpcs?.list ?? []
    const kw = nodeSearch.trim().toLowerCase()
    return kw ? nodes.filter(n => (n.remarks || '').toLowerCase().includes(kw)) : nodes
  }, [selectedGroup, nodeSearch])
  const canNext = assignStep === 0 ? assignSelectedContainers.length > 0
    : assignStep === 1 ? !!assignGroupId
    : assignMode === 'random' ? !!assignGroupId : !!assignNodeId
  // 随机模式到第2步即可确认，指定节点需要第3步选具体节点
  const isLastStep = assignStep === 2 || (assignStep === 1 && assignMode === 'random')

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <button onClick={() => { setShowAssign(true); setAssignStep(0); setAssignSelectedContainers([]); setAssignGroupId(null); setAssignNodeId(null); setGroupSearch(''); setNodeSearch('') }}
          disabled={!selectedIP} className="px-2 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">
          {t('vpc:rule.assignVpcNode')}
        </button>
        {selectedRules.length > 0 && (
          <button onClick={() => ask(t('vpc:rule.batchClearTitle'), t('vpc:rule.confirmBatchClear', { count: selectedRules.length }), handleBatchClear)}
            className="px-2 py-1 text-xs bg-amber-50 text-amber-600 rounded hover:bg-amber-100">
            {t('vpc:rule.batchClear')}
          </button>
        )}
        {selectedRules.length > 0 && <span className="text-xs text-blue-500 bg-blue-50 px-2 py-0.5 rounded-full">{t('vpc:rule.selected', { count: selectedRules.length })}</span>}
      </div>

      {rules.length === 0 ? (
        <div className="text-xs text-gray-300 text-center py-6">{selectedIP ? t('vpc:rule.noRules') : t('vpc:rule.selectDevice')}</div>
      ) : (
        <div className="border border-gray-100 rounded-lg overflow-hidden">
          <div className="grid grid-cols-[30px_1fr_100px_1fr_1fr_240px] gap-x-3 px-3 py-1.5 text-[10px] text-gray-400 bg-gray-100 font-medium text-center">
            <span></span><span>{t('vpc:rule.colMachineName')}</span><span>{t('vpc:rule.colMachineIP')}</span><span>{t('vpc:rule.colSourceGroup')}</span><span>{t('vpc:rule.colNodeAlias')}</span><span>{t('table.action')}</span>
          </div>
          {rules.map(r => (
            <div key={r.id} className={`grid grid-cols-[30px_1fr_100px_1fr_1fr_240px] gap-x-3 px-3 py-1.5 border-t border-gray-50 hover:bg-gray-50/50 items-center text-center ${r.enabled === false ? 'opacity-50' : ''}`}>
              <input type="checkbox" checked={selectedRules.some(s => s.id === r.id)} onChange={() => toggleRule(r)} className="w-3 h-3" />
              <div className="flex items-center justify-center gap-1.5 min-w-0">
                <TooltipCell text={r.containerName} className="text-xs text-gray-700" />
                {r.enabled === false && (
                  <span className="text-[10px] px-1 py-px rounded bg-gray-100 text-gray-400 shrink-0">{t('vpc:rule.disabledBadge')}</span>
                )}
              </div>
              <span className="text-xs text-gray-500 font-mono">{r.containerIP}</span>
              <TooltipCell text={r.groupName} className="text-xs text-gray-500" />
              <TooltipCell text={r.vpcRemarks} className="text-xs text-gray-500" />
              <div className="flex gap-1 justify-center">
                <button onClick={() => handleToggleEnable(r)}
                  className={`px-1.5 py-0.5 text-[10px] rounded ${r.enabled === false ? 'bg-blue-50 text-blue-600 hover:bg-blue-100' : 'bg-gray-100 text-gray-500 hover:bg-gray-200'}`}>
                  {r.enabled === false ? t('vpc:rule.enableOn') : t('vpc:rule.enableOff')}
                </button>
                <button onClick={() => handleToggleDns(r)} className="px-1.5 py-0.5 text-[10px] rounded bg-amber-50 text-amber-600 hover:bg-amber-100">
                  {r.whiteListDns?.length ? t('vpc:rule.dnsOff') : t('vpc:rule.dnsOn')}
                </button>
                <button onClick={() => ask(t('vpc:rule.clearRuleTitle'), t('vpc:rule.confirmClearRule'), () => handleClearRule(r.containerName))} className="px-1.5 py-0.5 text-[10px] rounded bg-red-50 text-red-500 hover:bg-red-100">{t('vpc:rule.clear')}</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {showAssign && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div className="bg-white rounded-lg shadow-xl p-4 w-96" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium text-gray-700">
                {t('vpc:rule.assignStep' + (assignStep + 1))}
              </h4>
              <button onClick={() => setShowAssign(false)} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            </div>

            {assignStep === 0 && (
              <div className="max-h-60 overflow-y-auto space-y-0.5">
                {assignContainers.length > 0 && (
                  <label className="flex items-center gap-2 text-xs py-1 hover:bg-gray-50 px-2 rounded cursor-pointer border-b border-gray-100 font-medium">
                    <input type="checkbox"
                      checked={assignSelectedContainers.length === assignContainers.length}
                      onChange={() => setAssignSelectedContainers(
                        assignSelectedContainers.length === assignContainers.length ? [] : [...assignContainers]
                      )} className="w-3 h-3" />
                    <span className="text-gray-500">{t('vpc:rule.selectAll', { count: assignContainers.length })}</span>
                  </label>
                )}
                {(() => {
                  const running = assignContainers.filter(c => c.status === 'running')
                  const stopped = assignContainers.filter(c => c.status !== 'running')
                  return <>
                    {running.length > 0 && <div className="text-[10px] text-green-600 font-medium px-2 pt-1">{t('vpc:rule.running', { count: running.length })}</div>}
                    {running.map(c => (
                      <label key={c.id} className="flex items-center gap-2 text-xs py-1 hover:bg-gray-50 px-2 rounded cursor-pointer border-b border-gray-50">
                        <input type="checkbox" checked={assignSelectedContainers.some(s => s.id === c.id)}
                          onChange={() => setAssignSelectedContainers(prev =>
                            prev.some(s => s.id === c.id) ? prev.filter(s => s.id !== c.id) : [...prev, c]
                          )} className="w-3 h-3" />
                        <span className="flex-1">{c.name}</span>
                        <span className="text-[10px] text-green-500">●</span>
                      </label>
                    ))}
                    {stopped.length > 0 && <div className="text-[10px] text-gray-400 font-medium px-2 pt-1 border-t border-gray-100 mt-1">{t('vpc:rule.stopped', { count: stopped.length })}</div>}
                    {stopped.map(c => (
                      <label key={c.id} className="flex items-center gap-2 text-xs py-1 hover:bg-gray-50 px-2 rounded cursor-pointer border-b border-gray-50 opacity-60">
                        <input type="checkbox" checked={assignSelectedContainers.some(s => s.id === c.id)}
                          onChange={() => setAssignSelectedContainers(prev =>
                            prev.some(s => s.id === c.id) ? prev.filter(s => s.id !== c.id) : [...prev, c]
                          )} className="w-3 h-3" />
                        <span className="flex-1">{c.name}</span>
                        <span className="text-[10px] text-gray-400">○</span>
                      </label>
                    ))}
                  </>
                })()}
              </div>
            )}

            {assignStep === 1 && (
              <div className="space-y-1">
                <div className="flex gap-3 mb-2">
                  <label className="flex items-center gap-1 text-xs">
                    <input type="radio" checked={assignMode === 'specified'} onChange={() => setAssignMode('specified')} />{t('vpc:rule.specifiedNode')}
                  </label>
                  <label className="flex items-center gap-1 text-xs">
                    <input type="radio" checked={assignMode === 'random'} onChange={() => setAssignMode('random')} />{t('vpc:rule.randomAssign')}
                  </label>
                </div>
                <div className="relative mb-2">
                  <Search size={13} className="absolute left-2 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none" />
                  <input type="text" value={groupSearch} onChange={e => setGroupSearch(e.target.value)}
                    placeholder={t('vpc:rule.searchGroup')}
                    className="w-full text-xs pl-7 pr-2 py-1.5 bg-gray-50 border border-gray-200 rounded-md outline-none focus:border-blue-400 placeholder:text-gray-300" />
                </div>
                {/* 限高滚动（与云机/节点步一致）：分组多时不再撑长弹窗 */}
                <div className="max-h-60 overflow-y-auto space-y-0.5">
                  {filteredGroups.map(g => (
                    <label key={g.id} className="flex items-center gap-2 text-xs py-1 hover:bg-gray-50 px-2 rounded cursor-pointer border-b border-gray-50">
                      <input type="radio" checked={assignGroupId === g.id} onChange={() => setAssignGroupId(g.id)} />
                      <span className="flex-1 truncate">{g.alias}</span>
                      <span className="text-gray-400 text-[10px]">{t('vpc:rule.nodeCount', { count: g.vpcs?.list?.length || 0 })}</span>
                    </label>
                  ))}
                  {filteredGroups.length === 0 && (
                    <div className="text-xs text-gray-300 text-center py-6">{t('vpc:rule.noMatch')}</div>
                  )}
                </div>
                {assignMode === 'random' && assignGroupId && (
                  <div className="text-xs text-amber-600 bg-amber-50 p-2 rounded mt-2">{t('vpc:rule.randomHint')}</div>
                )}
              </div>
            )}

            {assignStep === 2 && assignMode === 'specified' && (
              <div>
                <div className="relative mb-2">
                  <Search size={13} className="absolute left-2 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none" />
                  <input type="text" value={nodeSearch} onChange={e => setNodeSearch(e.target.value)}
                    placeholder={t('vpc:rule.searchNode')}
                    className="w-full text-xs pl-7 pr-2 py-1.5 bg-gray-50 border border-gray-200 rounded-md outline-none focus:border-blue-400 placeholder:text-gray-300" />
                </div>
                <div className="max-h-60 overflow-y-auto space-y-0.5">
                  {filteredNodes.map(node => (
                    <label key={node.id} className="flex items-center gap-2 text-xs py-1 hover:bg-gray-50 px-2 rounded cursor-pointer border-b border-gray-50">
                      <input type="radio" checked={assignNodeId === node.id} onChange={() => setAssignNodeId(node.id)} />
                      <span className="flex-1 truncate">{node.remarks}</span>
                      <span className="text-gray-400 text-[10px]">{node.protocol}</span>
                    </label>
                  ))}
                  {filteredNodes.length === 0 && (
                    <div className="text-xs text-gray-300 text-center py-6">{t('vpc:rule.noMatch')}</div>
                  )}
                </div>
              </div>
            )}

            <div className="flex justify-end gap-2 mt-3">
              {assignStep > 0 && <button onClick={() => setAssignStep((assignStep - 1) as AssignStep)} className="px-3 py-1 text-xs text-gray-500">{t('button.back')}</button>}
              {!isLastStep ? (
                <button onClick={nextStep} disabled={!canNext} className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">{t('button.next')}</button>
              ) : (
                <button onClick={handleAssign} disabled={assignLoading || !canNext}
                  className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">
                  {assignLoading ? t('vpc:rule.assigning') : t('vpc:rule.confirmAssign')}
                </button>
              )}
            </div>
          </div>
        </div>
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={async () => { await confirmAction.onConfirm(); setConfirmAction(null) }} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

export default VpcRulesSection
