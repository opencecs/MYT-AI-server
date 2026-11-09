import { useState, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import type { ApprovalRequest, FormRequest, SubAgent, SubAgentStep, NotificationItem, ToolCall } from './agentTypes'

let notifId = 0

/** 管理 Agent 事件中需要 UI 交互/状态展示的部分（审批、表单、子Agent、通知等） */
export function useAgentEvents(
  setMessages: React.Dispatch<React.SetStateAction<any[]>>,
  setSending: React.Dispatch<React.SetStateAction<boolean>>,
  clearMessages: () => void,
  loadHistory: () => void,
) {
  const { t } = useTranslation()
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([])
  const [formRequest, setFormRequest] = useState<FormRequest | null>(null)
  const [subAgents, setSubAgents] = useState<SubAgent[]>([])
  const [notifications, setNotifications] = useState<NotificationItem[]>([])
  const [loadedSkills, setLoadedSkills] = useState<string[]>([])
  const subAgentsRef = useRef<SubAgent[]>([])

  const respondApproval = useCallback((id: string, ws: { send: (data: string) => void } | null, approved: boolean) => {
    if (!ws) return
    ws.send(JSON.stringify({ jsonrpc: '2.0', method: 'approval.respond', params: { approval_id: id, approved } }))
    setApprovals(prev => prev.filter(a => a.id !== id))
  }, [])

  const respondForm = useCallback((id: string, ws: { send: (data: string) => void } | null, formData: Record<string, string>, cancelled: boolean) => {
    if (!ws) return
    ws.send(JSON.stringify({ jsonrpc: '2.0', method: 'form.respond', params: { form_id: id, form_data: formData, cancelled } }))
    setFormRequest(null)
  }, [])

  const dismissNotification = useCallback((id: string) => {
    setNotifications(prev => prev.filter(n => n.id !== id))
  }, [])

  const updateSubAgent = useCallback((id: string, fn: (sa: SubAgent) => SubAgent) => {
    setSubAgents(prev => {
      const next = prev.map(sa => sa.id === id ? fn(sa) : sa)
      subAgentsRef.current = next
      return next
    })
  }, [])

  /** 处理事件，返回 true 表示已消费（不需要走默认的 assistant 消息更新逻辑） */
  const handleEvent = useCallback((type: string, payload: Record<string, unknown>, assistantRef: React.MutableRefObject<any>, updateAssistant: (patch: any) => void, ws: { send: (data: string) => void } | null): boolean => {
    switch (type) {
      // ── 工具审批 ──
      case 'approval_request': {
        const id = payload.approval_id as string || payload.approval_id as any
        if (id) {
          setApprovals(prev => [...prev, {
            id,
            toolName: (payload.approval_tool as string) || (payload.tool_name as string) || '',
            toolArgs: (payload.tool_args as string) || '',
            round: (payload.round as number) || 0,
          }])
        }
        return true
      }

      // ── 表单请求（clarify 工具） ──
      case 'form_request': {
        const id = payload.form_id as string
        const fields = payload.form_schema as any[]
        if (id && fields) {
          setFormRequest({
            id,
            title: (payload.form_title as string) || t('agent:form.title'),
            fields: Array.isArray(fields) ? fields : [],
          })
        }
        return true
      }

      // ── 工具执行进度 ──
      case 'tool_progress': {
        const ref = assistantRef.current
        if (!ref) return true
        const calls = [...(ref.toolCalls || [])] as ToolCall[]
        let progress: any = payload.progress
        if (typeof progress === 'string') { try { progress = JSON.parse(progress) } catch {} }
        const round = payload.round as number
        for (let i = calls.length - 1; i >= 0; i--) {
          if (!calls[i].endedAt && calls[i].round === round) {
            (calls[i] as any).progress = progress
            break
          }
        }
        updateAssistant({ toolCalls: calls })
        return true
      }

      // ── 完整思考块（非 delta） ──
      case 'thinking': {
        const ref = assistantRef.current
        if (!ref) return true
        updateAssistant({ status: 'thinking', thinking: (ref.thinking || '') + (payload.text as string || '') })
        return true
      }

      // ── 子 Agent 事件 ──
      case 'subagent_start': {
        const sa: SubAgent = {
          id: (payload.subagent_id as string) || 'sa_' + Date.now(),
          goal: (payload.text as string) || '',
          deviceId: (payload.tool_name as string) || '',
          status: 'running',
          steps: [],
          startedAt: Date.now(),
        }
        setSubAgents(prev => { const next = [...prev.filter(s => s.status === 'running'), sa]; subAgentsRef.current = next; return next })
        return true
      }
      case 'subagent_tool_call': {
        const subId = (payload.subagent_id as string) || ''
        const toolName = (payload.tool_name as string) || (payload.text as string) || ''
        const isThinking = toolName === 'thinking'
        const isReply = toolName === 'reply'
        const isTDelta = (payload.text as string) === 'thinking_delta'
        const isRDelta = (payload.text as string) === 'reply_delta'
        setSubAgents(prev => {
          const next = prev.map(sa => {
            if (subId && sa.id !== subId) return sa
            if (!subId && sa.status !== 'running') return sa
            const steps = [...sa.steps]
            if (isTDelta) {
              const last = steps[steps.length - 1]
              if (last && last.kind === 'thinking' && !last.done) last.reasoning += (payload.subagent_reasoning as string) || ''
              else steps.push({ id: 'ss_' + Date.now(), kind: 'thinking', reasoning: (payload.subagent_reasoning as string) || '', done: false, startedAt: Date.now() })
            } else if (isRDelta) {
              const last = steps[steps.length - 1]
              if (last && last.kind === 'reply' && !last.done) last.reply += (payload.subagent_reply as string) || ''
              else steps.push({ id: 'ss_' + Date.now(), kind: 'reply', reply: (payload.subagent_reply as string) || '', done: false, startedAt: Date.now() })
            } else if (isThinking) {
              steps.push({ id: 'ss_' + Date.now(), kind: 'thinking', reasoning: (payload.subagent_reasoning as string) || '', done: true, startedAt: Date.now() })
            } else if (isReply) {
              steps.push({ id: 'ss_' + Date.now(), kind: 'reply', reply: (payload.subagent_reply as string) || '', done: true, startedAt: Date.now() })
            } else {
              steps.push({ id: 'ss_' + Date.now(), kind: 'tool', toolName, toolArgs: (payload.tool_args as string) || '', done: false, startedAt: Date.now() })
            }
            return { ...sa, steps }
          })
          subAgentsRef.current = next
          return next
        })
        return true
      }
      case 'subagent_tool_result': {
        const subId = (payload.subagent_id as string) || ''
        const toolName = (payload.tool_name as string) || ''
        const success = payload.success !== false
        setSubAgents(prev => {
          const next = prev.map(sa => {
            if (subId && sa.id !== subId) return sa
            if (!subId && sa.status !== 'running') return sa
            const steps = [...sa.steps]
            for (let i = steps.length - 1; i >= 0; i--) {
              if (steps[i].kind === 'tool' && !steps[i].done && steps[i].toolName === toolName) {
                steps[i] = { ...steps[i], done: true, success, toolResult: (payload.tool_result as string) || '', endedAt: Date.now() }
                break
              }
            }
            return { ...sa, steps }
          })
          subAgentsRef.current = next
          return next
        })
        return true
      }
      case 'subagent_done': {
        const subId = (payload.subagent_id as string) || ''
        const success = payload.success !== false
        const summary = (payload.text as string) || ''
        setSubAgents(prev => {
          const next = prev.map(sa => {
            if (subId && sa.id !== subId) return sa
            if (!subId && sa.status !== 'running') return sa
            return { ...sa, status: success ? 'done' : 'failed', summary, endedAt: Date.now() }
          })
          subAgentsRef.current = next
          return next
        })
        return true
      }

      // ── 技能加载 ──
      case 'skills_loaded': {
        setLoadedSkills((payload.skill_names as string[]) || [])
        return true
      }

      // ── 通知 ──
      case 'notification': {
        const text = (payload.notification as string) || (payload.text as string) || ''
        const desc = (payload.task_desc as string) || ''
        if (text) {
          const id = 'n' + (++notifId)
          setNotifications(prev => [...prev, { id, desc, text }])
          setTimeout(() => setNotifications(prev => prev.filter(n => n.id !== id)), 6000)
        }
        return true
      }

      // ── 会话重置（其他设备触发） ──
      case 'session_reset': {
        clearMessages()
        setSending(false)
        return true
      }

      // ── 历史清除 ──
      case 'history_cleared': {
        clearMessages()
        return true
      }

      default:
        return false
    }
  }, [t, updateSubAgent])

  return {
    approvals, formRequest, subAgents, notifications, loadedSkills,
    respondApproval, respondForm, dismissNotification,
    handleEvent,
  }
}
