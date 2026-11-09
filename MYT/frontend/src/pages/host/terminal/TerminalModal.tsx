import { useState, useEffect, useRef, useCallback } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { isMac } from '../../../utils/platform'
import '@xterm/xterm/css/xterm.css'
import { useTerminalSession } from './useTerminalSession'
import { useDraggable } from '../../../hooks/useDraggable'

// 终端悬浮窗（唯一实现，local / public / v2 / v4 共用同一套 UI）。
// 数据源由 useTerminalSession 按模式切换：
//   - local / public：设备 SDK WebSocket exec（ws://{ip}:8000/link/exec）
//   - v2 / v4：ADB 直连会话（v2 容器 / v4 实例，支持 logcat / top 等流式命令）
//
// 形态：非模态浮窗（无遮罩，背景界面可交互），标题栏可拖动、右下角可拉伸，
// 多实例并存时点击置顶（模块级 z 计数器）。拖动/拉伸/置顶只改 style，
// 不触碰组件渲染结构——xterm 的建连 effect 依赖 [deviceIP, containerName]，
// 任何结构性重渲染都会导致会话重建。
//
// 渲染位置：createPortal 到 document.body。浮窗渲染在 host 页 tabPane 内时，
// 切 Tab 被 keep-alive 容器 display:none 隐藏（fixed 后代一并不可见），故挂 body
// 与 React 树位置解耦——切 Tab 浮窗保持可见，WS 会话不受影响。事件冒泡沿
// React 树不变，DOM 冒泡沿 body；全局点击监听均用自身 ref.contains / closest
// 判断，不依赖浮窗是页面子树的 DOM 结构。
interface Props {
  deviceIP: string
  containerName: string
  displayName?: string
  /** v2 部署模式：终端走容器 ADB 直连（deviceIP 此时是设备序列号） */
  isV2?: boolean
  /** v4 部署模式：终端走实例 ADB 直连（deviceIP 此时是宿主机 IP） */
  isV4?: boolean
  /** 初始位置（视口像素坐标，可选；不传则按视口居中略偏上） */
  initialX?: number
  initialY?: number
  onClose: () => void
}

const MIN_W = 420
const MIN_H = 300

// 拖动/拉伸的上界：主窗口自绘标题栏（App.tsx 顶栏 h-8 = 32px）的下缘——
// 浮窗顶部不得越过该线，保证不盖住右上角的最小化/最大化/关闭按钮。
const TITLEBAR_H = 32

// 点击置顶的模块级计数器：每个浮窗实例记住自己拿到的 z 值，点击时取新最大值。
// 层级约定：普通弹窗 z-50 起、ConfirmDialog z-[60]——终端浮窗压在其下（z-40 档）。
// 封顶 49：多实例反复点击置顶时不再无限上涨，永远低于一切 modal 遮罩（z-50 起），
// 也低于主窗口自绘标题栏（z-[70]）——浮窗拖到任何位置都盖不住右上角窗口控制按钮。
let zCounter = 40
const Z_MAX = 49
const nextZ = () => (zCounter < Z_MAX ? ++zCounter : zCounter)

