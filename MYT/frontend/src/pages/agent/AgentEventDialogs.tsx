import { useState } from 'react'
import { X, Shield, HelpCircle, Check, Bell, Bot, ChevronDown, ChevronRight } from 'lucide-react'
import { agentC as c } from './agentTheme'
import type { ApprovalRequest, FormRequest, SubAgent, NotificationItem, FormField } from './agentTypes'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

/** 审批弹窗：Agent 执行工具前请求用户同意/拒绝 */
export function ApprovalDialog({ approvals, onRespond }: {
  approvals: ApprovalRequest[]
  onRespond: (id: string, approved: boolean) => void
}) {
  if (approvals.length === 0) return null
  const a = approvals[0]
  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
      {/* max-h + flex-col：内容超高时头部（标题/X）与底部按钮固定可见，中段内部滚动——
          flex 居中的超高弹窗会把顶部溢出视口外且不可滚动，X 与按钮永久不可达 */}
      <div className="w-full max-w-md max-h-[90vh] flex flex-col rounded-2xl border shadow-xl p-5 animate-in fade-in zoom-in duration-200" style={{ borderColor: c.line, background: c.surface }}>
        <div className="flex items-center gap-2 mb-3 shrink-0">
          <Shield className="size-5" style={{ color: c.warn }} />
          <span className="text-sm font-semibold" style={{ color: c.text }}>工具审批请求</span>
        </div>
        <div className="mb-4 flex-1 overflow-y-auto">
          <div className="text-xs mb-1" style={{ color: c.textMuted }}>工具</div>
          <code className="text-sm px-2 py-1 rounded" style={{ background: c.surfaceAlt, color: c.accent }}>{a.toolName}</code>
          {a.toolArgs && (
            <div className="mt-4">
              <div className="text-xs mb-1" style={{ color: c.textMuted }}>参数</div>
              <pre className="text-xs p-2 rounded" style={{ background: c.surfaceAlt, color: c.textMuted }}>{a.toolArgs}</pre>
            </div>
          )}
        </div>
        <div className="flex justify-end gap-2 shrink-0">
          <button onClick={() => onRespond(a.id, false)} className="px-4 py-2 text-sm rounded-lg border transition-colors hover:opacity-80" style={{ borderColor: c.line, color: c.textMuted }}>
            拒绝
          </button>
          <button onClick={() => onRespond(a.id, true)} className="px-4 py-2 text-sm rounded-lg text-white transition-opacity hover:opacity-90" style={{ background: c.accent }}>
            同意执行
          </button>
        </div>
      </div>
    </div>
  )
}

/** 表单弹窗：clarify 工具向用户提问 */
export function FormDialog({ form, onRespond }: {
  form: FormRequest | null
  onRespond: (id: string, data: Record<string, string>, cancelled: boolean) => void
}) {
  const [values, setValues] = useState<Record<string, string>>({})
  if (!form) return null

  const handleSubmit = () => {
    onRespond(form.id, values, false)
    setValues({})
  }
  const handleCancel = () => {
    onRespond(form.id, {}, true)
    setValues({})
  }

  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
      {/* max-h + flex-col：表单字段多时卡片高度超视口——flex 居中的超高弹窗会把顶部
          （标题/X）溢出到屏幕外且不可滚动，X/前段字段/底部按钮全部不可达（只能刷新页面）。
          头部与底部固定，字段区内部滚动。 */}
      <div className="w-full max-w-md max-h-[90vh] flex flex-col rounded-2xl border shadow-xl p-5 animate-in fade-in zoom-in duration-200" style={{ borderColor: c.line, background: c.surface }}>
        <div className="flex items-center justify-between mb-3 shrink-0">
          <div className="flex items-center gap-2">
            <HelpCircle className="size-5" style={{ color: c.accent }} />
            <span className="text-sm font-semibold" style={{ color: c.text }}>{form.title}</span>
          </div>
          <button onClick={handleCancel} className="transition-opacity hover:opacity-70"><X className="size-4" style={{ color: c.textMuted }} /></button>
        </div>
        <div className="space-y-3 flex-1 overflow-y-auto">
          {form.fields.map((f: FormField) => (
            <div key={f.name}>
              <label className="text-xs mb-1 block" style={{ color: c.textMuted }}>{f.label}{f.required ? ' *' : ''}</label>
              {f.type === 'select' && f.options ? (
                <select value={values[f.name] || f.default || ''} onChange={e => setValues(v => ({ ...v, [f.name]: e.target.value }))}
                  className="w-full text-sm px-3 py-2 rounded-lg border outline-none" style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }}>
                  <option value="">-- 选择 --</option>
                  {f.options.map(o => <option key={o} value={o}>{o}</option>)}
                </select>
              ) : f.type === 'textarea' ? (
                <textarea value={values[f.name] || f.default || ''} onChange={e => setValues(v => ({ ...v, [f.name]: e.target.value }))}
                  placeholder={f.placeholder || ''} rows={3}
                  className="w-full text-sm px-3 py-2 rounded-lg border outline-none resize-vertical" style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
              ) : (
                <input type={f.type === 'number' ? 'number' : 'text'} value={values[f.name] || f.default || ''}
                  onChange={e => setValues(v => ({ ...v, [f.name]: e.target.value }))}
                  placeholder={f.placeholder || ''}
                  className="w-full text-sm px-3 py-2 rounded-lg border outline-none" style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
              )}
            </div>
          ))}
        </div>
        <div className="flex justify-end gap-2 mt-4 shrink-0">
          <button onClick={handleCancel} className="px-4 py-2 text-sm rounded-lg border transition-colors hover:opacity-80" style={{ borderColor: c.line, color: c.textMuted }}>取消</button>
          <button onClick={handleSubmit} className="px-4 py-2 text-sm rounded-lg text-white transition-opacity hover:opacity-90" style={{ background: c.accent }}>提交</button>
        </div>
      </div>
    </div>
  )
}

