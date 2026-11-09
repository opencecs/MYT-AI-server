import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Upload, Download, ChevronDown, ChevronRight, X, CheckCircle2, XCircle } from 'lucide-react'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'
import type { DeviceProgress, TaskQueueItem } from '../../../hooks/taskQueue/types'

/** 弹窗内传输进度区：进行中显示汇总进度条 + 当前文件，终态显示成功/失败汇总与失败明细 */

/** 判断任务是否终态 */
function isTerminal(status: TaskQueueItem['status']): boolean {
  return status === 'completed' || status === 'failed' || status === 'canceled'
}

/** 单任务展示状态：running / done */
type DisplayState = 'running' | 'done'

interface DisplayTask {
  task: TaskQueueItem
  state: DisplayState
  /** 传输方向：up=本机→云机，down=云机→本机 */
  dir: 'up' | 'down'
  dp: DeviceProgress | undefined
  /** 本设备失败明细 */
  failures: Array<{ filePath?: string; error: string }>
}

/** 过滤与归一化：fileManager 相关任务（uploadFile / 文件管理触发的 downloadImage），只看本设备；
 * 终态任务只保留最近结束的一个（完整历史在任务队列面板看） */
function buildDisplayTasks(tasks: TaskQueueItem[], deviceIP: string): DisplayTask[] {
  const running: DisplayTask[] = []
  let lastDone: DisplayTask | null = null
  for (const task of tasks ?? []) {
    const isUpload = task.type === 'uploadFile'
    const isDownload = task.type === 'downloadImage' && task.fileManagerDownload === true
    if (!isUpload && !isDownload) continue
    if (!(task.deviceIps ?? []).includes(deviceIP)) continue
    const dir: 'up' | 'down' = isUpload ? 'up' : 'down'
    // deviceIps/failedTargets/deviceProgress 类型上非空，但后端/持久化 JSON 可能给出 null，访问处统一防御
    const dp = task.deviceProgress?.[deviceIP]
    const failures = (task.failedTargets ?? [])
      .filter(ft => ft.deviceIp === deviceIP)
      .map(ft => ({ filePath: ft.filePath as string | undefined, error: ft.error }))
    const item: DisplayTask = { task, state: isTerminal(task.status) ? 'done' : 'running', dir, dp, failures }
    if (item.state === 'running') running.push(item)
    else if (!lastDone || (task.endTime ?? 0) > (lastDone.task.endTime ?? 0)) lastDone = item
  }
  return lastDone ? [...running, lastDone] : running
}

function FileManagerProgress({ deviceIP }: { deviceIP: string }) {
  const { t } = useTranslation()
  const { tasks, removeTask } = useTaskQueue()
  const [expanded, setExpanded] = useState(false)

  const displayTasks = useMemo(() => buildDisplayTasks(tasks, deviceIP), [tasks, deviceIP])

  /** 进行中的任务（可能不止一个，合并展示） */
  const running = displayTasks.filter(d => d.state === 'running')
  /** 最近一个终态任务 */
  const lastDone = displayTasks.filter(d => d.state === 'done').pop()

  const fmtSpeed = (speed?: number) =>
    speed != null ? `${speed >= 1 ? speed.toFixed(1) : speed.toFixed(2)} MB/s` : null

  if (running.length === 0 && !lastDone) return null

  return (
    <div className="border-t border-gray-100 bg-indigo-50/40 px-4 py-2 text-xs" data-testid="file-manager-progress">
      {/* 进行中：汇总进度条 + 当前文件 */}
      {running.map(({ task, dir, dp }) => {
        const total = dp?.total ?? task.total
        const doneCount = (dp?.completed ?? task.completed) + (dp?.failed ?? task.failed)
        const pct = total > 0 ? Math.round(doneCount / total * 100) : 0
        return (
          <div key={task.id} className="flex items-center gap-3">
            {dir === 'up'
              ? <Upload className="size-3.5 shrink-0 text-indigo-500" />
              : <Download className="size-3.5 shrink-0 text-indigo-500" />}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-0.5">
                <span className="shrink-0 text-gray-600">
                  {t('host:terminal.fileManager.progressSummary', { done: doneCount, total })}
                </span>
                <span className="shrink-0 text-gray-400 tabular-nums">{pct}%</span>
              </div>
              <div className="h-1.5 rounded-full bg-indigo-100 overflow-hidden">
                <div className="h-full rounded-full bg-indigo-500 transition-all duration-300"
                  style={{ width: `${pct}%` }} />
              </div>
              {dp?.currentFile && (
                <div className="mt-0.5 flex items-center gap-2 min-w-0">
                  <span className="truncate text-gray-500" title={dp.currentFile}>{dp.currentFile}</span>
                  {fmtSpeed(dp.speed) && <span className="shrink-0 text-gray-400 tabular-nums">{fmtSpeed(dp.speed)}</span>}
                  {dp.uploaded != null && dp.totalBytes != null && dp.totalBytes > 0 && (
                    <span className="shrink-0 text-gray-400 tabular-nums">
                      {(dp.uploaded / 1024 / 1024).toFixed(1)} / {(dp.totalBytes / 1024 / 1024).toFixed(1)} MB
                    </span>
                  )}
                </div>
              )}
            </div>
          </div>
        )
      })}
      {/* 终态：可折叠的汇总 + 失败明细 */}
      {lastDone && (
        <div>
          <div className="flex items-center gap-2">
            <button className="flex flex-1 min-w-0 items-center gap-2 text-left" onClick={() => setExpanded(v => !v)}>
              {expanded ? <ChevronDown className="size-3.5 text-gray-400" /> : <ChevronRight className="size-3.5 text-gray-400" />}
              {lastDone.dir === 'up'
                ? <Upload className="size-3.5 shrink-0 text-gray-400" />
                : <Download className="size-3.5 shrink-0 text-gray-400" />}
              {lastDone.task.failed > 0
                ? <span className="text-rose-600">{t('host:terminal.fileManager.doneWithFailures', { ok: lastDone.task.completed, fail: lastDone.task.failed })}</span>
                : <span className="text-emerald-600">{t('host:terminal.fileManager.doneAllSuccess', { ok: lastDone.task.completed })}</span>}
              <span className="truncate text-gray-400" title={t('host:terminal.fileManager.viewInTaskQueue')}>
                {t('host:terminal.fileManager.viewInTaskQueue')}
              </span>
            </button>
            <button onClick={() => removeTask(lastDone.task.id)} aria-label={t('host:terminal.fileManager.close')}
              className="shrink-0 size-5 flex items-center justify-center rounded text-gray-400 hover:text-gray-600 hover:bg-gray-100 transition-colors">
              <X className="size-3.5" />
            </button>
          </div>
          {expanded && lastDone.failures.length > 0 && (
            <div className="mt-1 space-y-0.5 pl-6">
              {lastDone.failures.map((f, i) => (
                <div key={i} className="flex items-start gap-1.5">
                  <XCircle className="size-3.5 shrink-0 mt-0.5 text-rose-400" />
                  <div className="min-w-0">
                    {f.filePath && <div className="truncate text-gray-600" title={f.filePath}>{f.filePath.split(/[\\/]/).pop()}</div>}
                    <div className="text-rose-500">{f.error}</div>
                  </div>
                </div>
              ))}
            </div>
          )}
          {expanded && lastDone.failures.length === 0 && (
            <div className="mt-1 flex items-center gap-1.5 pl-6">
              <CheckCircle2 className="size-3.5 shrink-0 text-emerald-400" />
              <span className="text-gray-500">{t('host:terminal.fileManager.allSuccessDetail')}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default FileManagerProgress
