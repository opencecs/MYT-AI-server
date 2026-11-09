import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import * as IscsiTargetService from '../../../bindings/github.com/moyunteng/myt/iscsitargetservicewrapper'
import { showToast } from '../../components/Toast'

export interface TargetStatus {
  running: boolean
  iqn: string
  portal: string
  apiAddr: string
}

export interface DiskInfo {
  name: string
  lun: number
  size_gb: number
  type: string
  iqn: string
  storagePath: string
}

export interface IscsiTarget {
  status: TargetStatus
  disks: DiskInfo[]
  loading: boolean
  refreshStatus: () => Promise<void>
  refreshDisks: () => Promise<void>
  startTarget: () => Promise<void>
  stopTarget: () => Promise<void>
  createDisk: (name: string, sizeGB: number, storageDir: string) => Promise<void>
  deleteDisk: (name: string, force?: boolean) => Promise<void>
  resizeDisk: (deviceIP: string, name: string, newSizeGB: number) => Promise<boolean>
}

export function useIscsiTarget(): IscsiTarget {
  const { t } = useTranslation()
  const [status, setStatus] = useState<TargetStatus>({ running: false, iqn: '', portal: '', apiAddr: '' })
  const [disks, setDisks] = useState<DiskInfo[]>([])
  const [loading, setLoading] = useState(false)

  const refreshStatus = useCallback(async () => {
    try {
      const s = await IscsiTargetService.GetTargetStatus() as TargetStatus
      setStatus(s)
    } catch (e) {
      console.warn('[iSCSI Target] GetTargetStatus failed:', e)
    }
  }, [])

  const refreshDisks = useCallback(async () => {
    try {
      const list = await IscsiTargetService.ListDisks() as DiskInfo[]
      setDisks(list || [])
    } catch (e) {
      console.warn('[iSCSI Target] ListDisks failed:', e)
    }
  }, [])

  useEffect(() => {
    refreshStatus()
    refreshDisks()
  }, [refreshStatus, refreshDisks])

  const startTarget = async () => {
    setLoading(true)
    try {
      await IscsiTargetService.StartTarget()
      showToast(t('diskless:toast.targetStarted'))
      await refreshStatus()
    } catch (e: any) {
      showToast(t('diskless:toast.targetStartFailed', { error: e }), 'error')
    } finally {
      setLoading(false)
    }
  }

  const stopTarget = async () => {
    setLoading(true)
    try {
      await IscsiTargetService.StopTarget()
      showToast(t('diskless:toast.targetStopped'))
      await refreshStatus()
    } catch (e: any) {
      showToast(t('diskless:toast.targetStopFailed', { error: e }), 'error')
    } finally {
      setLoading(false)
    }
  }

  const createDisk = async (name: string, sizeGB: number, storageDir: string, diskType: string = 'fixed') => {
    if (!name.trim()) { showToast(t('diskless:toast.enterDiskName'), 'error'); return }
    if (sizeGB <= 0) { showToast(t('diskless:toast.enterValidDiskSize'), 'error'); return }
    setLoading(true)
    try {
      const result = await IscsiTargetService.CreateDisk(name.trim(), sizeGB, diskType, storageDir) as any
      showToast(t('diskless:toast.diskCreated', { lun: result?.lun ?? '-' }))
      await refreshDisks()
    } catch (e: any) {
      showToast(t('diskless:toast.createFailed', { error: e }), 'error')
    } finally {
      setLoading(false)
    }
  }

  const deleteDisk = async (name: string, force: boolean = false) => {
    if (!name.trim()) { showToast(t('diskless:toast.enterDiskName'), 'error'); return }
    setLoading(true)
    try {
      await IscsiTargetService.DeleteDisk(name.trim(), force)
      showToast(t('diskless:toast.diskDeleted'))
      await refreshDisks()
    } catch (e: any) {
      showToast(t('diskless:toast.deleteFailed', { error: e }), 'error')
    } finally {
      setLoading(false)
    }
  }

  const resizeDisk = async (deviceIP: string, name: string, newSizeGB: number): Promise<boolean> => {
    if (!name.trim()) { showToast(t('diskless:toast.diskNameEmpty'), 'error'); return false }
    if (newSizeGB <= 0) { showToast(t('diskless:toast.enterValidNewSize'), 'error'); return false }
    setLoading(true)
    try {
      await IscsiTargetService.ResizeDisk(deviceIP, name.trim(), newSizeGB)
      showToast(t('diskless:toast.diskResized', { name, size: newSizeGB }))
      await refreshDisks()
      return true
    } catch (e: any) {
      showToast(t('diskless:toast.resizeFailed', { error: e }), 'error')
      return false
    } finally {
      setLoading(false)
    }
  }

  return {
    status, disks, loading,
    refreshStatus, refreshDisks,
    startTarget, stopTarget,
    createDisk, deleteDisk, resizeDisk,
  }
}
