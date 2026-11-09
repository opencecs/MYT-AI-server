import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Pencil, Info, FolderInput, LogOut, ChevronRight, RotateCw, Monitor, Trash2, ClipboardList, HardDrive, Globe, ArrowUpCircle, Package } from 'lucide-react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import { useTranslation } from 'react-i18next'
import { useDeployMode } from '../hooks/useDeployMode'
import { useDeviceGroupMode } from '../hooks/useDeviceGroupMode'
import { DeviceGroup, DeviceState } from '../types/index'

gsap.registerPlugin(useGSAP)

/** 菜单宽度（与根节点 w-[190px] 保持一致，flyout 翻向判断用） */
const MENU_W = 190
/** flyout 与父菜单右缘的间隙（px） */
const FLYOUT_GAP = 4

export interface ContextMenuState {
  visible: boolean
  x: number
  y: number
  deviceId: string
  /** 右键设备当前所属的分组 ID 列表（单分组模式只有一个，显示时取 [0]） */
  currentGroupIds: string[]
}

interface Props {
  menu: ContextMenuState
  groups: DeviceGroup[]
  /** 右键的设备（设置备注/设备信息的目标） */
  device: DeviceState | undefined
  /** 当前备注（菜单外维护，弹窗打开时取最新） */
  note: string | undefined
  selectedDeviceIds: Set<string>
  onSetNote: (device: DeviceState) => void
  onDeviceInfo: (device: DeviceState) => void
  /** 右键菜单：重启 ARM 设备（不传则不显示；仅在线设备，调用方弹确认框） */
  onRebootDevice?: (device: DeviceState) => void
  /** 右键菜单：魔云互联面板 */
  onMytPanel?: (device: DeviceState) => void
  /** 右键菜单：公网穿透（frp，弹窗内填服务器信息） */
  onTunnel?: (device: DeviceState) => void
  /** 右键菜单：镜像清理 */
  onDockerCleanup?: (device: DeviceState) => void
  /** 右键菜单：日志清理（设备级） */
  onLogCleanup?: (device: DeviceState) => void
  /** 右键菜单：格式化磁盘（危险，弹窗内二次确认） */
  onFormatDisk?: (device: DeviceState) => void
  /** 右键菜单：SDK 升级（仅在线设备；v2/local 分支逻辑由调用方实现） */
  onSdkUpgrade?: (device: DeviceState) => void
  /** SDK 升级是否处于部署模式 v2（v2 文案用「更新」，与 DeviceInfoBar 一致） */
  sdkUpgradeV2?: boolean
  /** 右键菜单：tebox SDK 安装（仅 local 模式显示；确认/执行由调用方处理） */
  onTeboxInstall?: (device: DeviceState) => void
  onAddToGroup: (groupId: string, deviceId: string) => void
  onRemoveFromGroup: (groupId: string, deviceId: string) => void
  onBatchMoveDevicesToGroup: (moves: { deviceId: string; fromGroupId: string | null; toGroupId: string | null }[]) => void
  onClose: () => void
}

/**
 * 设备右键上下文菜单：设置备注 / 设备信息 / 维护操作 / 分组。
 * 维护操作（重启/魔云互联/镜像清理/日志清理/格式化/SDK 升级）仅在线设备，回调可选。
 * 「分组」hover/点击向右弹出 flyout 子菜单（portal 到 body，靠右缘自动翻向左侧），点击外部关闭。
 * 菜单本体 gsap 入场（scale+fade）。
 */
