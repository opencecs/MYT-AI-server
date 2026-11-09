/**
 * 容器状态操作意图保护。
 *
 * 背景：点击开机/关机/重启等操作后，后端会立即回查设备容器列表，但此刻设备端
 * 容器状态迁移尚未完成（StopAndroid 返回 200 ≠ 容器已停止），立即查询拿到的仍是
 * 迁移前的旧状态。若前端合并时无条件采用，UI 会"翻回"旧状态——表现为点了开关机
 * 没反应、要手动刷新。
 *
 * 机制：emitSlotStatus 时记录操作意图（starting → 期望 running；stopped → 期望
 * stopped；resetting/removing → 期望同态过渡，有效期 30s）。各页合并
 * androidlist:updated 数据时经 resolveStatus
 * 过滤：意图未过期且后端尚未到达期望终态时，继续显示乐观状态；到达终态或过期后
 * 回归后端数据。后端的 dirty 追踪（5s 补查）保证最终一致。
 */

const INTENT_EXPIRE_MS = 30_000

/** 意图期望的终态：running（开机/重启链路）、stopped（关机/删除）、resetting / removing（过渡态） */
type IntentExpect = 'running' | 'stopped' | 'resetting' | 'removing'

interface StatusIntent {
  expect: IntentExpect
  until: number
}

const intents = new Map<string, StatusIntent>()

/** 乐观状态 → 期望终态（starting/restarting/resetting → running；stopped/removed → stopped；removing → removing） */
function expectOf(status: string): IntentExpect {
  switch (status) {
    case 'stopped': return 'stopped'
    case 'removed': return 'stopped'
    case 'removing': return 'removing'
    case 'resetting': return 'resetting'
    default: return 'running'
  }
}

/** emitSlotStatus 内部调用：记录一次乐观状态的操作意图 */
export function recordStatusIntent(key: string, status: string): void {
  intents.set(key, { expect: expectOf(status), until: Date.now() + INTENT_EXPIRE_MS })
}

/** 匹配某个容器的意图（emit 时的 key 可能基于 ip 也可能是 deviceId） */
function matchIntent(ip: string, deviceId: string | undefined, indexNum: number): { intent: StatusIntent; key: string } | undefined {
  const now = Date.now()
  const candidates = deviceId ? [`${ip}_${indexNum}`, `${deviceId}_${indexNum}`] : [`${ip}_${indexNum}`]
  for (const key of candidates) {
    const intent = intents.get(key)
    if (!intent) continue
    if (intent.until <= now) {
      intents.delete(key)
      continue
    }
    return { intent, key }
  }
  return undefined
}

/**
 * 清除操作意图（手动刷新后调用）：手动刷新是优先级最高且强制的语义——
 * 刷新返回后列表已反映设备真实状态，残留的乐观意图会继续把真实状态
 * 遮蔽成 starting/stopped（表现为「点了刷新仍看到旧状态」）。
 * 不传 ids 清除全部未过期意图（刷新是全局语义）。
 */
export function clearStatusIntents(ids?: string[]): void {
  if (!ids) {
    intents.clear()
    return
  }
  for (const id of ids) {
    intents.delete(id)
  }
}

/**
 * 合并后端容器列表时逐条解析应显示的状态：
 * - 有未过期意图：后端已到达期望终态 → 采纳后端并清除意图；否则维持乐观状态
 * - 无意图：原样返回后端状态
 *
 * 例外：created（从未运行过的新容器）不被意图覆盖为 starting——创建云机场景
 * （emitSlotStatus starting + 新容器 created）会让新容器显示「启动中」spinner，
 * 但它根本没被要求开机（同坑位已有运行实例时新建默认不开机），显示「已创建」
 * 才是真实语义。手动开机的场景容器会很快经 WS start 事件变为 running。
 *
 * resetting/removing 意图：期望值到达（容器真实进入 resetting/removing）即采纳后端
 * 并清意图——过渡态的乐观值与真实值语义一致（重置中/删除中），到达即收敛。
 */
export function resolveStatus<T extends { ip: string; deviceId?: string; indexNum: number; status: string }>(a: T): string {
  const real = (a.status || '').toLowerCase()
  if (real === 'created') return a.status
  const hit = matchIntent(a.ip, a.deviceId, a.indexNum)
  if (!hit) return a.status
  if (real === hit.intent.expect) {
    intents.delete(hit.key)
    return a.status
  }
  switch (hit.intent.expect) {
    case 'stopped': return 'stopped'
    case 'removing': return 'removing'
    case 'resetting': return 'resetting'
    default: return 'starting'
  }
}
