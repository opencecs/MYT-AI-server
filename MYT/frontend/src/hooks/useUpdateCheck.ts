import { useState, useEffect, createContext, useContext } from 'react'
import { Events } from '@wailsio/runtime'
import * as AppService from '../../bindings/github.com/moyunteng/myt/appservice'
import i18n from '../i18n'

export interface UpdateResult {
  hasUpdate: boolean
  version?: string
  downloadUrl?: string
  checksum?: string
  fileSize?: number
  releaseNotes?: string
  mandatory?: boolean
  error?: string
}

type UpdateStep = 'idle' | 'checking' | 'found' | 'downloading' | 'error'

interface UpdateState {
  updateStep: UpdateStep
  updateResult: UpdateResult | null
  updateError: string
  downloadProgress: number
  downloadStep: string
  checkUpdate: () => void
  startDownload: (downloadUrl: string, checksum?: string) => void
  dismissUpdate: () => void
}

const UpdateContext = createContext<UpdateState | null>(null)

export const UpdateProvider = UpdateContext.Provider

export function useUpdateState() {
  return useContext(UpdateContext) as UpdateState
}

export function useUpdateCheck() {
  const [updateStep, setUpdateStep] = useState<UpdateStep>('idle')
  const [updateResult, setUpdateResult] = useState<UpdateResult | null>(null)
  const [updateError, setUpdateError] = useState('')
  const [downloadProgress, setDownloadProgress] = useState(0)
  const [downloadStep, setDownloadStep] = useState('')
  const [autoCheck, setAutoCheck] = useState(true)
  const [autoDownload, setAutoDownload] = useState(false)

  useEffect(() => {
    AppService.GetAutoCheckUpdate().then((v: boolean) => setAutoCheck(v)).catch(() => {})
    AppService.GetAutoDownloadUpdate().then((v: boolean) => setAutoDownload(v)).catch(() => {})
  }, [])

  // 监听后端下载进度和结果事件
  useEffect(() => {
    const off = Events.On('update:download-progress', (ev: any) => {
      const data = ev?.data
      if (!data) return

      // 有 error 表示失败
      if (data.error) {
        setUpdateStep('error')
        setUpdateError(data.error)
        return
      }

      // 更新进度百分比和步骤文案
      if (data.progress != null) {
        setDownloadProgress(data.progress)
      }
      if (data.step) {
        setDownloadStep(data.step)
      }
    })
    return () => { off() }
  }, [])

  useEffect(() => {
    if (autoCheck) {
      checkUpdate()
    }
  }, [autoCheck])

  const checkUpdate = async () => {
    setUpdateStep('checking')
    setUpdateResult(null)
    setUpdateError('')
    try {
      const result = await AppService.CheckUpdate() as unknown as UpdateResult
      setUpdateResult(result)
      if (result.hasUpdate) {
        if (autoDownload && result.downloadUrl) {
          startDownload(result.downloadUrl, result.checksum)
        } else {
          setUpdateStep('found')
        }
      } else {
        setUpdateStep('idle')
      }
    } catch {
      setUpdateResult({ hasUpdate: false, error: i18n.t('settings:update.checkFailed') })
      setUpdateStep('idle')
    }
  }

  const startDownload = async (downloadUrl: string, checksum?: string) => {
    if (!downloadUrl) return
    setUpdateStep('downloading')
    setDownloadProgress(0)
    setUpdateError('')
    try {
      // 异步下载，立即返回，进度和结果通过事件推送
      await AppService.DownloadUpdate(downloadUrl, checksum ?? '')
    } catch (err: any) {
      setUpdateStep('error')
      setUpdateError(err?.message || i18n.t('settings:update.downloadFailed'))
    }
  }

  const dismissUpdate = () => {
    setUpdateStep('idle')
  }

  return {
    updateStep,
    updateResult,
    updateError,
    downloadProgress,
    downloadStep,
    checkUpdate,
    startDownload,
    dismissUpdate,
  }
}
