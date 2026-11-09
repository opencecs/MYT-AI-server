import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2, X } from 'lucide-react'
import { useModalEnter } from '../../hooks/useModalAnimation'
import * as V4Service from '../../../bindings/github.com/moyunteng/myt/v4servicewrapper'
import { showToast } from '../../components/Toast'
import type { V4RomTaskStatus } from '../../../bindings/github.com/moyunteng/myt/models'
import { IMAGE_FILE_NAMES, formatBytes, type RomVariantRow } from './romShared'

interface Props {
  host: string
  /** 目标变体（缺失的文件会被逐个下载） */
  variant: RomVariantRow
  onClose: () => void
  /** 全部文件下载结束（成功或失败）后回调，页面据此刷新列表 */
  onDone: () => void
}

/** 单文件下载状态（前端视图模型） */
interface FileTask {
  filename: string
  taskId: string
  status: 'idle' | 'pending' | 'running' | 'completed' | 'failed'
  downloaded: number
  total: number
  error: string
}

/** 轮询间隔：镜像动辄几百 MB，1.5s 粒度足够，也不至于打满设备端 */
const POLL_INTERVAL = 1500

/**
 * ROM 下载弹窗：为一个变体补齐缺失的三件套。
 *
 * 服务端契约（POST /image/pull）是**按单文件**下载的：一个变体要三次调用，每次
 * 返回独立 task_id，再用 GET /image/status/:task_id 查进度。因此这里按缺失文件
 * 逐个串行下载——串行而非并行，避免同变体多任务互相覆盖临时目录。
 *
 * 用户输入的是每个文件的下载 URL（镜像仓库地址由用户/运维提供，客户端不猜）。
 */
