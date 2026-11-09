import { useTranslation } from 'react-i18next'
import { TaskQueueItem, taskTypeLabel, taskStatusLabel, AgentStep } from '../../hooks/taskQueue/types'
import { formatBytes, timeText, targetDisplay, targetFullList, isTerminal, displayDeviceId } from './taskQueuePanelUtils'
import { barColor, pillCls, pillDotCls, tq } from './taskQueueTheme'
import Bar from './TaskQueueBar'

/** 单条任务行：状态色条 + 标题行 + 各类型进度/明细 + 操作按钮。纯展示，交互经回调上抛。 */
export default function TaskRow({ task, onCancel, onRemove, onOpenDetail }: { task: TaskQueueItem; onCancel: () => void; onRemove: () => void; onOpenDetail: () => void }) {
  const { t } = useTranslation()
  const label = t(taskTypeLabel[task.type] || task.type)
  const target = targetDisplay(task)
  const fullTargets = targetFullList(task)
  const terminal = isTerminal(task.status)
  const hasFail = task.failed > 0
  const hasDeviceProgress = task.deviceIps?.length > 0 && task.deviceProgress && Object.keys(task.deviceProgress).length > 0

  return (
    <div
      className="relative px-4 pl-[18px] py-[11px] border-b last:border-b-0 cursor-pointer group transition-colors"
      style={{ borderColor: tq.line }}
      onClick={onOpenDetail}
      onMouseEnter={e => (e.currentTarget.style.background = '#f7f9fc')}
      onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}
    >
      {/* 行首状态色条（通高圆头） */}
      <span className="absolute left-[5px] top-[10px] bottom-[9px] w-[3px] rounded-full" style={{ background: barColor(task.status, task.failed) }} />

      {/* 标题行 */}
      <div className="flex items-center gap-2">
        <span className="text-[12.5px] font-semibold shrink-0" style={{ color: tq.ink }}>{label}</span>
        {target && (
          <span className="text-xs truncate max-w-[150px] opacity-90" style={{ color: tq.run }} title={fullTargets.length > 1 ? fullTargets.join('\n') : target}>
            {target}
          </span>
        )}
        <span className={`ml-auto inline-flex items-center gap-1.5 text-[10px] font-semibold px-2 py-[2.5px] rounded-full shrink-0 ${pillCls(task.status)}`}>
          <i className={pillDotCls} />
          {t(taskStatusLabel[task.status])}
        </span>
        {task.startTime && <span className="text-[10.5px] font-mono shrink-0 tabular-nums" style={{ color: tq.mute }}>{timeText(task.startTime)}</span>}
      </div>

      {/* 设备分组进度（上传文件等） */}
      {hasDeviceProgress && (
        <div className="mt-2 flex flex-col gap-2">
          {task.deviceIps!.map(ip => {
            const dp = task.deviceProgress?.[ip]
            if (!dp) return null
            // 完成判断只看计数（云机粒度累计），不用 stage——同设备多云机时逐台完成都触发 stage='done'，不能当整组完成
            const doneCount = dp.completed + dp.failed
            const done = dp.total > 0 && doneCount >= dp.total
            const devErr = dp.failed > 0
            const pct = devErr || done ? 100 : (dp.currentProgress ?? 0)
            return (
              <div key={ip} className="flex flex-col gap-1">
                <div className="flex items-center gap-2 text-[11px]">
                  {/* 显示 IP（v2 任务里存的是序列号），进度键仍用原始标识 */}
                  <span className="font-mono shrink-0 text-[10.5px]" style={{ color: tq.sub, width: 95 }}>{displayDeviceId(ip)}</span>
                  <span className={`truncate flex-1 ${dp.stage === 'uploading' && dp.currentFile ? 'font-mono text-[10px]' : ''}`} style={{ color: dp.stage === 'uploading' && dp.currentFile ? tq.sub : tq.mute }} title={dp.currentFile}>
                    {dp.stage === 'uploading' && dp.totalBytes != null && dp.uploaded != null
                      ? `${dp.currentFile ? `${dp.currentFile} · ` : ''}${formatBytes(dp.uploaded)} / ${formatBytes(dp.totalBytes)}${dp.speed != null && dp.speed > 0 ? ` · ${dp.speed.toFixed(1)}MB/s` : ''}`
                      : dp.stage === 'installing'
                        ? `${dp.currentFile ? `${dp.currentFile} · ` : ''}${t('status.installing')}…`
                        : dp.stage === 'running'
                          ? `${t('taskQueue.processing')}…`
                          : dp.stage === 'uploading'
                            ? t('status.uploading')
                            // tebox 安装等带 stage 文案的任务：直接显示后端中文阶段 + 当前输出行（dp.error）
                            : dp.error
                              ? `${dp.stage} · ${dp.error}`
                              : `${dp.stage}…`}
                  </span>
                  <span className="shrink-0 font-semibold tabular-nums" style={{ color: tq.mute }}>{dp.total > 1 ? `${doneCount}/${dp.total}` : ''}</span>
                </div>
                <Bar pct={pct} tone={devErr ? 'err' : done ? 'ok' : dp.stage === 'installing' ? 'warn' : 'run'} />
              </div>
            )
          })}
        </div>
      )}

      {/* 通用进度（无设备分组） */}
      {!hasDeviceProgress && (
        (task.status === 'running' || task.status === 'completed' || task.progress > 0) && (
          <Bar
            pct={task.status === 'completed' ? 100 : task.progress}
            tone={task.status === 'failed' ? 'err' : task.status === 'completed' ? 'ok' : hasFail ? 'warn' : 'run'}
            indet={task.status === 'running' && task.progress === 0}
          />
        )
      )}

      {/* 计数行 */}
      {task.total > 0 && (
        <div className="mt-1.5 flex items-baseline gap-2.5 text-[11px]" style={{ color: tq.mute }}>
          <span className="font-bold text-[11.5px] tabular-nums" style={{ color: tq.ink }}>{task.completed + task.failed} / {task.total}</span>
          <span style={{ color: task.completed > 0 ? tq.ok : tq.mute }}>{t('taskQueue.row.success', { count: task.completed })}</span>
          {hasFail && <span className="font-semibold" style={{ color: tq.err }}>{t('taskQueue.row.failed', { count: task.failed })}</span>}
        </div>
      )}

      {/* 排队中提示 */}
      {task.status === 'queued' && (
        <div className="mt-1.5 text-[11px]" style={{ color: tq.mute }}>{t('taskQueue.row.queued')}</div>
      )}

      {/* 阶段序列（agent 步骤） */}
      {task.type === 'uninstallAgent' && task.agentSteps && task.agentSteps.length > 0 && (
        <div className="mt-2 flex flex-col gap-1">
          {task.agentSteps.map((step: AgentStep) => (
            <div key={step.index} className="flex items-center gap-2 text-[10.5px]">
              <span className="font-bold" style={{ color: step.status === 'success' ? tq.ok : step.status === 'failed' ? tq.err : step.status === 'running' ? tq.run : step.status === 'skipped' ? tq.mute : tq.faint }}>
                {step.status === 'success' ? '✓' : step.status === 'failed' ? '✗' : step.status === 'running' ? '●' : step.status === 'skipped' ? '—' : '○'}
              </span>
              <span className={step.status === 'success' ? 'line-through' : step.status === 'running' ? 'font-medium' : ''} style={{ color: step.status === 'failed' ? tq.err : step.status === 'running' ? tq.ink : tq.mute }}>
                {step.name}
              </span>
              {step.message && step.status === 'running' && <span className="truncate" style={{ color: tq.mute }}>{step.message}</span>}
            </div>
          ))}
        </div>
      )}

      {/* 执行步骤（cleanDisk 等） */}
      {task.type === 'cleanDisk' && task.steps && task.steps.length > 0 && (
        <div className="mt-2 max-h-[150px] overflow-auto rounded-[10px] px-2.5 py-2 font-mono text-[10.5px] space-y-0.5 leading-relaxed" style={{ background: tq.surface, border: `1px solid ${tq.line}`, color: tq.sub }}>
          {task.steps.map((step, i) => <div key={i} className="break-all">{step}</div>)}
        </div>
      )}

      {/* 创建实例的阶段 */}
      {task.type === 'create' && task.currentStep === 'image' && task.imageProgress != null && (
        <div className="mt-1.5 space-y-1">
          <span className="text-[11px]" style={{ color: tq.run }}>{t('taskQueue.progress.pushImage', { progress: Math.round(task.imageProgress) })}</span>
          <Bar pct={task.imageProgress} />
        </div>
      )}
      {task.type === 'create' && task.currentStep === 'create' && (
        <div className="mt-1.5 space-y-1">
          <div className="flex items-center justify-between text-[11px]">
            <span style={{ color: tq.ok }}>{t('taskQueue.progress.createMachine', { completed: task.completed + task.failed, total: task.total })}</span>
            <span style={{ color: tq.mute }}>{t('taskQueue.progress.successFail', { success: task.completed, failed: task.failed })}</span>
          </div>
          <Bar pct={((task.completed + task.failed) / Math.max(task.total, 1)) * 100} tone={task.failed > 0 ? 'warn' : 'ok'} />
        </div>
      )}

      {/* 类型特有补充信息 */}
      {(task.type === 'uploadImage' || task.type === 'downloadImage') && typeof task.imageName === 'string' && (
        <div className="mt-0.5 text-[11px]" style={{ color: tq.ok }}>{t('taskQueue.progress.imageName', { name: task.imageName as string })}</div>
      )}
      {task.type === 'uploadFile' && (
        <div className="mt-0.5 text-[11px]" style={{ color: tq.ok }}>{t('taskQueue.progress.fileCount', { count: Number(task.fileCount || 0), machines: Number(task.machineCount || 0) })}</div>
      )}
      {task.type === 'importAndroid' && task.deviceIps?.length > 0 && (
        <div className="mt-0.5 text-[11px]" style={{ color: tq.ok }}>{t('taskQueue.progress.targetDevice', { ip: task.deviceIps[0] })}</div>
      )}
      {task.type === 'exportAndroid' && task.exportStage && task.exportStage !== 'done' && (
        <div className="mt-1.5 space-y-0.5">
          <div className="text-[11px]" style={{ color: tq.run }}>
            {task.exportStage === 'downloading' ? t('status.downloading', { progress: task.exportProgress ?? 0 }) : (task.exportMsg || t('status.exporting'))}
          </div>
          {task.exportStage === 'downloading' && <Bar pct={task.exportProgress ?? 0} />}
        </div>
      )}
      {task.type === 'importAndroid' && task.importMsg && task.status === 'running' && (
        <div className="mt-0.5 text-[11px]" style={{ color: tq.run }}>{task.importMsg}</div>
      )}
      {task.type === 'moveAndroid' && task.moveStage && task.moveStage !== 'done' && (
        <div className="mt-0.5 text-[11px]" style={{ color: tq.run }}>{task.moveMessage || task.moveStage}</div>
      )}

      {/* 复制日志 */}
      {task.type === 'copy' && task.copyLogs && task.copyLogs.length > 0 && (
        <div className="mt-2 max-h-[160px] overflow-auto rounded-[10px] px-2.5 py-2 space-y-0.5" style={{ background: tq.surface, border: `1px solid ${tq.line}` }}>
          {task.copyLogs.map((log, i) => (
            <div key={i} className="flex items-center gap-1 text-[11px] font-mono">
              <span className="shrink-0" style={{ color: tq.mute }}>[{log.current}/{log.total}]</span>
              <span className="truncate flex-1" style={{ color: tq.sub }} title={log.name}>{log.name}</span>
              <span className="shrink-0 w-[18px] h-[15px] rounded text-[10px] font-bold text-center" style={{ color: log.status === 'success' ? tq.ok : log.status === 'failed' ? tq.err : tq.mute, background: log.status === 'success' ? '#e9f5ee' : log.status === 'failed' ? '#fdeeef' : '#eef1f6' }}>
                {log.status === 'success' ? '✓' : log.status === 'failed' ? '✗' : '…'}
              </span>
              <span className="truncate max-w-[120px] shrink-0" style={{ color: tq.mute }} title={log.message}>{log.message}</span>
            </div>
          ))}
        </div>
      )}

      {/* 失败明细 */}
      {task.failed > 0 && task.failedTargets?.length > 0 && (
        <div className="mt-2 rounded-[10px] px-2.5 py-1.5 text-[10.5px] flex flex-col gap-0.5" style={{ background: '#fdeeef', border: '1px solid #f5d9d9', color: tq.err }}>
          <div className="font-semibold flex justify-between" style={{ color: tq.sub }}>
            <span>{t('taskQueue.failure.reason')}</span>
            <span className="font-normal cursor-pointer" style={{ color: tq.mute }} onClick={(e) => { e.stopPropagation(); onOpenDetail() }}>
              {t('taskQueue.failure.expandAll', { count: task.failedTargets.length })}
            </span>
          </div>
          {task.type === 'create' ? (
            task.failedTargets.slice(0, 3).map((ft, i) => (
              <div key={i} className="font-mono break-all" title={ft.error || undefined}>{t('taskQueue.failure.slotError', { slot: ft.slot, error: ft.error || t('taskQueue.failure.unknownError') })}</div>
            ))
          ) : task.type === 'uploadFile' ? (
            task.failedTargets.slice(0, 3).map((ft, i) => (
              <div key={i} className="font-mono break-all" title={ft.error || undefined}>{ft.filePath ? String(ft.filePath).split('\\').pop() : t('taskQueue.failure.unknownFile')} -&gt; {ft.machineName || t('taskQueue.failure.unknownMachine')}: {ft.error || t('taskQueue.failure.unknownError')}</div>
            ))
          ) : (
            task.failedTargets.slice(0, 3).map((ft, i) => (
              <div key={i} className="font-mono break-all" title={ft.error || undefined}>{ft.machineName ? `${displayDeviceId(ft.deviceIp) || t('taskQueue.failure.unknownDevice')} ${ft.machineName}` : displayDeviceId(ft.deviceIp) || t('taskQueue.failure.unknownDevice')}: {ft.error || t('taskQueue.failure.unknownError')}</div>
            ))
          )}
          {task.failedTargets.length > 3 && <div style={{ color: tq.mute }}>{t('taskQueue.failure.moreRecords', { count: task.failedTargets.length - 3 })}</div>}
        </div>
      )}

      {/* 操作按钮 */}
      <div className="mt-2 flex gap-1.5">
        {(task.status === 'running' || task.status === 'queued' || task.status === 'pending') && (
          <button onClick={(e) => { e.stopPropagation(); onCancel() }}
            className="h-[22px] px-2.5 rounded-full border text-[10.5px] font-medium cursor-pointer transition-colors active:scale-[.97]"
            style={{ borderColor: '#f0c9c6', color: tq.err, background: '#fff' }}
            onMouseEnter={e => { e.currentTarget.style.background = tq.err; e.currentTarget.style.color = '#fff'; e.currentTarget.style.borderColor = tq.err }}
            onMouseLeave={e => { e.currentTarget.style.background = '#fff'; e.currentTarget.style.color = tq.err; e.currentTarget.style.borderColor = '#f0c9c6' }}>
            {t('button.cancel')}
          </button>
        )}
        {terminal && (
          <button onClick={(e) => { e.stopPropagation(); onRemove() }}
            className="h-[22px] px-2.5 rounded-full border text-[10.5px] font-medium cursor-pointer transition-colors active:scale-[.97]"
            style={{ borderColor: '#dfe4ec', color: tq.sub, background: '#fff' }}
            onMouseEnter={e => { e.currentTarget.style.background = '#f0f2f6'; e.currentTarget.style.color = tq.ink }}
            onMouseLeave={e => { e.currentTarget.style.background = '#fff'; e.currentTarget.style.color = tq.sub }}>
            {t('button.remove')}
          </button>
        )}
      </div>
    </div>
  )
}
