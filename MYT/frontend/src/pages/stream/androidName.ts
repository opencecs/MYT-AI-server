// 流媒体页云机名称显示工具：备注优先 + 短名拆分，规则与主机/云机 Tab 一致
// （utils/containerName.ts 的 containerDisplayName：≥3 段拆短名 + 坑位号防御）。
// 规范：视图显示用 androidDisplayName，hover title 与持久化数据保留原始全名。
import type { AndroidInfo } from '../../types/index'
import { containerDisplayName } from '../../utils/containerName'

/** 流媒体页云机显示名 = 备注 || 短名（与 SlotView/CardHeader/SwitchAndroidModal 同规则） */
export function androidDisplayName(a: AndroidInfo, notes: Record<string, string>): string {
  return notes[a.id] || containerDisplayName(a.name, a.deviceId, a.indexNum)
}

/**
 * 分发/推流记录的云机列显示名：
 * 能在 androids 缓存中按 id 找到容器记录 → 备注优先 + 短名；
 * 找不到（设备离线/记录较旧）→ 保存的全名 cloudMachineName 兜底。
 */
export function distributionDisplayName(
  record: { cloudMachineId?: string; cloudMachineName?: string },
  androids: AndroidInfo[],
  notes: Record<string, string>,
): string {
  const a = record.cloudMachineId ? androids.find(m => m.id === record.cloudMachineId) : undefined
  return (a ? androidDisplayName(a, notes) : '') || record.cloudMachineName || record.cloudMachineId || ''
}