function TerminalModal({ deviceIP, containerName, displayName, isV2 = false, isV4 = false, initialX, initialY, onClose }: Props) {
  const { t } = useTranslation()
  const termRef = useRef<HTMLDivElement>(null)
  const terminalInstance = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  // 终端尺寸（WS 登录后 / ADB 会话建立后首次同步用）
  const getSize = useCallback(() => {
    const dims = fitAddonRef.current?.proposeDimensions()
    return dims ? { cols: dims.cols, rows: dims.rows } : null
  }, [])

  // 输出写入 xterm（传输层回调）
  const onOutput = useCallback((data: Uint8Array) => {
    terminalInstance.current?.write(data)
  }, [])

  // 连接断开/出错后关闭弹窗（延迟由 hook 控制，与原实现一致）
  const onClosed = useCallback(() => onCloseRef.current(), [])

  const session = useTerminalSession({ isV2, isV4, deviceID: deviceIP, containerName, getSize, onOutput, onClosed })

  useEffect(() => {
    if (!termRef.current) return

    // 初始化 xterm
    const term = new Terminal({
      cursorBlink: true,
      theme: {
        background: '#1e1e1e',
        foreground: '#cccccc',
        cursor: '#cccccc',
        selection: '#444444',
        selectionForeground: '#ffffff',
        black: '#000000',
        red: '#ff4747',
        green: '#00ff00',
        yellow: '#ffff00',
        blue: '#4799ff',
        magenta: '#ff00ff',
        cyan: '#00ffff',
        white: '#cccccc',
        brightBlack: '#808080',
        brightRed: '#ff4747',
        brightGreen: '#00ff00',
        brightYellow: '#ffff00',
        brightBlue: '#4799ff',
        brightMagenta: '#ff00ff',
        brightCyan: '#00ffff',
        brightWhite: '#ffffff',
      },
      allowProposedApi: true,
      disableStdin: false,
      rendererType: 'canvas',
    })

    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(termRef.current)
    fitAddon.fit()
    term.focus()

    terminalInstance.current = term
    fitAddonRef.current = fitAddon

    // 复制支持
    term.onSelectionChange(() => {
      const sel = term.getSelection()
      if (sel) {
        navigator.clipboard.writeText(sel).catch(() => {})
      }
    })

    // Cmd+C/V (macOS) / Ctrl+C/V (Windows)
    term.attachCustomKeyEventHandler((event) => {
      if (event.type !== 'keydown') return true
      const modKey = isMac ? event.metaKey : event.ctrlKey
      if (modKey && event.key === 'c') {
        if (term.hasSelection()) {
          navigator.clipboard.writeText(term.getSelection()).catch(() => {})
          return false
        }
      } else if (modKey && event.key === 'v') {
        event.preventDefault()
        event.stopPropagation()
        navigator.clipboard.readText().then(text => {
          if (text) {
            const clean = text
              .replace(/[\u200B\u200C\u200D\uFEFF\u00A0]/g, '')
              .replace(/\r\n/g, '\n')
              .replace(/\r/g, '\n')
            if (clean) session.write(clean)
          }
        }).catch(() => {})
        return false
      }
      return true
    })

    // 输入转发
    term.onData(data => {
      let cleanData = data
      if (data.length > 1) {
        cleanData = data
          .replace(/[\u200B\u200C\u200D\uFEFF\u00A0]/g, '')
          .replace(/\r\n/g, '\n')
          .replace(/\r/g, '\n')
      }
      if (cleanData) session.write(cleanData)
    })

    // fit + PTY 尺寸同步（window resize / 容器 ResizeObserver 共用）
    const doFit = () => {
      if (fitAddonRef.current && terminalInstance.current) {
        fitAddonRef.current.fit()
        session.resize(terminalInstance.current.cols, terminalInstance.current.rows)
      }
    }
    const handleResize = () => doFit()
    window.addEventListener('resize', handleResize)

    // 浮窗拖动/拉伸改的是面板 style，xterm 容器尺寸随之变化——
    // 用 ResizeObserver 捕捉容器尺寸变化并实时 fit（窗口 resize 监听保留）
    const ro = new ResizeObserver(() => doFit())
    if (termRef.current) ro.observe(termRef.current)

    // 清理
    return () => {
      window.removeEventListener('resize', handleResize)
      ro.disconnect()
      try { term.dispose() } catch {}
      terminalInstance.current = null
      fitAddonRef.current = null
    }
    // session 的 write/resize 是稳定引用；设备/容器切换由 hook 内部重建连接
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deviceIP, containerName])

  // 会话建立后同步一次真实终端尺寸：hook 的 effect 先于本组件 xterm 初始化执行，
  // 建立连接时拿到的尺寸可能是初始化前的默认值（80x24），这里按实际尺寸校正 PTY
  useEffect(() => {
    if (session.status !== 'connected') return
    const term = terminalInstance.current
    if (!term || !fitAddonRef.current) return
    fitAddonRef.current.fit()
    session.resize(term.cols, term.rows)
  }, [session.status, session.resize])

  // ---- 浮窗定位/拉伸/置顶 ----
  const panelRef = useRef<HTMLDivElement>(null)
  const headerRef = useRef<HTMLDivElement>(null)
  // 初始位置/尺寸记在 ref（mount 后不随渲染变化）；拖动位移走 transform（见下），
  // 都直接作用于 DOM style，不触发 React 渲染
  const posRef = useRef({ x: 0, y: 0, initialized: false })
  const sizeRef = useRef({ w: 640, h: 420 })
  const [zIndex, setZIndex] = useState(() => nextZ())

  // 初始位置：props 传入优先，否则视口居中略偏上。
  // 位置与尺寸一并按视口/标题栏上界夹紧，避免 mount 时就已经盖住窗口控制按钮
  if (!posRef.current.initialized) {
    posRef.current.initialized = true
    if (initialX !== undefined && initialY !== undefined) {
      posRef.current.x = initialX
      posRef.current.y = initialY
    } else {
      posRef.current.x = Math.max(8, Math.round((window.innerWidth - 640) / 2))
      posRef.current.y = Math.max(TITLEBAR_H, Math.round((window.innerHeight - 420) / 2) - 40)
    }
    posRef.current.x = Math.max(8 - sizeRef.current.w, Math.min(posRef.current.x, window.innerWidth - 8))
    posRef.current.y = Math.max(TITLEBAR_H, Math.min(posRef.current.y, window.innerHeight - sizeRef.current.h))
  }

  // 拖动由 useDraggable 以 transform 位移实现：left/top（布局位置）保持不变，
  // transform 直接写 DOM、不进 React 渲染，重渲染也不会回跳
  const { startDrag } = useDraggable({ panelRef, handleRef: headerRef, constrainTop: TITLEBAR_H })

  // 右下角拉伸把手：mousedown 拖动改 width/height（最小 420x300），仅写 style。
  // 右/下边缘夹回视口（把手不跑出可视区）；面板顶部若因历史位置高于标题栏上界
  // （如视口缩小后再拉伸），向上拉伸时把顶边压回 TITLEBAR_H，不让浮窗盖住窗口按钮。
  const startResize = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const panel = panelRef.current
    if (!panel) return
    const rect = panel.getBoundingClientRect()
    const startW = rect.width
    const startH = rect.height
    const startX = e.clientX
    const startY = e.clientY
    // 拉伸中如需把顶边压回上界，同步把 top 下调（新增的高度全给顶部）
    const nudgeTop = Math.max(0, TITLEBAR_H - rect.top)
    const onMoveDoc = (ev: MouseEvent) => {
      const maxW = window.innerWidth - rect.left
      const maxH = window.innerHeight - rect.top
      const w = Math.min(Math.max(MIN_W, startW + ev.clientX - startX), Math.max(MIN_W, maxW))
      const h = Math.min(Math.max(MIN_H, startH + ev.clientY - startY), Math.max(MIN_H, maxH))
      panel.style.width = `${w}px`
      panel.style.height = `${h + nudgeTop}px`
      if (nudgeTop > 0) panel.style.top = `${rect.top - nudgeTop}px`
      sizeRef.current = { w, h: h + nudgeTop }
    }
    const onUp = () => {
      document.removeEventListener('mousemove', onMoveDoc)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mousemove', onMoveDoc)
    document.addEventListener('mouseup', onUp)
  }, [])

  const bringToFront = useCallback(() => setZIndex(nextZ()), [])

  // 进场动画：浮窗无遮罩，用全局 animate-fade-in（淡入+上移 keyframes，见 style.css）。
  // 动画放在内层卡片上——外层面板的 transform 专用于拖动位移，两者不能共处一层。
  // 渲染经 createPortal 挂 document.body：切 Tab 时 keep-alive 容器 display:none
  // 不会波及浮窗（fixed 后代随宿主不可见），会话与交互均不受影响。
  return createPortal(
    <div
      ref={panelRef}
      role="dialog"
      className="fixed"
      style={{
        left: posRef.current.x,
        top: posRef.current.y,
        width: sizeRef.current.w,
        height: sizeRef.current.h,
        zIndex,
      }}
      onMouseDown={bringToFront}
    >
      <div className="relative bg-[#1e1e1e] rounded-lg shadow-2xl w-full h-full flex flex-col animate-fade-in">
        {/* 标题栏（拖动手柄） */}
        <div
          ref={headerRef}
          onMouseDown={startDrag}
          className="flex items-center justify-between px-4 py-2 bg-[#2d2d2d] rounded-t-lg border-b border-[#404040] cursor-move select-none"
        >
          <div className="flex items-center gap-2">
            <button onMouseDown={e => e.stopPropagation()} onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            <span className="text-sm font-medium text-gray-300">{t('host:terminal.title', { title: displayName || containerName })}</span>
            {session.status === 'connecting' && <span className="text-xs text-yellow-400">{t('host:terminal.connecting')}</span>}
            {session.status === 'connected' && <span className="text-xs text-green-400">{t('host:terminal.connected')}</span>}
            {session.status === 'disconnected' && <span className="text-xs text-red-400">{t('host:terminal.disconnected')}</span>}
            {session.status === 'error' && <span className="text-xs text-red-400">{session.error || t('host:terminal.connectionFailed')}</span>}
          </div>
          <button onMouseDown={e => e.stopPropagation()} onClick={onClose}
            className="px-3 py-0.5 text-xs text-gray-400 border border-[#555] rounded hover:bg-red-500 hover:text-white hover:border-red-500 transition-colors">
            {t('host:terminal.close')}
          </button>
        </div>
        {/* 终端内容区 */}
        <div ref={termRef} className="flex-1 px-1 py-1 overflow-hidden" />
        {/* 右下角拉伸把手 */}
        <div
          onMouseDown={startResize}
          className="absolute bottom-0 right-0 w-4 h-4 cursor-nwse-resize"
        >
          <div className="absolute bottom-1 right-1 w-2 h-2 border-b-2 border-r-2 border-[#666]" />
        </div>
      </div>
    </div>,
    document.body,
  )
}

export default TerminalModal
