import { AndroidInfo, SlotExpiry, SlotInfo } from '../../types/index'
import { compareIP } from '../../utils/ipCompare'

// deviceEqual 收敛到 utils/deviceMerge（DeviceSidebar 等共享组件复用），此处重导出保住既有导入面
export { deviceEqual } from '../../utils/deviceMerge'

/**
 * 坑位授权信息归一化：QuerySlotInfo 返回的 SlotInfo[] → UI 用的 SlotExpiry[]。
 * 三处共用（useHostEvents 的坑位轮询 / MaintenancePage 的当前设备 / 创建云机弹窗的目标设备），
 * 逐字符相同，故收敛到此（2026-09-22）。
 */
export function toSlotExpiryList(slots: SlotInfo[]): SlotExpiry[] {
  return slots.map(s => ({ index: s.index, expire_time: s.expire_time, state: s.state }))
}

/** 深比较两个数组是否 JSON 相等 */
export function jsonEqual<T>(a: T[], b: T[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (JSON.stringify(a[i]) !== JSON.stringify(b[i])) return false
  }
  return true
}

/** 深比较两个 Record<string, T[]> 是否 JSON 相等 */
export function jsonEqualMap<T>(a: Record<string, T[]>, b: Record<string, T[]>): boolean {
  const ak = Object.keys(a)
  const bk = Object.keys(b)
  if (ak.length !== bk.length) return false
  for (const k of ak) {
    if (!jsonEqual(a[k] ?? [], b[k] ?? [])) return false
  }
  return true
}

/**
 * 「视图范围」容器集合：右侧两视图（截图/坑位）与 V4 实例网格的展示集合，
 * ActionBar 的 N/M 分母与全选范围同源。
 *
 * 语义（2026-09-26 取消设备↔容器联动后）：
 * - 设备勾选与容器勾选（侧栏过滤器）完全独立——勾选设备 → 该设备全部可见容器；
 *   未勾选设备 → 仅 sidebar 命中的容器。两者并集即展示范围。
 * - 收窄：某设备下 sidebar 命中任一容器 → 该设备只展示被勾选的那些（勾选设备同样收窄）。
 *
 * 抽成纯函数的原因：各部署模式（local/public/v2/v4）的云机页共用同一套
 * 「勾选 → 展示范围」语义，各写一份必然演化分叉。
 *
 * @param statusVisible 可见性谓词（isStatusVisible 绑定 showOffline）
 * @param compare       排序比较器（compareAndroidByDeviceThenSlot 绑定部署模式）
 */
export function computeViewScopeAndroids(
  androidList: AndroidInfo[],
  selectedDeviceIds: Set<string>,
  sidebarAndroidIds: Set<string>,
  statusVisible: (a: AndroidInfo) => boolean,
  compare: (a: AndroidInfo, b: AndroidInfo) => number,
): AndroidInfo[] {
  const sidebarHit = new Set<string>()
  for (const a of androidList) {
    if (sidebarAndroidIds.has(a.id) && statusVisible(a)) sidebarHit.add(a.deviceId)
  }
  if (sidebarHit.size === 0) {
    return androidList
      .filter(a => selectedDeviceIds.has(a.deviceId) && statusVisible(a))
      .sort(compare)
  }
  return androidList
    .filter(a =>
      statusVisible(a) &&
      (sidebarAndroidIds.has(a.id) ||
        (!sidebarHit.has(a.deviceId) && selectedDeviceIds.has(a.deviceId))))
    .sort(compare)
}

/**
 * 按设备 + 坑位号排序的比较器：截图模式多选统一排序键。
 * - local 本地模式：按设备 IP 数值序，同 IP 内按坑位号升序
 * - public 公网模式：按设备 ID 字符串序，同 ID 内按坑位号升序
 * 同设备的坑位相邻（分组排序）。
 */
export function compareAndroidByDeviceThenSlot(
  a: AndroidInfo,
  b: AndroidInfo,
  deployMode: 'local' | 'public'
): number {
  let cmp: number
  if (deployMode === 'public') {
    cmp = a.deviceId < b.deviceId ? -1 : a.deviceId > b.deviceId ? 1 : 0
  } else {
    cmp = compareIP(a.ip, b.ip)
  }
  if (cmp !== 0) return cmp
  return a.indexNum - b.indexNum
}
