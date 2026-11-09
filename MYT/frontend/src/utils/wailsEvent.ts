import { Events } from '@wailsio/runtime'
import { recordStatusIntent } from './statusIntent'

/**
 * Wails v3 事件封装。
 *
 * 背景：Wails v3 的 Events.Emit(name, data) 只接受【单个】data 参数，
 * 多余参数会被静默丢弃；Events.On 的回调签名是 (ev: WailsEvent) => void，
 * 真实数据在 ev.data。项目中存在大量误用（Emit 传多参数、On 回调直接取多参数），
 * 本封装统一屏蔽这些细节。
 *
 * 约定：
 * - 发送方：emitEvent(name, data)，data 必须是单个值（对象/字符串等），不要传多参数。
 * - 接收方：onEvent(name, (data) => {...})，回调直接拿到 data，无需解包 ev.data。
 */

/** 发送事件（data 只能是单个值）。 */
export function emitEvent(name: string, data: unknown = null): Promise<boolean> {
  return Events.Emit(name as never, data as never)
}

/** 订阅事件，回调直接接收 data。返回取消订阅函数。 */
export function onEvent(name: string, handler: (data: unknown) => void): () => void {
  return Events.On(name as never, ((ev: { data?: unknown }) => {
    handler(ev?.data)
  }) as never)
}

/** 订阅事件（仅触发一次）。返回取消订阅函数。 */
export function onceEvent(name: string, handler: (data: unknown) => void): () => void {
  return Events.Once(name as never, ((ev: { data?: unknown }) => {
    handler(ev?.data)
  }) as never)
}

/** screenshot:slot-status 事件数据。 */
export interface SlotStatusEvent {
  key: string   // `${ip}_${indexNum}`
  status: string
}

/** 发送 screenshot:slot-status 事件。同时记录操作意图（防旧状态回灌，见 statusIntent.ts）。 */
export function emitSlotStatus(key: string, status: string): Promise<boolean> {
  recordStatusIntent(key, status)
  return emitEvent('screenshot:slot-status', { key, status })
}

/** 订阅 screenshot:slot-status 事件。返回取消订阅函数。 */
export function onSlotStatus(handler: (e: SlotStatusEvent) => void): () => void {
  return onEvent('screenshot:slot-status', (data) => {
    if (data && typeof data === 'object' && 'key' in data && 'status' in data) {
      handler(data as SlotStatusEvent)
    }
  })
}
