import i18n from '../../i18n'
import type { ChatMessage, ToolCall, HistoryRecord } from './agentTypes'

let msgIdCounter = 0
export const genId = () => `${Date.now()}-${++msgIdCounter}`

/** 历史记录 → ChatMessage 转换 */
export function recordToMessage(rec: HistoryRecord): ChatMessage | null {
  const role = rec.role === 'user' ? 'user' : rec.role === 'system' ? 'assistant' : 'assistant'
  const ts = rec.created_at ? new Date(rec.created_at).getTime() : Date.now()
  const dbId = parseInt(rec.id, 10) || undefined
  const base: ChatMessage = {
    id: genId(), role, text: rec.content || '', timestamp: ts, status: 'done', dbId,
    images: rec.image ? rec.image.split('\n').filter(Boolean) : undefined,
    platform: rec.platform, sender: rec.sender,
  }
  if (role === 'assistant' && rec.steps && rec.steps.length > 0) {
    let thinking = ''
    const toolCalls: ToolCall[] = []
    for (const s of rec.steps) {
      if (s.type === 'reasoning') {
        thinking += (s.args || s.content || '')
      } else if (s.type === 'tool') {
        toolCalls.push({
          name: s.name || i18n.t('agent:chat.toolFallback'), args: s.args, result: s.result, success: true, round: 0,
          startedAt: ts, endedAt: ts,
        })
      }
    }
    if (thinking) base.thinking = thinking
    if (toolCalls.length > 0) base.toolCalls = toolCalls
  }
  return base
}
