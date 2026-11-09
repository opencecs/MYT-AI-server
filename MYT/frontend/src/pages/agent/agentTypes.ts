/** MYT Agent 对话相关类型定义 */

/** Go 后端返回的连接信息 */
export interface AgentConnInfo {
  baseUrl: string
  wsUrl: string
  eventsUrl: string
  sessionToken: string
  uid: string
}

/** JSON-RPC 2.0 请求 */
export interface JsonRpcRequest {
  jsonrpc: '2.0'
  id: number
  method: string
  params?: Record<string, unknown>
}

/** JSON-RPC 2.0 响应 */
export interface JsonRpcResponse {
  jsonrpc: '2.0'
  id: number
  result?: unknown
  error?: { code: number; message: string }
}

/** /events 推送的事件 */
export interface AgentEvent {
  jsonrpc: '2.0'
  method: 'event'
  params: {
    type: string
    session_id: string
    payload: Record<string, unknown>
  }
}

/** 工具调用 */
export interface ToolCall {
  name: string
  args?: string
  result?: string
  success?: boolean
  round: number
  startedAt?: number
  endedAt?: number
}

/** 思考段 */
export interface ThinkingSeg {
  id: string
  text: string
  round: number
}

/** 聊天消息 */
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  text: string
  timestamp: number
  thinking?: string
  toolCalls?: ToolCall[]
  status: 'sending' | 'thinking' | 'replying' | 'done' | 'error'
  error?: string
  /** Agent 轮次起止（用于步骤计时） */
  startedAt?: number
  endedAt?: number
  /** 数据库记录 ID（历史加载时从 rec.id 解析，撤回时用于删除 DB 记录） */
  dbId?: number
  /** 附带图片（base64 列表） */
  images?: string[]
  /** 消息来源平台 */
  platform?: string
  /** 发送者名称 */
  sender?: string
}

/** 连接状态 */
export type ConnState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'error'

/** 投屏项 */
export interface ScreenCastItemData {
  id: string
  name: string
  device_id: string
  device_name?: string
  container: string
  index: number
  player_url: string
  signal_port: number
  media_port: number
  status: string
}

/** 历史聊天记录（来自 session.history） */
export interface HistoryRecord {
  id: string
  role: string       // user | agent | assistant | system
  content: string
  image?: string
  steps?: HistoryStep[]
  platform?: string
  sender?: string
  created_at?: string
}

export interface HistoryStep {
  type: 'reasoning' | 'tool' | string
  name?: string
  args?: string
  content?: string
  result?: string
}

/** 上下文信息（来自 session.status） */
export interface ContextInfo {
  status: string
  active_sessions: number
  max_sessions: number
  tokens: number
  msg_count: number
  cycle_num: number
}

/** 工具审批请求（approval_request 事件） */
export interface ApprovalRequest {
  id: string
  toolName: string
  toolArgs: string
  round: number
}

/** 表单请求（form_request 事件，clarify 工具触发） */
export interface FormRequest {
  id: string
  title: string
  fields: FormField[]
}

export interface FormField {
  name: string
  label: string
  type: string
  required?: boolean
  default?: string
  options?: string[]
  placeholder?: string
}

/** 子 Agent 信息 */
export interface SubAgent {
  id: string
  goal: string
  deviceId: string
  status: 'running' | 'done' | 'failed'
  steps: SubAgentStep[]
  summary?: string
  startedAt: number
  endedAt?: number
}

export interface SubAgentStep {
  id: string
  kind: 'thinking' | 'reply' | 'tool'
  toolName?: string
  toolArgs?: string
  reasoning?: string
  reply?: string
  toolResult?: string
  success?: boolean
  done: boolean
  startedAt: number
  endedAt?: number
}

/** 通知项 */
export interface NotificationItem {
  id: string
  desc: string
  text: string
}
