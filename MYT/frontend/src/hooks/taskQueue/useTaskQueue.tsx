import { createContext, ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { TaskQueueItem, CreateTaskInput } from './types'
import * as TaskService from '../../../bindings/github.com/moyunteng/myt/internal/taskqueue/service'
import { cast } from '../../utils/cast'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { setDeviceIdToIPMap } from '../../components/taskQueue/taskQueuePanelUtils'
import { genId, eventData, isTerminal, extractDeviceIps, initDeviceProgress, isBackendTaskId, upsertBackendTask, mergeBackendTasks } from './taskQueueHelpers'
import { useTaskQueueEvents } from './useTaskQueueEvents'

interface TaskQueueApi {
  tasks: TaskQueueItem[]
  activeCount: number
  failedCount: number
  isPanelOpen: boolean
  focusedTaskId: string | undefined
  openPanel: (taskId?: string) => void
  closePanel: () => void
  /** 全局任务详情弹窗（App 层渲染，不依赖队列面板开关） */
  detailTaskId: string | undefined
  openTaskDetail: (taskId: string) => void
  closeTaskDetail: () => void
  addTask: (input: CreateTaskInput) => Promise<string>
  updateTask: (id: string, patch: Partial<TaskQueueItem>) => void
  cancelTask: (id: string) => void
  removeTask: (id: string) => void
  clearFinished: () => void
}

// 取消处理器注册表：任务类型 → 取消函数（各模块注册专用取消逻辑）
const cancelHandlers: Record<string, (taskId: string, task: TaskQueueItem) => void> = {}

/** 注册某类型任务的专用取消处理器（如镜像下载调 CancelDownload） */
export function registerCancelHandler(type: string, fn: (taskId: string, task: TaskQueueItem) => void) {
  cancelHandlers[type] = fn
}

// 任务级 AbortController 注册表：前端编排任务（如 create）注册，取消时 abort 中断编排
const taskAbortControllers: Map<string, AbortController> = new Map()

/** 注册任务的 AbortController（前端编排任务用，取消时 abort 中断 await 链） */
export function registerTaskAbort(taskId: string, ac: AbortController) {
  taskAbortControllers.set(taskId, ac)
}

/** 注销任务的 AbortController（编排结束时调用） */
export function unregisterTaskAbort(taskId: string) {
  taskAbortControllers.delete(taskId)
}

const TaskQueueContext = createContext<TaskQueueApi | null>(null)

export function TaskQueueProvider({ children }: { children: ReactNode }) {
  const [tasks, setTasks] = useState<TaskQueueItem[]>([])
  const [isPanelOpen, setPanelOpen] = useState(false)
  const [focusedTaskId, setFocusedTaskId] = useState<string | undefined>()
  const [detailTaskId, setDetailTaskId] = useState<string | undefined>()
  const tasksRef = useRef<TaskQueueItem[]>([])
  useEffect(() => { tasksRef.current = tasks }, [tasks])

  // ====== 设备标识显示映射（v2：任务里存的序列号 → 面板显示 IP） ======
  useEffect(() => {
    const load = () => {
      DiscoverService.GetDeviceList().then((list) => {
        setDeviceIdToIPMap((list ?? []) as { device_id: string; ip: string }[])
      }).catch(() => {})
    }
    load()
    const off = Events.On('devicelist:updated', load)
    return off
  }, [])

  // ====== 后端任务队列同步（批量操作类任务） ======
  // 初始化拉取 + 订阅 task:updated，合并到 tasks（按 ID 去重，后端任务优先）
  useEffect(() => {
    TaskService.List().then((list) => {
      const backend = cast<TaskQueueItem[]>(list ?? [])
      setTasks(prev => mergeBackendTasks(prev, backend))
    }).catch(() => {})
    const off = Events.On('task:updated', (evt: unknown) => {
      const t = cast<TaskQueueItem>(eventData<Record<string, unknown>>(evt))
      if (!t || !t.id) return
      setTasks(prev => upsertBackendTask(prev, t))
    })
    return () => { off() }
  }, [])

  const openPanel = useCallback((taskId?: string) => {
    setFocusedTaskId(taskId)
    setPanelOpen(true)
  }, [])

  const closePanel = useCallback(() => {
    setPanelOpen(false)
  }, [])

  const closeTaskDetail = useCallback(() => {
    setDetailTaskId(undefined)
  }, [])

  const addTask = useCallback(async (input: CreateTaskInput): Promise<string> => {
    const deviceIps = input.deviceIps || extractDeviceIps(input.targets || [])
    const total = input.total ?? (input.type === 'uploadFile' ? 0 : (input.targets?.length ?? 0))
    // 直接在后端创建任务（返回真实 ID，单任务单条目，无本地临时任务去重问题）
    // details 携带全部字段（含类型特有字段），后端存到 Details，前端通过 task:updated 事件同步
    const details = { ...input, targets: input.targets, deviceIps, total }
    try {
      const realId = await TaskService.CreateSingle(input.type, JSON.stringify(details))
      return realId
    } catch {
      // 降级：后端创建失败，本地生成临时任务（无后端持久化/取消）
      const localId = (input as Record<string, unknown>).id as string || genId()
      const task: TaskQueueItem = {
        ...input, id: localId, type: input.type, status: 'pending', total,
        completed: 0, failed: 0, progress: 0, targets: input.targets ? [...input.targets] : [],
        deviceIps, deviceProgress: initDeviceProgress(input, deviceIps),
        // startTime=提交时刻：与后端 Submit 语义一致（queue.go 创建即写入），保证新任务排序在最上
        startTime: Date.now(), endTime: null, failedTargets: [], currentStep: null,
        imageProgress: null, copyLogs: [], agentSteps: [], agentAction: undefined,
      }
      setTasks(prev => [task, ...prev].slice(0, 200))
      return localId
    }
  }, [])

  const updateTask = useCallback((id: string, patch: Partial<TaskQueueItem>) => {
    // 本地即时更新（后端事件到达后覆盖）
    setTasks(prev => prev.map(t => {
      if (t.id !== id) return t
      const next = { ...t, ...patch }
      if (patch.status && isTerminal(patch.status) && !next.endTime) {
        next.endTime = Date.now()
      }
      return next
    }).slice(0, 200))
    // 同步到后端（持久化 + 多窗口 + 取消轮询）
    if (isBackendTaskId(id)) {
      TaskService.Update(id, JSON.stringify(patch)).catch(() => {})
    }
  }, [])

  const openTaskDetail = useCallback((taskId: string) => {
    // 查看即确认：把查看时刻的失败数记到任务字段 failedAck，角标（未读提醒语义）对应清零；
    // 之后失败数再增长（如仍在跑又挂了新的）会重新计入角标。
    // 走 updateTask 持久化到后端任务上——重启后任务从磁盘恢复，确认状态不丢
    // （曾用本地 state 方案：重启即失效，角标复现）。
    const task = tasksRef.current.find(t => t.id === taskId)
    if (task && task.failed > 0 && (task.failedAck ?? 0) < task.failed) {
      updateTask(taskId, { failedAck: task.failed })
    }
    setDetailTaskId(taskId)
  }, [updateTask])

  const cancelTask = useCallback((id: string) => {
    // 优先调专用取消处理器（如镜像下载调 CancelDownload），真正中断执行
    const task = tasksRef.current.find(t => t.id === id)
    if (task && cancelHandlers[task.type]) {
      cancelHandlers[task.type](id, task)
    }
    // 中断前端编排任务（create 等）：abort 其 AbortController
    const ac = taskAbortControllers.get(id)
    if (ac) {
      ac.abort()
      unregisterTaskAbort(id)
    }
    // 同步后端状态（标记 canceled）
    if (task && !cancelHandlers[task.type]) {
      // 无专用 handler 的任务，仍通知后端取消（批量任务的 context，或长任务标记状态）
      if (isBackendTaskId(id)) TaskService.Cancel(id).catch(() => {})
      else updateTask(id, { status: 'canceled', endTime: Date.now() })
    }
  }, [updateTask])

  const removeTask = useCallback((id: string) => {
    if (isBackendTaskId(id)) {
      TaskService.Remove(id).then(() => setTasks(prev => prev.filter(t => t.id !== id))).catch(() => {})
    } else {
      setTasks(prev => prev.filter(t => t.id !== id))
    }
  }, [])

  const clearFinished = useCallback(() => {
    TaskService.ClearFinished().catch(() => {})
    setTasks(prev => prev.filter(t => !isTerminal(t.status) || !isBackendTaskId(t.id)))
  }, [])

  // Wails 事件监听已拆分到 useTaskQueueEvents.ts
  useTaskQueueEvents(setTasks)

  const sortedTasks = useMemo(() => {
    // 纯时间倒序：最新提交的在最上面（后端 Submit 即写入 startTime=提交时刻）。
    // 不按状态分级置顶——失败任务不常驻队首（用户决策 2026-09-05）。
    return [...tasks].sort((a, b) => (b.startTime ?? b.endTime ?? 0) - (a.startTime ?? a.endTime ?? 0))
  }, [tasks])

  const value = useMemo<TaskQueueApi>(() => ({
    tasks: sortedTasks,
    activeCount: tasks.filter(t => t.status === 'running' || t.status === 'pending' || t.status === 'queued').length,
    failedCount: tasks.filter(t => t.status === 'failed' && t.failed > (t.failedAck ?? 0)).length,
    isPanelOpen,
    focusedTaskId,
    openPanel,
    closePanel,
    detailTaskId,
    openTaskDetail,
    closeTaskDetail,
    addTask,
    updateTask,
    cancelTask,
    removeTask,
    clearFinished,
  }), [sortedTasks, tasks, isPanelOpen, focusedTaskId, openPanel, closePanel, detailTaskId, openTaskDetail, closeTaskDetail, addTask, updateTask, cancelTask, removeTask, clearFinished])

  return <TaskQueueContext.Provider value={value}>{children}</TaskQueueContext.Provider>
}

export function useTaskQueue() {
  const ctx = useContext(TaskQueueContext)
  if (!ctx) throw new Error('useTaskQueue must be used within TaskQueueProvider')
  return ctx
}

/** 等待任务到达终态（task:updated 事件驱动；超时返回 null）。
 * 批量操作类任务（restart/delete 等）由后端 executor 执行，前端提交后用本函数等待结果做收尾
 * （toast/版本回查等），**不要**在前端再直调 SDK 执行同一操作——会导致双发。 */
export function waitForTaskEnd(taskId: string, timeoutMs = 10 * 60 * 1000): Promise<TaskQueueItem | null> {
  return new Promise(resolve => {
    const timer = setTimeout(() => { off(); resolve(null) }, timeoutMs)
    const off = Events.On('task:updated', (evt: unknown) => {
      const t = cast<TaskQueueItem>(eventData<Record<string, unknown>>(evt))
      if (!t || t.id !== taskId || !isTerminal(t.status)) return
      clearTimeout(timer)
      off()
      resolve(t)
    })
  })
}

// 辅助函数已拆分到 taskQueueHelpers.ts
