import { useState, useEffect, useCallback } from 'react'
import { Events } from '@wailsio/runtime'
import * as ImageService from '../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import type { MirrorItem } from '../types/index'
import { useTaskQueue, registerCancelHandler, registerTaskAbort, unregisterTaskAbort } from '../hooks/taskQueue/useTaskQueue'

interface DownloadState {
  stage: 'downloading' | 'extracting'
  progress: number // 正数=百分比(0-100)，负数=已下载MB的负值
}

interface UploadDeviceState {
  progress: number
  done: boolean
  success: boolean
  error: string
}

// 下载完成回调类型
type DownloadCompleteCallback = (mirrorId: string, success: boolean) => void

// 模块级状态：切页面不丢失
let downloadCompleteCallback: DownloadCompleteCallback | null = null
const _downloads: Record<string, DownloadState> = {}
let _downloadsListeners: Array<(d: Record<string, DownloadState>) => void> = []

function _notifyDownloadsListeners() {
  const snapshot = { ..._downloads }
  _downloadsListeners.forEach(fn => fn(snapshot))
}

export function onDownloadComplete(cb: DownloadCompleteCallback) {
  downloadCompleteCallback = cb
}

// 获取当前下载中状态（不依赖 hook）
export function getActiveDownloads(): Record<string, DownloadState> {
  return { ..._downloads }
}

export function useDownloadProgress() {
  const [downloads, setDownloads] = useState<Record<string, DownloadState>>({ ..._downloads })
  const { addTask, updateTask } = useTaskQueue()

  useEffect(() => {
    // 注册监听，模块级事件更新时同步到组件 state
    _downloadsListeners.push(setDownloads)
    // 首次挂载时同步已有数据
    setDownloads({ ..._downloads })
    return () => {
      _downloadsListeners = _downloadsListeners.filter(fn => fn !== setDownloads)
    }
  }, [])

  useEffect(() => {
    const off1 = Events.On('download-progress', (evt: Record<string, unknown>) => {
      const { mirrorId, stage, progress } = (evt.data || evt) as { mirrorId: string; stage: 'downloading' | 'extracting'; progress: number }
      _downloads[mirrorId] = { stage, progress }
      _notifyDownloadsListeners()
    })
    const off2 = Events.On('download-complete', (evt: Record<string, unknown>) => {
      const { mirrorId, success } = (evt.data || evt) as { mirrorId: string; success: boolean }
      delete _downloads[mirrorId]
      _notifyDownloadsListeners()
      if (downloadCompleteCallback) {
        downloadCompleteCallback(mirrorId, success)
      }
    })
    return () => { off1(); off2() }
  }, [])

  const startDownload = useCallback(async (mirror: MirrorItem) => {
    _downloads[mirror.id] = { stage: 'downloading', progress: 0 }
    _notifyDownloadsListeners()
    const ac = new AbortController()
    // 加入任务队列，id = mirrorId，以便事件更新时匹配
    const taskId = await addTask({ type: 'downloadImage', id: mirror.id, targets: [], deviceIps: [], total: 1, imageName: mirror.name, imageUrl: mirror.url, startTime: Date.now() })
    registerTaskAbort(taskId, ac)
    try {
      await ImageService.DownloadMirror(mirror.id, mirror.name, mirror.url, mirror.ttype2.join(','), mirror.os_ver, mirror.sys_ver, mirror.sys_ver_des, mirror.udesc)
      if (ac.signal.aborted) return
    } catch (err) {
      if (ac.signal.aborted) {
        updateTask(mirror.id, { status: 'canceled', endTime: Date.now() })
        return
      }
      console.error('Download failed:', err)
      delete _downloads[mirror.id]
      _notifyDownloadsListeners()
      updateTask(mirror.id, { status: 'failed', endTime: Date.now() })
    } finally {
      unregisterTaskAbort(taskId)
    }
  }, [addTask, updateTask])

  const cancelDownload = useCallback((mirrorId: string) => {
    ImageService.CancelDownload(mirrorId)
    updateTask(mirrorId, { status: 'canceled', endTime: Date.now() })
  }, [updateTask])

  return { downloads, startDownload, cancelDownload }
}

export function useUploadProgress() {
  const [uploads, setUploads] = useState<Record<string, Record<string, UploadDeviceState>>>({})

  useEffect(() => {
    const off1 = Events.On('upload-progress', (evt: Record<string, unknown>) => {
      const { mirrorId, deviceIP, progress } = (evt.data || evt) as { mirrorId: string; deviceIP: string; progress: number }
      setUploads(prev => ({
        ...prev,
        [mirrorId]: {
          ...prev[mirrorId],
          [deviceIP]: { ...(prev[mirrorId]?.[deviceIP]), progress, done: false, success: false, error: '' }
        }
      }))
    })
    const off2 = Events.On('upload-complete', (evt: Record<string, unknown>) => {
      const { mirrorId, deviceIP, success, error } = (evt.data || evt) as { mirrorId: string; deviceIP: string; success: boolean; error: string }
      setUploads(prev => ({
        ...prev,
        [mirrorId]: {
          ...prev[mirrorId],
          [deviceIP]: { ...(prev[mirrorId]?.[deviceIP]), progress: 100, done: true, success, error: error || '' }
        }
      }))
    })
    return () => { off1(); off2() }
  }, [])

  const clearUpload = useCallback((mirrorId: string) => {
    setUploads(prev => {
      const next = { ...prev }
      delete next[mirrorId]
      return next
    })
  }, [])

  const cancelDevice = useCallback((mirrorId: string, deviceIP: string) => {
    ImageService.CancelUpload(mirrorId, deviceIP)
  }, [])

  const cancelAll = useCallback((mirrorID: string) => {
    ImageService.CancelAllUploads(mirrorID)
  }, [])

  return { uploads, clearUpload, cancelDevice, cancelAll }
}

// 注册镜像下载/上传任务的取消处理器（任务面板取消按钮调这些，真正中断执行）
registerCancelHandler('downloadImage', (taskId) => {
  ImageService.CancelDownload(taskId)
})
registerCancelHandler('uploadImage', (taskId) => {
  ImageService.CancelAllUploads(taskId)
})
