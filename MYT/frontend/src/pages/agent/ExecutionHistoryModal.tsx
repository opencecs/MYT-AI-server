import { useState, useEffect, useCallback } from 'react'
import { X, ChevronDown, Loader2, Brain, Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import { tasksApi, type AsyncTaskItem } from './agentApi'
import type { AgentWsClient } from './agentWs'

interface Props {
  client: AgentWsClient | null
  title: string
  onClose: () => void
}

interface ExecItem extends AsyncTaskItem {}

function ExecutionHistoryModal({ client, title, onClose }: Props) {
  const { t } = useTranslation()
  const [items, setItems] = useState<ExecItem[]>([])
  const [selected, setSelected] = useState<ExecItem | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    if (!client) return
    try {
      const res = await tasksApi.executions(client, title, 1, 50)
      setItems(res as unknown as ExecItem[] || [])
    } catch { /* */ }
    setLoading(false)
  }, [client, title])

  useEffect(() => { load() }, [load])
  useEffect(() => {
    if (!client) return
    const t = setInterval(load, 2000)
    return () => clearInterval(t)
  }, [load, client])

  // 详情自动刷新
  useEffect(() => {
    if (!selected || !client) return
    const t = setInterval(async () => {
      try {
        const res = await tasksApi.executions(client, title, 1, 50)
        const updated = (res as unknown as ExecItem[] || []).find(x => x.id === selected.id)
        if (updated) setSelected(updated)
      } catch { /* */ }
    }, 1500)
    return () => clearInterval(t)
  }, [selected, client, title])

  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/30" onClick={onClose}>
      <div className="rounded-xl border shadow-xl w-[720px] max-w-[calc(100vw-32px)] max-h-[80vh] flex flex-col" style={{ borderColor: c.line, background: c.surface }} onClick={e => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b px-5 py-3" style={{ borderColor: c.line }}>
          <span className="text-sm font-semibold" style={{ color: c.text }}>{t('agent:execHistory.title', { title })}</span>
          <span className="text-[10px]" style={{ color: c.textMuted }}>{t('agent:execHistory.count', { count: items.length })}</span>
          <button onClick={onClose} className="ml-auto" style={{ color: c.textMuted }}><X className="size-4" /></button>
        </div>
        <div className="flex flex-1 overflow-hidden">
          {/* 左侧列表 */}
          <div className="w-48 border-r overflow-y-auto" style={{ borderColor: c.line }}>
            {loading ? <div className="py-8 text-center"><Loader2 className="mx-auto size-4 animate-spin" style={{ color: c.accent }} /></div>
              : items.length === 0 ? <div className="py-8 text-center text-xs" style={{ color: c.textMuted }}>{t('agent:execHistory.empty')}</div>
              : items.map(item => (
                <div key={item.id} onClick={() => setSelected(item)}
                  className="flex items-center gap-2 px-3 py-2 border-b cursor-pointer hover:bg-black/[0.02]" style={{ borderColor: c.line, background: selected?.id === item.id ? `${c.accent}08` : undefined }}>
                  <span className="size-2 rounded-full shrink-0" style={{ background: item.status === 'done' ? c.success : item.status === 'running' ? c.accent : item.status === 'failed' ? c.danger : c.warn }} />
                  <div className="min-w-0 flex-1">
                    <div className="text-[10px] font-mono truncate" style={{ color: c.textMuted }}>{item.id.slice(0, 12)}</div>
                    <div className="text-[10px]" style={{ color: c.textMuted }}>{item.duration_seconds ? `${item.duration_seconds.toFixed(1)}s` : item.status}</div>
                  </div>
                </div>
              ))}
          </div>
          {/* 右侧详情 */}
          <div className="flex-1 overflow-y-auto p-4">
            {selected ? <ExecDetail item={selected} /> : <div className="py-12 text-center text-xs" style={{ color: c.textMuted }}>{t('agent:execHistory.selectHint')}</div>}
          </div>
        </div>
      </div>
    </div>
  )
}

