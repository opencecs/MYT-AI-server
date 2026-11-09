import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Globe, Copy, Pencil, Trash2 } from 'lucide-react'
import * as VpcApi from '../../../api/vpcApi'
import { VpcNodeRule } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import { useConfirm } from '../../../hooks/useConfirm'
import ConfirmDialog from '../../../components/ConfirmDialog'
import EmptyState from '../shared/EmptyState'
import SectionCard from '../shared/SectionCard'
import DomainRuleEditor, { RuleChip } from '../shared/DomainRuleEditor'
import AddRuleModal, { AddRuleMode } from './AddRuleModal'
import CopyRuleModal from './CopyRuleModal'
import { DomainRule, buildDomains, parseDomainRule, filterSelectableRules } from '../shared/VpcDomainShared'
import { DomainRow, computeConflictKeys, rulesForContainer } from './useDomainOverview'

interface Props {
  selectedIP: string
  rules: VpcNodeRule[]
  overview: { rows: DomainRow[]; loaded: number; total: number; loading: boolean; error: boolean }
  onChanged: () => void
}

const btnIcon = 'w-6 h-6 flex items-center justify-center rounded transition-colors disabled:opacity-30 disabled:hover:bg-transparent'

interface EditTarget {
  kind: 'filter' | 'direct'
  scope: 'container' | 'global'
  containerID: string
  containerName: string
  containerIP: string
  rules: DomainRule[]
}

const kindBadge = (kind: 'filter' | 'direct') => kind === 'filter' ? 'bg-blue-50 text-blue-600' : 'bg-emerald-50 text-emerald-600'

/** 行内编辑目标组（同一容器同一类型的规则块） */
function groupKey(r: DomainRow): string {
  return `${r.kind}|${r.scope}|${r.containerID}`
}

