import { useTranslation } from 'react-i18next'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import { tq } from './taskQueueTheme'
import { ClockGlyph } from './TaskQueueGlyphs'
import TaskRow from './TaskQueueRow'

/** 任务队列浮层面板（TaskQueueButton 的 portal 内容）：头部统计 + 任务行列表。行渲染见 TaskQueueRow。 */
export default function TaskQueuePanel() {
  const { t } = useTranslation()
  const { tasks, cancelTask, removeTask, clearFinished, openTaskDetail } = useTaskQueue()
  const running = tasks.filter(tg => tg.status === 'running' || tg.status === 'pending' || tg.status === 'queued').length
  const ok = tasks.filter(tg => tg.status === 'completed').length
  const failed = tasks.filter(tg => tg.status === 'failed').length
  const finishedCount = ok + failed + tasks.filter(tg => tg.status === 'canceled').length

  return (
    <div className="w-[420px] max-h-[560px] bg-white rounded-[14px] overflow-hidden flex flex-col"
      style={{ boxShadow: '0 0 0 1px rgba(22,26,33,.08), 0 2px 6px rgba(22,26,33,.05), 0 24px 56px -12px rgba(22,26,33,.24)' }}>
      {/* 头部：蓝底图标 + 标题 + 三段计数点 + 清空 */}
      <div className="h-[46px] px-3 pl-4 border-b flex items-center gap-2.5 shrink-0"
        style={{ borderColor: tq.line, background: 'linear-gradient(180deg,#f9fafd,#f3f6fa)' }}>
        <span className="w-[24px] h-[24px] rounded-[8px] flex items-center justify-center shrink-0"
          style={{ background: '#e3edfa', color: tq.run, boxShadow: 'inset 0 0 0 1px rgba(47,111,214,.16)' }}>
          <ClockGlyph />
        </span>
        <span className="text-[13px] font-bold tracking-tight" style={{ color: tq.ink }}>{t('taskQueue.title')}</span>
        <span className="flex items-center gap-2 ml-1 text-[10.5px] font-semibold tabular-nums" style={{ color: tq.sub }} title={t('taskQueue.counts', { running, ok, failed })}>
          <i className="w-[5px] h-[5px] rounded-full shrink-0" style={{ background: running ? tq.run : tq.faint }} />{running}
          <i className="w-[5px] h-[5px] rounded-full shrink-0" style={{ background: ok ? tq.ok : tq.faint }} />{ok}
          <i className="w-[5px] h-[5px] rounded-full shrink-0" style={{ background: failed ? tq.err : tq.faint }} />{failed}
        </span>
        <span className="flex-1" />
        <button disabled={finishedCount === 0} onClick={clearFinished}
          className="h-[24px] px-2.5 rounded-full border text-[10.5px] font-medium cursor-pointer transition-colors active:scale-[.97] disabled:opacity-40 disabled:cursor-default disabled:active:scale-100"
          style={{ borderColor: '#dfe4ec', color: tq.sub, background: '#fff' }}
          onMouseEnter={e => { if (!e.currentTarget.disabled) { e.currentTarget.style.color = tq.err; e.currentTarget.style.borderColor = '#f0c9c6'; e.currentTarget.style.background = '#fdeeef' } }}
          onMouseLeave={e => { e.currentTarget.style.color = tq.sub; e.currentTarget.style.borderColor = '#dfe4ec'; e.currentTarget.style.background = '#fff' }}>
          {t('taskQueue.clearFinished')}
        </button>
      </div>

      {/* 列表 */}
      <div className="flex-1 overflow-y-auto">
        {tasks.length === 0 ? (
          <div className="py-14 flex flex-col items-center gap-3">
            <span className="w-12 h-12 rounded-full flex items-center justify-center"
              style={{ background: '#f0f2f6', color: tq.faint, boxShadow: 'inset 0 0 0 1px rgba(22,26,33,.05)' }}>
              <ClockGlyph size={20} />
            </span>
            <div className="text-[12px]" style={{ color: tq.mute }}>{t('taskQueue.empty')}</div>
          </div>
        ) : (
          tasks.map(task => (
            <TaskRow
              key={task.id}
              task={task}
              onCancel={() => cancelTask(task.id)}
              onRemove={() => removeTask(task.id)}
              onOpenDetail={() => openTaskDetail(task.id)}
            />
          ))
        )}
      </div>
    </div>
  )
}
