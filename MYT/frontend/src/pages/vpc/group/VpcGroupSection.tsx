import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FolderPlus, Pencil, RefreshCw, Trash2, Gauge, Loader2, FolderOpen } from 'lucide-react'
import { VpcGroup } from '../../../types/index'
import ConfirmDialog from '../../../components/ConfirmDialog'
import TooltipCell from '../../../components/TooltipCell'
import CreateGroupModal from './CreateGroupModal'
import EditNodeModal from './EditNodeModal'
import { useVpcGroupSection } from '../hooks/useVpcGroupSection'
import SectionCard from '../shared/SectionCard'
import EmptyState from '../shared/EmptyState'

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  onRefresh: () => void
}

const iconBtn = 'p-1 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded-md transition-colors disabled:opacity-40 cursor-pointer'
const iconBtnDanger = 'p-1 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-md transition-colors cursor-pointer'

function VpcGroupSection({ selectedIP, groups, onRefresh }: Props) {
  const { t } = useTranslation()
  const [showCreate, setShowCreate] = useState(false)

  const {
    editAliasId, setEditAliasId, editAliasVal, setEditAliasVal,
    selectedNodes, latencies,
    editingNode, setEditingNode,
    confirmAction, setConfirmAction, ask,
    handleDeleteGroup, handleUpdateGroup, handleSaveAlias,
    handleDeleteNode, handleBatchDelete,
    handleTestSpeed, handleBatchSpeedTest,
    toggleNode, isGroupAllSelected, toggleGroupAll,
  } = useVpcGroupSection({ selectedIP, groups, onRefresh })

  const latencyKey = (id: number) => latencies[id]
  // latency.failed:<原因> / latency.parseFailed 等前缀态一律按失败红字渲染；
  // 原因文本从 failed: 后截取，单元格放不下用省略号 + title 全量
  const latencyFailed = (k?: string) => !!k && k !== 'latency.testing' && (k.startsWith('latency.') || k.startsWith('latency.failed:'))
  const latencyReason = (k?: string) => {
    if (!k) return undefined
    if (k.startsWith('latency.failed:')) return k.slice('latency.failed:'.length)
    if (k.startsWith('latency.')) return t('vpc:' + k)
    return undefined
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <button onClick={() => setShowCreate(true)} disabled={!selectedIP}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
          <FolderPlus size={13} />
          {t('vpc:group.add')}
        </button>
        {selectedNodes.length > 0 && (
          <>
            <button onClick={() => {
              const sel = new Set(selectedNodes.map(n => n.id))
              const targets = groups.flatMap(g => g.vpcs?.list || []).filter(n => sel.has(n.id))
              handleBatchSpeedTest(targets)
            }}
              className="flex items-center gap-1 px-2.5 py-1.5 text-xs text-blue-600 bg-blue-50 rounded-lg hover:bg-blue-100 transition-colors">
              <Gauge size={13} />
              {t('vpc:group.batchSpeedTest')}
            </button>
            <button onClick={() => ask(t('vpc:group.batchDeleteTitle'), t('vpc:group.confirmBatchDeleteNodes', { count: selectedNodes.length }), handleBatchDelete)}
              className="flex items-center gap-1 px-2.5 py-1.5 text-xs text-red-500 bg-red-50 rounded-lg hover:bg-red-100 transition-colors">
              <Trash2 size={13} />
              {t('vpc:group.batchDelete')}
            </button>
            <span className="text-[11px] text-blue-600 bg-blue-50 px-2 py-0.5 rounded-full">{t('vpc:rule.selected', { count: selectedNodes.length })}</span>
          </>
        )}
      </div>

      {groups.map(g => (
        <SectionCard key={g.id} className="overflow-hidden">
          {/* 分组头：别名 + 统计 + 操作 */}
          <div className="flex items-center justify-between px-4 py-2.5 bg-gray-50/70 border-b border-gray-100">
            {editAliasId === g.id ? (
              <div className="flex items-center gap-1.5">
                <input value={editAliasVal} onChange={e => setEditAliasVal(e.target.value)}
                  className="text-xs px-2 py-1 border border-gray-200 rounded-lg outline-none focus:border-blue-400 w-36" />
                <button onClick={() => handleSaveAlias(g.id)}
                  className="px-2 py-1 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600">{t('button.save')}</button>
                <button onClick={() => setEditAliasId(null)}
                  className="px-2 py-1 text-xs text-gray-500 bg-gray-100 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
              </div>
            ) : (
              <div className="flex items-center gap-2 min-w-0">
                <FolderOpen size={14} className="text-blue-500 shrink-0" />
                <span className="text-xs font-medium text-gray-700 truncate">{g.alias}</span>
                <span className="text-[11px] text-gray-400 shrink-0">{t('vpc:group.nodes', { count: g.vpcs?.list?.length || 0 })}</span>
              </div>
            )}
            {editAliasId !== g.id && (
              <div className="flex items-center gap-1 shrink-0">
                <button title={t('vpc:group.editAlias')} onClick={() => { setEditAliasId(g.id); setEditAliasVal(g.alias) }} className={iconBtn}>
                  <Pencil size={13} />
                </button>
                <button title={t('vpc:group.update')} onClick={() => handleUpdateGroup(g.id)} className={iconBtn}>
                  <RefreshCw size={13} />
                </button>
                <button title={t('button.delete')}
                  onClick={() => ask(t('vpc:group.deleteTitle'), t('vpc:group.confirmDelete', { alias: g.alias }), () => handleDeleteGroup(g.id))}
                  className={iconBtnDanger}>
                  <Trash2 size={13} />
                </button>
              </div>
            )}
          </div>

          {/* 节点列表 */}
          {g.vpcs?.list && g.vpcs.list.length > 0 && (
            <div className="px-4 py-1">
              <div className="flex items-center justify-between py-1.5">
                <label className="flex items-center gap-2 text-[11px] text-gray-400 cursor-pointer">
                  <input type="checkbox" checked={isGroupAllSelected(g)} onChange={() => toggleGroupAll(g)} className="w-3 h-3" />
                  {t('vpc:group.selectAllNodes')}
                </label>
              </div>
              {g.vpcs.list.map(node => {
                const lat = latencyKey(node.id)
                return (
                  <div key={node.id} className="flex items-center gap-3 py-2 text-xs border-t border-gray-50 hover:bg-gray-50/60 transition-colors rounded px-1 -mx-1">
                    <input type="checkbox" checked={selectedNodes.some(n => n.id === node.id)} onChange={() => toggleNode(node)} className="w-3 h-3 ml-1" />
                    <span className="text-gray-600 w-28 truncate shrink-0" title={node.protocol}>{node.protocol}</span>
                    <TooltipCell text={node.remarks} className="flex-1 min-w-0 text-gray-600" />
                    <span
                      className={`w-24 text-right shrink-0 truncate ${latencyFailed(lat) ? 'text-red-400' : lat === 'latency.testing' ? 'text-blue-400' : 'text-gray-400'}`}
                      title={latencyReason(lat)}
                    >
                      {lat ? (lat.startsWith('latency.failed:') ? latencyReason(lat) : lat.startsWith('latency.') ? t('vpc:' + lat) : lat) : '-'}
                    </span>
                    <div className="flex items-center gap-0.5 shrink-0">
                      <button
                        title={t('vpc:group.speedTest')}
                        onClick={() => handleTestSpeed(node)}
                        disabled={!selectedIP || lat === 'latency.testing'}
                        className={`${iconBtn} disabled:cursor-not-allowed`}
                      >
                        {lat === 'latency.testing' ? <Loader2 size={13} className="animate-spin" /> : <Gauge size={13} />}
                      </button>
                      <button title={t('vpc:group.editNode')} onClick={() => setEditingNode(node)} className={iconBtn}>
                        <Pencil size={13} />
                      </button>
                      <button title={t('button.delete')} onClick={() => handleDeleteNode(node)} className={iconBtnDanger}>
                        <Trash2 size={13} />
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
          {(!g.vpcs?.list || g.vpcs.list.length === 0) && (
            <div className="px-4 py-3 text-[11px] text-gray-300">{t('vpc:group.noNodes')}</div>
          )}
        </SectionCard>
      ))}

      {groups.length === 0 && (
        <SectionCard>
          <EmptyState icon={FolderOpen} title={selectedIP ? t('vpc:group.noGroups') : t('vpc:group.selectDevice')}
            actionLabel={selectedIP ? t('vpc:group.add') : undefined}
            onAction={selectedIP ? () => setShowCreate(true) : undefined} />
        </SectionCard>
      )}

      {showCreate && (
        <CreateGroupModal selectedIP={selectedIP} groups={groups} onClose={() => setShowCreate(false)} onRefresh={onRefresh} />
      )}

      {editingNode && selectedIP && (
        <EditNodeModal selectedIP={selectedIP} node={editingNode} onClose={() => setEditingNode(null)} onRefresh={onRefresh} />
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={confirmAction.onConfirm} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

export default VpcGroupSection
