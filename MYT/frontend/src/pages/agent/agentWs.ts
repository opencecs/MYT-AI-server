import type { AgentConnInfo, JsonRpcRequest, JsonRpcResponse, AgentEvent } from './agentTypes'
import { isLoginRequiredError } from '../../hooks/useAuth'

type EventHandler = (event: AgentEvent) => void
/** connected=false 时 reason 可能为 'login-required'（需要登录，已停止自动重试） */
type StatusHandler = (connected: boolean, reason?: string) => void

const MAX_RETRY_DELAY = 30000       // 指数退避上限
const IDLE_TIMEOUT = 5 * 60 * 1000  // 双通道 5 分钟无消息判定假死
const HEARTBEAT_INTERVAL = 60000    // 假死检查周期

/**
 * MYT Agent WebSocket 客户端：双连接 (/ws 发请求 + /events 收事件)。
 *
 * 关键设计：
 * - 网关 session JWT 拼在 WS URL 中且 15 分钟过期。每次建连/重连都先调
 *   getConnInfo() 取新 token（Go 端自动 refresh/重登），杜绝拿过期 token 重连；
 * - connectSeq 代际标记：每次 connect 抢占新代际并拆除旧 socket，旧 socket 的
 *   open/close 回调按代际丢弃，消除手动重连后野定时器、重复连接、事件串台竞态；
 * - 需要登录错误停止无限重试并通知 UI 弹登录框，其余失败继续指数退避。
 */