function ExecDetail({ item }: { item: ExecItem }) {
  const steps = (item.steps as Array<Record<string, unknown>> | undefined) || []
  // 合并步骤：思考段 + 工具调用
  const merged: Array<{ type: 'thinking' | 'tool' | 'reply'; content?: string; name?: string; args?: string; result?: string; count?: number }> = []
  for (const s of steps) {
    const t = s.type as string
    if (t === 'reasoning' || t === 'thinking') {
      merged.push({ type: 'thinking', content: (s.content as string) || (s.args as string) || '' })
    } else if (t === 'tool') {
      const last = merged[merged.length - 1]
      if (last && last.type === 'tool' && last.name === (s.name as string)) {
        last.count = (last.count || 1) + 1
      } else {
        merged.push({ type: 'tool', name: s.name as string, args: s.args as string, result: s.result as string, count: 1 })
      }
    } else if (t === 'reply' || t === 'message') {
      merged.push({ type: 'reply', content: (s.content as string) || (s.result as string) || '' })
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 mb-2">
        <span className="size-2 rounded-full" style={{ background: item.status === 'done' ? c.success : item.status === 'running' ? c.accent : item.status === 'failed' ? c.danger : c.warn }} />
        <span className="text-xs font-mono" style={{ color: c.textMuted }}>{item.id.slice(0, 12)}</span>
        {item.duration_seconds && <span className="text-[10px]" style={{ color: c.textMuted }}>{item.duration_seconds.toFixed(1)}s</span>}
      </div>
      {item.detail && <div className="text-xs rounded-lg p-3 mb-2" style={{ background: c.surfaceAlt, color: c.text }}>{item.detail}</div>}
      {merged.map((s, i) => {
        if (s.type === 'thinking') return <ThinkingBlock key={i} text={s.content || ''} />
        if (s.type === 'tool') return <ToolCard key={i} name={s.name || ''} args={s.args} result={s.result} count={s.count} />
        return <div key={i} className="text-xs rounded-lg p-3" style={{ background: c.surfaceAlt, color: c.text }}>{s.content}</div>
      })}
      {item.error && <div className="text-xs rounded-lg p-3" style={{ background: `${c.danger}10`, color: c.danger, border: `1px solid ${c.danger}20` }}>{item.error}</div>}
    </div>
  )
}

function ThinkingBlock({ text }: { text: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  if (!text) return null
  return (
    <div>
      <button onClick={() => setOpen(!open)} className="flex items-center gap-1.5 py-0.5">
        <Brain className="size-3" style={{ color: c.think, opacity: 0.6 }} />
        <span className="text-[10px]" style={{ color: c.textMuted }}>{t('agent:execHistory.thinkingProcess')}</span>
        <ChevronDown className="size-2.5 transition-transform" style={{ color: c.textMuted, transform: open ? 'rotate(180deg)' : 'none' }} />
      </button>
      {open && <div className="mt-1 rounded-lg p-2 text-[11px] max-h-32 overflow-y-auto whitespace-pre-wrap" style={{ background: c.surfaceAlt, color: c.textMuted, border: `1px solid ${c.line}` }}>{text}</div>}
    </div>
  )
}

function ToolCard({ name, args, result, count }: { name: string; args?: string; result?: string; count?: number }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="rounded-lg border" style={{ borderColor: c.line, background: c.surface }}>
      <button onClick={() => setOpen(!open)} className="flex items-center gap-2 w-full px-3 py-1.5 text-left">
        <Check className="size-3" style={{ color: c.success }} />
        <code className="text-[11px] font-mono" style={{ color: c.text }}>{name}</code>
        {count && count > 1 && <span className="text-[10px] px-1 rounded" style={{ background: c.surfaceAlt, color: c.textMuted }}>×{count}</span>}
        {(args || result) && <ChevronDown className="ml-auto size-3 transition-transform" style={{ color: c.textMuted, transform: open ? 'rotate(180deg)' : 'none' }} />}
      </button>
      {open && (args || result) && (
        <div className="border-t px-3 py-2 space-y-1.5" style={{ borderColor: c.line }}>
          {args && <pre className="text-[11px] font-mono whitespace-pre-wrap break-all max-h-32 overflow-y-auto" style={{ color: c.textMuted }}>{args}</pre>}
          {result && <pre className="text-[11px] font-mono whitespace-pre-wrap break-all max-h-40 overflow-y-auto" style={{ color: 'rgba(61,214,140,0.85)' }}>{result}</pre>}
        </div>
      )}
    </div>
  )
}

export default ExecutionHistoryModal
