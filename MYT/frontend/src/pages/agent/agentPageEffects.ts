import { useEffect, useRef } from 'react'
import { useActiveTab } from '../../hooks/useNavigation'
import type { ChatMessage, ConnState } from './agentTypes'

/** AgentPage 的 useEffect 群：空格录音/可见性/自动连接/撤回/滚动/TTS */
export function useAgentPageEffects(opts: {
  connState: ConnState
  loggedIn: boolean
  recording: boolean
  startRecording: (setInput: (t: string) => void) => void
  stopRecording: () => void
  connect: () => Promise<void>
  setInputText: (t: string) => void
  messages: ChatMessage[]
  watchMessages: (msgs: ChatMessage[]) => void
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)
  const pageRef = useRef<HTMLDivElement>(null)
  const connectedRef = useRef(false)
  const lastHeightRef = useRef(0)
  // Tab 可见性：keep-alive（display:none）架构下用 App 的 activeTab 判断——
  // IntersectionObserver 对 display:none 容器不触发回调，曾导致切走 Tab 后
  // portal 到 body 的投屏 iframe 层不隐藏（bug 修复，2026-10-15）。
  const pageVisible = useActiveTab() === 'agent'

  // 空格键按住说话
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const tag = (document.activeElement?.tagName ?? '').toUpperCase()
      const inField = tag === 'INPUT' || tag === 'TEXTAREA'
      if (e.code !== 'Space' || e.repeat || inField || opts.recording || opts.connState !== 'connected') return
      e.preventDefault()
      opts.startRecording(opts.setInputText)
    }
    const onKeyUp = (e: KeyboardEvent) => {
      if (e.code !== 'Space' || !opts.recording) return
      e.preventDefault()
      opts.stopRecording()
    }
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('keyup', onKeyUp)
    }
  }, [opts.recording, opts.connState, opts.startRecording, opts.stopRecording, opts.setInputText])

  // 登录后自动连接
  useEffect(() => {
    if (!opts.loggedIn) {
      connectedRef.current = false
      return
    }
    if (connectedRef.current) return
    connectedRef.current = true
    opts.connect().catch(() => {})
  }, [opts.connect, opts.loggedIn])

  // 撤回时退回输入框
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail
      if (detail) opts.setInputText(detail)
    }
    window.addEventListener('message-recalled', handler)
    return () => window.removeEventListener('message-recalled', handler)
  }, [])

  // 滚动监听
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const onScroll = () => {
      autoScrollRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 200
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  }, [])

  // ResizeObserver 监听内容高度变化
  useEffect(() => {
    const scrollEl = scrollRef.current
    const listEl = listRef.current
    if (!scrollEl || !listEl) return
    let raf = 0
    const ro = new ResizeObserver(() => {
      if (raf) return
      raf = requestAnimationFrame(() => {
        raf = 0
        const h = scrollEl.scrollHeight
        if (autoScrollRef.current && h > lastHeightRef.current) {
          scrollEl.scrollTop = scrollEl.scrollHeight
        }
        lastHeightRef.current = h
      })
    })
    ro.observe(listEl)
    return () => { if (raf) cancelAnimationFrame(raf); ro.disconnect() }
  }, [opts.messages.length === 0])

  // 新消息时强制滚底
  useEffect(() => {
    if (opts.messages.length === 0) return
    autoScrollRef.current = true
    requestAnimationFrame(() => {
      if (scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    })
  }, [opts.messages.length])

  // TTS: 监听助手回复完成
  useEffect(() => { opts.watchMessages(opts.messages) }, [opts.messages, opts.watchMessages])

  return { scrollRef, listRef, pageRef, pageVisible }
}
