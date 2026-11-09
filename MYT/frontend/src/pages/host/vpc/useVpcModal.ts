import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import * as VpcService from '../../../../bindings/github.com/moyunteng/myt/vpcservicewrapper'
import { AndroidInfo, VpcGroup, VpcNodeRule } from '../../../types/index'
import { useAndroidNotes } from '../../../hooks/useNoteSync'

interface UseVpcModalParams {
  deviceIP: string
  androidName: string
  androids: AndroidInfo[]
}

export function useVpcModal({ deviceIP, androidName, androids }: UseVpcModalParams) {
  const { t } = useTranslation()
  const [groups, setGroups] = useState<VpcGroup[]>([])
  const [rules, setRules] = useState<VpcNodeRule[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const [selectedGroupId, setSelectedGroupId] = useState<string>('')
  const [assignMode, setAssignMode] = useState<'specified' | 'random'>('specified')
  const [selectedNodeId, setSelectedNodeId] = useState<number | null>(null)
  const { notes } = useAndroidNotes()

  const loadData = async () => {
    setLoading(true); setError('')
    try {
      const [g, r] = await Promise.all([
        VpcService.ListVpcGroups(deviceIP) as unknown as VpcGroup[],
        VpcService.GetContainerRules(deviceIP) as unknown as VpcNodeRule[],
      ])
      setGroups(g || [])
      setRules(r || [])
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  const myRule = rules.find(r => r.containerName === androidName)
  const inVpc = !!myRule && myRule.status > 0
  const currentGroupName = myRule?.groupName || ''

  const currentGroup = groups.find(g => g.alias === currentGroupName)
  const effectiveGroupId = inVpc ? (currentGroup ? String(currentGroup.id) : '') : selectedGroupId
  const selectedGroup = groups.find(g => String(g.id) === effectiveGroupId)

  const handleGroupChange = (groupId: string) => {
    setSelectedGroupId(groupId)
    setSelectedNodeId(null)
  }

  const getVpcNodeId = (): number | null => {
    if (!selectedGroup?.vpcs?.list?.length) return null
    if (assignMode === 'random') {
      const nodes = selectedGroup.vpcs.list
      return nodes[Math.floor(Math.random() * nodes.length)].id
    }
    return selectedNodeId
  }

  const handleJoin = async () => {
    const vpcNodeID = getVpcNodeId()
    if (!vpcNodeID) {
      if (assignMode === 'specified') setError(t('host:vpc.selectNode'))
      else setError(t('host:vpc.noNodesInGroup'))
      return
    }
    setSaving(true); setError(''); setSuccess('')
    try {
      await VpcService.AddVpcRule(deviceIP, androidName, vpcNodeID)
      setSuccess(t('host:vpc.joinedVpc'))
      await loadData()
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  const handleLeave = async () => {
    setSaving(true); setError(''); setSuccess('')
    try {
      await VpcService.DeleteVpcRule(deviceIP, androidName)
      setSuccess(t('host:vpc.leftVpc'))
      setSelectedGroupId('')
      setSelectedNodeId(null)
      await loadData()
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  // 启用/停用绑定（绑定关系保留）：停用后流量直连；老固件无 enabled 字段按启用处理
  const handleToggleEnable = async () => {
    if (!myRule) return
    const enable = myRule.enabled === false
    setSaving(true); setError(''); setSuccess('')
    try {
      await VpcService.EnableVpcRule(deviceIP, myRule.id, enable)
      setSuccess(enable ? t('host:vpc.enabledVpc') : t('host:vpc.disabledVpc'))
      await loadData()
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  const groupsWithNodes = groups.filter(g => g.vpcs?.list?.length > 0)
  const canJoin = selectedGroupId && (assignMode === 'random' ? true : !!selectedNodeId)

  const getNote = (containerName: string) => {
    const android = androids.find(a => a.name === containerName)
    if (!android) return ''
    return notes[android.id] || ''
  }

  return {
    groups, rules, loading, saving, error, success,
    selectedGroupId, assignMode, selectedNodeId,
    inVpc, currentGroupName, effectiveGroupId, selectedGroup,
    groupsWithNodes, canJoin,
    ruleEnabled: inVpc && myRule ? myRule.enabled !== false : true,
    handleGroupChange, handleJoin, handleLeave, handleToggleEnable,
    setAssignMode, setSelectedNodeId, setError, setSuccess,
    getNote,
  }
}
