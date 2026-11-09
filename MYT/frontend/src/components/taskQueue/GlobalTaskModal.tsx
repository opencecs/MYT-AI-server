import { useState, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { X, Copy, Check, Ban } from 'lucide-react'
import { useModalEnter, animateModalOut } from '../../hooks/useModalAnimation'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import { taskTypeLabel, taskStatusLabel, type TaskQueueItem } from '../../hooks/taskQueue/types'
import { targetDisplay, displayDeviceId } from './taskQueuePanelUtils'
import { pillCls, pillDotCls } from './taskQueueTheme'
import { cast } from '../../utils/cast'

function timeText(ts: number | null): string {
  if (!ts) return '-'
  return new Date(ts).toLocaleString()
}

function duration(start: number | null, end: number | null): string {
  if (!start) return '-'
  const ms = (end ?? Date.now()) - start
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60000)}m${Math.floor((ms % 60000) / 1000)}s`
}

/**
 * 全局任务详情弹窗（App 层渲染，毛玻璃拟态遮罩居中；不依赖队列面板开关）。
 * 统一弹窗骨架：46px 头部（surface-2 + 上下文副行 + 状态 pill）、
 * 统计数字条、失败明细、执行步骤/传输日志（等宽）、底部操作条（取消任务 / 复制结果）。
 */
export function GlobalTaskModal() {
  const { t } = useTranslation()
  const { detailTaskId, closeTaskDetail, tasks, cancelTask } = useTaskQueue()
  const rootRef = useRef<HTMLDivElement>(null)
  const closingRef = useRef(false)
  const [copied, setCopied] = useState(false)
  useModalEnter(rootRef)

  // 关闭走退场动画（× / 遮罩共用；closing 防重入）
  const requestClose = useCallback(() => {
    if (closingRef.current) return
    closingRef.current = true
    animateModalOut(rootRef.current, () => { closingRef.current = false; closeTaskDetail() })
  }, [closeTaskDetail])

  const task = detailTaskId ? tasks.find(t2 => t2.id === detailTaskId) : undefined
  if (!detailTaskId || !task) return null

  const label = t(taskTypeLabel[task.type] || task.type)
  const target = targetDisplay(task)
  const savedPath = cast<Record<string, unknown>>(task).savedPath
  const isRunning = task.status === 'running' || task.status === 'pending' || task.status === 'queued'

  const buildResultText = (task: TaskQueueItem): string => {
    const lines: string[] = []
    lines.push(`任务: ${t(taskTypeLabel[task.type] || task.type)}`)
    lines.push(`状态: ${t(taskStatusLabel[task.status])}`)
    lines.push(`总数: ${task.total}  成功: ${task.completed}  失败: ${task.failed}`)
    if (task.startTime) lines.push(`开始: ${timeText(task.startTime)}`)
    if (task.endTime) lines.push(`结束: ${timeText(task.endTime)}`)
    if (task.startTime) lines.push(`耗时: ${duration(task.startTime, task.endTime)}`)
    if (task.error) lines.push(`错误: ${task.error}`)
    if (task.failedTargets?.length) {
      lines.push('', '失败详情:')
      task.failedTargets.forEach(ft => {
        // 显示 IP（v2 任务里存的是序列号）
        const ipText = displayDeviceId(ft.deviceIp)
        const ftTarget = ft.machineName ? `${ipText}/${ft.machineName}` : ipText
        const slot = ft.slot != null ? ` 槽位${ft.slot}` : ''
        lines.push(`  ${ftTarget}${slot}: ${ft.error}`)
      })
    }
    if (task.copyLogs?.length) {
      lines.push('', '复制日志:')
      task.copyLogs.forEach(l => lines.push(`  ${l.message}`))
    }
    if (task.agentSteps?.length) {
      lines.push('', '智能体步骤:')
      task.agentSteps.forEach(s => lines.push(`  ${s.name}: ${s.status}${s.message ? ' - ' + s.message : ''}`))
    }
    if (task.steps?.length) {
      lines.push('', '执行步骤:')
      task.steps.forEach((s, i) => lines.push(`  ${i + 1}. ${s}`))
    }
    if (savedPath) lines.push(`保存路径: ${savedPath}`)
    return lines.join('\n')
  }

  const handleCopy = async () => {
    const text = buildResultText(task)
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      const ta = document.createElement('textarea')
      ta.value = text
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div
      ref={rootRef}
      className="fixed inset-0 z-[80] flex items-center justify-center bg-black/30 backdrop-blur-sm modal-overlay"
      onClick={requestClose}
    >
      <div
        className="bg-white rounded-xl shadow-[0_0_0_1px_rgba(22,30,46,.06),0_2px_4px_rgba(22,30,46,.05),0_24px_64px_-12px_rgba(22,30,46,.24)] w-[540px] max-w-[calc(100vw-32px)] max-h-[78vh] flex flex-col modal-content overflow-hidden"
        role="dialog" aria-modal="true" aria-labelledby="task-detail-title"
        onClick={e => e.stopPropagation()}
      >
        {/* 头部：46px surface-2 + 上下文副行 + 状态 pill */}
        <div className="h-[46px] px-4 flex items-center gap-2.5 border-b border-[#e6eaf1] bg-[#f7f9fc] shrink-0">
          <div className="min-w-0">
            <div id="task-detail-title" className="text-[13px] font-bold text-[#16181d] leading-tight truncate">{label}</div>
            <div className="text-[10.5px] text-[#8f99a9] leading-tight truncate">{target || task.deviceIps?.[0] || '-'}</div>
          </div>
          <span className={`ml-auto inline-flex items-center gap-1.5 text-[10px] font-semibold px-2 py-[3px] rounded-full shrink-0 ${pillCls(task.status)}`}>
            <i className={pillDotCls} />
            {t(taskStatusLabel[task.status])}
          </span>
          <button onClick={requestClose} className="text-[#8f99a9] hover:text-[#16181d] hover:bg-[#f0f2f6] w-7 h-7 flex items-center justify-center rounded-lg shrink-0">
            <X size={15} />
          </button>
        </div>

        {/* 内容 */}
        <div className="flex-1 overflow-y-auto px-4 py-4 space-y-3.5">
          {/* 统计数字条 */}
          <div className="flex items-stretch border border-[#e6eaf1] rounded-[10px] overflow-hidden bg-[#f7f9fc]">
            <div className="flex-1 px-3.5 py-2.5 border-r border-[#e6eaf1]">
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider">{t('taskQueue.detail.total')}</div>
              <div className="text-[17px] font-bold tabular-nums text-[#16181d] mt-0.5">{task.total}</div>
            </div>
            <div className="flex-1 px-3.5 py-2.5 border-r border-[#e6eaf1]">
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider">{t('taskQueue.detail.success')}</div>
              <div className="text-[17px] font-bold tabular-nums text-[#1e8e52] mt-0.5">{task.completed}</div>
            </div>
            <div className="flex-1 px-3.5 py-2.5 border-r border-[#e6eaf1]">
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider">{t('taskQueue.detail.failed')}</div>
              <div className={`text-[17px] font-bold tabular-nums mt-0.5 ${task.failed > 0 ? 'text-[#d4483f]' : 'text-[#16181d]'}`}>{task.failed}</div>
            </div>
            <div className="flex-1 px-3.5 py-2.5 bg-white">
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider">{t('taskQueue.detail.duration')}</div>
              <div className="text-[14px] font-bold tabular-nums text-[#16181d] mt-1">{duration(task.startTime, task.endTime)}</div>
            </div>
          </div>

          {/* 时间行 */}
          <div className="flex gap-5 text-[10.5px] text-[#8f99a9] font-mono flex-wrap">
            {task.startTime && <span>{t('taskQueue.detail.start', { time: timeText(task.startTime) })}</span>}
            {task.endTime && <span>{t('taskQueue.detail.end', { time: timeText(task.endTime) })}</span>}
          </div>

          {/* 错误 */}
          {!!task.error && (
            <div className="bg-[#fdeeef] border border-[#f5d9d9] rounded-lg px-3 py-2 text-[11px] text-[#d4483f] break-all">
              {String(task.error)}
            </div>
          )}

          {/* 失败明细 */}
          {task.failedTargets?.length > 0 && (
            <section>
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider mb-1.5 flex items-center gap-1.5">
                {t('taskQueue.detail.failedTargets')}
                <span className="font-normal normal-case tracking-normal text-[#8f99a9]">{task.failedTargets.length} / {task.total}</span>
              </div>
              <div className="flex flex-col gap-1.5">
                {task.failedTargets.map((ft, i) => (
                  <div key={i} className="flex flex-wrap items-baseline gap-x-2.5 gap-y-0.5 text-[11px] px-2.5 py-1.5 rounded-lg bg-[#fdeeef] border border-[#f5d9d9]">
                    <span className="font-mono text-[10.5px] text-[#5a6474] shrink-0">
                      {/* 显示 IP（v2 任务里存的是序列号） */}
                      {ft.machineName ? `${displayDeviceId(ft.deviceIp)} / ${ft.machineName}` : displayDeviceId(ft.deviceIp)}
                      {ft.slot != null ? ` ${t('taskQueue.progress.slot', { slot: ft.slot })}` : ''}
                    </span>
                    <span className="text-[#d4483f] break-all min-w-0 flex-1">{String(ft.error)}</span>
                  </div>
                ))}
              </div>
            </section>
          )}

          {/* 复制日志 */}
          {task.copyLogs && task.copyLogs.length > 0 && (
            <section>
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider mb-1.5">{t('taskQueue.detail.logs')}</div>
              <div className="bg-[#f7f9fc] border border-[#e6eaf1] rounded-[9px] px-2.5 py-2 font-mono text-[10.5px] text-[#5a6474] max-h-40 overflow-y-auto space-y-0.5 leading-relaxed">
                {task.copyLogs.map((l, i) => (
                  <div key={i} className="truncate"><span className="text-[#8f99a9]">[{l.current}/{l.total}]</span> {l.name} {l.message}</div>
                ))}
              </div>
            </section>
          )}

          {/* 智能体步骤 */}
          {task.agentSteps && task.agentSteps.length > 0 && (
            <section>
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider mb-1.5">{t('taskQueue.detail.agentSteps')}</div>
              <div className="flex flex-col gap-1">
                {task.agentSteps.map((s, i) => (
                  <div key={i} className="flex items-center gap-2 text-[11px]">
                    <span className={s.status === 'success' ? 'text-[#1e8e52] font-bold' : s.status === 'failed' ? 'text-[#d4483f] font-bold' : s.status === 'running' ? 'text-[#2f6fd6] font-bold' : 'text-[#8f99a9] font-bold'}>●</span>
                    <span className={s.status === 'success' ? 'text-[#8f99a9] line-through' : s.status === 'failed' ? 'text-[#d4483f]' : s.status === 'running' ? 'text-[#16181d] font-medium' : 'text-[#8f99a9]'}>{s.name}</span>
                    {s.message && s.status === 'running' && <span className="text-[#8f99a9] truncate">{s.message}</span>}
                  </div>
                ))}
              </div>
            </section>
          )}

          {/* 执行步骤 */}
          {task.steps && task.steps.length > 0 && (
            <section>
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider mb-1.5">{t('taskQueue.detail.steps')}</div>
              <div className="bg-[#f7f9fc] border border-[#e6eaf1] rounded-[9px] px-2.5 py-2 font-mono text-[10.5px] text-[#5a6474] space-y-0.5 leading-relaxed">
                {task.steps.map((s, i) => (
                  <div key={i} className={task.currentStepIndex === i ? 'text-[#2f6fd6] font-semibold' : ''}>
                    <span className="text-[#8f99a9]">[{i + 1}/{task.steps!.length}]</span> {s}
                  </div>
                ))}
              </div>
            </section>
          )}

          {/* 保存路径 */}
          {!!savedPath && (
            <section>
              <div className="text-[9.5px] font-bold text-[#8f99a9] uppercase tracking-wider mb-1.5">{t('taskQueue.detail.savedPath')}</div>
              <div className="bg-[#f7f9fc] border border-[#e6eaf1] rounded-[9px] px-2.5 py-2 font-mono text-[10.5px] text-[#5a6474] break-all">{String(savedPath)}</div>
            </section>
          )}
        </div>

        {/* 底部操作条 */}
        <div className="px-4 py-3 border-t border-[#e6eaf1] flex items-center gap-2 bg-[#f7f9fc] shrink-0">
          {isRunning && (
            <button
              onClick={() => cancelTask(task.id)}
              className="h-[30px] px-4 rounded-lg text-xs font-semibold text-white bg-[#d4483f] shadow-[0_1px_0_rgba(255,255,255,.18)_inset,0_2px_8px_rgba(212,72,63,.28)] hover:brightness-110 active:scale-[.98] flex items-center gap-1.5 cursor-pointer"
            >
              <Ban size={13} />{t('taskQueue.detail.cancelTask')}
            </button>
          )}
          <span className="flex-1" />
          <button onClick={handleCopy}
            className="h-[30px] px-3.5 rounded-lg text-xs font-semibold text-[#2f6fd6] bg-[#e9f1fc] border border-[#c4d9f4] hover:border-[#2f6fd6] active:scale-[.98] flex items-center gap-1.5 cursor-pointer">
            {copied ? <Check size={13} /> : <Copy size={13} />}
            {copied ? t('taskQueue.detail.copied') : t('taskQueue.detail.copyResult')}
          </button>
        </div>
      </div>
    </div>
  )
}

export default GlobalTaskModal
