import { useRef, useState } from 'react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Sparkles, User, Check, Loader2, ChevronDown, Brain, Copy, Undo2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import i18n from '../../i18n'
import { agentC as c } from './agentTheme'
import type { ChatMessage, ToolCall, AgentConnInfo } from './agentTypes'
import { parseUserFileAttachments, FileCard, ClickableImage, transformAttachments, createMdComponents, buildFileHref } from './agentFileRender'

gsap.registerPlugin(useGSAP)

function fmtDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  const sec = ms / 1000
  return sec < 10 ? `${sec.toFixed(1)}${i18n.t('agent:chat.unitSecond')}` : `${Math.round(sec)}${i18n.t('agent:chat.unitSecond')}`
}

function formatTime(ts: number): string {
  const d = new Date(ts)
  const now = new Date()
  const time = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  if (d.toDateString() === now.toDateString()) return time
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${time}`
}

/** 用户消息：右对齐蓝色气泡 */
function UserMessage({ msg, canRecall, onRecall, connInfo }: { msg: ChatMessage; canRecall?: boolean; onRecall?: () => void; connInfo?: AgentConnInfo | null }) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDivElement>(null)
  const [copied, setCopied] = useState(false)
  useGSAP(() => {
    gsap.fromTo(ref.current, { x: 16, opacity: 0 }, { x: 0, opacity: 1, duration: 0.35, ease: 'power2.out' })
  }, { scope: ref })
  const { cleanText, files } = parseUserFileAttachments(msg.text)
  const baseUrl = connInfo?.baseUrl ?? ''
  const token = connInfo?.sessionToken ?? ''
  return (
    <div ref={ref} className="animate-fadeIn relative mb-6 flex justify-end">
      <div className="flex max-w-[80%] flex-col items-end gap-1">
        {msg.images && msg.images.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {msg.images.map((img, i) => <ClickableImage key={i} src={`data:image/jpeg;base64,${img}`} alt="upload" className="max-h-64 rounded-2xl object-contain" />)}
          </div>
        )}
        {files.map((f, i) => (
          <div key={i} className="w-full max-w-xs"><FileCard href={buildFileHref(f.path, baseUrl, token)} filename={f.name} /></div>
        ))}
        {cleanText && (
          <div className="flex items-start gap-2.5">
            <div className="rounded-2xl rounded-br-md px-4 py-2.5 text-sm leading-relaxed text-white whitespace-pre-wrap" style={{ background: c.accent, overflowWrap: 'anywhere' }}>
              {cleanText}
            </div>
            <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full" style={{ background: c.surfaceAlt, border: `1px solid ${c.line}` }}>
              <User className="size-3.5" style={{ color: c.textMuted }} />
            </span>
          </div>
        )}
        <div className="flex items-center gap-1">
          <span className="text-[10px]" style={{ color: c.textMuted }}>{formatTime(msg.timestamp)}</span>
          {msg.text && (
            <button onClick={() => { navigator.clipboard.writeText(msg.text); setCopied(true); setTimeout(() => setCopied(false), 2000) }}
              className="flex items-center gap-0.5 rounded px-1 py-0.5 text-[10px] hover:bg-black/5" style={{ color: c.textMuted }}>
              {copied ? <Check className="size-3" style={{ color: c.success }} /> : <Copy className="size-3" />} {t('agent:chat.copy')}
            </button>
          )}
          {canRecall && onRecall && (
            <button onClick={onRecall} title={t('agent:chat.recall')}
              className="flex items-center gap-0.5 rounded px-1 py-0.5 text-[10px] hover:bg-black/5" style={{ color: c.textMuted }}>
              <Undo2 className="size-3" /> {t('agent:chat.recall')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

/** 步骤汇总折叠块（思考 + 工具） */
function StepsBlock({ msg }: { msg: ChatMessage }) {
  const { t } = useTranslation()
  const tools = msg.toolCalls || []
  const hasTools = tools.length > 0
  const hasThinking = Boolean(msg.thinking)
  const isDone = msg.status === 'done' || msg.status === 'error'
  const [open, setOpen] = useState(!isDone)
  const [thinkOpen, setThinkOpen] = useState(true)
  const thinkRef = useRef<HTMLDivElement>(null)

  // 思考流式自动滚动
  useGSAP(() => {
    if (thinkRef.current && thinkOpen && !isDone) {
      thinkRef.current.scrollTop = thinkRef.current.scrollHeight
    }
  }, { dependencies: [msg.thinking], scope: thinkRef })

  const toolCount = tools.length
  const totalSec = msg.startedAt && msg.endedAt ? fmtDuration(msg.endedAt - msg.startedAt) : ''

  if (!hasThinking && !hasTools) return null

  return (
    <div className="mb-2.5">
      <button onClick={() => setOpen(!open)} className="flex w-full items-center gap-2 text-left">
        <span className="inline-block h-3 w-0.5 rounded-full" style={{ background: c.think }} />
        <span className="flex items-center">
          {isDone ? <Check className="size-3" style={{ color: c.success }} /> : <Loader2 className="size-3 animate-spin" style={{ color: c.think }} />}
        </span>
        <span className="text-[11px] font-medium" style={{ color: c.think }}>
          {isDone ? (totalSec ? t('agent:chat.thoughtWithDuration', { duration: totalSec }) : t('agent:chat.thoughtDone')) : t('agent:chat.thinking')}
        </span>
        {toolCount > 0 && <span className="text-[10px]" style={{ color: c.textMuted }}>{t('agent:chat.toolCount', { count: toolCount })}</span>}
        <ChevronDown className="size-3 transition-transform" style={{ color: c.textMuted, transform: open ? 'rotate(180deg)' : 'none' }} />
      </button>

      <div className="overflow-hidden transition-all duration-300" style={{ maxHeight: open ? 'none' : 0, opacity: open ? 1 : 0, marginTop: open ? 6 : 0 }}>
        <div className="ml-3 pb-2 relative">
          <div className="absolute bottom-1 left-1.5 top-1 w-px" style={{ background: c.line, opacity: 0.5 }} />

          {/* 思考段 */}
          {hasThinking && (
            <div className="relative mb-2">
              <button onClick={() => setThinkOpen(!thinkOpen)} className="flex items-center gap-1.5 py-0.5 text-left">
                <span className="absolute -left-[1px] flex size-2 items-center justify-center rounded-full" style={{ background: c.think, border: `1.5px solid ${c.surface}` }} />
                <Brain className="size-2.5 ml-3.5" style={{ color: c.think, opacity: 0.6 }} />
                <span className="text-[10px]" style={{ color: c.textMuted }}>{!thinkOpen ? t('agent:chat.thinkingProcess') : ''}</span>
                <ChevronDown className="size-2.5 transition-transform" style={{ color: c.textMuted, opacity: 0.5, transform: thinkOpen ? 'rotate(180deg)' : 'none' }} />
              </button>
              {thinkOpen && (
                <div ref={thinkRef} className="ml-3.5 max-h-[200px] overflow-y-auto rounded-lg px-3 py-2 text-[11px] leading-relaxed whitespace-pre-wrap break-words"
                  style={{ background: c.surfaceAlt, color: c.textMuted, border: `1px solid ${c.line}` }}>
                  {msg.thinking}
                </div>
              )}
            </div>
          )}

          {/* 工具行 */}
          {tools.map((t, i) => <ToolLine key={i} tool={t} />)}
        </div>
      </div>
    </div>
  )
}

/** 工具单行 */
function ToolLine({ tool }: { tool: ToolCall }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const done = tool.endedAt != null
  let args = tool.args || ''
  try { if (args) args = JSON.stringify(JSON.parse(args), null, 2) } catch {}
  let result = tool.result || ''
  try { if (result) result = JSON.stringify(JSON.parse(result), null, 2) } catch {}
  const hasDetail = Boolean(args.trim() || result.trim())
  const dur = tool.startedAt && tool.endedAt ? fmtDuration(tool.endedAt - tool.startedAt) : ''

  return (
    <div className="relative mb-1.5">
      <span className="absolute -left-[1px] top-1.5 flex size-2 items-center justify-center rounded-full"
        style={{ background: done ? (tool.success === false ? c.danger : c.success) : c.accent, border: `1.5px solid ${c.surface}` }} />
      <button onClick={() => hasDetail && setOpen(!open)}
        className={`flex w-full items-center gap-2 py-1 text-left ml-3.5 ${hasDetail ? 'cursor-pointer' : 'cursor-default'}`}>
        <span className="flex size-4 shrink-0 items-center justify-center">
          {done ? <Check className="size-3" style={{ color: tool.success === false ? c.danger : c.success }} />
            : <Loader2 className="size-3 animate-spin" style={{ color: c.accent }} />}
        </span>
        <code className="font-mono text-[11px] font-medium" style={{ color: c.text }}>{tool.name}</code>
        <span className="text-[10px]" style={{ color: done ? c.textMuted : c.accent }}>
          {done ? (dur || t('agent:chat.toolDone')) : t('agent:chat.toolExecuting')}
        </span>
        {hasDetail && <ChevronDown className="ml-auto size-3 shrink-0 transition-transform" style={{ color: c.textMuted, opacity: 0.5, transform: open ? 'rotate(180deg)' : 'none' }} />}
      </button>
      {open && hasDetail && (
        <div className="ml-3.5 space-y-2 rounded-lg border px-3 py-2" style={{ borderColor: c.line, background: c.surfaceAlt }}>
          {args.trim() && (
            <div>
              <div className="mb-0.5 text-[10px] font-semibold uppercase tracking-wider" style={{ color: c.textMuted }}>Input</div>
              <pre className="max-h-40 overflow-auto font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all" style={{ color: c.textMuted }}>{args}</pre>
            </div>
          )}
          {result.trim() && (
            <div>
              <div className="mb-0.5 text-[10px] font-semibold uppercase tracking-wider" style={{ color: c.textMuted }}>{t('agent:chat.result')}</div>
              <pre className="max-h-60 overflow-auto font-mono text-[11px] leading-relaxed whitespace-pre-wrap break-all" style={{ color: 'rgba(61,214,140,0.85)' }}>{result}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/** Agent 消息：左对齐紫色节点 + 步骤 + Markdown 回复 */
function AssistantMessage({ msg, connInfo }: { msg: ChatMessage; connInfo?: AgentConnInfo | null }) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDivElement>(null)
  const [copied, setCopied] = useState(false)
  useGSAP(() => {
    gsap.fromTo(ref.current, { y: 12, opacity: 0 }, { y: 0, opacity: 1, duration: 0.35, ease: 'power2.out' })
  }, { scope: ref })

  const isThinking = msg.status === 'thinking' || msg.status === 'replying'
  const noContent = !msg.text && !msg.thinking && !(msg.toolCalls && msg.toolCalls.length > 0)
  const baseUrl = connInfo?.baseUrl ?? ''
  const token = connInfo?.sessionToken ?? ''
  const transformed = msg.text ? transformAttachments(msg.text, baseUrl, token) : ''
  const mdComps = createMdComponents(baseUrl, token)

  return (
    <div ref={ref} className="relative mb-6 pl-12">
      <span className={`absolute left-2.5 top-1 flex size-3 items-center justify-center rounded-full border-2 ${isThinking ? 'animate-pulse' : ''}`}
        style={{ borderColor: c.think, background: c.surface }}>
        <Sparkles className="size-2" style={{ color: c.think }} />
      </span>
      <div className="space-y-1.5 pt-0.5">
        <StepsBlock msg={msg} />
        {msg.images && msg.images.length > 0 && (
          <div className="mb-1 flex flex-wrap gap-1.5">
            {msg.images.map((img, i) => <ClickableImage key={i} src={`data:image/jpeg;base64,${img}`} alt="content" className="max-h-64 rounded-lg object-contain" />)}
          </div>
        )}
        {transformed && (
          <div className="prose-chat text-sm leading-relaxed" style={{ color: c.text }}>
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={mdComps}>{transformed}</ReactMarkdown>
          </div>
        )}
        {noContent && isThinking && (
          <div className="flex items-center gap-2 pt-0.5">
            <Loader2 className="size-3.5 animate-spin" style={{ color: c.accent }} />
            <span className="text-xs" style={{ color: c.textMuted }}>{t('agent:chat.thinkingEllipsis')}</span>
          </div>
        )}
        {msg.status === 'error' && (
          <div className="text-xs rounded-md px-3 py-1.5" style={{ color: c.danger, background: `${c.danger}10`, border: `1px solid ${c.danger}20` }}>
            {msg.error || t('agent:chat.errorOccurred')}
          </div>
        )}
        {msg.status === 'done' && msg.text && (
          <div className="flex items-center gap-1">
            <span className="text-[10px]" style={{ color: c.textMuted }}>{formatTime(msg.timestamp)}</span>
            <button onClick={() => { navigator.clipboard.writeText(msg.text); setCopied(true); setTimeout(() => setCopied(false), 2000) }}
              className="flex items-center gap-0.5 rounded px-1 py-0.5 text-[10px] hover:bg-black/5" style={{ color: c.textMuted }}>
              {copied ? <Check className="size-3" style={{ color: c.success }} /> : <Copy className="size-3" />} {t('agent:chat.copy')}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

export default function AgentMessage({ message, canRecall, onRecall, connInfo }: { message: ChatMessage; canRecall?: boolean; onRecall?: () => void; connInfo?: AgentConnInfo | null }) {
  return message.role === 'user'
    ? <UserMessage msg={message} canRecall={canRecall} onRecall={onRecall} connInfo={connInfo} />
    : <AssistantMessage msg={message} connInfo={connInfo} />
}
