import { useCallback, useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as V2Service from '../../../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import * as V4Service from '../../../../bindings/github.com/moyunteng/myt/v4servicewrapper'

// 终端传输层：同一套终端 UI（TerminalModal）下的两种数据源。
//   - local / public：设备 SDK 的 WebSocket exec 网关 ws://{ip}:8000/link/exec
//     （login / stdin / resize / heartbeat，二进制帧即输出）
//   - adb（v2 / v4）：ADB 端口的原生 ADB 会话（v2 容器 5555 → 主机 5000+index；
//     v4 实例 adbd 监听 REST 下发的 adb_port），经 Wails 绑定 +
//     adbterm:data（base64）/ adbterm:exit 事件
// 两种传输对外只暴露 status / error / write / resize，UI 侧零差异。

export type TerminalStatus = 'connecting' | 'connected' | 'disconnected' | 'error'

export interface TerminalSession {
  status: TerminalStatus
  error: string
  write: (data: string) => void
  resize: (cols: number, rows: number) => void
}

interface Options {
  isV2: boolean
  /** v4 部署模式：终端走实例 ADB 直连（deviceIP 此时是宿主机 IP） */
  isV4?: boolean
  deviceID: string
  containerName: string
  /** 取当前终端尺寸（WS 登录后首次同步 / v2 会话建立后首次同步用） */
  getSize: () => { cols: number; rows: number } | null
  onOutput: (data: Uint8Array) => void
  /** 连接断开/出错后由本 hook 延迟回调，宿主据此关闭弹窗 */
  onClosed: () => void
}

interface TermDataEvent {
  sessionId: string
  stream: number
  data: string // base64
}

interface TermExitEvent {
  sessionId: string
  code: number
}

/** base64ToBytes base64 → Uint8Array（xterm 需原始字节，JSON 事件不能安全承载二进制） */
function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

export function useTerminalSession(opts: Options): TerminalSession {
  const { isV2, isV4, deviceID, containerName } = opts
  const isAdb = isV2 || !!isV4
  const [status, setStatus] = useState<TerminalStatus>('connecting')
  const [error, setError] = useState('')

  // 回调与取值经 ref 转发，避免宿主重渲染导致重连
  const outputRef = useRef(opts.onOutput)
  const closedRef = useRef(opts.onClosed)
  const sizeRef = useRef(opts.getSize)
  outputRef.current = opts.onOutput
  closedRef.current = opts.onClosed
  sizeRef.current = opts.getSize

  const writeRef = useRef<((data: string) => void) | null>(null)
  const resizeRef = useRef<((cols: number, rows: number) => void) | null>(null)

  useEffect(() => {
    let disposed = false
    const closeLater = () => {
      setTimeout(() => { if (!disposed) closedRef.current() }, 3000)
    }

    if (!isAdb) {
      // ---- local / public：设备 SDK WebSocket exec ----
      const wsProtocol = location.protocol === 'https:' ? 'wss://' : 'ws://'
      let ws: WebSocket | null = null
      let heartbeat: ReturnType<typeof setInterval> | null = null
      const sendJson = (obj: Record<string, unknown>) => {
        if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj))
      }
      const sendResize = () => {
        const dims = sizeRef.current()
        if (dims) sendJson({ type: 'resize', cols: dims.cols, rows: dims.rows })
      }

      // 密码：先读 localStorage，再向后端补取（WS 连接同步，先连后补）
      let wsUrl = `${wsProtocol}${deviceID}:8000/link/exec`
      try {
        const saved = localStorage.getItem('devicePasswords')
        if (saved) {
          const pwd = JSON.parse(saved)[deviceID]
          if (pwd) wsUrl = `${wsProtocol}admin:${pwd}@${deviceID}:8000/link/exec`
        }
      } catch {}

      const connect = async () => {
        let finalUrl = wsUrl
        try {
          const pwd = await SdkApiService.GetDevicePassword(deviceID)
          if (pwd) {
            finalUrl = `${wsProtocol}admin:${pwd}@${deviceID}:8000/link/exec`
            try {
              const saved = localStorage.getItem('devicePasswords')
              const passwords = saved ? JSON.parse(saved) : {}
              passwords[deviceID] = pwd
              localStorage.setItem('devicePasswords', JSON.stringify(passwords))
            } catch {}
          }
        } catch {}
        if (disposed) return

        ws = new WebSocket(finalUrl)
        ws.binaryType = 'arraybuffer'
        ws.onopen = () => {
          if (disposed) return
          setStatus('connected')
          sendJson({ type: 'login', container_id: containerName, shell: '/bin/sd' })
          sendResize()
          heartbeat = setInterval(() => sendJson({ type: 'heartbeat' }), 30000)
        }
        ws.onmessage = (event) => {
          if (disposed) return
          const raw = event.data
          const bytes = typeof raw === 'string'
            ? new TextEncoder().encode(raw)
            : new Uint8Array(raw as ArrayBuffer)
          // SDK 的连接提示不入终端（原实现同款）
          if (new TextDecoder().decode(bytes).includes('connected to container')) return
          outputRef.current(bytes)
        }
        ws.onclose = () => {
          if (disposed) return
          setStatus('disconnected')
          if (heartbeat) clearInterval(heartbeat)
          closeLater()
        }
        ws.onerror = () => {
          if (disposed) return
          setStatus('error')
          closeLater()
        }
      }
      void connect()

      writeRef.current = data => sendJson({ type: 'stdin', data })
      resizeRef.current = (cols, rows) => sendJson({ type: 'resize', cols, rows })

      return () => {
        disposed = true
        if (heartbeat) clearInterval(heartbeat)
        if (ws && ws.readyState === WebSocket.OPEN) ws.close()
      }
    }

    // ---- adb（v2 / v4）：ADB 直连会话（会话管理同构，仅打开方式不同）----
    let sid = ''
    const pending: Uint8Array[] = []
    // 事件载荷兼容两种投递形态（ev.data 包装 / 直接投递），与 useSlotTags 同款兜底
    const payloadOf = (ev: unknown): Record<string, unknown> =>
      ((ev as Record<string, unknown>)?.data ?? ev) as Record<string, unknown>
    const offData = Events.On('adbterm:data', (ev) => {
      const d = payloadOf(ev) as unknown as TermDataEvent
      if (!d || !d.sessionId) return
      const bytes = base64ToBytes(d.data)
      if (!sid) { pending.push(bytes); return } // 会话建立前先缓冲
      if (d.sessionId !== sid) return
      outputRef.current(bytes)
    })
    const offExit = Events.On('adbterm:exit', (ev) => {
      const d = payloadOf(ev) as unknown as TermExitEvent
      if (!d || d.sessionId !== sid) return
      setStatus('disconnected')
      closeLater()
    })

    const dims = sizeRef.current() ?? { cols: 80, rows: 24 }
    // v2：deviceID 是设备序列号（Go 侧 resolveDevice 反查 IP + dc list 查容器端口）；
    // v4：deviceID 是宿主机 IP，实例按名称定位（Go 侧 REST 实时查 adb_port）
    const open = isV4
      ? V4Service.OpenV4Terminal(deviceID, containerName, dims.cols, dims.rows)
      : V2Service.OpenV2Terminal(deviceID, containerName, dims.cols, dims.rows)
    open
      .then((id) => {
        if (disposed) {
          if (isV4) V4Service.CloseV4Terminal(id).catch(() => {})
          else V2Service.CloseV2Terminal(id).catch(() => {})
          return
        }
        sid = id
        for (const chunk of pending) outputRef.current(chunk)
        pending.length = 0
        setStatus('connected')
        const now = sizeRef.current()
        if (now) {
          if (isV4) V4Service.ResizeV4Terminal(id, now.cols, now.rows).catch(() => {})
          else V2Service.ResizeV2Terminal(id, now.cols, now.rows).catch(() => {})
        }
      })
      .catch((e: unknown) => {
        if (disposed) return
        setError(String(e).replace(/^Error:\s*/, ''))
        setStatus('error')
      })

    writeRef.current = (data) => {
      if (!sid) return
      if (isV4) V4Service.WriteV4Terminal(sid, data).catch(() => {})
      else V2Service.WriteV2Terminal(sid, data).catch(() => {})
    }
    resizeRef.current = (cols, rows) => {
      if (!sid) return
      if (isV4) V4Service.ResizeV4Terminal(sid, cols, rows).catch(() => {})
      else V2Service.ResizeV2Terminal(sid, cols, rows).catch(() => {})
    }

    return () => {
      disposed = true
      offData()
      offExit()
      const id = sid
      sid = ''
      if (id) {
        if (isV4) V4Service.CloseV4Terminal(id).catch(() => {})
        else V2Service.CloseV2Terminal(id).catch(() => {})
      }
    }
  }, [isAdb, isV4, deviceID, containerName])

  const write = useCallback((data: string) => writeRef.current?.(data), [])
  const resize = useCallback((cols: number, rows: number) => resizeRef.current?.(cols, rows), [])

  return { status, error, write, resize }
}