/** 通知 Toast */
export function NotificationToasts({ notifications, onDismiss }: {
  notifications: NotificationItem[]
  onDismiss: (id: string) => void
}) {
  if (notifications.length === 0) return null
  return (
    <div className="fixed right-4 top-4 z-[150] flex w-72 flex-col gap-2">
      {notifications.map(n => (
        <div key={n.id} className="animate-in fade-in slide-in-from-right duration-300 overflow-hidden rounded-xl border shadow-lg" style={{ borderColor: c.line, background: c.surface }}>
          <div className="flex items-center gap-2 border-b px-3 py-2" style={{ borderColor: c.line, background: c.surfaceAlt }}>
            <Bell className="size-3.5 shrink-0" style={{ color: c.accent }} />
            <span className="truncate text-[11px] font-medium" style={{ color: c.text }}>{n.desc || '通知'}</span>
            <button onClick={() => onDismiss(n.id)} className="ml-auto shrink-0 transition-opacity hover:opacity-70"><X className="size-3" style={{ color: c.textMuted }} /></button>
          </div>
          <p className="px-3 py-2.5 text-xs leading-relaxed" style={{ color: c.textMuted }}>{n.text}</p>
        </div>
      ))}
    </div>
  )
}

/** 子 Agent 执行面板 */
export function SubAgentPanel({ subAgents }: { subAgents: SubAgent[] }) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  if (subAgents.length === 0) return null
  const toggle = (id: string) => setExpanded(p => ({ ...p, [id]: !p[id] }))

  return (
    <div className="mb-2 space-y-1">
      {subAgents.map(sa => {
        const isOpen = expanded[sa.id] !== false
        const isRunning = sa.status === 'running'
        return (
          <div key={sa.id} className="rounded-lg border" style={{ borderColor: c.line, background: c.surfaceAlt }}>
            <button onClick={() => toggle(sa.id)} className="flex w-full items-center gap-2 px-3 py-2">
              {isOpen ? <ChevronDown className="size-3" style={{ color: c.textMuted }} /> : <ChevronRight className="size-3" style={{ color: c.textMuted }} />}
              <Bot className="size-3.5" style={{ color: isRunning ? c.accent : sa.status === 'done' ? c.success : c.danger }} />
              <span className="text-xs truncate flex-1" style={{ color: c.text }}>{sa.goal || sa.deviceId || '子Agent'}</span>
              {isRunning && <span className="text-[10px] px-1.5 py-0.5 rounded animate-pulse" style={{ background: c.accent + '15', color: c.accent }}>运行中</span>}
              {sa.status === 'done' && <Check className="size-3" style={{ color: c.success }} />}
            </button>
            {isOpen && (
              <div className="px-3 pb-2 space-y-1 max-h-48 overflow-y-auto">
                {sa.steps.map((s, i) => (
                  <div key={s.id} className="text-xs">
                    {s.kind === 'thinking' && s.reasoning && (
                      <div className="p-1.5 rounded" style={{ background: c.surface }}>
                        <span style={{ color: c.textMuted }}>💭 {s.reasoning.slice(0, 200)}{s.reasoning.length > 200 ? '...' : ''}</span>
                      </div>
                    )}
                    {s.kind === 'reply' && s.reply && (
                      <div className="p-1.5 rounded" style={{ background: c.surface }}>
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>{s.reply}</ReactMarkdown>
                      </div>
                    )}
                    {s.kind === 'tool' && (
                      <div className="p-1.5 rounded flex items-start gap-1.5" style={{ background: c.surface }}>
                        <span style={{ color: s.done ? (s.success ? c.success : c.danger) : c.textMuted }}>{s.done ? '✓' : '⟳'}</span>
                        <div className="flex-1 min-w-0">
                          <span style={{ color: c.text }}>{s.toolName}</span>
                          {s.done && s.toolResult && <pre className="mt-0.5 text-[10px] whitespace-pre-wrap" style={{ color: c.textMuted }}>{s.toolResult.slice(0, 300)}</pre>}
                        </div>
                      </div>
                    )}
                  </div>
                ))}
                {sa.summary && <div className="p-1.5 text-xs" style={{ color: c.textMuted }}>📋 {sa.summary}</div>}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
