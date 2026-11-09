import * as MytAgentService from '../../../bindings/github.com/moyunteng/myt/mytagentservice'
import type { AgentConnInfo, ScreenCastItemData } from './agentTypes'

export const PAGE_SIZE = 9
export const RENEW_INTERVAL = 3 * 60 * 1000

export function buildPlayerSrc(item: ScreenCastItemData, info: AgentConnInfo, muted: boolean): string {
  const containerParam = item.container ? `&container=${encodeURIComponent(item.container)}` : item.index ? `&index=${item.index}` : ''
  const codec = item.player_url?.match(/[?&]v=([^&]+)/)?.[1] || 'h264'
  // iframe 不受 CORS 限制，直接用公网 URL
  return `${info.baseUrl}/api/screencast/player?device_id=${encodeURIComponent(item.device_id)}${containerParam}&quality=1&v=${codec}&token=${encodeURIComponent(info.sessionToken)}&muted=${muted ? 1 : 0}`
}

export function handleStop(item: ScreenCastItemData, _info: AgentConnInfo, onRemove?: (id: string) => void) {
  const containerParam = item.container ? `&container=${encodeURIComponent(item.container)}` : item.index ? `&index=${item.index}` : ''
  MytAgentService.HttpProxy('GET', `/api/screencast/stop?device_id=${encodeURIComponent(item.device_id)}${containerParam}`, '')
    .then(() => onRemove?.(item.id)).catch(() => {})
}

export function handleStopAll(_info: AgentConnInfo, onClear?: () => void) {
  MytAgentService.HttpProxy('GET', '/api/screencast/stop', '')
    .then(() => onClear?.()).catch(() => {})
}