function VpcDomainTable({ selectedIP, rules: allRules, overview, onChanged }: Props) {
  const { t } = useTranslation()
  const rules = filterSelectableRules(allRules)
  // 新增一律走 AddRuleModal（左列容器单选 + 右列预填编辑）；
  // 编辑已有规则保持行内编辑——两种模式共用 DomainRuleEditor 与全量 Set 提交语义，保存后均经 onChanged() 刷新总览。
  const [addMode, setAddMode] = useState<AddRuleMode | null>(null)
  const [editing, setEditing] = useState<EditTarget | null>(null)
  const [saving, setSaving] = useState(false)
  const [copySource, setCopySource] = useState<DomainRow | null>(null)
  const { confirmAction, setConfirmAction, ask } = useConfirm()

  const conflictKeys = useMemo(() => computeConflictKeys(overview.rows), [overview.rows])

  // 按组聚合行（保持加载顺序：全局在前，容器按 rules 顺序）
  const blocks = useMemo(() => {
    const map = new Map<string, { rows: DomainRow[]; sample: DomainRow }>()
    for (const r of overview.rows) {
      const k = groupKey(r)
      if (!map.has(k)) map.set(k, { rows: [], sample: r })
      map.get(k)!.rows.push(r)
    }
    return [...map.values()]
  }, [overview.rows])

  /** 打开行内编辑：预填该组现有规则回显（内存副本，保存时一次 Set 全量提交） */
  const openEdit = (sample: DomainRow) => {
    const raws = rulesForContainer(overview.rows, sample.kind, sample.containerID, sample.scope)
    setEditing({
      kind: sample.kind,
      scope: sample.scope,
      containerID: sample.containerID,
      containerName: sample.containerName,
      containerIP: sample.containerIP,
      rules: raws.length ? raws.map(parseDomainRule) : [{ prefix: 'domain:', value: '' }],
    })
  }

  /** 行内编辑保存 = 全量 Set 提交（设备端 API 即全量替换语义；与弹窗保存共用同一分支语义） */
  const handleSave = async () => {
    if (!editing || !selectedIP) return
    const ds = buildDomains(editing.rules)
    if (!ds.length) { showToast(t('vpc:toast.fillDomainRules'), 'error'); return }
    setSaving(true)
    try {
      if (editing.scope === 'global') {
        await VpcApi.SetGlobalDomainFilter(selectedIP, ds)
      } else if (editing.kind === 'filter') {
        await VpcApi.SetDomainFilter(selectedIP, editing.containerID, ds)
      } else {
        await VpcApi.SetDomainDirect(selectedIP, editing.containerID, ds)
      }
      showToast(t('vpc:toast.setSuccess'))
      setEditing(null)
      onChanged()
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setSaving(false) }
  }

  /** 行内单条删除：内存副本移除后全量提交（空列表 = 清空该组） */
  const handleRowDelete = (row: DomainRow) => {
    const group = blocks.find(b => groupKey(b.sample) === groupKey(row))
    if (!group) return
    ask(t('vpc:domain.deleteRuleTitle'), t('vpc:domain.confirmDeleteRule', { domain: row.raw }), async () => {
      const raws = group.rows.filter(r => r.key !== row.key).map(r => r.raw)
      try {
        if (row.scope === 'global') {
          if (!raws.length) await VpcApi.ClearGlobalDomainFilter(selectedIP)
          else await VpcApi.SetGlobalDomainFilter(selectedIP, raws)
        } else if (row.kind === 'filter') {
          if (!raws.length) await VpcApi.ClearDomainFilter(selectedIP, row.containerID)
          else await VpcApi.SetDomainFilter(selectedIP, row.containerID, raws)
        } else {
          if (!raws.length) await VpcApi.ClearDomainDirect(selectedIP, row.containerID)
          else await VpcApi.SetDomainDirect(selectedIP, row.containerID, raws)
        }
        showToast(t('vpc:toast.deleteSuccess'))
        onChanged()
      } catch { showToast(t('vpc:toast.deleteFailed'), 'error') }
    })
  }

  if (overview.loading && overview.rows.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-gray-400">
        <svg className="w-5 h-5 animate-spin mb-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        {overview.total > 0 && <span className="text-xs">{t('vpc:domain.loadingProgress', { loaded: overview.loaded, total: overview.total })}</span>}
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {/* 工具栏：三个入口均打开 AddRuleModal（左列选目标 + 右列预填编辑） */}
      <div className="flex items-center gap-2">
        <button onClick={() => setAddMode('filter')} className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 transition-colors">{t('vpc:domain.addFilterRule')}</button>
        <button onClick={() => setAddMode('direct')} className="px-3 py-1.5 text-xs bg-emerald-500 text-white rounded-lg hover:bg-emerald-600 transition-colors">{t('vpc:domain.addDirectRule')}</button>
        <button onClick={() => setAddMode('global')}
          className="px-3 py-1.5 text-xs text-blue-600 bg-blue-50 rounded-lg hover:bg-blue-100 transition-colors">{t('vpc:domain.setGlobalFilter')}</button>
        {overview.loading && <span className="text-[11px] text-gray-400 ml-1">{t('vpc:domain.loadingProgress', { loaded: overview.loaded, total: overview.total })}</span>}
      </div>

      {/* 行内编辑器（仅编辑已有规则块，新增走 AddRuleModal 弹窗）：固定渲染在表格上方 */}
      {editing && (
        <div className="p-3 border border-blue-100 bg-blue-50/40 rounded-xl space-y-2.5">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-gray-700">
              {t('vpc:domain.editingScope', {
                scope: editing.scope === 'global' ? t('vpc:domain.globalScope') : editing.containerName,
                kind: editing.kind === 'filter' ? t('vpc:domain.badgeFilter') : t('vpc:domain.badgeDirect'),
              })}
            </span>
          </div>
          <DomainRuleEditor rules={editing.rules} onChange={rs => setEditing({ ...editing, rules: rs })} />
          <div className="flex justify-end gap-2">
            <button onClick={() => setEditing(null)} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
            <button onClick={handleSave} disabled={saving}
              className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
              {saving ? t('vpc:domain.processing') : t('button.save')}
            </button>
          </div>
        </div>
      )}

      {blocks.length === 0 ? (
        <SectionCard>
          <EmptyState icon={Globe} title={t('vpc:domain.noRulesOverview')} subtitle={t('vpc:domain.noRulesHint')}
            actionLabel={t('vpc:domain.addFilterRule')} onAction={() => setAddMode('filter')} />
        </SectionCard>
      ) : (
        <SectionCard className="overflow-hidden">
          {/* 表头 */}
          <div className="grid grid-cols-[130px_64px_56px_1fr_120px] gap-2 px-4 py-2 bg-gray-50/70 text-[10px] text-gray-400 font-medium">
            <span>{t('vpc:domain.colContainer')}</span>
            <span>{t('vpc:domain.colScope')}</span>
            <span>{t('vpc:domain.colKind')}</span>
            <span>{t('vpc:domain.colDomain')}</span>
            <span className="text-right">{t('vpc:domain.colActions')}</span>
          </div>
          {blocks.map(block => {
            const s = block.sample
            const gk = groupKey(s)
            return (
              <div key={gk} className="border-t border-gray-50">
                {/* 组头：容器 + 作用域 + 类型徽标 */}
                <div className="grid grid-cols-[130px_64px_56px_1fr_120px] gap-2 items-center px-4 py-1.5 bg-gray-50/40">
                  <div className="flex items-center gap-1.5 min-w-0">
                    {s.scope === 'global'
                      ? <><Globe size={13} className="text-amber-500 shrink-0" /><span className="text-xs text-gray-700 font-medium truncate">{t('vpc:domain.globalScope')}</span></>
                      : <span className="text-xs text-gray-700 font-medium truncate" title={s.containerName}>{s.containerName}</span>}
                  </div>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full w-fit ${s.scope === 'global' ? 'bg-amber-50 text-amber-600' : 'bg-gray-100 text-gray-500'}`}>
                    {s.scope === 'global' ? t('vpc:domain.badgeGlobal') : t('vpc:domain.badgeContainer')}
                  </span>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full w-fit ${kindBadge(s.kind)}`}>
                    {s.kind === 'filter' ? t('vpc:domain.badgeFilter') : t('vpc:domain.badgeDirect')}
                  </span>
                  <span className="text-[10px] text-gray-300">{t('vpc:domain.ruleCount', { count: block.rows.length })}</span>
                  <div className="flex justify-end">
                    <button onClick={() => openEdit(s)} className="text-[10px] px-2 py-0.5 rounded text-blue-600 bg-blue-50 hover:bg-blue-100 transition-colors">{t('button.edit')}</button>
                  </div>
                </div>
                {/* 规则行 */}
                {block.rows.map(row => (
                  <div key={row.key} className="grid grid-cols-[130px_64px_56px_1fr_120px] gap-2 items-center px-4 py-1.5 border-t border-gray-50 hover:bg-gray-50/60 transition-colors">
                    <span className="text-[10px] text-gray-300 font-mono truncate">{s.scope === 'global' ? '' : s.containerIP}</span>
                    <span /><span />
                    <span className="flex items-center gap-1.5 min-w-0">
                      <RuleChip rule={row.rule} />
                      {conflictKeys.has(row.raw) && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-yellow-100 text-yellow-600 shrink-0" title={t('vpc:domain.conflictHint')}>{t('vpc:domain.conflict')}</span>
                      )}
                    </span>
                    <div className="flex justify-end gap-1">
                      <button onClick={() => setCopySource(row)} title={t('vpc:domain.copyTo')}
                        className={`${btnIcon} text-gray-400 hover:text-blue-600 hover:bg-blue-50`}>
                        <Copy size={14} />
                      </button>
                      <button onClick={() => openEdit(s)} title={t('button.edit')}
                        className={`${btnIcon} text-gray-400 hover:text-blue-600 hover:bg-blue-50`}>
                        <Pencil size={14} />
                      </button>
                      <button onClick={() => handleRowDelete(row)} title={t('button.delete')}
                        className={`${btnIcon} text-gray-400 hover:text-red-500 hover:bg-red-50`}>
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )
          })}
        </SectionCard>
      )}

      {/* 新增规则弹窗：左列容器/全局单选 + 右列预填编辑（新增走弹窗、编辑走行内的双模式约定见组件头注释） */}
      {addMode && (
        <AddRuleModal selectedIP={selectedIP} mode={addMode} allRules={allRules} overviewRows={overview.rows}
          onClose={() => setAddMode(null)}
          onSaved={() => { setAddMode(null); onChanged() }} />
      )}

      {/* 复制到… 弹窗：目标容器多选（逻辑在 CopyRuleModal） */}
      {copySource && (
        <CopyRuleModal selectedIP={selectedIP} source={copySource} containers={rules} overviewRows={overview.rows}
          onClose={() => setCopySource(null)}
          onSaved={() => { setCopySource(null); onChanged() }} />
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={async () => { await confirmAction.onConfirm(); setConfirmAction(null) }} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

export default VpcDomainTable
