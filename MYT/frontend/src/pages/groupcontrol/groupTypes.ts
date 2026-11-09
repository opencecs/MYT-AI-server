import { DeviceGroup, AdbResultItem } from '../../types/index'

/** 分组树中的节点类型 */
export type GroupNode = {
  type: 'group'
  group: DeviceGroup
} | {
  type: 'ungrouped'
}

/** 左侧栏中展开的分组 ID 集合 */
export type ExpandedGroups = Set<string>

/** 批量投屏模式 */
export type BatchCastMode = 'control' | 'projection'

/** ADB 快捷命令模板 */
export interface AdbShortcut {
  label: string
  command: string
}

/** 批量 ADB 执行状态 */
export interface AdbExecution {
  running: boolean
  command: string
  results: AdbResultItem[]
}
