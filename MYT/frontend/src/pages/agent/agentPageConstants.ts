import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import { MessageSquare, Sparkles, Clock, Radio, Activity } from 'lucide-react'
import { agentC as c } from './agentTheme'
import type { ConnState } from './agentTypes'

gsap.registerPlugin(useGSAP)

export type View = 'chat' | 'skills' | 'tasks' | 'platforms' | 'monitor'

export const VIEWS: { id: View; labelKey: string; icon: typeof MessageSquare }[] = [
  { id: 'chat', labelKey: 'agent:view.chat', icon: MessageSquare },
  { id: 'skills', labelKey: 'agent:view.skills', icon: Sparkles },
  { id: 'tasks', labelKey: 'agent:view.tasks', icon: Clock },
  { id: 'platforms', labelKey: 'agent:view.platforms', icon: Radio },
  { id: 'monitor', labelKey: 'agent:view.monitor', icon: Activity },
]

export const stateMeta: Record<ConnState, { labelKey: string; color: string }> = {
  idle: { labelKey: 'agent:conn.idle', color: c.textMuted },
  connecting: { labelKey: 'agent:conn.connecting', color: c.warn },
  connected: { labelKey: 'agent:conn.connected', color: c.success },
  reconnecting: { labelKey: 'agent:conn.reconnecting', color: c.warn },
  error: { labelKey: 'agent:conn.error', color: c.danger },
}
