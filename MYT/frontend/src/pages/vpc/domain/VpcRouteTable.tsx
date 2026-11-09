import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowLeftRight, ChevronUp, ChevronDown, Pencil, Plus, Trash2 } from 'lucide-react'
import * as VpcApi from '../../../api/vpcApi'
import { DomainRouteEntry, VpcGroup, VpcNodeRule } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import { useConfirm } from '../../../hooks/useConfirm'
import ConfirmDialog from '../../../components/ConfirmDialog'
import EmptyState from '../shared/EmptyState'
import SectionCard from '../shared/SectionCard'
import VpcModal from '../shared/VpcModal'
import { ContainerBadge } from '../shared/VpcDomainShared'
import DomainRuleEditor, { RuleChip } from '../shared/DomainRuleEditor'
import { DomainRule, parseDomainRule, buildDomains } from '../shared/VpcDomainShared'
import AddRouteModal from './AddRouteModal'
import { RouteRow } from './useRouteOverview'

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  rules: VpcNodeRule[]
  overview: { rows: RouteRow[]; loaded: number; total: number; loading: boolean }
  onChanged: () => void
}

interface RouteEdit {
  domainRules: DomainRule[]
  vpcID: string
}

const COLORS = ['text-blue-600 bg-blue-50', 'text-emerald-600 bg-emerald-50', 'text-purple-600 bg-purple-50', 'text-amber-600 bg-amber-50', 'text-rose-600 bg-rose-50']

const btnIcon = 'w-6 h-6 flex items-center justify-center rounded transition-colors disabled:opacity-30 disabled:hover:bg-transparent'

function parseRouteToEdit(entry: DomainRouteEntry): RouteEdit {
  const domainRules = entry.domains.map(parseDomainRule)
  return { domainRules: domainRules.length > 0 ? domainRules : [{ prefix: 'domain:', value: '' }], vpcID: String(entry.vpcID) }
}

/** 路由行内编辑目标：容器只读（杜绝弹窗内切容器把 A 的规则整写到 B 的覆盖事故） */
interface EditTarget {
  containerID: string
  containerName: string
  containerIP: string
  routes: RouteEdit[]
  /** 编辑的单条下标；-1 = 追加一条 */
  index: number
}

