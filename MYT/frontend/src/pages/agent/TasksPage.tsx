import { useState, useEffect, useCallback, useRef } from 'react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import { Clock, Plus, Trash2, Play, Pause, Bell, Bot, History, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import i18n from '../../i18n'
import { agentC as c } from './agentTheme'
import { tasksApi, type TaskItem } from './agentApi'
import type { AgentWsClient } from './agentWs'
import TaskFormDialog from './TaskFormDialog'
import ExecutionHistoryModal from './ExecutionHistoryModal'

gsap.registerPlugin(useGSAP)

interface Props {
  client: AgentWsClient | null
  onConfirm: (msg: string) => Promise<boolean>
  onAlert: (msg: string) => void
}

const STATUS_COLORS: Record<string, string> = { active: c.success, paused: c.warn, done: c.textMuted, failed: c.danger, deleted: c.textMuted }

function TasksPage({ client, onConfirm, onAlert }: Props) {
  const { t } = useTranslation()
  const STATUS_LABELS: Record<string, string> = { active: t('agent:tasks.statusActive'), paused: t('agent:tasks.statusPaused'), done: t('agent:tasks.statusDone'), failed: t('agent:tasks.statusFailed'), deleted: t('agent:tasks.statusDeleted') }
  const [tasks, setTasks] = useState<TaskItem[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [historyTitle, setHistoryTitle] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    if (!client) return
    setLoading(true)
    try { setTasks(await tasksApi.list(client) || []) } catch { /* */ }
    setLoading(false)
  }, [client])

  useEffect(() => { load() }, [load])

  useGSAP(() => {
    if (loading || tasks.length === 0) return
    gsap.fromTo('.task-card', { x: -16, opacity: 0 }, { x: 0, opacity: 1, duration: 0.3, stagger: 0.05, ease: 'power2.out' })
  }, { dependencies: [tasks, loading], scope: containerRef })

  const handleDelete = async (id: string, desc: string) => {
    if (!await onConfirm(t('agent:tasks.confirm.delete', { desc }))) return
    try { await tasksApi.delete(client!, id); await load() } catch (e) { onAlert(t('agent:tasks.toast.deleteFailed', { error: e })) }
  }

  const toggleStatus = async (task: TaskItem) => {
    const next = task.status === 'active' ? 'paused' : 'active'
    try { await tasksApi.updateStatus(client!, task.id, next); await load() } catch (e) { onAlert(t('agent:tasks.toast.operationFailed', { error: e })) }
  }

  const stats = { active: 0, paused: 0, total: tasks.length }
  tasks.forEach(t => { if (t.status === 'active') stats.active++; else if (t.status === 'paused') stats.paused++ })

  return (
    <div ref={containerRef} className="h-full flex flex-col overflow-hidden" style={{ background: c.bg }}>
      <div className="flex items-center gap-3 border-b px-5 py-3" style={{ borderColor: c.line, background: c.surface }}>
        <Clock className="size-4" style={{ color: c.accent }} />
        <span className="text-sm font-semibold" style={{ color: c.text }}>{t('agent:tasks.title')}</span>
        <div className="flex items-center gap-2 ml-2">
          <span className="flex items-center gap-1.5 text-[10px] px-2.5 py-1 rounded-full" style={{ background: `${c.success}12`, color: c.success }}>
            <span className="size-1.5 rounded-full" style={{ background: c.success }} />{t('agent:tasks.statActive', { count: stats.active })}
          </span>
          <span className="flex items-center gap-1.5 text-[10px] px-2.5 py-1 rounded-full" style={{ background: `${c.warn}12`, color: c.warn }}>
            <span className="size-1.5 rounded-full" style={{ background: c.warn }} />{t('agent:tasks.statPaused', { count: stats.paused })}
          </span>
        </div>
        <button onClick={() => setShowForm(true)} className="ml-auto flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs text-white transition-opacity hover:opacity-90" style={{ background: c.accent }}>
          <Plus className="size-3.5" />{t('agent:tasks.newTask')}
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-5">
        {loading ? <div className="py-20 text-center"><Loader2 className="mx-auto size-6 animate-spin" style={{ color: c.accent }} /></div>
          : tasks.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-3">
              <div className="flex size-14 items-center justify-center rounded-2xl" style={{ background: `${c.accent}10`, border: `1px solid ${c.accent}20` }}>
                <Clock className="size-7" style={{ color: c.accent, opacity: 0.5 }} />
              </div>
              <div className="text-center">
                <div className="text-sm font-medium" style={{ color: c.text }}>{t('agent:tasks.empty')}</div>
                <div className="mt-1 text-xs" style={{ color: c.textMuted }}>{t('agent:tasks.emptyHint')}</div>
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
              {tasks.map(task => (
                <div key={task.id} className="task-card rounded-2xl border p-4 transition-all hover:shadow-sm" style={{ borderColor: c.line, background: c.surface }}>
                  <div className="flex items-start gap-3">
                    <span className="size-2.5 rounded-full mt-1.5 shrink-0" style={{ background: STATUS_COLORS[task.status] }} />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <span className="text-sm font-medium" style={{ color: c.text }}>{task.description}</span>
                        <span className="flex items-center gap-1 text-[10px] px-2 py-0.5 rounded-full" style={{ background: c.surfaceAlt, color: c.textMuted }}>
                          {task.task_type === 'autonomous' ? <Bot className="size-2.5" /> : <Bell className="size-2.5" />}
                          {task.task_type === 'autonomous' ? t('agent:tasks.typeAutonomous') : t('agent:tasks.typeNotification')}
                        </span>
                      </div>
                      <div className="flex items-center gap-4 text-[11px]" style={{ color: c.textMuted }}>
                        {task.cron_expr && <span>{t('agent:tasks.cycle')} <code className="font-mono px-1 py-0.5 rounded" style={{ background: c.surfaceAlt }}>{task.cron_expr}</code></span>}
                        {task.run_once_at && <span>{t('agent:tasks.execute')} {new Date(task.run_once_at).toLocaleString(i18n.language)}</span>}
                        {task.last_fired && <span>{t('agent:tasks.lastFired')} {new Date(task.last_fired).toLocaleString(i18n.language)}</span>}
                        <span style={{ color: STATUS_COLORS[task.status] }}>{STATUS_LABELS[task.status]}</span>
                      </div>
                    </div>
                    <div className="flex items-center gap-1 shrink-0">
                      <button onClick={() => setHistoryTitle(task.description)} title={t('agent:tooltip.execHistory')} className="rounded-lg p-1.5 transition-colors hover:bg-black/5" style={{ color: c.textMuted }}><History className="size-4" /></button>
                      {(task.status === 'active' || task.status === 'paused') && (
                        <button onClick={() => toggleStatus(task)} title={task.status === 'active' ? t('agent:tooltip.pause') : t('agent:tooltip.resume')} className="rounded-lg p-1.5 transition-colors hover:bg-black/5" style={{ color: c.textMuted }}>
                          {task.status === 'active' ? <Pause className="size-4" /> : <Play className="size-4" />}
                        </button>
                      )}
                      <button onClick={() => handleDelete(task.id, task.description)} title={t('agent:tooltip.delete')} className="rounded-lg p-1.5 transition-colors hover:bg-red-500/10" style={{ color: c.danger }}><Trash2 className="size-4" /></button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
      </div>

      {showForm && <TaskFormDialog client={client} onClose={() => setShowForm(false)} onCreated={load} onAlert={onAlert} />}
      {historyTitle && <ExecutionHistoryModal client={client} title={historyTitle} onClose={() => setHistoryTitle(null)} />}
    </div>
  )
}

export default TasksPage
