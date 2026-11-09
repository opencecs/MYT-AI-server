/**
 * 容器名称显示规则（两种命名规范都处理）：
 * 1. 壳子式：`{设备ID}_{坑位号}_{名称段}`（如 c043b1cf..._1_T1001）；
 * 2. pc-client 式：`{用户名|时间戳}_{坑位号}_{实例序号}`（如 myname_4_1、1729012345678_4_1），
 *    第一段不是 deviceId。
 * 统一规则：按 `_` 分割段数 ≥3 时取第三段及以后作为显示名（不要求前缀是 deviceId）；
 * 不足 3 段显示原名。
 */

/**
 * 在最后一个下划线处拆分容器名，返回 [前缀, 简短名]（旧客户端重命名逻辑）：
 * 重命名时保留前缀、只替换后面的用户可读部分。
 * 无 `_` 或 `_` 在末尾时前缀为空串、整名作为短名返回。
 */
export function splitContainerName(name: string): [string, string] {
  if (!name || !name.includes('_')) return ['', name]
  const idx = name.lastIndexOf('_')
  if (idx === -1 || idx === name.length - 1) return ['', name]
  return [name.substring(0, idx + 1), name.substring(idx + 1)]
}

/**
 * 容器名的视图显示名：
 * 按 `_` 分割，段数 ≥3 时取第三段及以后 join（名称段本身可能含 `_`），
 * 不再要求前缀匹配所属设备 deviceId（pc-client 式命名第一段是用户名/时间戳）；
 * 拆分出的名称段为空且 indexNum 有值时兜底 `T{1000+indexNum}`。
 * 段数不足 3 → 原名返回。
 *
 * 坑位号防御：pc-client 式名称第三段是实例序号，正常取 slice(2) 即可；但用户输入名可能
 * 自带 `_`（如用户名 `my_name` → 容器 `my_name_4_1` 共 4 段），此时第三段是坑位号、
 * slice(2) 会把「坑位号_实例序号」（4_1）当显示名。
 * 防御：段数 ≥4 且第三段（parts[2]）为纯数字且等于 indexNum（说明取到的是坑位号）→
 * 改取 slice(3)；slice(3) 为空则显示原名。
 * 注意：① 判定键是第三段而非 slice(2) 结果——否则实例序号恰等于 indexNum 的正常名
 * （如 `1729012345678_4_4`）会被误退回原名；② 需段数 ≥4 才触发——3 段名的第三段是
 * 实例序号而非坑位号（`myname_4_4` 与 `1729012345678_1_1` 同构，统一显示实例序号 `4`/`1`）。
 */
export function containerDisplayName(name: string, deviceId?: string, indexNum?: number): string {
  if (!name) return indexNum === undefined ? name : `T${1000 + indexNum}`
  const parts = name.split('_')
  // deviceId 本身至少占 1 段 + 坑位号 1 段，名称段从第 3 段起；
  // 段数 < 3 说明名称段缺失（如容器名恰为 前缀_坑位号），不拆显示原名。
  if (parts.length < 3) return name
  // 用户名自带 `_`（≥4 段）且第三段是坑位号 → 真正的实例序号从第 4 段起，改取 slice(3)；
  // slice(3) 为空则显示原名。
  if (parts.length >= 4 && indexNum !== undefined && /^\d+$/.test(parts[2]) && Number(parts[2]) === indexNum) {
    const inst = parts.slice(3).join('_')
    return inst || name
  }
  const remark = parts.slice(2).join('_')
  if (!remark) return indexNum === undefined ? name : `T${1000 + indexNum}`
  return remark
}
