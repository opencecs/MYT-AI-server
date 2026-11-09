import { useState, useCallback,  useRef} from 'react'
import { X, Clock, Calendar, Repeat, Bell, Bot } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import { tasksApi, type TaskItem } from './agentApi'
import type { AgentWsClient } from './agentWs'
import { useModalEnter } from '../../hooks/useModalAnimation'

interface Props {
  client: AgentWsClient | null
  onClose: () => void
  onCreated: () => void
  onAlert: (msg: string) => void
}

const CRON_PRESETS = [
  { labelKey: 'agent:taskForm.cronPresets.everyMinute', value: '* * * * *' },
  { labelKey: 'agent:taskForm.cronPresets.every30Min', value: '*/30 * * * *' },
  { labelKey: 'agent:taskForm.cronPresets.everyHour', value: '0 * * * *' },
  { labelKey: 'agent:taskForm.cronPresets.daily9', value: '0 9 * * *' },
  { labelKey: 'agent:taskForm.cronPresets.weeklyMon9', value: '0 9 * * 1' },
  { labelKey: 'agent:taskForm.cronPresets.monthly1st', value: '0 0 1 * *' },
]

function TaskFormDialog({ client, onClose, onCreated, onAlert }: Props) {
  const { t } = useTranslation()
  const [desc, setDesc] = useState('')
  const [taskType, setTaskType] = useState<'autonomous' | 'notification'>('autonomous')
  const [mode, setMode] = useState<'cron' | 'once'>('cron')
  const [cronExpr, setCronExpr] = useState('0 * * * *')
  const [runOnceAt, setRunOnceAt] = useState('')
  const [creating, setCreating] = useState(false)

  const handleCreate = useCallback(async () => {
    if (!desc.trim() || !client) return
    if (mode === 'cron' && !cronExpr.trim()) { onAlert(t('agent:taskForm.toast.needCron')); return }
    if (mode === 'once' && !runOnceAt) { onAlert(t('agent:taskForm.toast.needTime')); return }
    setCreating(true)
    try {
      await tasksApi.create(client, {
        description: desc.trim(),
        task_type: taskType,
        cron_expr: mode === 'cron' ? cronExpr.trim() : undefined,
        run_once_at: mode === 'once' ? new Date(runOnceAt).toISOString() : undefined,
      })
      onCreated()
      onClose()
    } catch (e) { onAlert(t('agent:taskForm.toast.createFailed', { error: e })) }
    setCreating(false)
  }, [desc, client, mode, cronExpr, runOnceAt, taskType, onAlert, onCreated, onClose, t])

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="rounded-xl border shadow-xl w-[460px] max-w-[calc(100vw-32px)] max-h-[80vh] flex flex-col modal-content" style={{ borderColor: c.line, background: c.surface }} onClick={e => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b px-5 py-3" style={{ borderColor: c.line }}>
          <Clock className="size-4" style={{ color: c.accent }} />
          <span className="text-sm font-semibold" style={{ color: c.text }}>{t('agent:taskForm.title')}</span>
          <button onClick={onClose} className="ml-auto" style={{ color: c.textMuted }}><X className="size-4" /></button>
        </div>
        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          {/* 描述 */}
          <div>
            <label className="text-[11px] font-semibold uppercase tracking-wider mb-1.5 block" style={{ color: c.textMuted }}>{t('agent:taskForm.descLabel')}</label>
            <textarea value={desc} onChange={e => setDesc(e.target.value)} rows={3} placeholder={t('agent:taskForm.descPlaceholder')}
              className="w-full rounded-lg border px-3 py-2 text-sm outline-none focus:border-blue-400 resize-none"
              style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
          </div>
          {/* 类型 */}
          <div>
            <label className="text-[11px] font-semibold uppercase tracking-wider mb-1.5 block" style={{ color: c.textMuted }}>{t('agent:taskForm.typeLabel')}</label>
            <div className="grid grid-cols-2 gap-2">
              <TypeOption active={taskType === 'autonomous'} onClick={() => setTaskType('autonomous')} icon={<Bot className="size-4" />} label={t('agent:taskForm.typeAutonomous')} desc={t('agent:taskForm.typeAutonomousDesc')} />
              <TypeOption active={taskType === 'notification'} onClick={() => setTaskType('notification')} icon={<Bell className="size-4" />} label={t('agent:taskForm.typeNotification')} desc={t('agent:taskForm.typeNotificationDesc')} />
            </div>
          </div>
          {/* 模式 */}
          <div>
            <label className="text-[11px] font-semibold uppercase tracking-wider mb-1.5 block" style={{ color: c.textMuted }}>{t('agent:taskForm.modeLabel')}</label>
            <div className="grid grid-cols-2 gap-2">
              <TypeOption active={mode === 'cron'} onClick={() => setMode('cron')} icon={<Repeat className="size-4" />} label={t('agent:taskForm.modeCron')} desc={t('agent:taskForm.modeCronDesc')} />
              <TypeOption active={mode === 'once'} onClick={() => setMode('once')} icon={<Calendar className="size-4" />} label={t('agent:taskForm.modeOnce')} desc={t('agent:taskForm.modeOnceDesc')} />
            </div>
          </div>
          {/* Cron */}
          {mode === 'cron' && (
            <div>
              <label className="text-[11px] font-semibold uppercase tracking-wider mb-1.5 block" style={{ color: c.textMuted }}>{t('agent:taskForm.cronLabel')}</label>
              <input value={cronExpr} onChange={e => setCronExpr(e.target.value)} placeholder="0 * * * *"
                className="w-full rounded-lg border px-3 py-2 text-sm font-mono outline-none focus:border-blue-400"
                style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
              <div className="flex flex-wrap gap-1 mt-2">
                {CRON_PRESETS.map(p => (
                  <button key={p.value} onClick={() => setCronExpr(p.value)} className="rounded px-2 py-0.5 text-[10px] hover:bg-black/5" style={{ background: c.surfaceAlt, color: c.textMuted }}>{t(p.labelKey)}</button>
                ))}
              </div>
            </div>
          )}
          {/* 单次时间 */}
          {mode === 'once' && (
            <div>
              <label className="text-[11px] font-semibold uppercase tracking-wider mb-1.5 block" style={{ color: c.textMuted }}>{t('agent:taskForm.execTimeLabel')}</label>
              <input type="datetime-local" value={runOnceAt} onChange={e => setRunOnceAt(e.target.value)}
                className="w-full rounded-lg border px-3 py-2 text-sm outline-none focus:border-blue-400"
                style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
            </div>
          )}
        </div>
        <div className="flex justify-end gap-2 border-t px-5 py-3" style={{ borderColor: c.line }}>
          <button onClick={onClose} className="px-4 py-1.5 text-xs rounded-lg hover:bg-black/5" style={{ color: c.textMuted }}>{t('button.cancel')}</button>
          <button onClick={handleCreate} disabled={!desc.trim() || creating} className="px-4 py-1.5 text-xs rounded-lg text-white disabled:opacity-40" style={{ background: c.accent }}>
            {creating ? t('agent:taskForm.creating') : t('button.create')}
          </button>
        </div>
      </div>
    </div>
  )
}

function TypeOption({ active, onClick, icon, label, desc }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string; desc: string }) {
  return (
    <button onClick={onClick} className="flex items-center gap-2 rounded-lg border p-2.5 text-left transition-colors"
      style={{ borderColor: active ? c.accent : c.line, background: active ? `${c.accent}08` : c.surface }}>
      <span style={{ color: active ? c.accent : c.textMuted }}>{icon}</span>
      <div>
        <div className="text-xs font-medium" style={{ color: active ? c.accent : c.text }}>{label}</div>
        <div className="text-[10px]" style={{ color: c.textMuted }}>{desc}</div>
      </div>
    </button>
  )
}

export default TaskFormDialog
export type { TaskItem }
