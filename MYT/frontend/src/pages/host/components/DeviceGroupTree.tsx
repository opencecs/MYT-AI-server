import { useState } from 'react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { DeviceState, DeviceGroup } from '../../../types/index'
import GroupTreeSection from './GroupTreeSection'
import DeviceContextMenu, { ContextMenuState } from '../../../components/DeviceContextMenu'
import { useDeviceGroupMode } from '../../../hooks/useDeviceGroupMode'

interface Props {
  groups: DeviceGroup[]
  expandedGroups: Set<string>
  devices: DeviceState[]
  selectedDeviceIds: Set<string>
  ungroupedId: string
  onToggleExpand: (groupId: string) => void
  onRenameGroup: (groupId: string, name: string) => void
  onDeleteGroup: (groupId: string) => void
  onMoveDevices: (moves: { deviceId: string; fromGroupId: string | null; toGroupId: string | null }[]) => void
  onAddToGroup: (groupId: string, deviceId: string) => void
  onRemoveFromGroup: (groupId: string, deviceId: string) => void
  /** 右键菜单：设置备注弹窗 */
  onSetNote: (device: DeviceState) => void
  /** 右键菜单：设备信息弹窗 */
  onDeviceInfo: (device: DeviceState) => void
  /** 右键菜单：重启 ARM 设备（不传则菜单不显示该项） */
  onRebootDevice?: (device: DeviceState) => void
  /** 右键菜单：魔云互联 / 公网穿透 / 镜像清理 / 日志清理 / 格式化 */
  onMytPanel?: (device: DeviceState) => void
  onTunnel?: (device: DeviceState) => void
  onDockerCleanup?: (device: DeviceState) => void
  onLogCleanup?: (device: DeviceState) => void
  onFormatDisk?: (device: DeviceState) => void
  /** 右键菜单：SDK 升级（仅在线设备；不传则菜单不显示该项） */
  onSdkUpgrade?: (device: DeviceState) => void
  /** SDK 升级 v2 模式标记（文案「更新」） */
  sdkUpgradeV2?: boolean
  /** 右键菜单：tebox SDK 安装（仅 local 模式；不传则菜单不显示该项） */
  onTeboxInstall?: (device: DeviceState) => void
  /** 当前备注映射（菜单打开备注弹窗时取最新值） */
  notes?: Record<string, string>
  /** 分组全选（三态 checkbox）：select=true 勾选该组全部可见设备，false 取消；不传则不渲染 */
  onSelectGroupDevices?: (deviceIds: string[], select: boolean) => void
  renderCard: (device: DeviceState) => ReactNode
}

interface DragPayload {
  deviceIds: string[]
  /** 每台拖动设备当前所属的全部分组 ID（单分组模式每组最多 1 个） */
  currentGroupIds: string[][]
}

const DRAG_TYPE = 'application/x-myt-device'

/**
 * 设备分组树：分组标题 + 设备卡片（复用群控分组数据）。
 * 卡片可拖拽到分组/未分组标题上进行移动，右键弹出分组菜单。
 */