export class AgentWsClient {
  private getConnInfo: () => Promise<AgentConnInfo>
  private ws: WebSocket | null = null
  private events: WebSocket | null = null
  private reqId = 0
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void; timer: number }>()
  private eventHandlers = new Set<EventHandler>()
  private statusHandlers = new Set<StatusHandler>()
  private reconnectTimer: number | null = null
  private heartbeatTimer: number | null = null
  private lastMsgAt = 0
  private retryCount = 0
  private closed = false
  private loginRequired = false
  private connectSeq = 0
  private wsSettle: ((e: Error) => void) | null = null // teardown 时结算进行中的 connectWs

  constructor(getConnInfo: () => Promise<AgentConnInfo>) {
    this.getConnInfo = getConnInfo
  }

  onEvent(handler: EventHandler): () => void {
    this.eventHandlers.add(handler)
    return () => { this.eventHandlers.delete(handler) }
  }

  onStatus(handler: StatusHandler): () => void {
    this.statusHandlers.add(handler)
    return () => { this.statusHandlers.delete(handler) }
  }

  /** 建立连接：抢占新代际、取新 token、拆除进行中的旧连接 */
  async connect(): Promise<void> {
    if (this.closed || this.loginRequired) return
    const seq = ++this.connectSeq
    this.teardownSockets()
    const info = await this.acquireConnInfo()
    if (this.closed || seq !== this.connectSeq) return
    try {
      await this.connectWs(info, seq)
    } catch (e) {
      if (seq !== this.connectSeq) return // 被更新的 connect 取代：静默让位
      throw e
    }
    this.connectEvents(info, seq)
  }

  /** 手动重连：清除停试状态，立即取新 token 重连 */
  async reconnect(): Promise<void> {
    this.loginRequired = false
    this.cancelReconnectTimer()
    this.retryCount = 0
    await this.connect()
  }

  /** 关闭客户端：停止一切重连与后台定时器 */
  close(): void {
    this.closed = true
    this.cancelReconnectTimer()
    this.stopHeartbeat()
    this.connectSeq++
    this.teardownSockets()
    this.pending.forEach((p) => { clearTimeout(p.timer); p.reject(new Error('Connection closed')) })
    this.pending.clear()
  }

  /** 发送 JSON-RPC 请求，等待响应 */
  send(method: string, params?: Record<string, unknown>): Promise<unknown> {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error('WebSocket not connected'))
    }
    const id = ++this.reqId
    const req: JsonRpcRequest = { jsonrpc: '2.0', id, method, params }
    return new Promise((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pending.delete(id)
        reject(new Error('Request timeout'))
      }, 30000)
      this.pending.set(id, { resolve, reject, timer })
      this.ws!.send(JSON.stringify(req))
    })
  }

  /** 发送请求，不等待响应（用于流式消息） */
  sendNoWait(method: string, params?: Record<string, unknown>): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    const id = ++this.reqId
    const req: JsonRpcRequest = { jsonrpc: '2.0', id, method, params }
    this.ws.send(JSON.stringify(req))
  }

  // ---------- 内部实现 ----------

  /** 取新网关 token。需要登录：记住并停止无限重试；其余失败：继续退避重试。均抛出供调用方处理 */
  private async acquireConnInfo(): Promise<AgentConnInfo> {
    try {
      return await this.getConnInfo()
    } catch (e) {
      if (isLoginRequiredError(e)) {
        this.loginRequired = true
        this.cancelReconnectTimer()
        this.notifyStatus(false, 'login-required')
      } else {
        this.notifyStatus(false)
        this.scheduleReconnect()
      }
      throw e
    }
  }

  /** 拆除现有 socket：置空回调（旧 onclose 不再触发重连）、结算进行中的 connectWs、拒绝挂起 RPC */
  private teardownSockets(): void {
    const ws = this.ws
    const ev = this.events
    this.ws = null
    this.events = null
    this.stopHeartbeat()
    const settle = this.wsSettle
    this.wsSettle = null
    if (ws) {
      ws.onopen = ws.onmessage = ws.onerror = ws.onclose = null
      try { ws.close() } catch { /* ignore */ }
    }
    if (ev) {
      ev.onopen = ev.onmessage = ev.onerror = ev.onclose = null
      try { ev.close() } catch { /* ignore */ }
    }
    if (settle) settle(new Error('superseded'))
    if (this.pending.size > 0) {
      this.pending.forEach((p) => { clearTimeout(p.timer); p.reject(new Error('Reconnecting')) })
      this.pending.clear()
    }
  }

  private connectWs(info: AgentConnInfo, seq: number): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const ws = new WebSocket(info.wsUrl)
      this.ws = ws
      this.wsSettle = reject
      ws.onopen = () => {
        if (seq !== this.connectSeq) return
        this.wsSettle = null
        this.lastMsgAt = Date.now()
        this.retryCount = 0
        resolve()
      }
      ws.onmessage = (e) => {
        if (seq !== this.connectSeq) return
        this.lastMsgAt = Date.now()
        this.handleWsMessage(e.data)
      }
      ws.onerror = () => {
        if (seq !== this.connectSeq) return
        this.wsSettle = null
        reject(new Error('WebSocket connection failed'))
      }
      ws.onclose = () => {
        if (seq !== this.connectSeq) return
        this.wsSettle = null
        if (!this.closed && !this.loginRequired) this.scheduleReconnect()
      }
    })
  }

  private connectEvents(info: AgentConnInfo, seq: number): void {
    const ev = new WebSocket(info.eventsUrl)
    this.events = ev
    ev.onopen = () => {
      if (seq !== this.connectSeq) return
      this.lastMsgAt = Date.now()
      this.retryCount = 0
      this.notifyStatus(true)
      this.startHeartbeat()
    }
    ev.onmessage = (e) => {
      if (seq !== this.connectSeq) return
      this.lastMsgAt = Date.now()
      this.handleEventMessage(e.data)
    }
    ev.onerror = () => {
      if (seq !== this.connectSeq) return
      this.notifyStatus(false)
    }
    ev.onclose = () => {
      if (seq !== this.connectSeq) return
      this.notifyStatus(false)
      this.stopHeartbeat()
      if (!this.closed && !this.loginRequired) this.scheduleReconnect()
    }
  }

  private handleWsMessage(data: string): void {
    try {
      const msg = JSON.parse(data) as JsonRpcResponse
      if (msg.id != null && this.pending.has(msg.id)) {
        const p = this.pending.get(msg.id)!
        clearTimeout(p.timer)
        this.pending.delete(msg.id)
        if (msg.error) {
          p.reject(new Error(msg.error.message))
        } else {
          p.resolve(msg.result)
        }
      }
    } catch { /* ignore parse errors */ }
  }

  private handleEventMessage(data: string): void {
    try {
      const msg = JSON.parse(data) as AgentEvent
      if (msg.method === 'event') {
        this.eventHandlers.forEach((h) => h(msg))
      }
    } catch { /* ignore */ }
  }

  /** 心跳免死：双通道 5 分钟无消息判定连接假死，拆除后走重连（重连会取新 token） */
  private startHeartbeat(): void {
    this.stopHeartbeat()
    this.heartbeatTimer = window.setInterval(() => {
      if (this.closed || this.loginRequired) { this.stopHeartbeat(); return }
      if (Date.now() - this.lastMsgAt > IDLE_TIMEOUT) {
        console.warn('[agent-ws] 5min no message, forcing reconnect')
        this.teardownSockets()
        this.scheduleReconnect()
      }
    }, HEARTBEAT_INTERVAL)
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) { clearInterval(this.heartbeatTimer); this.heartbeatTimer = null }
  }

  /** 指数退避：1s -> 2s -> 4s -> 8s -> 16s，上限 30s */
  private scheduleReconnect(): void {
    if (this.reconnectTimer || this.closed || this.loginRequired) return
    const delay = Math.min(1000 * 2 ** this.retryCount, MAX_RETRY_DELAY)
    this.retryCount++
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.connect().catch((e) => this.handleConnectError(e))
    }, delay)
  }

  private cancelReconnectTimer(): void {
    if (this.reconnectTimer) { clearTimeout(this.reconnectTimer); this.reconnectTimer = null }
  }

  /** 连接失败统一处理：需要登录 → 停止重试 + 通知 UI；其余 → 继续退避重试 */
  private handleConnectError(e: unknown): void {
    if (this.closed) return
    if (isLoginRequiredError(e)) {
      this.loginRequired = true
      this.cancelReconnectTimer()
      this.notifyStatus(false, 'login-required')
      return
    }
    this.notifyStatus(false)
    this.scheduleReconnect()
  }

  private notifyStatus(connected: boolean, reason?: string): void {
    this.statusHandlers.forEach((h) => h(connected, reason))
  }
}
