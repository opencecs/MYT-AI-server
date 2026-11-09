import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Search, ChevronLeft, ChevronRight, Circle } from 'lucide-react'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { VpcGroup, VpcNodeRule, AndroidInfo } from '../../../types/index'
import { cast } from '../../../utils/cast'
import VpcModal from '../shared/VpcModal'

export interface AssignModalProps {
  selectedIP: string
  groups: VpcGroup[]
  rules: VpcNodeRule[]
  onClose: () => void
  onAssign: (containers: AndroidInfo[], groupId: number | null, nodeId: number | null, mode: 'specified' | 'random') => Promise<boolean>
  submitting: boolean
}

type AssignStep = 0 | 1 | 2

/**
 * 分配 VPC 节点三步弹窗（纯 UI）：①选云机 → ②选分组（指定/随机模式）→ ③选节点。
 * 提交动作由父组件 onAssign 完成（返回 false 表示失败，弹窗保持打开）。
 */
export default function AssignVpcModal({ selectedIP, groups, rules, onClose, onAssign, submitting }: AssignModalProps) {
  const { t } = useTranslation()
  const [assignStep, setAssignStep] = useState<AssignStep>(0)
  const [assignContainers, setAssignContainers] = useState<AndroidInfo[]>([])
  const [assignSelectedContainers, setAssignSelectedContainers] = useState<AndroidInfo[]>([])
  const [assignGroupId, setAssignGroupId] = useState<number | null>(null)
  const [assignNodeId, setAssignNodeId] = useState<number | null>(null)
  const [assignMode, setAssignMode] = useState<'specified' | 'random'>('specified')
  // 分组/节点搜索（弹窗内按名称过滤，只影响显示不影响已选项）
  const [groupSearch, setGroupSearch] = useState('')
  const [nodeSearch, setNodeSearch] = useState('')

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

  useEffect(() => { loadContainers() }, [selectedIP])

  const nextStep = () => {
    if (assignStep === 0 && assignSelectedContainers.length === 0) return
    setAssignStep((assignStep + 1) as AssignStep)
  }

  const handleConfirm = async () => {
    const ok = await onAssign(assignSelectedContainers, assignGroupId, assignNodeId, assignMode)
    if (ok) onClose()
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
    <VpcModal title={t('vpc:rule.assignStep' + (assignStep + 1))} onClose={onClose} width="w-96"
      footer={<>
        {assignStep > 0 && (
          <button onClick={() => setAssignStep((assignStep - 1) as AssignStep)}
            className="flex items-center gap-1 px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">
            <ChevronLeft size={13} />{t('button.back')}
          </button>
        )}
        {!isLastStep ? (
          <button onClick={nextStep} disabled={!canNext}
            className="flex items-center gap-1 px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
            {t('button.next')}<ChevronRight size={13} />
          </button>
        ) : (
          <button onClick={handleConfirm} disabled={submitting || !canNext}
            className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
            {submitting ? t('vpc:rule.assigning') : t('vpc:rule.confirmAssign')}
          </button>
        )}
      </>}>
      {assignStep === 0 && (
        <div className="max-h-60 overflow-y-auto space-y-0.5">
          {assignContainers.length > 0 && (
            <label className="flex items-center gap-2 text-xs py-1.5 hover:bg-gray-50 px-2 rounded-lg cursor-pointer border-b border-gray-100 font-medium">
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
              {running.length > 0 && <div className="text-[11px] text-emerald-600 font-medium px-2 pt-1">{t('vpc:rule.running', { count: running.length })}</div>}
              {running.map(c => (
                <label key={c.id} className="flex items-center gap-2 text-xs py-1.5 hover:bg-gray-50 px-2 rounded-lg cursor-pointer border-b border-gray-50">
                  <input type="checkbox" checked={assignSelectedContainers.some(s => s.id === c.id)}
                    onChange={() => setAssignSelectedContainers(prev =>
                      prev.some(s => s.id === c.id) ? prev.filter(s => s.id !== c.id) : [...prev, c]
                    )} className="w-3 h-3" />
                  <span className="flex-1">{c.name}</span>
                  <Circle size={9} className="text-emerald-500 fill-emerald-500" />
                </label>
              ))}
              {stopped.length > 0 && <div className="text-[11px] text-gray-400 font-medium px-2 pt-1 border-t border-gray-100 mt-1">{t('vpc:rule.stopped', { count: stopped.length })}</div>}
              {stopped.map(c => (
                <label key={c.id} className="flex items-center gap-2 text-xs py-1.5 hover:bg-gray-50 px-2 rounded-lg cursor-pointer border-b border-gray-50 opacity-60">
                  <input type="checkbox" checked={assignSelectedContainers.some(s => s.id === c.id)}
                    onChange={() => setAssignSelectedContainers(prev =>
                      prev.some(s => s.id === c.id) ? prev.filter(s => s.id !== c.id) : [...prev, c]
                    )} className="w-3 h-3" />
                  <span className="flex-1">{c.name}</span>
                  <Circle size={9} className="text-gray-300" />
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
              className="w-full text-xs pl-7 pr-2 py-1.5 bg-gray-50 border border-gray-200 rounded-lg outline-none focus:border-blue-400 placeholder:text-gray-300" />
          </div>
          {/* 限高滚动（与云机/节点步一致）：分组多时不再撑长弹窗 */}
          <div className="max-h-60 overflow-y-auto space-y-0.5">
            {filteredGroups.map(g => (
              <label key={g.id} className="flex items-center gap-2 text-xs py-1.5 hover:bg-gray-50 px-2 rounded-lg cursor-pointer border-b border-gray-50">
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
            <div className="text-[11px] text-amber-600 bg-amber-50 p-2 rounded-lg mt-2">{t('vpc:rule.randomHint')}</div>
          )}
        </div>
      )}

      {assignStep === 2 && assignMode === 'specified' && (
        <div>
          <div className="relative mb-2">
            <Search size={13} className="absolute left-2 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none" />
            <input type="text" value={nodeSearch} onChange={e => setNodeSearch(e.target.value)}
              placeholder={t('vpc:rule.searchNode')}
              className="w-full text-xs pl-7 pr-2 py-1.5 bg-gray-50 border border-gray-200 rounded-lg outline-none focus:border-blue-400 placeholder:text-gray-300" />
          </div>
          <div className="max-h-60 overflow-y-auto space-y-0.5">
            {filteredNodes.map(node => (
              <label key={node.id} className="flex items-center gap-2 text-xs py-1.5 hover:bg-gray-50 px-2 rounded-lg cursor-pointer border-b border-gray-50">
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
    </VpcModal>
  )
}
