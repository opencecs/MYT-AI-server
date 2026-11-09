import type { ReactNode } from 'react'
import type { AndroidInfo, ViewMode, Orientation, MirrorItem, DeviceState, SlotExpiry } from '../../../types/index'

export interface ActionBarProps {
  viewMode: ViewMode
  /** 模式切换回调。不传则不渲染「截图/坑位」模式切换（宿主页无双模式语义时用，如主机 tab）。 */
  onViewModeChange?: (mode: ViewMode) => void
  /** 第一行左侧信息区（模式切换之后、按钮组之前）。主机 tab 注入设备 API/固件版本等信息。 */
  infoSlot?: ReactNode
  orientation: Orientation
  onOrientationChange: (o: Orientation) => void
  selectedAndroids: AndroidInfo[]
  allAndroids: AndroidInfo[]
  slotAndroids: AndroidInfo[]
  mirrors: MirrorItem[]
  devices: DeviceState[]
  readyKeys: Set<string>
  slotExpiry: Record<string, SlotExpiry[]>
  onSelectAllAndroids: () => void
  onDeselectAllAndroids: () => void
  hasSelectedDevices: boolean
}

export interface ConfirmInfo { title: string; message: string; confirmLabel: string; danger?: boolean; onConfirm: () => void }
