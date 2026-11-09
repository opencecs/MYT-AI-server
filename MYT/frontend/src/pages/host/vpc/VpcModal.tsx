import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo } from '../../../types/index'
import { useVpcModal } from './useVpcModal'
import VpcNetworkOverview from './VpcNetworkOverview'
import { useModalEnter } from '../../../hooks/useModalAnimation'

interface Props {
  deviceIP: string
  androidName: string
  displayName?: string
  androids: AndroidInfo[]
  onClose: () => void
}

function VpcModal({ deviceIP, androidName, displayName, androids, onClose }: Props) {
  const { t } = useTranslation()
  const {
    rules, saving, error, success,
    assignMode, selectedNodeId,
    inVpc, currentGroupName, effectiveGroupId, selectedGroup, ruleEnabled,
    groupsWithNodes, canJoin,
    handleGroupChange, handleJoin, handleLeave, handleToggleEnable,
    setAssignMode, setSelectedNodeId,
    getNote,
  } = useVpcModal({ deviceIP, androidName, androids })

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[640px] h-[70vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        {/* 标题栏 */}
        <div className="px-5 py-3 border-b border-gray-100 shrink-0 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-medium text-gray-700">{t('host:vpc.title', { name: displayName || androidName })}</h3>
            {inVpc ? (
              <span className={`text-xs px-2 py-0.5 rounded ${ruleEnabled ? 'bg-green-50 text-green-600' : 'bg-gray-100 text-gray-400'}`}>
                {ruleEnabled
                  ? t('host:vpc.joined', { group: currentGroupName })
                  : t('host:vpc.disabledIn', { group: currentGroupName })}
              </span>
            ) : (
              <span className="text-xs px-2 py-0.5 rounded bg-gray-50 text-gray-400">{t('host:vpc.notJoined')}</span>
            )}
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        <div className="flex-1 overflow-hidden flex flex-col">
          {/* 左右分栏：分组 + 节点 */}
          {groupsWithNodes.length > 0 ? (
            <div className="flex-1 flex min-h-0 m-5 border border-gray-200 rounded-lg overflow-hidden">
              {/* 左侧：分组列表 */}
              <div className="w-[200px] shrink-0 border-r border-gray-100 flex flex-col">
                <div className="px-3 py-2 bg-gray-50 border-b border-gray-100">
                  <span className="text-xs font-medium text-gray-500">{t('host:vpc.groupList')}</span>
                </div>
                <div className="flex-1 overflow-y-auto scrollbar-thin">
                  {groupsWithNodes.map(g => (
                    <div key={g.id}
                      onClick={() => { if (!inVpc) handleGroupChange(String(g.id)) }}
                      className={`px-3 py-2.5 transition-colors border-b border-gray-50 border-l-2 ${
                        effectiveGroupId === String(g.id) ? 'bg-blue-50 border-l-blue-500' :
                        inVpc ? 'border-l-transparent' : 'cursor-pointer hover:bg-gray-50 border-l-transparent'
                      }`}>
                      <div className="text-xs font-medium text-gray-700 truncate">{g.alias}</div>
                      <div className="flex items-center gap-1 mt-0.5">
                        <span className={`text-[10px] px-1 py-px rounded ${
                          g.type === 1 ? 'bg-blue-100 text-blue-500' : 'bg-purple-100 text-purple-500'
                        }`}>{g.type === 1 ? t('host:vpc.builtin') : t('host:vpc.custom')}</span>
                        <span className="text-[10px] text-gray-400">{t('host:vpc.nodeCount', { count: g.vpcs?.list?.length || 0 })}</span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              {/* 右侧：节点 */}
              <div className="flex-1 flex flex-col min-w-0">
                {selectedGroup ? (
                  <>
                    <div className="px-3 py-2 bg-gray-50 border-b border-gray-100 flex items-center justify-between">
                      <span className="text-xs font-medium text-gray-500 truncate">{selectedGroup.alias}</span>
                      {!inVpc && (
                        <div className="flex gap-2 shrink-0">
                          <label className="flex items-center gap-1 text-[11px] cursor-pointer">
                            <input type="radio" checked={assignMode === 'specified'} onChange={() => { setAssignMode('specified'); setSelectedNodeId(null) }} className="accent-blue-500" />
                            {t('host:vpc.specified')}
                          </label>
                          <label className="flex items-center gap-1 text-[11px] cursor-pointer">
                            <input type="radio" checked={assignMode === 'random'} onChange={() => setAssignMode('random')} className="accent-blue-500" />
                            {t('host:vpc.random')}
                          </label>
                        </div>
                      )}
                    </div>
                    <div className="flex-1 overflow-y-auto scrollbar-thin">
                      {inVpc ? (
                        selectedGroup.vpcs?.list?.map(node => (
                          <div key={node.id} className="flex items-center gap-2 px-3 py-2 border-b border-gray-50">
                            <span className="text-xs text-gray-700 truncate flex-1">{node.remarks}</span>
                            <span className="text-[10px] text-gray-400 shrink-0">{node.protocol}</span>
                          </div>
                        ))
                      ) : assignMode === 'specified' ? (
                        selectedGroup.vpcs.list.map(node => (
                          <label key={node.id}
                            className={`flex items-center gap-2 px-3 py-2 cursor-pointer transition-colors border-b border-gray-50 ${
                              selectedNodeId === node.id ? 'bg-blue-50' : 'hover:bg-gray-50'
                            }`}>
                            <input type="radio" name="vpcNode" checked={selectedNodeId === node.id}
                              onChange={() => setSelectedNodeId(node.id)} disabled={saving}
                              className="accent-blue-500 shrink-0" />
                            <span className="text-xs text-gray-700 truncate flex-1">{node.remarks}</span>
                            <span className="text-[10px] text-gray-400 shrink-0">{node.protocol}</span>
                          </label>
                        ))
                      ) : (
                        <div className="p-3">
                          <div className="text-xs text-amber-600 bg-amber-50 p-2.5 rounded-lg">{t('host:vpc.randomAssign')}</div>
                        </div>
                      )}
                    </div>
                  </>
                ) : (
                  <div className="flex-1 flex items-center justify-center">
                    <span className="text-xs text-gray-400">{t('host:vpc.selectGroupFirst')}</span>
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="flex-1 flex items-center justify-center m-5">
              <p className="text-xs text-gray-400">{t('host:vpc.noVpcNodes')}</p>
            </div>
          )}

          {/* VPC 网络概况 */}
          <VpcNetworkOverview rules={rules} androidName={androidName} getNote={getNote} />
        </div>

        {/* 消息 + 按钮 */}
        <div className="shrink-0 border-t border-gray-100">
          {(success || error) && (
            <div className="px-5 pt-2">
              {success && <div className="text-xs text-green-600 bg-green-50 p-2 rounded-lg">{success}</div>}
              {error && <div className="text-xs text-red-500 bg-red-50 p-2 rounded-lg">{error}</div>}
            </div>
          )}
          <div className="flex justify-end gap-2 px-5 py-3">
            <button onClick={onClose} disabled={saving} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200 disabled:opacity-50">{t('host:vpc.close')}</button>
            {inVpc && (
              <>
                <button onClick={handleToggleEnable} disabled={saving}
                  className={`px-4 py-1.5 text-sm rounded-lg disabled:opacity-50 ${ruleEnabled ? 'bg-gray-500 text-white hover:bg-gray-600' : 'bg-blue-500 text-white hover:bg-blue-600'}`}>
                  {saving ? t('host:vpc.processing') : ruleEnabled ? t('host:vpc.disableVpc') : t('host:vpc.enableVpc')}
                </button>
                <button onClick={handleLeave} disabled={saving}
                  className="px-4 py-1.5 text-sm bg-red-500 text-white rounded-lg hover:bg-red-600 disabled:opacity-50">
                  {saving ? t('host:vpc.processing') : t('host:vpc.leaveVpc')}
                </button>
              </>
            )}
            {!inVpc && (
              <button onClick={handleJoin} disabled={saving || !canJoin}
                className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">
                {saving ? t('host:vpc.processing') : t('host:vpc.joinVpc')}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default VpcModal