function VpcRouteTable({ selectedIP, groups, rules: allRules, overview, onChanged }: Props) {
  const { t } = useTranslation()
  // 新增走 AddRouteModal（同款左右布局弹窗）；编辑保持容器只读 VpcModal，杜绝弹窗内切容器覆盖事故
  const [showAdd, setShowAdd] = useState(false)
  const [editing, setEditing] = useState<EditTarget | null>(null)
  const [saving, setSaving] = useState(false)
  const { confirmAction, setConfirmAction, ask } = useConfirm()

  const findVpcAlias = (vpcID: number): string => {
    for (const g of groups) {
      const node = g.vpcs?.list?.find(n => n.id === vpcID)
      if (node) return `${g.alias} / ${node.remarks || node.protocol}`
    }
    return `#${vpcID}`
  }

  const findVpcGroupColor = (vpcID: number): string => {
    const groupIdx = groups.findIndex(g => g.vpcs?.list?.some(n => n.id === vpcID))
    return COLORS[groupIdx >= 0 ? groupIdx % COLORS.length : 0]
  }

  /** 编辑单条：预填回显该条（域名组 + 目标节点），容器只读 */
  const openEdit = (row: RouteRow, index: number) => {
    const existing = index >= 0 ? [parseRouteToEdit(row.routes[index])] : [{ domainRules: [{ prefix: 'domain:', value: '' } as DomainRule], vpcID: '' }]
    setEditing({ containerID: row.containerID, containerName: row.containerName, containerIP: row.containerIP, routes: existing, index })
  }

  /** 保存 = 该容器 routes 内存副本改一条后 SetDomainRoutes 全量提交（数组顺序即优先级） */
  const handleSave = async () => {
    if (!editing || !selectedIP) return
    const row = overview.rows.find(r => r.containerID === editing.containerID)
    const base: DomainRouteEntry[] = editing.index >= 0 && row ? row.routes.filter((_, i) => i !== editing.index) : (row?.routes || [])
    const entry = editing.routes[0]
    const domains = buildDomains(entry.domainRules)
    const vpcID = Number(entry.vpcID)
    if (!domains.length || !vpcID) { showToast(t('vpc:toast.fillDomainRule'), 'error'); return }
    const next = [...base]
    const insertAt = editing.index >= 0 ? editing.index : next.length
    next.splice(insertAt, 0, { domains, vpcID })
    setSaving(true)
    try {
      await VpcApi.SetDomainRoutes(selectedIP, editing.containerID, next)
      showToast(t('vpc:toast.setSuccess'))
      setEditing(null)
      onChanged()
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setSaving(false) }
  }

  /** 单条删除：副本移除后全量提交 */
  const handleDelete = (row: RouteRow, index: number) => {
    const next = row.routes.filter((_, i) => i !== index)
    ask(t('vpc:route.deleteTitle'), t('vpc:route.confirmDeleteOne'), async () => {
      try {
        if (!next.length) await VpcApi.ClearDomainRoutes(selectedIP, row.containerID)
        else await VpcApi.SetDomainRoutes(selectedIP, row.containerID, next)
        showToast(t('vpc:toast.deleteSuccess'))
        onChanged()
      } catch { showToast(t('vpc:toast.deleteFailed'), 'error') }
    })
  }

  /** 上移/下移：内存副本调序后全量提交（数组顺序即优先级） */
  const handleMove = async (row: RouteRow, index: number, dir: -1 | 1) => {
    const target = index + dir
    if (target < 0 || target >= row.routes.length) return
    const next = [...row.routes]
    ;[next[index], next[target]] = [next[target], next[index]]
    try {
      await VpcApi.SetDomainRoutes(selectedIP, row.containerID, next)
      showToast(t('vpc:toast.setSuccess'))
      onChanged()
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
  }

  if (overview.loading && overview.rows.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-gray-400">
        <svg className="w-5 h-5 animate-spin mb-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        {overview.total > 0 && <span className="text-xs">{t('vpc:route.loadingProgress', { loaded: overview.loaded, total: overview.total })}</span>}
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {/* 工具栏：添加中转路由（选容器 → 行内编辑器追加一条） */}
      <div className="flex items-center gap-2">
        <button onClick={() => setShowAdd(true)}
          className="flex items-center gap-1 px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 transition-colors">
          <Plus size={13} />{t('vpc:route.addRoute')}
        </button>
        {overview.loading && overview.rows.length > 0 && (
          <span className="text-[11px] text-gray-400 ml-1">{t('vpc:route.loadingProgress', { loaded: overview.loaded, total: overview.total })}</span>
        )}
      </div>

      {overview.rows.length === 0 ? (
        <SectionCard>
          <EmptyState icon={ArrowLeftRight} title={t('vpc:route.noRoutesOverview')}
            subtitle={overview.total > 0 ? t('vpc:route.loadingProgress', { loaded: overview.loaded, total: overview.total }) : t('vpc:route.noRoutesHint')}
            actionLabel={t('vpc:route.addRoute')} onAction={() => setShowAdd(true)} />
        </SectionCard>
      ) : (
        <SectionCard className="overflow-hidden">
          {/* 表头 */}
          <div className="grid grid-cols-[160px_44px_1fr_150px_150px] gap-2 px-4 py-2 bg-gray-50/70 text-[10px] text-gray-400 font-medium">
            <span>{t('vpc:route.colContainer')}</span>
            <span>{t('vpc:route.colPriority')}</span>
            <span>{t('vpc:route.colDomains')}</span>
            <span>{t('vpc:route.colTarget')}</span>
            <span className="text-right">{t('vpc:route.colActions')}</span>
          </div>
          {overview.rows.map(row => (
            <div key={row.containerID} className="border-t border-gray-50">
              {row.routes.map((route, i) => (
                <div key={i} className="grid grid-cols-[160px_44px_1fr_150px_150px] gap-2 items-center px-4 py-2 border-t border-gray-50 first:border-t-0 hover:bg-gray-50/60 transition-colors">
                  {/* 容器列：仅首行显示（跨行合并视觉） */}
                  <div className="min-w-0">
                    {i === 0 ? <span className="text-xs text-gray-700 font-medium truncate block" title={row.containerName}>{row.containerName}</span> : <span className="text-[10px] text-gray-200 font-mono pl-2">└</span>}
                  </div>
                  {/* 序号即优先级 */}
                  <span className="text-[10px] text-gray-300 font-mono">#{i + 1}</span>
                  {/* 域名组徽标 */}
                  <div className="flex flex-wrap gap-1 min-w-0">
                    {route.domains.map((d, j) => {
                      const rule = parseDomainRule(d)
                      return <RuleChip key={j} rule={rule} />
                    })}
                  </div>
                  {/* 目标节点 */}
                  <span className={`text-xs font-medium px-2 py-0.5 rounded-md w-fit truncate ${findVpcGroupColor(route.vpcID)}`}>
                    {findVpcAlias(route.vpcID)}
                  </span>
                  {/* 操作：编辑/删除/上移/下移 */}
                  <div className="flex justify-end gap-0.5">
                    <button onClick={() => handleMove(row, i, -1)} disabled={i === 0} title={t('vpc:route.moveUp')}
                      className={`${btnIcon} text-gray-400 hover:text-blue-600 hover:bg-blue-50`}>
                      <ChevronUp size={14} />
                    </button>
                    <button onClick={() => handleMove(row, i, 1)} disabled={i === row.routes.length - 1} title={t('vpc:route.moveDown')}
                      className={`${btnIcon} text-gray-400 hover:text-blue-600 hover:bg-blue-50`}>
                      <ChevronDown size={14} />
                    </button>
                    <button onClick={() => openEdit(row, i)} title={t('button.edit')}
                      className={`${btnIcon} text-gray-400 hover:text-blue-600 hover:bg-blue-50`}>
                      <Pencil size={14} />
                    </button>
                    <button onClick={() => handleDelete(row, i)} title={t('button.delete')}
                      className={`${btnIcon} text-gray-400 hover:text-red-500 hover:bg-red-50`}>
                      <Trash2 size={14} />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          ))}
        </SectionCard>
      )}

      {/* 行内编辑弹窗：容器只读徽标（不可切换，杜绝覆盖事故） */}
      {editing && (
        <VpcModal title={editing.index >= 0 ? t('vpc:route.editRouteTitle') : t('vpc:route.addRouteTitle')}
          subtitle={t('vpc:route.dialogSubtitle')} onClose={() => setEditing(null)} width="w-80"
          footer={<>
            <button onClick={() => setEditing(null)} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
            <button onClick={handleSave} disabled={saving}
              className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
              {saving ? t('vpc:route.saving') : t('button.save')}
            </button>
          </>}>
          <div className="space-y-3">
            <div>
              <label className="text-[10px] text-gray-400 block mb-1">{t('vpc:route.container')}</label>
              <ContainerBadge name={editing.containerName || editing.containerID} ip={editing.containerIP} />
            </div>
            <div>
              <label className="text-[10px] text-gray-400 block mb-1.5">{t('vpc:route.domainRules')}</label>
              <DomainRuleEditor
                rules={editing.routes[0].domainRules}
                onChange={dr => setEditing({ ...editing, routes: [{ ...editing.routes[0], domainRules: dr }] })} />
            </div>
            <div>
              <label className="text-[10px] text-gray-400 block mb-1.5">{t('vpc:route.targetVpcNode')}</label>
              <select value={editing.routes[0].vpcID} onChange={e => setEditing({ ...editing, routes: [{ ...editing.routes[0], vpcID: e.target.value }] })}
                className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded-lg outline-none focus:border-blue-400 bg-white">
                <option value="">{t('vpc:domainShared.selectVpcNode')}</option>
                {groups.map(g => g.vpcs?.list?.map(n => (
                  <option key={n.id} value={String(n.id)}>{g.alias} / {n.remarks || n.protocol}</option>
                )))}
              </select>
            </div>
          </div>
        </VpcModal>
      )}

      {/* 新增路由弹窗：与 AddRuleModal 同款左右布局（左列容器单选 + 右列编辑器） */}
      {showAdd && (
        <AddRouteModal selectedIP={selectedIP} groups={groups} allRules={allRules}
          onClose={() => setShowAdd(false)}
          onSaved={() => { setShowAdd(false); onChanged() }} />
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={async () => { await confirmAction.onConfirm(); setConfirmAction(null) }} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

export default VpcRouteTable
