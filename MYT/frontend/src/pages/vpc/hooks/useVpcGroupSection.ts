import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as VpcApi from '../../../api/vpcApi'
import { VpcGroup, VpcNode } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import { useConfirm } from '../../../hooks/useConfirm'

interface UseVpcGroupSectionParams {
  selectedIP: string
  groups: VpcGroup[]
  onRefresh: () => void
}

export function useVpcGroupSection({ selectedIP, groups, onRefresh }: UseVpcGroupSectionParams) {
  const { t } = useTranslation()
  const [editAliasId, setEditAliasId] = useState<number | null>(null)
  const [editAliasVal, setEditAliasVal] = useState('')
  const [selectedNodes, setSelectedNodes] = useState<VpcNode[]>([])
  const [latencies, setLatencies] = useState<Record<number, string>>({})
  const { confirmAction, setConfirmAction, ask } = useConfirm()
  const [editingNode, setEditingNode] = useState<VpcNode | null>(null)

  const handleDeleteGroup = async (id: number) => {
    if (!selectedIP) return
    try { await VpcApi.DeleteVpcGroup(selectedIP, id); setConfirmAction(null); onRefresh() }
    catch { showToast(t('vpc:toast.deleteGroupFailed'), 'error') }
  }

  const handleUpdateGroup = async (id: number) => {
    if (!selectedIP) return
    try { await VpcApi.UpdateVpcGroup(selectedIP, id); onRefresh(); showToast(t('vpc:toast.updateSuccess')) }
    catch { showToast(t('vpc:toast.updateGroupFailed'), 'error') }
  }

  const handleSaveAlias = async (id: number) => {
    if (!selectedIP || !editAliasVal.trim()) return
    try { await VpcApi.UpdateVpcGroupAlias(selectedIP, id, editAliasVal.trim()); setEditAliasId(null); onRefresh() }
    catch { showToast(t('vpc:toast.updateAliasFailed'), 'error') }
  }

  const handleDeleteNode = async (node: VpcNode) => {
    if (!selectedIP) return
    try { await VpcApi.DeleteVpc(selectedIP, node.id); setConfirmAction(null); onRefresh() }
    catch { showToast(t('vpc:toast.deleteNodeFailed'), 'error') }
  }

  const handleBatchDelete = async () => {
    let failCount = 0
    for (const node of selectedNodes) {
      try { await VpcApi.DeleteVpc(selectedIP, node.id) } catch { failCount++ }
    }
    setSelectedNodes([]); setConfirmAction(null); onRefresh()
    if (failCount > 0) showToast(t('vpc:toast.nodesDeleteFailed', { count: failCount }), 'error')
  }

  const handleTestSpeed = async (node: VpcNode) => {
    if (!selectedIP) return
    // 测速中防重入（按钮已禁用，这里兜底批量/连点场景）
    if (latencies[node.id] === 'latency.testing') return
    setLatencies(prev => ({ ...prev, [node.id]: 'latency.testing' }))
    try {
      let profile: { server: string; serverPort: number }
      try { profile = JSON.parse(node.profile) } catch {
        setLatencies(prev => ({ ...prev, [node.id]: 'latency.parseFailed' }))
        return
      }
      if (!profile.server || !profile.serverPort) {
        setLatencies(prev => ({ ...prev, [node.id]: 'latency.noAddress' }))
        return
      }
      const result = await VpcApi.TestVpcLatency(selectedIP, `${profile.server}:${profile.serverPort}`) as { latency: string; msg: string }
      setLatencies(prev => ({ ...prev, [node.id]: result.msg ? `latency.failed:${result.msg}` : result.latency || '0' }))
    } catch (err) {
      // 错误透出到单元格：超时单独文案，其余带原因（title 全量展示）
      const msg = err instanceof Error ? err.message : String(err)
      const isTimeout = /timeout|DeadlineExceeded|timed? ?out/i.test(msg)
      setLatencies(prev => ({ ...prev, [node.id]: isTimeout ? 'latency.timeout' : `latency.failed:${msg}` }))
    }
  }

  const handleBatchSpeedTest = async (nodes: VpcNode[]) => {
    await Promise.all(nodes.map(node => handleTestSpeed(node)))
  }

  const toggleNode = (node: VpcNode) => {
    setSelectedNodes(prev => prev.some(n => n.id === node.id) ? prev.filter(n => n.id !== node.id) : [...prev, node])
  }

  const groupNodes = (g: VpcGroup) => g.vpcs?.list || []

  const isGroupAllSelected = (g: VpcGroup) => {
    const nodes = groupNodes(g)
    return nodes.length > 0 && nodes.every(n => selectedNodes.some(s => s.id === n.id))
  }

  const toggleGroupAll = (g: VpcGroup) => {
    const nodes = groupNodes(g)
    if (isGroupAllSelected(g)) {
      setSelectedNodes(prev => prev.filter(n => !nodes.some(gn => gn.id === n.id)))
    } else {
      setSelectedNodes(prev => {
        const existing = new Set(prev.map(n => n.id))
        return [...prev, ...nodes.filter(n => !existing.has(n.id))]
      })
    }
  }

  return {
    editAliasId, setEditAliasId, editAliasVal, setEditAliasVal,
    selectedNodes, latencies,
    editingNode, setEditingNode,
    confirmAction, setConfirmAction, ask,
    handleDeleteGroup, handleUpdateGroup, handleSaveAlias,
    handleDeleteNode, handleBatchDelete,
    handleTestSpeed, handleBatchSpeedTest,
    toggleNode, groupNodes, isGroupAllSelected, toggleGroupAll,
  }
}