function DeviceGroupTree({
  groups, expandedGroups, devices, selectedDeviceIds, ungroupedId,
  onToggleExpand, onRenameGroup, onDeleteGroup, onMoveDevices,
  onAddToGroup, onRemoveFromGroup, onSetNote, onDeviceInfo, onRebootDevice,
  onMytPanel, onTunnel, onDockerCleanup, onLogCleanup, onFormatDisk, onSdkUpgrade, sdkUpgradeV2, onTeboxInstall, notes,
  onSelectGroupDevices, renderCard,
}: Props) {
  const { t } = useTranslation()
  const [singleGroup] = useDeviceGroupMode()
  const [dragOverGroupId, setDragOverGroupId] = useState<string | null>(null)
  const [draggingCount, setDraggingCount] = useState(0)
  const [contextMenu, setContextMenu] = useState<ContextMenuState>({
    visible: false, x: 0, y: 0, deviceId: '', currentGroupIds: [],
  })

  // 设备→分组映射（单分组模式设备只属一组；多分组模式收集其全部所属组）
  const deviceGroupIdsMap = new Map<string, string[]>()
  for (const g of groups) {
    for (const id of g.deviceIds) {
      const arr = deviceGroupIdsMap.get(id)
      if (arr) arr.push(g.id)
      else deviceGroupIdsMap.set(id, [g.id])
    }
  }
  const groupedDeviceIds = new Set(groups.flatMap(g => g.deviceIds))
  const ungroupedDevices = devices.filter(d => !groupedDeviceIds.has(d.device_id))

  // ====== 拖拽 ======

  const handleDragStart = (e: React.DragEvent, deviceId: string) => {
    const isSelected = selectedDeviceIds.has(deviceId)
    const dragIds = isSelected ? Array.from(selectedDeviceIds) : [deviceId]
    const currentGroupIds = dragIds.map(id => deviceGroupIdsMap.get(id) ?? [])
    const payload: DragPayload = { deviceIds: dragIds, currentGroupIds }
    e.dataTransfer.setData(DRAG_TYPE, JSON.stringify(payload))
    e.dataTransfer.effectAllowed = 'move'
    setDraggingCount(dragIds.length)
  }

  const handleDragEnd = () => {
    setDraggingCount(0)
    setDragOverGroupId(null)
  }

  const handleDragOver = (e: React.DragEvent, groupId: string) => {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    setDragOverGroupId(groupId)
  }

  const handleDragLeave = () => {
    setDragOverGroupId(null)
  }

  const handleDrop = (e: React.DragEvent, targetGroupId: string) => {
    e.preventDefault()
    setDragOverGroupId(null)
    setDraggingCount(0)
    try {
      const raw = e.dataTransfer.getData(DRAG_TYPE)
      if (!raw) return
      const { deviceIds, currentGroupIds } = JSON.parse(raw) as DragPayload
      const moves: { deviceId: string; fromGroupId: string | null; toGroupId: string | null }[] = []
      for (let i = 0; i < deviceIds.length; i++) {
        const deviceId = deviceIds[i]
        const owned = currentGroupIds[i] ?? []
        // ungroupedId 表示移到未分组（只移出，不加入任何组）
        const toGroupId = targetGroupId === ungroupedId ? null : targetGroupId
        if (singleGroup) {
          // 单分组：保持移动语义（从当前组移出）
          const fromGroupId = owned[0] ?? null
          if (fromGroupId === toGroupId) continue
          moves.push({ deviceId, fromGroupId, toGroupId })
        } else {
          // 多分组：仅添加，不摘旧组；已在目标组则跳过
          if (owned.includes(targetGroupId)) continue
          moves.push({ deviceId, fromGroupId: null, toGroupId })
        }
      }
      if (moves.length > 0) {
        onMoveDevices(moves)
      }
    } catch {}
  }

  // ====== 右键菜单 ======

  const handleContextMenu = (e: React.MouseEvent, deviceId: string) => {
    e.preventDefault()
    e.stopPropagation()
    setContextMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      deviceId,
      currentGroupIds: deviceGroupIdsMap.get(deviceId) ?? [],
    })
  }

  const closeContextMenu = () => {
    setContextMenu(prev => ({ ...prev, visible: false }))
  }

  // ====== 渲染设备卡片 ======

  const renderDeviceCard = (device: DeviceState) => (
    <div
      key={device.device_id}
      draggable
      onDragStart={(e) => handleDragStart(e, device.device_id)}
      onDragEnd={handleDragEnd}
      onContextMenu={(e) => handleContextMenu(e, device.device_id)}
      className="animate-fade-in"
    >
      {renderCard(device)}
    </div>
  )

  return (
    <>
      {draggingCount > 1 && (
        <div className="text-[10px] text-blue-500 bg-blue-50 text-center py-0.5">
          {t('groupcontrol:dragging', { count: draggingCount })}
        </div>
      )}
      {groups.map(group => {
        const groupDevices = group.deviceIds
          .map(id => devices.find(d => d.device_id === id))
          .filter((d): d is DeviceState => !!d)
        // 分组三态（相对「过滤后可见设备」的勾选集合）：every→checked、some→半选
        const groupSelectedCount = groupDevices.filter(d => selectedDeviceIds.has(d.device_id)).length
        return (
          <GroupTreeSection
            key={group.id}
            title={group.name}
            count={groupDevices.length}
            isExpanded={expandedGroups.has(group.id)}
            isDragOver={dragOverGroupId === group.id}
            checked={groupDevices.length > 0 && groupSelectedCount === groupDevices.length}
            indeterminate={groupSelectedCount > 0 && groupSelectedCount < groupDevices.length}
            onToggleSelectGroup={onSelectGroupDevices
              ? () => onSelectGroupDevices(
                  groupDevices.map(d => d.device_id),
                  groupSelectedCount < groupDevices.length,
                )
              : undefined}
            onToggleExpand={() => onToggleExpand(group.id)}
            onDragOver={(e) => handleDragOver(e, group.id)}
            onDragLeave={handleDragLeave}
            onDrop={(e) => handleDrop(e, group.id)}
            onRename={(name) => onRenameGroup(group.id, name)}
            onDelete={() => onDeleteGroup(group.id)}
          >
            {expandedGroups.has(group.id) && groupDevices.map(renderDeviceCard)}
          </GroupTreeSection>
        )
      })}

      <GroupTreeSection
        title={t('groupcontrol:ungrouped')}
        count={ungroupedDevices.length}
        isExpanded={expandedGroups.has(ungroupedId)}
        isUngrouped
        isDragOver={dragOverGroupId === ungroupedId}
        checked={ungroupedDevices.length > 0 && ungroupedDevices.every(d => selectedDeviceIds.has(d.device_id))}
        indeterminate={ungroupedDevices.some(d => selectedDeviceIds.has(d.device_id))
          && !ungroupedDevices.every(d => selectedDeviceIds.has(d.device_id))}
        onToggleSelectGroup={onSelectGroupDevices
          ? () => onSelectGroupDevices(
              ungroupedDevices.map(d => d.device_id),
              !ungroupedDevices.every(d => selectedDeviceIds.has(d.device_id)),
            )
          : undefined}
        onToggleExpand={() => onToggleExpand(ungroupedId)}
        onDragOver={(e) => handleDragOver(e, ungroupedId)}
        onDragLeave={handleDragLeave}
        onDrop={(e) => handleDrop(e, ungroupedId)}
      >
        {expandedGroups.has(ungroupedId) && ungroupedDevices.map(renderDeviceCard)}
      </GroupTreeSection>

      <DeviceContextMenu
        menu={contextMenu}
        groups={groups}
        device={devices.find(d => d.device_id === contextMenu.deviceId)}
        note={notes?.[contextMenu.deviceId]}
        selectedDeviceIds={selectedDeviceIds}
        onSetNote={onSetNote}
        onDeviceInfo={onDeviceInfo}
        onRebootDevice={onRebootDevice}
        onMytPanel={onMytPanel}
        onTunnel={onTunnel}
        onDockerCleanup={onDockerCleanup}
        onLogCleanup={onLogCleanup}
        onFormatDisk={onFormatDisk}
        onSdkUpgrade={onSdkUpgrade}
        sdkUpgradeV2={sdkUpgradeV2}
        onTeboxInstall={onTeboxInstall}
        onAddToGroup={onAddToGroup}
        onRemoveFromGroup={onRemoveFromGroup}
        onBatchMoveDevicesToGroup={onMoveDevices}
        onClose={closeContextMenu}
      />
    </>
  )
}

export default DeviceGroupTree
