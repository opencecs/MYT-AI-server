import { useState, useEffect, useCallback, useRef } from 'react'
import { Events } from '@wailsio/runtime'
import * as GroupControlService from '../../bindings/github.com/moyunteng/myt/groupcontrolservicewrapper'
import { DeviceGroup } from '../types/index'
import { cast } from '../utils/cast'
import { useDeviceGroupMode } from './useDeviceGroupMode'

/** 未分组虚拟 ID */
export const UNGROUPED_ID = '__ungrouped__'

/**
 * 共享设备分组 hook（云机页 / 群控页共用同一份分组数据）。
 * 后端 device_groups.json 按部署模式(local/public)隔离，前端用 getDeviceId 统一设备ID。
 * 监听 devicegroups:updated 事件，任一 Tab 修改分组后所有 Tab 自动同步。
 */
export function useDeviceGroups() {
  const [groups, setGroups] = useState<DeviceGroup[]>([])
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set([UNGROUPED_ID]))
  // 回调内读最新值用 ref（避免把 groups/singleGroup 挂进依赖导致回调频繁重建）
  const groupsRef = useRef(groups)
  groupsRef.current = groups
  const [singleGroup] = useDeviceGroupMode()
  const singleGroupRef = useRef(singleGroup)
  singleGroupRef.current = singleGroup

  const refreshGroups = useCallback(async () => {
    try {
      const g = cast<DeviceGroup[]>(await GroupControlService.GetDeviceGroups())
      setGroups(g ?? [])
    } catch {}
  }, [])

  useEffect(() => {
    refreshGroups()
    const off = Events.On('devicegroups:updated', () => { refreshGroups() })
    return () => { off() }
  }, [refreshGroups])

  const createGroup = useCallback(async (name: string) => {
    try { await GroupControlService.CreateDeviceGroup(name); await refreshGroups() } catch {}
  }, [refreshGroups])

  const deleteGroup = useCallback(async (groupId: string) => {
    try { await GroupControlService.DeleteDeviceGroup(groupId); await refreshGroups() } catch {}
  }, [refreshGroups])

  const renameGroup = useCallback(async (groupId: string, name: string) => {
    try { await GroupControlService.UpdateDeviceGroup(groupId, name); await refreshGroups() } catch {}
  }, [refreshGroups])

  const addDeviceToGroup = useCallback(async (groupId: string, deviceId: string) => {
    try { await GroupControlService.AddDeviceToGroup(groupId, deviceId); await refreshGroups() } catch {}
  }, [refreshGroups])

  const removeDeviceFromGroup = useCallback(async (groupId: string, deviceId: string) => {
    try { await GroupControlService.RemoveDeviceFromGroup(groupId, deviceId); await refreshGroups() } catch {}
  }, [refreshGroups])

  /**
   * 批量移动设备到目标分组（移出原组 + 加入目标组，最后统一刷新）。
   * 「设备单分组」互斥在此下沉兜底：fromGroupId 为 null 但有目标组的条目
   * （防未来入口再漏摘旧组），按当前 groups 快照补齐该设备所属的其他组并移出，
   * 保证单分组模式下设备最终只属一组；多分组模式原样透传。
   */
  const batchMoveDevicesToGroup = useCallback(async (moves: { deviceId: string; fromGroupId: string | null; toGroupId: string | null }[]) => {
    try {
      for (const m of moves) {
        if (!m.fromGroupId && m.toGroupId && singleGroupRef.current) {
          const owned = groupsRef.current
            .filter(g => g.deviceIds.includes(m.deviceId) && g.id !== m.toGroupId)
          for (const g of owned) {
            await GroupControlService.RemoveDeviceFromGroup(g.id, m.deviceId)
          }
        }
        if (m.fromGroupId && m.fromGroupId !== m.toGroupId) {
          await GroupControlService.RemoveDeviceFromGroup(m.fromGroupId, m.deviceId)
        }
        if (m.toGroupId && m.fromGroupId !== m.toGroupId) {
          await GroupControlService.AddDeviceToGroup(m.toGroupId, m.deviceId)
        }
      }
      await refreshGroups()
    } catch { await refreshGroups() }
  }, [refreshGroups])

  const toggleGroupExpand = useCallback((groupId: string) => {
    setExpandedGroups(prev => {
      const next = new Set(prev)
      next.has(groupId) ? next.delete(groupId) : next.add(groupId)
      return next
    })
  }, [])

  return {
    groups, expandedGroups,
    refreshGroups,
    createGroup, deleteGroup, renameGroup,
    addDeviceToGroup, removeDeviceFromGroup, batchMoveDevicesToGroup,
    toggleGroupExpand,
  }
}
