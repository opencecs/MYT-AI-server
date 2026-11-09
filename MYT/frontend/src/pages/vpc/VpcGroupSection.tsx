import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { VpcGroup } from '../../types/index'
import ConfirmDialog from '../../components/ConfirmDialog'
import TooltipCell from '../../components/TooltipCell'
import CreateGroupModal from './CreateGroupModal'
import EditNodeModal from './EditNodeModal'
import { useVpcGroupSection } from './useVpcGroupSection'

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  onRefresh: () => void
}

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
    toggleNode, groupNodes, isGroupAllSelected, toggleGroupAll,
  } = useVpcGroupSection({ selectedIP, groups, onRefresh })

  const b = 'px-1.5 py-0.5 text-[10px] rounded whitespace-nowrap cursor-pointer'
  const bBlue = `${b} bg-blue-50 text-blue-600 hover:bg-blue-100`
  const bRed = `${b} bg-red-50 text-red-500 hover:bg-red-100`

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <button onClick={() => setShowCreate(true)} disabled={!selectedIP}
          className="px-2 py-1 text-xs bg-green-50 text-green-600 rounded hover:bg-green-100 disabled:opacity-50">{t('vpc:group.add')}</button>
      </div>

      {groups.map(g => (
        <div key={g.id} className="border border-gray-100 rounded-lg overflow-hidden">
          <div className="flex items-center justify-between px-3 py-2 bg-gray-50">
            {editAliasId === g.id ? (
              <div className="flex items-center gap-1">
                <input value={editAliasVal} onChange={e => setEditAliasVal(e.target.value)}
                  className="text-xs px-2 py-0.5 border border-gray-200 rounded outline-none w-32" />
                <button onClick={() => handleSaveAlias(g.id)} className={bBlue}>{t('button.save')}</button>
                <button onClick={() => setEditAliasId(null)} className={`${b} bg-gray-100 text-gray-500`}>{t('button.cancel')}</button>
              </div>
            ) : <span className="text-xs font-medium text-gray-700">{g.alias}</span>}
            <div className="flex gap-1">
              {editAliasId !== g.id && <button onClick={() => { setEditAliasId(g.id); setEditAliasVal(g.alias) }} className={bBlue}>{t('vpc:group.editAlias')}</button>}
              <button onClick={() => handleUpdateGroup(g.id)} className={bBlue}>{t('vpc:group.update')}</button>
              <button onClick={() => ask(t('vpc:group.deleteTitle'), t('vpc:group.confirmDelete', { alias: g.alias }), () => handleDeleteGroup(g.id))} className={bRed}>{t('button.delete')}</button>
            </div>
          </div>

          {g.vpcs?.list && g.vpcs.list.length > 0 && (
            <div className="px-3 py-1">
              <div className="flex items-center justify-between mb-1">
                <div className="flex items-center gap-1">
                  <input type="checkbox" checked={isGroupAllSelected(g)} onChange={() => toggleGroupAll(g)} className="w-3 h-3" />
                  <span className="text-[10px] text-gray-400">{t('vpc:group.nodes', { count: g.vpcs.list.length })}</span>
                </div>
                <div className={`flex gap-1 ${selectedNodes.length > 0 ? 'visible' : 'invisible'}`}>
                  <button onClick={() => handleBatchSpeedTest(groupNodes(g).filter(n => selectedNodes.some(s => s.id === n.id)))} className={bBlue}>{t('vpc:group.batchSpeedTest')}</button>
                  <button onClick={() => ask(t('vpc:group.batchDeleteTitle'), t('vpc:group.confirmBatchDeleteNodes', { count: selectedNodes.length }), handleBatchDelete)} className={bRed}>{t('vpc:group.batchDelete')}</button>
                </div>
              </div>
              {g.vpcs.list.map(node => (
                <div key={node.id} className="flex items-center gap-6 py-0.5 text-[11px] border-t border-gray-50">
                  <input type="checkbox" checked={selectedNodes.some(n => n.id === node.id)} onChange={() => toggleNode(node)} className="w-3 h-3" />
                  <span className="text-gray-600 w-32 truncate shrink-0" title={node.protocol}>{node.protocol}</span>
                  <TooltipCell text={node.remarks} className="flex-1 min-w-0 text-gray-500" />
                  <span className={`w-14 text-right ${latencies[node.id] === 'latency.failed' || latencies[node.id] === 'latency.parseFailed' || latencies[node.id] === 'latency.noAddress' ? 'text-red-400' : 'text-gray-400'}`}>
                    {latencies[node.id] ? (latencies[node.id].startsWith('latency.') ? t('vpc:' + latencies[node.id]) : latencies[node.id]) : '-'}
                  </span>
                  <button onClick={() => handleTestSpeed(node)} className={bBlue}>{t('vpc:group.speedTest')}</button>
                  <button onClick={() => setEditingNode(node)} className={bBlue}>{t('vpc:group.editNode')}</button>
                  <button onClick={() => handleDeleteNode(node)} className={bRed}>{t('button.delete')}</button>
                </div>
              ))}
            </div>
          )}
        </div>
      ))}

      {groups.length === 0 && <div className="text-xs text-gray-300 text-center py-6">{selectedIP ? t('vpc:group.noGroups') : t('vpc:group.selectDevice')}</div>}

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
