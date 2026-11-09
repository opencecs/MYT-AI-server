import { useState, useEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import * as MytAgentService from '../../../bindings/github.com/moyunteng/myt/mytagentservice'
import { isLoginRequiredError, useAuth } from '../../hooks/useAuth'
import type { AgentConnInfo, ChatMessage, ToolCall, ConnState, HistoryRecord, ContextInfo, ScreenCastItemData } from './agentTypes'
import { AgentWsClient } from './agentWs'
import { genId, recordToMessage } from './useMytAgentHelpers'
import { useHandleScreencast, useSyncScreencastSessions } from './useScreencastSessions'
import { useAgentEvents } from './useAgentEvents'

/** MYT Agent 对话主 hook */
export function useMytAgent() {
  const { t } = useTranslation()
  const auth = useAuth()
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [connState, setConnState] = useState<ConnState>('idle')
  const [sending, setSending] = useState(false)
  const [contextInfo, setContextInfo] = useState<ContextInfo | null>(null)
  const [hasMoreHistory, setHasMoreHistory] = useState(false)
  const [screencastItems, setScreencastItems] = useState<ScreenCastItemData[]>([])
  const [connInfo, setConnInfo] = useState<AgentConnInfo | null>(null)
  const clientRef = useRef<AgentWsClient | null>(null)
  const connInfoRef = useRef<AgentConnInfo | null>(null)
  const assistantMsgRef = useRef<ChatMessage | null>(null)
  const messagesRef = useRef<ChatMessage[]>([])
  const oldestIdRef = useRef<number>(0)
  const loadingMoreRef = useRef(false)

  // 保持 messagesRef 同步
  useEffect(() => { messagesRef.current = messages }, [messages])

  // 取新网关连接信息（token），并同步 connInfo 状态（文件链接/TTS/投屏等都在用）
  // Go 端 ensureToken 幂等：10 分钟内直接返回，过期自动 refresh/重登
  const fetchConnInfo = useCallback(async (): Promise<AgentConnInfo> => {
    const info = (await MytAgentService.GetConnectionInfo()) as AgentConnInfo
    connInfoRef.current = info
    setConnInfo(info)
    return info
  }, [])

  // 连接 MYT Agent
  const connect = useCallback(async () => {
    setConnState('connecting')
    const prev = clientRef.current
    if (prev) { prev.close(); clientRef.current = null }
    const client = new AgentWsClient(fetchConnInfo)
    client.onStatus((ok, reason) => {
      if (ok) {
        // 连接成功统一后处理（覆盖首次连接 / 自动重连 / 后台自愈），回调在 events 通道就绪后异步触发
        setConnState('connected')
        loadHistory()
        refreshContext()
        syncScreencastSessions()
      } else if (reason === 'login-required') {
        setConnState('error')
        auth.openLoginPrompt()
      } else {
        setConnState('reconnecting')
      }
    })
    client.onEvent((event) => handleEvent(event))
    // 先入 ref：首次建连失败时客户端仍后台自动重试，网络恢复后自行愈合
    clientRef.current = client
    try {
      // 连接成功后处理由 onStatus 回调统一负责
      await client.connect()
    } catch (e) {
      // login-required：弹登录弹窗（客户端已停止无限重试）；其余失败：客户端已在后台退避重试
      if (isLoginRequiredError(e)) {
        setConnState('error')
        auth.openLoginPrompt()
      }
    }
  }, [])

  // 断开
  useEffect(() => {
    return () => { clientRef.current?.close(); clientRef.current = null }
  }, [])

  // 发送消息（支持附带图片 base64 列表）
  const sendMessage = useCallback(async (text: string, images?: string[]) => {
    if ((!text.trim() && (!images || images.length === 0)) || !clientRef.current) return
    setSending(true)

    const userMsg: ChatMessage = {
      id: genId(), role: 'user',       text: text.trim() || (images && images.length > 0 ? t('agent:chat.analyzeImages') : ''),
      timestamp: Date.now(), status: 'done',
    }
    const assistantMsg: ChatMessage = {
      id: genId(), role: 'assistant', text: '',
      timestamp: Date.now(), status: 'thinking',
      startedAt: Date.now(),
    }
    assistantMsgRef.current = assistantMsg

    setMessages((prev) => [...prev, userMsg, assistantMsg])

    try {
      const params: Record<string, unknown> = { text: userMsg.text }
      if (images && images.length > 0) params.image = images.join('\n')
      clientRef.current.sendNoWait('chat.send', params)
    } catch (e) {
      updateAssistant({ status: 'error', error: String(e) })
      setSending(false)
    }
  }, [t])

  // 更新当前助手消息
  const updateAssistant = useCallback((patch: Partial<ChatMessage>) => {
    const ref = assistantMsgRef.current
    if (!ref) return
    Object.assign(ref, patch)
    setMessages((prev) => prev.map((m) => m.id === ref.id ? { ...ref } : m))
  }, [])

  // 处理事件流
  const handleEvent = useCallback((event: { params: { type: string; payload: Record<string, unknown> } }) => {
    const { type, payload } = event.params
    const ref = assistantMsgRef.current

    // 先交给新事件处理器（审批、表单、子Agent、通知等），返回 true 表示已消费
    if (agentEvents.handleEvent(type, payload, assistantMsgRef, updateAssistant, clientRef.current)) return

    if (!ref) return

    switch (type) {
      case 'thinking_delta': {
        const delta = payload.text as string || ''
        updateAssistant({ status: 'thinking', thinking: (ref.thinking || '') + delta })
        break
      }
      case 'tool_calling': {
        const round = payload.round as number || 0
        const tool: ToolCall = {
          name: payload.tool_name as string || '',
          args: payload.tool_args as string || '',
          round,
          startedAt: Date.now(),
        }
        const calls = [...(ref.toolCalls || []), tool]
        updateAssistant({ toolCalls: calls, status: 'thinking' })
        break
      }
      case 'tool_result': {
        const calls = [...(ref.toolCalls || [])]
        const last = calls[calls.length - 1]
        if (last) {
          last.result = payload.tool_result as string || ''
          last.success = payload.success as boolean
          last.endedAt = Date.now()
        }
        updateAssistant({ toolCalls: calls })
        break
      }
      case 'reply_delta': {
        const delta = payload.text as string || ''
        updateAssistant({ text: (ref.text || '') + delta, status: 'replying' })
        break
      }
      case 'reply_end': {
        updateAssistant({ status: 'done', text: payload.text as string || ref.text, endedAt: Date.now() })
        break
      }
      case 'done': {
        updateAssistant({ status: 'done', endedAt: Date.now() })
        setSending(false)
        break
      }
      case 'error': {
        updateAssistant({ status: 'error', error: payload.text as string || t('agent:chat.unknownError'), endedAt: Date.now() })
        setSending(false)
        break
      }
      case 'screencast': {
        handleScreencast(payload)
        break
      }
    }
  }, [updateAssistant, t])

  // 取消生成
  const cancel = useCallback(() => {
    clientRef.current?.sendNoWait('chat.cancel')
    setSending(false)
    updateAssistant({ status: 'done' })
  }, [updateAssistant])

  // 加载历史（首次连接后）
  const loadHistory = useCallback(async () => {
    if (!clientRef.current) return
    try {
      const res = await clientRef.current.send('session.history', { limit: 15 }) as { messages?: HistoryRecord[]; has_more?: boolean } | null
      const records = res?.messages || []
      setHasMoreHistory(res?.has_more ?? records.length >= 15)
      if (records.length > 0) {
        const firstId = parseInt(records[0].id, 10)
        if (!isNaN(firstId)) oldestIdRef.current = firstId
        const msgs: ChatMessage[] = []
        for (const rec of records) {
          const m = recordToMessage(rec)
          if (m) msgs.push(m)
        }
        setMessages(msgs)
      }
    } catch (e) { console.error('[loadHistory] failed:', e) }
  }, [])

  // 加载更多历史（向上翻页）
  const loadMoreHistory = useCallback(async () => {
    if (loadingMoreRef.current || !hasMoreHistory || !clientRef.current) return
    loadingMoreRef.current = true
    try {
      const beforeId = oldestIdRef.current
      if (!beforeId) return
      const res = await clientRef.current.send('session.history', { limit: 15, before_id: beforeId }) as { messages?: HistoryRecord[]; has_more?: boolean } | null
      const records = res?.messages || []
      setHasMoreHistory(res?.has_more ?? records.length >= 15)
      if (records.length === 0) return
      const firstId = parseInt(records[0].id, 10)
      if (!isNaN(firstId)) oldestIdRef.current = firstId
      const newMsgs: ChatMessage[] = []
      for (const rec of records) {
        const m = recordToMessage(rec)
        if (m) newMsgs.push(m)
      }
      setMessages((prev) => [...newMsgs, ...prev])
    } catch (e) { console.error('[loadMoreHistory] failed:', e) }
    finally { loadingMoreRef.current = false }
  }, [hasMoreHistory])

  // 刷新上下文信息
  const refreshContext = useCallback(async () => {
    if (!clientRef.current) return
    try {
      const res = await clientRef.current.send('session.status') as ContextInfo | null
      if (res) setContextInfo(res)
    } catch { /* ignore */ }
  }, [])

  // 重置会话上下文（保留聊天记录）
  const resetSession = useCallback(() => {
    clientRef.current?.sendNoWait('session.reset')
    refreshContext()
  }, [refreshContext])

  // 撤回指定用户消息及其后的所有消息（默认撤回最后一条用户消息）
  const recall = useCallback(async (msgId?: string) => {
    if (sending) return  // 生成中不能撤回
    const prev = messagesRef.current
    let targetIdx = -1
    if (msgId) {
      for (let i = 0; i < prev.length; i++) {
        if (prev[i].id === msgId && prev[i].role === 'user') { targetIdx = i; break }
      }
    } else {
      for (let i = prev.length - 1; i >= 0; i--) {
        if (prev[i].role === 'user') { targetIdx = i; break }
      }
    }
    if (targetIdx < 0) return

    // 统计从 targetIdx 到末尾的用户消息数（chat.recall 一次只撤一条）
    let recallCount = 0
    for (let i = targetIdx; i < prev.length; i++) {
      if (prev[i].role === 'user') recallCount++
    }

    // 先发 RPC，成功后再更新 UI
    try {
      for (let i = 0; i < recallCount; i++) {
        await clientRef.current?.send('chat.recall')
      }
    } catch (e) {
      console.warn('[recall] failed:', e)
      return
    }

    // 删除 DB 历史记录（chat_messages 表），否则网页端仍能看到
    const recalled = prev[targetIdx]
    if (recalled.dbId) {
      try { await clientRef.current?.send('chat.deleteHistory', { from_id: recalled.dbId }) }
      catch (e) { console.warn('[recall] delete DB record failed:', e) }
    }

    setMessages(prev.slice(0, targetIdx))
    // 通知输入框退回撤回的文本
    window.dispatchEvent(new CustomEvent('message-recalled', { detail: prev[targetIdx]?.text || '' }))
    refreshContext()
  }, [sending, refreshContext])

  // 清空聊天记录（本地 + 后端）
  const clearHistory = useCallback(async () => {
    setMessages([])
    setHasMoreHistory(false)
    oldestIdRef.current = 0
    const info = connInfoRef.current
    if (info) {
      try { await MytAgentService.HttpProxy('DELETE', '/api/chat/history', '') } catch { /* ignore */ }
    }
    refreshContext()
  }, [refreshContext])

  // 搜索聊天记录（REST GET /api/chat/history）
  const searchHistory = useCallback(async (query: string, page: number, dateFrom?: string, dateTo?: string) => {
    const info = connInfoRef.current
    if (!info) return { messages: [] as HistoryRecord[], total: 0 }
    const params = new URLSearchParams()
    if (query) params.set('q', query)
    params.set('page', String(page))
    params.set('limit', '20')
    if (dateFrom) params.set('from', dateFrom)
    if (dateTo) params.set('to', dateTo)
    try {
      const res = await MytAgentService.HttpProxy('GET', `/api/chat/history?${params}`, '')
      const data = JSON.parse(res)
      return { messages: (data.messages || data.list || []) as HistoryRecord[], total: data.total || 0 }
    } catch { return { messages: [], total: 0 } }
  }, [])

  // 处理投屏事件（已拆分到 useScreencastSessions.ts）
  const handleScreencast = useHandleScreencast(setScreencastItems, connInfoRef)
  const syncScreencastSessions = useSyncScreencastSessions(setScreencastItems)

  // 新事件处理（审批、表单、子Agent、通知等）
  const clearMessagesLocal = useCallback(() => {
    setMessages([])
    setHasMoreHistory(false)
    oldestIdRef.current = 0
  }, [])
  const agentEvents = useAgentEvents(setMessages, setSending, clearMessagesLocal, loadHistory)

  // 手动重连：客户端重连前会自己取新 token（修复拿 URL 里过期 JWT 无限重连的问题）
  // 重连成功后处理（历史/上下文/投屏同步）由 onStatus 回调统一负责
  const reconnect = useCallback(async () => {
    const client = clientRef.current
    if (client) {
      try {
        await client.reconnect()
      } catch (e) {
        // 客户端已内部分级通知（login-required → 弹窗 / 网络失败 → 继续退避），这里兜底
        if (isLoginRequiredError(e)) {
          setConnState('error')
          auth.openLoginPrompt()
        }
      }
    } else {
      await connect()
    }
  }, [])

  return { messages, connState, sending, contextInfo, hasMoreHistory, connInfo, screencastItems, setScreencastItems, connect, reconnect, sendMessage, cancel, resetSession, recall, loadMoreHistory, refreshContext, clearHistory, searchHistory, client: clientRef.current,
    approvals: agentEvents.approvals, formRequest: agentEvents.formRequest, subAgents: agentEvents.subAgents, notifications: agentEvents.notifications, loadedSkills: agentEvents.loadedSkills,
    respondApproval: agentEvents.respondApproval, respondForm: agentEvents.respondForm, dismissNotification: agentEvents.dismissNotification,
  }
}
