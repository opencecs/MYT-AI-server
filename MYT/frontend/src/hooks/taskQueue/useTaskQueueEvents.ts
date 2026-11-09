import { useEffect, useRef, Dispatch, SetStateAction } from 'react'
import { Events } from '@wailsio/runtime'
import { TaskQueueItem, DeviceProgress, CopyLog, AgentStep } from './types'
import i18n from '../../i18n'
import { eventData, isTerminal } from './taskQueueHelpers'

/** 订阅所有 Wails 任务进度事件，更新 tasks 状态 */
export function useTaskQueueEvents(setTasks: Dispatch<SetStateAction<TaskQueueItem[]>>) {
  const uploadSpeedCache = useRef<Map<string, { t: number; bytes: number }>>(new Map())

  // ====== 镜像下载 ======
  useEffect(() => {
    const off1 = Events.On('download-progress', (evt: unknown) => {
      const { mirrorId, progress } = eventData<{ mirrorId: string; stage: string; progress: number }>(evt)
      setTasks(prev => prev.map(t => {
        if (t.type !== 'downloadImage' || t.id !== mirrorId) return t
        return { ...t, status: 'running', progress: Math.round(progress), startTime: t.startTime || Date.now() }
      }))
    })
    const off2 = Events.On('download-complete', (evt: unknown) => {
      const { mirrorId, success, error } = eventData<{ mirrorId: string; success: boolean; error?: string }>(evt)
      setTasks(prev => prev.map(t => {
        if (t.type !== 'downloadImage' || t.id !== mirrorId) return t
        return { ...t, status: success ? 'completed' : 'failed', progress: success ? 100 : t.progress, error, endTime: Date.now() }
      }))
    })
    return () => { off1(); off2() }
  }, [])

  // ====== 镜像上传 ======
  useEffect(() => {
    const off1 = Events.On('upload-progress', (evt: unknown) => {
      const { mirrorId, deviceIP, progress } = eventData<{ mirrorId: string; deviceIP: string; progress: number }>(evt)
      setTasks(prev => prev.map(t => {
        if (t.type !== 'uploadImage' || t.id !== mirrorId) return t
        const dp = { ...(t.deviceProgress || {}) } as Record<string, DeviceProgress>
        dp[deviceIP] = { ...(dp[deviceIP] || { total: 1, completed: 0, failed: 0 }), currentProgress: Math.round(progress) }
        const devs = Object.values(dp)
        const p = devs.length > 0 ? Math.round(devs.reduce((s, d) => s + (d.currentProgress || d.completed / d.total * 100), 0) / devs.length) : 0
        return { ...t, status: 'running', deviceProgress: dp, progress: p, startTime: t.startTime || Date.now() }
      }))
    })
    const off2 = Events.On('upload-complete', (evt: unknown) => {
      const { mirrorId, deviceIP, success, error } = eventData<{ mirrorId: string; deviceIP: string; success: boolean; error?: string }>(evt)
      setTasks(prev => prev.map(t => {
        if (t.type !== 'uploadImage' || t.id !== mirrorId) return t
        const dp = { ...(t.deviceProgress || {}) } as Record<string, DeviceProgress>
        const dpItem = dp[deviceIP] || { total: 1, completed: 0, failed: 0 }
        dp[deviceIP] = { ...dpItem, completed: success ? dpItem.total : dpItem.completed, failed: success ? dpItem.failed : dpItem.failed + 1, error: error || '', currentProgress: success ? 100 : dpItem.currentProgress }
        const allDone = Object.values(dp).every(d => d.completed + d.failed >= d.total)
        const hasFailed = Object.values(dp).some(d => d.failed > 0)
        return { ...t, deviceProgress: dp, status: allDone ? (hasFailed ? 'failed' : 'completed') : 'running', progress: allDone ? 100 : t.progress, endTime: allDone ? Date.now() : null }
      }))
    })
    return () => { off1(); off2() }
  }, [])

  // ====== 复制云机 ======
  useEffect(() => {
    const off = Events.On('android:copy-progress', (evt: unknown) => {
      const d = eventData<{ deviceIP: string; current: number; total: number; status: string; message: string }>(evt)
      setTasks(prev => prev.map(t => {
        if (t.type !== 'copy' || !t.deviceIps.includes(d.deviceIP) || isTerminal(t.status)) return t
        const progress = d.total > 0 ? Math.round(d.current / d.total * 100) : t.progress
        const logs: CopyLog[] = [...(t.copyLogs || [])]
        if (d.message) {
          const existing = logs.find(l => l.current === d.current && l.total === d.total)
          if (existing) {
            existing.status = d.status === 'done' ? 'success' : d.status === 'error' ? 'failed' : 'running'
            existing.message = d.message
          } else {
            logs.push({ current: d.current, total: d.total, name: d.message, status: d.status === 'done' ? 'success' : d.status === 'error' ? 'failed' : 'running', message: d.message })
          }
        }
        if (d.status === 'done') return { ...t, progress: 100, status: 'completed', endTime: Date.now(), copyLogs: logs }
        if (d.status === 'error') return { ...t, progress, status: 'failed', error: d.message, endTime: Date.now(), copyLogs: logs }
        return { ...t, progress, copyLogs: logs }
      }))
    })
    return () => { off() }
  }, [])

  // ====== 安卓导入 ======
  useEffect(() => {
    const off = Events.On('android:import-progress', (evt: unknown) => {
      const d = eventData<{ deviceIP: string; machineName: string; uploaded: number; total: number; progress: number; stage?: string; msg?: string }>(evt)
      if (!d.deviceIP) return
      setTasks(prev => prev.map(t => {
        if (t.type !== 'importAndroid' || !t.deviceIps.includes(d.deviceIP) || isTerminal(t.status)) return t
        return { ...t, status: 'running', progress: Math.min(100, d.progress), importStage: d.stage, importMsg: d.msg, startTime: t.startTime || Date.now() }
      }))
    })
    return () => { off() }
  }, [])

  // ====== 安卓导出 ======
  useEffect(() => {
    const off = Events.On('android:export-progress', (evt: unknown) => {
      const d = eventData<{ deviceIP: string; stage: string; progress: number; msg?: string; error?: string }>(evt)
      if (!d.deviceIP) return
      setTasks(prev => prev.map(t => {
        if (t.type !== 'exportAndroid' || !t.deviceIps.includes(d.deviceIP) || isTerminal(t.status)) return t
        if (d.stage === 'error') {
          return { ...t, status: 'failed', error: d.error || i18n.t('host:toast.exportFailed'), endTime: Date.now() }
        }
        if (d.stage === 'downloading') {
          return { ...t, status: 'running', exportStage: 'downloading', exportMsg: undefined, exportProgress: Math.min(100, d.progress) }
        }
        // 设备端导出 SSE 阶段（config/snapshot/pause/sync/unpause/e2fsck/resize2fs/gzip/tar/collect/pack/complete）
        return { ...t, status: 'running', exportStage: d.stage, exportMsg: d.msg, progress: Math.min(100, d.progress), startTime: t.startTime || Date.now() }
      }))
    })
    return () => { off() }
  }, [])

  // ====== 安卓跨设备移动 ======
  useEffect(() => {
    const stageProgress: Record<string, number> = { preparing: 10, exporting: 30, transferring: 70, cleanup: 90 }
    const off = Events.On('android:move-progress', (evt: unknown) => {
      const d = eventData<{ srcIP: string; dstIP: string; stage: string; message: string }>(evt)
      if (!d.srcIP) return
      setTasks(prev => prev.map(t => {
        if (t.type !== 'moveAndroid' || !t.deviceIps.includes(d.srcIP) || isTerminal(t.status)) return t
        const progress = stageProgress[d.stage] ?? t.progress
        return { ...t, status: 'running', moveStage: d.stage, moveMessage: d.message, progress, startTime: t.startTime || Date.now() }
      }))
    })
    return () => { off() }
  }, [])

  // ====== 文件上传进度 ======
  useEffect(() => {
    const off = Events.On('file:upload-progress', (evt: unknown) => {
      const d = eventData<{ deviceIP: string; androidName: string; fileName: string; progress: number; stage: string; uploaded?: number; totalBytes?: number }>(evt)
      if (!d.deviceIP) return
      setTasks(prev => prev.map(t => {
        if (t.type !== 'uploadFile' || !t.deviceIps.includes(d.deviceIP) || isTerminal(t.status)) return t
        const dp = { ...(t.deviceProgress || {}) } as Record<string, DeviceProgress>
        const cur = dp[d.deviceIP] || { total: 1, completed: 0, failed: 0 }
        // 算速度（仅上传阶段）
        let speed: number | undefined
        let uploaded = cur.uploaded
        let totalBytes = cur.totalBytes
        if (d.stage === 'uploading' && d.uploaded != null && d.totalBytes != null) {
          uploaded = d.uploaded
          totalBytes = d.totalBytes
          const key = `${d.deviceIP}|${d.fileName}`
          const now = Date.now()
          const last = uploadSpeedCache.current.get(key)
          if (last && now > last.t) {
            const dt = (now - last.t) / 1000
            const db = d.uploaded - last.bytes
            if (dt > 0 && db >= 0) speed = db / 1024 / 1024 / dt
          }
          uploadSpeedCache.current.set(key, { t: now, bytes: d.uploaded })
        } else if (d.stage === 'done' || d.stage === 'installing') {
          speed = undefined
          uploadSpeedCache.current.delete(`${d.deviceIP}|${d.fileName}`)
        }
        // done/failed 阶段：该设备完成/失败一个文件，计数累计（云机粒度）
        const newCompleted = d.stage === 'done' ? cur.completed + 1 : cur.completed
        const newFailed = d.stage === 'failed' ? cur.failed + 1 : cur.failed
        // 进度条混合基准：已完成数 + 当前文件进度(0-100 → 折算 1/total)，单调递增不回跳；
        // done/failed 直接取计数折算（事件 progress 是单容器值，直接取会假满）
        const denom = Math.max(cur.total, 1)
        const doneCount = newCompleted + newFailed
        const countPct = Math.round(doneCount / denom * 100)
        const blendedPct = Math.min(100, Math.round((doneCount + Math.min(100, d.progress) / 100) / denom * 100))
        dp[d.deviceIP] = {
          ...cur,
          completed: newCompleted,
          failed: newFailed,
          currentProgress: d.stage === 'done' || d.stage === 'failed' ? countPct : blendedPct,
          stage: d.stage,
          uploaded,
          totalBytes,
          speed,
          currentFile: d.fileName,
        }
        // 总进度统一按计数基准：与编排方 updateTask 的 (completed+failed)/total 同口径，
        // 避免两个进度源（事件平均值 vs 完成数百分比）交替覆盖导致进度条来回跳
        const devs = Object.values(dp)
        const totalSum = devs.reduce((s, x) => s + x.total, 0)
        const doneSum = devs.reduce((s, x) => s + x.completed + x.failed, 0)
        const p = totalSum > 0 ? Math.round(doneSum / totalSum * 100) : 0
        return { ...t, status: 'running', deviceProgress: dp, progress: p, startTime: t.startTime || Date.now() }
      }))
    })
    return () => { off() }
  }, [])

  // ====== 智能体卸载 ======
  useEffect(() => {
    const off = Events.On('agent:install-progress', (evt: unknown) => {
      const d = eventData<{ deviceIP: string; action: string; steps: AgentStep[]; current: number; total: number; done: boolean; success: boolean; error?: string }>(evt)
      if (!d.deviceIP) return
      setTasks(prev => prev.map(t => {
        const isMatch = t.type === 'uninstallAgent' && t.deviceIps.includes(d.deviceIP) && !isTerminal(t.status)
        if (!isMatch) return t
        const progress = d.total > 0 ? Math.round(d.current / d.total * 100) : t.progress
        if (d.done) {
          return { ...t, progress: d.success ? 100 : progress, status: d.success ? 'completed' : 'failed', error: d.error, endTime: Date.now(), agentSteps: d.steps || t.agentSteps }
        }
        return { ...t, progress, status: 'running', agentSteps: d.steps || t.agentSteps, startTime: t.startTime || Date.now() }
      }))
    })
    return () => { off() }
  }, [])
}