export default function RomPullDialog({ host, variant, onClose, onDone }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  // 缺失文件 = 需要下载的文件；全部就绪时不允许打开（列表按钮会禁用）
  const missing = variant.missing.length > 0 ? variant.missing : IMAGE_FILE_NAMES
  const [urls, setUrls] = useState<Record<string, string>>(() => {
    const init: Record<string, string> = {}
    for (const f of missing) init[f] = ''
    return init
  })
  const [tasks, setTasks] = useState<FileTask[]>(() => missing.map(f => ({
    filename: f, taskId: '', status: 'idle' as const, downloaded: 0, total: 0, error: '',
  })))
  const [running, setRunning] = useState(false)
  // 停止标记：卸载/关闭后不再 setState（轮询定时器也随之清理）
  const stoppedRef = useRef(false)

  useEffect(() => () => { stoppedRef.current = true }, [])

  const patchTask = useCallback((filename: string, patch: Partial<FileTask>) => {
    if (stoppedRef.current) return
    setTasks(prev => prev.map(t => (t.filename === filename ? { ...t, ...patch } : t)))
  }, [])

  /** 轮询单个任务直到 completed/failed */
  const pollUntilDone = useCallback(async (filename: string, taskId: string) => {
    for (;;) {
      if (stoppedRef.current) return false
      let st: V4RomTaskStatus | null = null
      try {
        st = await V4Service.RomImageStatus(host, taskId)
      } catch (e) {
        // 查询失败不终止：网络抖动/设备忙时下一轮重试（服务端任务仍在跑）
        patchTask(filename, { error: String(e) })
        await new Promise(r => setTimeout(r, POLL_INTERVAL))
        continue
      }
      if (!st) return false
      patchTask(filename, {
        status: st.status as FileTask['status'],
        downloaded: st.downloaded,
        total: st.total,
        error: st.error || '',
      })
      if (st.status === 'completed') return true
      if (st.status === 'failed') return false
      await new Promise(r => setTimeout(r, POLL_INTERVAL))
    }
  }, [host, patchTask])

  const start = useCallback(async () => {
    if (running) return
    // 校验：每个缺失文件都必须填 URL（服务端需要它才知道从哪下载）
    for (const f of missing) {
      if (!urls[f]?.trim()) {
        showToast(t('rom:pull.urlRequired', { filename: f }), 'error')
        return
      }
    }
    setRunning(true)
    let failed = 0
    for (const f of missing) {
      if (stoppedRef.current) break
      patchTask(f, { status: 'pending', error: '', downloaded: 0, total: 0 })
      try {
        const res = await V4Service.PullRomImage(host, variant.variant, urls[f].trim(), f)
        if (!res?.taskId) throw new Error(t('rom:pull.noTask'))
        patchTask(f, { taskId: res.taskId, status: 'running' })
        const ok = await pollUntilDone(f, res.taskId)
        if (!ok) failed++
      } catch (e) {
        patchTask(f, { status: 'failed', error: String(e) })
        failed++
      }
    }
    if (stoppedRef.current) return
    setRunning(false)
    if (failed === 0) {
      showToast(t('rom:pull.allDone', { variant: variant.variant }))
    } else {
      showToast(t('rom:pull.partialFailed', { count: failed }), 'warning')
    }
    onDone()
  }, [running, missing, urls, host, variant.variant, t, patchTask, pollUntilDone, onDone])

  const statusText = (task: FileTask) => {
    switch (task.status) {
      case 'idle': return t('rom:pull.waiting')
      case 'pending': return t('rom:pull.queued')
      case 'running': {
        if (task.total > 0) {
          const pct = Math.min(100, Math.round((task.downloaded / task.total) * 100))
          return `${pct}%（${formatBytes(task.downloaded)} / ${formatBytes(task.total)}）`
        }
        return `${formatBytes(task.downloaded)}`
      }
      case 'completed': return t('rom:status.ready')
      case 'failed': return task.error || t('rom:status.failed')
    }
  }

  const fieldCls = 'w-full rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-xs text-gray-700 outline-none placeholder:text-gray-400 focus:border-blue-400 disabled:bg-gray-50 disabled:text-gray-400'

  return (
    <div
      className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay"
      ref={rootRef}
      role="dialog"
      aria-modal="true"
      onKeyDown={e => { if (e.key === 'Escape' && !running) onClose() }}
    >
      <div className="bg-white rounded-lg shadow-xl w-[560px] max-w-[calc(100vw-32px)] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center px-5 pt-4 pb-3 border-b border-gray-100">
          <h3 className="text-sm font-semibold text-gray-800">
            {t('rom:pull.title', { variant: variant.variant })}
          </h3>
          <button
            onClick={onClose}
            disabled={running}
            className="ml-auto text-gray-400 hover:text-gray-600 disabled:opacity-40 cursor-pointer"
            aria-label={t('button.cancel')}
          >
            <X size={16} />
          </button>
        </div>

        <div className="px-5 py-4 flex flex-col gap-3">
          <p className="text-[11px] text-gray-500 leading-relaxed">{t('rom:pull.hint')}</p>
          <div className="text-[11px] text-gray-400">
            {t('rom:pull.targetHost', { host })}
          </div>
          {missing.map(f => {
            const task = tasks.find(x => x.filename === f)
            const failed = task?.status === 'failed'
            const done = task?.status === 'completed'
            return (
              <div key={f} className="flex flex-col gap-1">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono text-gray-600 w-28 shrink-0">{f}</span>
                  <input
                    className={fieldCls}
                    value={urls[f] ?? ''}
                    onChange={e => setUrls(prev => ({ ...prev, [f]: e.target.value }))}
                    placeholder={t('rom:pull.urlPlaceholder')}
                    disabled={running || done}
                  />
                </div>
                {task && task.status !== 'idle' && (
                  <div className="flex items-center gap-1.5 pl-30 text-[11px]">
                    {task.status === 'running' || task.status === 'pending'
                      ? <Loader2 size={11} className="animate-spin text-blue-500" />
                      : null}
                    <span className={
                      done ? 'text-green-600' : failed ? 'text-red-500' : 'text-gray-500'
                    }>{statusText(task)}</span>
                  </div>
                )}
              </div>
            )
          })}
        </div>

        <div className="flex justify-end gap-2 px-5 pb-4">
          <button
            onClick={onClose}
            disabled={running}
            className="px-3 py-1.5 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700 disabled:opacity-50 cursor-pointer"
          >
            {running ? t('rom:pull.running') : t('button.cancel')}
          </button>
          <button
            onClick={start}
            disabled={running}
            className="flex items-center gap-1 px-3 py-1.5 text-xs bg-blue-500 rounded hover:bg-blue-600 text-white disabled:opacity-60 cursor-pointer"
          >
            {running && <Loader2 size={12} className="animate-spin" />}
            {t('rom:pull.start')}
          </button>
        </div>
      </div>
    </div>
  )
}
