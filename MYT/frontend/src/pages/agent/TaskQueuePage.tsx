import { useState, useEffect, useCallback, useRef } from 'react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import { Activity, X, ChevronDown, RefreshCw, ChevronLeft, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import { tasksApi, type AsyncTaskItem } from './agentApi'
import type { AgentConnInfo } from './agentTypes'
import type { AgentWsClient } from './agentWs'

gsap.registerPlugin(useGSAP)

const STATUS_COLORS: Record<string, string> = { pending: c.warn, running: c.accent, done: c.success, failed: c.danger }

interface Props { client: AgentWsClient | null; connInfo: AgentConnInfo }

function TaskQueuePage({ client, connInfo: _connInfo }: Props) {
  const { t } = useTranslation()
  const TYPE_LABELS: Record<string, string> = {
    memory_extract: t('agent:taskQueue.typeMemoryExtract'), memory_contradict: t('agent:taskQueue.typeMemoryContradict'), memory_maintain: t('agent:taskQueue.typeMemoryMaintain'), memory_merge: t('agent:taskQueue.typeMemoryMerge'),
    checkpoint: t('agent:taskQueue.typeCheckpoint'), context_rebuild: t('agent:taskQueue.typeContextRebuild'), soft_prune: t('agent:taskQueue.typeSoftPrune'), device_sync: t('agent:taskQueue.typeDeviceSync'), scheduled_task: t('agent:taskQueue.typeScheduledTask'),
  }
  const STATUS_LABELS: Record<string, string> = { pending: t('agent:taskQueue.statusPending'), running: t('agent:taskQueue.statusRunning'), done: t('agent:taskQueue.statusDone'), failed: t('agent:taskQueue.statusFailed') }
  const [tasks, setTasks] = useState<AsyncTaskItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [typeFilter, setTypeFilter] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<AsyncTaskItem | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const pageSize = 20
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const load = useCallback(async () => {
    if (!client) return
    setLoading(true)
    try {
      const res = await tasksApi.queue(client, page, pageSize, typeFilter)
      setTasks(res.tasks || [])
      setTotal(res.total || 0)
    } catch { /* */ }
    setLoading(false)
  }, [client, page, typeFilter])

  useEffect(() => { load() }, [load])
  useEffect(() => {
    if (!autoRefresh) return
    const t = setInterval(load, 2000)
    return () => clearInterval(t)
  }, [autoRefresh, load])

  useEffect(() => {
    if (!selected || !client) return
    const t = setInterval(async () => {
      try {
        const res = await tasksApi.queue(client, 1, 1000)
        const updated = (res.tasks || []).find(t => t.id === selected.id)
        if (updated) setSelected(updated)
      } catch { /* */ }
    }, 1500)
    return () => clearInterval(t)
  }, [selected, client])

  const stats = tasks.reduce((acc, t) => { acc[t.status] = (acc[t.status] || 0) + 1; return acc }, {} as Record<string, number>)

  useGSAP(() => {
    if (tasks.length === 0) return
    gsap.fromTo('.task-row', { x: -12, opacity: 0 }, { x: 0, opacity: 1, duration: 0.25, stagger: 0.03, ease: 'power2.out' })
  }, { dependencies: [tasks], scope: containerRef })

  return (
    <div ref={containerRef} className="flex h-full flex-col overflow-hidden" style={{ background: c.bg }}>
      <div className="flex items-center gap-3 border-b px-5 py-3" style={{ borderColor: c.line, background: c.surface }}>
        <Activity className="size-4" style={{ color: c.accent }} />
        <span className="text-sm font-semibold" style={{ color: c.text }}>{t('agent:taskQueue.title')}</span>
        <span className="text-[11px]" style={{ color: c.textMuted }}>{t('agent:taskQueue.total', { count: total })}</span>
        <div className="ml-auto flex items-center gap-2">
          <select value={typeFilter} onChange={e => { setTypeFilter(e.target.value); setPage(1) }}
            className="rounded-lg border px-3 py-1.5 text-xs outline-none cursor-pointer" style={{ borderColor: c.line, background: c.surface, color: c.text }}>
            <option value="">{t('agent:taskQueue.allTypes')}</option>
            {Object.entries(TYPE_LABELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
          </select>
          <button onClick={load} title={t('agent:tooltip.refresh')} className="rounded-lg p-1.5 transition-colors hover:bg-black/5" style={{ color: c.textMuted }}>
            <RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button onClick={() => setAutoRefresh(!autoRefresh)} title={t('agent:tooltip.autoRefresh')}
            className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[10px] transition-colors" style={{ color: autoRefresh ? c.accent : c.textMuted, background: autoRefresh ? `${c.accent}12` : c.surfaceAlt }}>
            <span className="size-1.5 rounded-full" style={{ background: autoRefresh ? c.accent : c.textMuted }} />
            {autoRefresh ? t('agent:taskQueue.autoRefresh') : t('agent:taskQueue.manualRefresh')}
          </button>
        </div>
      </div>

      <div className="flex gap-2.5 px-5 py-3 border-b" style={{ borderColor: c.line, background: c.surface }}>
        {(['running', 'done', 'failed', 'pending'] as const).map(s => (
          <div key={s} className="flex items-center gap-2 rounded-xl px-3.5 py-2" style={{ background: c.surfaceAlt }}>
            <span className="size-2.5 rounded-full" style={{ background: STATUS_COLORS[s] }} />
            <span className="text-[11px]" style={{ color: c.textMuted }}>{STATUS_LABELS[s]}</span>
            <span className="text-sm font-bold tabular-nums" style={{ color: c.text }}>{stats[s] || 0}</span>
          </div>
        ))}
      </div>

      <div className="flex-1 overflow-y-auto">
        {tasks.length === 0 && !loading ? (
          <div className="flex h-full flex-col items-center justify-center gap-3">
            <div className="flex size-14 items-center justify-center rounded-2xl" style={{ background: `${c.accent}10`, border: `1px solid ${c.accent}20` }}>
              <Activity className="size-7" style={{ color: c.accent, opacity: 0.5 }} />
            </div>
            <div className="text-sm font-medium" style={{ color: c.text }}>{t('agent:taskQueue.empty')}</div>
          </div>
        ) : (
          tasks.map(task => (
            <div key={task.id} onClick={() => setSelected(task)}
              className="task-row flex items-center gap-3 border-b px-5 py-3 cursor-pointer transition-colors hover:bg-black/[0.02]" style={{ borderColor: c.line }}>
              <span className="size-2.5 shrink-0 rounded-full" style={{ background: STATUS_COLORS[task.status] || c.textMuted }} />
              <span className="text-xs font-mono shrink-0" style={{ color: c.textMuted }}>{task.id.slice(0, 8)}</span>
              <span className="text-xs truncate flex-1" style={{ color: c.text }}>{task.title}</span>
              <span className="text-[10px] shrink-0 px-2 py-0.5 rounded-full" style={{ background: c.surfaceAlt, color: c.textMuted }}>{TYPE_LABELS[task.type] || task.type}</span>
              <span className="text-[10px] shrink-0 font-medium" style={{ color: STATUS_COLORS[task.status] }}>{STATUS_LABELS[task.status]}</span>
              {task.duration_seconds && <span className="text-[10px] shrink-0 tabular-nums" style={{ color: c.textMuted }}>{task.duration_seconds.toFixed(1)}s</span>}
              <ChevronDown className="size-3 shrink-0" style={{ color: c.textMuted, opacity: 0.5 }} />
            </div>
          ))
        )}
      </div>

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-3 border-t py-2.5" style={{ borderColor: c.line, background: c.surface }}>
          <button onClick={() => setPage(p => Math.max(1, p - 1))} disabled={page === 1} className="rounded-lg p-1.5 disabled:opacity-30 hover:bg-black/5" style={{ color: c.textMuted }}><ChevronLeft className="size-4" /></button>
          <span className="text-xs tabular-nums" style={{ color: c.textMuted }}>{page} / {totalPages}</span>
          <button onClick={() => setPage(p => Math.min(totalPages, p + 1))} disabled={page >= totalPages} className="rounded-lg p-1.5 disabled:opacity-30 hover:bg-black/5" style={{ color: c.textMuted }}><ChevronRight className="size-4" /></button>
        </div>
      )}

      {selected && (
        <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/30" onClick={() => setSelected(null)}>
          <div className="rounded-2xl border shadow-2xl w-[560px] max-w-[calc(100vw-32px)] max-h-[80vh] flex flex-col" style={{ borderColor: c.line, background: c.surface }} onClick={e => e.stopPropagation()}>
            <div className="flex items-center gap-2.5 border-b px-5 py-3.5" style={{ borderColor: c.line }}>
              <span className="size-2.5 rounded-full" style={{ background: STATUS_COLORS[selected.status] }} />
              <span className="text-sm font-semibold" style={{ color: c.text }}>{selected.title}</span>
              <span className="text-[10px] font-mono" style={{ color: c.textMuted }}>{selected.id}</span>
              <button onClick={() => setSelected(null)} className="ml-auto transition-opacity hover:opacity-70" style={{ color: c.textMuted }}><X className="size-4" /></button>
            </div>
            <div className="flex-1 overflow-y-auto p-5 space-y-4">
              <div className="grid grid-cols-3 gap-2.5">
                <Meta label={t('agent:taskQueue.metaType')} value={TYPE_LABELS[selected.type] || selected.type} />
                <Meta label={t('agent:taskQueue.metaStatus')} value={STATUS_LABELS[selected.status]} />
                <Meta label={t('agent:taskQueue.metaDuration')} value={selected.duration_seconds ? `${selected.duration_seconds.toFixed(1)}s` : '-'} />
                <Meta label={t('agent:taskQueue.metaCreated')} value={fmtTime(selected.created_at)} />
                <Meta label={t('agent:taskQueue.metaStarted')} value={selected.started_at ? fmtTime(selected.started_at) : '-'} />
                <Meta label={t('agent:taskQueue.metaFinished')} value={selected.finished_at ? fmtTime(selected.finished_at) : '-'} />
              </div>
              {selected.detail && (
                <div>
                  <div className="text-[10px] font-semibold uppercase tracking-wider mb-1.5" style={{ color: c.textMuted }}>{t('agent:taskQueue.detailLabel')}</div>
                  <pre className="text-[11px] whitespace-pre-wrap break-words rounded-xl p-3.5" style={{ background: c.surfaceAlt, color: c.text }}>{selected.detail}</pre>
                </div>
              )}
              {selected.error && (
                <div>
                  <div className="text-[10px] font-semibold uppercase tracking-wider mb-1.5" style={{ color: c.danger }}>{t('agent:taskQueue.errorLabel')}</div>
                  <pre className="text-[11px] whitespace-pre-wrap break-words rounded-xl p-3.5" style={{ background: `${c.danger}10`, color: c.danger, border: `1px solid ${c.danger}20` }}>{selected.error}</pre>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function Meta({ label, value }: { label: string; value: string }) {
  return <div className="flex items-center justify-between rounded-xl px-3.5 py-2" style={{ background: c.surfaceAlt }}>
    <span className="text-[11px]" style={{ color: c.textMuted }}>{label}</span>
    <span className="text-[11px] font-medium" style={{ color: c.text }}>{value}</span>
  </div>
}

function fmtTime(s: string): string {
  if (!s) return '-'
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export default TaskQueuePage