function DeviceContextMenu({ menu, groups, device, note, selectedDeviceIds, onSetNote, onDeviceInfo, onRebootDevice,
  onMytPanel, onTunnel, onDockerCleanup, onLogCleanup, onFormatDisk, onSdkUpgrade, sdkUpgradeV2, onTeboxInstall,
  onAddToGroup, onRemoveFromGroup, onBatchMoveDevicesToGroup, onClose }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  // 「设备单分组」开关：单分组（默认）保持切换语义，多分组支持一属多组
  const [singleGroup] = useDeviceGroupMode()
  // v2 主机维护通道有限：公网穿透/Docker清理/日志清理/格式化仍砍（壳子无对应
  // 方法）；魔云互联已并入 SSH 链路（与各模式一致），入口不再限制
  const v2 = mode === 'v2'
  const ref = useRef<HTMLDivElement>(null)
  const groupTriggerRef = useRef<HTMLButtonElement>(null)
  const groupFlyoutRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ x: menu.x, y: menu.y })
  const [groupOpen, setGroupOpen] = useState(false)
  // flyout 定位：默认贴父菜单右缘（left: 100%）；右缘放不下则翻到父菜单左侧
  const [flyoutPos, setFlyoutPos] = useState<{ left: number; top: number; flip: boolean } | null>(null)
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const clearCloseTimer = () => {
    if (closeTimer.current) { clearTimeout(closeTimer.current); closeTimer.current = null }
  }

  // hover 进出带 150ms 延迟：横穿父菜单到 flyout 的空隙不误关
  const openGroup = () => { clearCloseTimer(); setGroupOpen(true) }
  const scheduleCloseGroup = () => {
    clearCloseTimer()
    closeTimer.current = setTimeout(() => setGroupOpen(false), 150)
  }

  // 点击外部关闭（flyout 是 portal 到 body 的兄弟浮层，一并纳入判定）
  useEffect(() => {
    if (!menu.visible) return
    const handler = (e: MouseEvent) => {
      const target = e.target as Node
      if (ref.current?.contains(target) || groupFlyoutRef.current?.contains(target)) return
      onClose()
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [menu.visible, onClose])

  // 菜单重新打开/关闭时重置分组 flyout 与挂起的关闭定时器
  useEffect(() => {
    if (menu.visible) {
      setGroupOpen(false)
      clearCloseTimer()
    }
    return clearCloseTimer
  }, [menu.visible])

  // 视口边界检测
  useEffect(() => {
    const el = ref.current
    if (!el || !menu.visible) return
    const h = el.offsetHeight
    const w = el.offsetWidth
    const maxX = window.innerWidth - w - 4
    const nx = Math.max(4, Math.min(menu.x, maxX))
    const margin = 4
    const vh = window.innerHeight
    let ny = menu.y
    if (menu.y + h + margin > vh) ny = vh - h - margin
    if (ny < margin) ny = margin
    setPos({ x: nx, y: ny })
  }, [menu.visible, menu.x, menu.y])

  // flyout 定位（useLayoutEffect 避免首帧闪跳）：与「分组」项同 y 基线，右缘放不下翻到父菜单左侧，
  // 上下贴视口 clamp。依赖 pos 而非 menu.x/y——父菜单边界收敛后以最终位置为基准
  useLayoutEffect(() => {
    if (!groupOpen) { setFlyoutPos(null); return }
    const fly = groupFlyoutRef.current
    const trigger = groupTriggerRef.current
    if (!fly || !trigger) return
    const fh = fly.offsetHeight
    const fw = fly.offsetWidth
    const margin = 4
    const triggerRect = trigger.getBoundingClientRect()
    const top = Math.max(margin, Math.min(triggerRect.top, window.innerHeight - fh - margin))
    // 父菜单左缘 = 菜单 x；右缘放不下（加上间隙）则翻到左侧（flyout 右缘贴父菜单左缘）
    const rightGap = pos.x + MENU_W + FLYOUT_GAP
    const flip = rightGap + fw + margin > window.innerWidth
    const left = flip ? pos.x - fw - FLYOUT_GAP : rightGap
    setFlyoutPos({ left, top, flip })
  }, [groupOpen, pos, groups, menu.currentGroupIds, singleGroup])

  // 菜单入场动画（mount 即播放；每次 visible 变化时元素重挂载由父组件控制——
  // 这里对重开场景补一次 fromTo，保证连续打开也有入场）
  useGSAP(() => {
    if (!ref.current || !menu.visible) return
    gsap.fromTo(ref.current, { scale: 0.92, autoAlpha: 0 }, { scale: 1, autoAlpha: 1, duration: 0.18, ease: 'power3.out' })
  }, [menu.visible])

  if (!menu.visible) return null

  const { deviceId } = menu
  // 单分组模式取第一个分组作为「当前分组」（开关切换不清洗存量数据）
  const currentGroupId = singleGroup ? menu.currentGroupIds[0] ?? null : null
  // 多分组模式下设备已属的分组集合（组列表置灰）
  const ownedGroupIds = singleGroup
    ? new Set(menu.currentGroupIds.slice(0, 1))
    : new Set(menu.currentGroupIds)

  // 如果右键的设备是已选中的，分组操作作用于所有已选中的设备
  const isSelected = selectedDeviceIds.has(deviceId)
  const sameGroupSelectedIds = isSelected ? Array.from(selectedDeviceIds) : [deviceId]
  const affectedCount = sameGroupSelectedIds.length

  // 设备 → 已属分组 ID 集（多分组批量添加的跳过判定用）。
  // 已知集 = 右键设备的 currentGroupIds（菜单打开时快照）；其余被选设备从
  // groups.deviceIds 推导（与 DeviceGroupTree.deviceGroupIdsMap 同构）。
  const ownedByTarget = new Map<string, string[]>()
  ownedByTarget.set(deviceId, menu.currentGroupIds)
  for (const g of groups) {
    for (const id of g.deviceIds) {
      if (id === deviceId) continue
      const arr = ownedByTarget.get(id)
      if (arr) arr.push(g.id)
      else ownedByTarget.set(id, [g.id])
    }
  }
  const deviceGroupIdsOf = (id: string): string[] => ownedByTarget.get(id) ?? []

  const handleRemoveFromGroup = (groupId: string) => {
    if (affectedCount <= 1) {
      onRemoveFromGroup(groupId, deviceId)
    } else {
      onBatchMoveDevicesToGroup(sameGroupSelectedIds.map(id => ({ deviceId: id, fromGroupId: groupId, toGroupId: null })))
    }
    onClose()
  }

  const handleAddToGroup = (targetGroupId: string) => {
    if (affectedCount <= 1) {
      // 单分组模式 = 切换语义：先把设备从当前组移出，再加入目标组（否则旧组不动，
      // 设备同时挂在两组）。目标组即当前组则无事可做。
      if (singleGroup && currentGroupId) {
        if (currentGroupId === targetGroupId) { onClose(); return }
        onBatchMoveDevicesToGroup([{ deviceId, fromGroupId: currentGroupId, toGroupId: targetGroupId }])
      } else {
        // 未分组设备（currentGroupId 为 null）或多分组模式：纯添加
        onAddToGroup(targetGroupId, deviceId)
      }
    } else {
      // 单分组模式 = 切换语义：每台设备先摘掉自己所属的全部其他组（存量双组
      // 设备会在此被清洗为单组），再加入目标组。
      // 多分组模式 = 纯添加（不摘旧组）：已属目标组的设备跳过（与
      // DeviceGroupTree.handleDrop 的 owned.includes 跳过逻辑同源——后端幂等性
      // 未确认，不重复提交）。
      const moves = sameGroupSelectedIds.flatMap(id => {
        const owned = deviceGroupIdsOf(id)
        if (singleGroup) {
          const removes = owned.filter(g => g !== targetGroupId)
            .map(g => ({ deviceId: id, fromGroupId: g, toGroupId: null }))
          return [...removes, { deviceId: id, fromGroupId: null, toGroupId: targetGroupId }]
        }
        if (owned.includes(targetGroupId)) return []
        return [{ deviceId: id, fromGroupId: null, toGroupId: targetGroupId }]
      })
      if (moves.length === 0) { onClose(); return }
      onBatchMoveDevicesToGroup(moves)
    }
    onClose()
  }

  const itemCls = 'w-full text-left flex items-center gap-2 px-3 py-1.5 text-xs text-gray-700 hover:bg-blue-50 hover:text-blue-600 cursor-pointer'

  return (
    <div
      ref={ref}
      role="menu"
      className="fixed bg-white rounded-lg shadow-lg border border-gray-200 z-[100] py-1 w-[190px] overflow-hidden"
      style={{ left: pos.x, top: pos.y }}
    >
      {/* 设置备注 */}
      <button className={itemCls} onClick={() => { if (device) onSetNote(device); onClose() }} title={note || undefined}>
        <Pencil size={14} className="shrink-0" />
        <span className="truncate">{t('groupcontrol:contextMenu.setNote')}</span>
        {note && <span className="ml-auto text-[10px] text-gray-400 truncate max-w-14 shrink-0" title={note}>{note}</span>}
      </button>
      {/* 设备信息 */}
      <button className={itemCls} onClick={() => { if (device) onDeviceInfo(device); onClose() }}>
        <Info size={14} className="shrink-0" />
        <span className="truncate">{t('groupcontrol:contextMenu.deviceInfo')}</span>
      </button>
      {/* 维护操作：仅在线 ARM 主机；确认/弹窗由调用方处理。v2 魔云互联已并入
          SSH 链路（2026-09-22），入口与其他模式一致 */}
      {device?.state === 0 && (onRebootDevice || onMytPanel || onTunnel || onDockerCleanup || onLogCleanup || onFormatDisk) && (
        <div className="border-t border-gray-100 my-1" />
      )}
      {onRebootDevice && device?.state === 0 && (
        <button className={itemCls} onClick={() => { onRebootDevice(device); onClose() }}>
          <RotateCw size={14} className="shrink-0" />
          <span className="truncate">{t('maintenance:rebootDevice')}</span>
        </button>
      )}
      {onMytPanel && device?.state === 0 && (
        <button className={itemCls} onClick={() => { onMytPanel(device); onClose() }}>
          <Monitor size={14} className="shrink-0" />
          <span className="truncate">{t('maintenance:mytPanel')}</span>
        </button>
      )}
      {onTunnel && !v2 && device?.state === 0 && (
        <button className={itemCls} onClick={() => { onTunnel(device); onClose() }}>
          <Globe size={14} className="shrink-0" />
          <span className="truncate">{t('maintenance:penetration.title')}</span>
        </button>
      )}
      {onDockerCleanup && !v2 && device?.state === 0 && (
        <button className={itemCls} onClick={() => { onDockerCleanup(device); onClose() }}>
          <Trash2 size={14} className="shrink-0" />
          <span className="truncate">{t('maintenance:dockerCleanup')}</span>
        </button>
      )}
      {onLogCleanup && !v2 && device?.state === 0 && (
        <button className={itemCls} onClick={() => { onLogCleanup(device); onClose() }}>
          <ClipboardList size={14} className="shrink-0" />
          <span className="truncate">{t('maintenance:logCleanup')}</span>
        </button>
      )}
      {onFormatDisk && !v2 && device?.state === 0 && (
        <button className={`${itemCls} text-red-500 hover:bg-red-50 hover:text-red-600`} onClick={() => { onFormatDisk(device); onClose() }}>
          <HardDrive size={14} className="shrink-0" />
          <span className="truncate">{t('maintenance:formatDisk')}</span>
        </button>
      )}
      {/* SDK 升级：仅在线设备（离线升级无意义）；文案与 DeviceInfoBar 一致——v2「更新」/其余「升级」 */}
      {onSdkUpgrade && device?.state === 0 && (
        <button className={itemCls} onClick={() => { onSdkUpgrade(device); onClose() }}>
          <ArrowUpCircle size={14} className="shrink-0" />
          <span className="truncate">{sdkUpgradeV2 ? t('maintenance:devicePwd.update') : t('maintenance:devicePwd.sdkUpgrade')}</span>
        </button>
      )}
      {/* tebox SDK 安装：仅 local（V3 本地）模式 + 在线设备——远程安装 tobox_player（SSH 链路） */}
      {onTeboxInstall && mode === 'local' && device?.state === 0 && (
        <button className={itemCls} onClick={() => { onTeboxInstall(device); onClose() }}>
          <Package size={14} className="shrink-0" />
          <span className="truncate">{t('maintenance:teboxInstall.menuItem')}</span>
        </button>
      )}

      <div className="border-t border-gray-100 my-1" />

      {/* 分组：hover/点击向右弹出 flyout 子菜单（点击切换，兼容触屏/键盘） */}
      <button
        ref={groupTriggerRef}
        className={`${itemCls} ${groupOpen ? 'text-blue-600 bg-blue-50' : ''}`}
        onClick={() => (groupOpen ? setGroupOpen(false) : openGroup())}
        onMouseEnter={openGroup}
        onMouseLeave={scheduleCloseGroup}
      >
        <FolderInput size={14} className="shrink-0" />
        <span className="truncate">{t('groupcontrol:contextMenu.group')}</span>
        <ChevronRight size={12} className={`ml-auto shrink-0 text-gray-400 transition-transform duration-200 ${groupOpen ? 'rotate-180' : ''}`} />
      </button>

      {/* 分组 flyout 子菜单：portal 到 body（父菜单有 gsap transform 会形成 fixed 包含块，
          内嵌会被困住）；定位/翻向由 useLayoutEffect 计算后再挂载显示 */}
      {groupOpen && createPortal(
        <div
          ref={groupFlyoutRef}
          role="menu"
          className="fixed bg-white rounded-lg shadow-lg border border-gray-200 z-[100] py-1 w-[190px] animate-fade-in"
          style={flyoutPos
            ? { left: flyoutPos.left, top: flyoutPos.top, visibility: 'visible' }
            : { visibility: 'hidden' }}
          onMouseEnter={openGroup}
          onMouseLeave={scheduleCloseGroup}
        >
          <div className="py-1">
            {/* 单分组模式：唯一「移出分组」（当前组）；多分组模式：逐组移除区 */}
            {singleGroup && currentGroupId && (
              <button
                className="w-full text-left flex items-center gap-2 px-3 py-1.5 text-xs text-red-500 hover:bg-red-50"
                onClick={() => handleRemoveFromGroup(currentGroupId)}
              >
                <LogOut size={13} className="shrink-0" />
                <span className="truncate">
                  {affectedCount > 1 ? t('groupcontrol:removeFromGroupCount', { count: affectedCount }) : t('groupcontrol:removeFromGroup')}
                </span>
              </button>
            )}
            {!singleGroup && menu.currentGroupIds.length > 0 && (
              <>
                <div className="px-3 pt-1 pb-0.5 text-[10px] text-gray-400">{t('groupcontrol:removeFromGroupTitle')}</div>
                {menu.currentGroupIds.map(gid => {
                  const g = groups.find(gr => gr.id === gid)
                  return (
                    <button
                      key={gid}
                      className="w-full text-left flex items-center gap-2 px-3 py-1.5 text-xs text-red-500 hover:bg-red-50"
                      onClick={() => handleRemoveFromGroup(gid)}
                    >
                      <LogOut size={13} className="shrink-0" />
                      <span className="truncate">
                        {affectedCount > 1
                          ? t('groupcontrol:removeFromGroupCount', { count: affectedCount })
                          : t('groupcontrol:removeFromGroupNamed', { name: g?.name ?? gid })}
                      </span>
                    </button>
                  )
                })}
              </>
            )}
            {groups.length > 0 ? (
              groups.map(g => {
                // 单分组：当前组置灰（点击=切换）；多分组：已属组置灰（点击=仅添加）
                const isCurrent = ownedGroupIds.has(g.id)
                return (
                  <button
                    key={g.id}
                    className={`w-full text-left flex items-center gap-2 px-3 py-1.5 text-xs ${
                      isCurrent ? 'text-gray-300 cursor-default' : 'text-gray-700 hover:bg-blue-50 hover:text-blue-600 cursor-pointer'
                    }`}
                    onClick={() => { if (!isCurrent) handleAddToGroup(g.id) }}
                    disabled={isCurrent}
                  >
                    <FolderInput size={13} className="shrink-0" />
                    <span className="truncate">{g.name}</span>
                    {isCurrent && <span className="ml-auto text-[10px] shrink-0">{t('groupcontrol:current')}</span>}
                  </button>
                )
              })
            ) : (
              <div className="px-3 py-1.5 text-xs text-gray-400">{t('groupcontrol:noGroups')}</div>
            )}
          </div>
        </div>,
        document.body,
      )}
    </div>
  )
}

export default DeviceContextMenu
