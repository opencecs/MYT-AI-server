import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as IscsiService from '../../../bindings/github.com/moyunteng/myt/iscsiservicewrapper'
import { showToast } from '../../components/Toast'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import type { IscsiManager, LocalNicInfo, LocalDiskInfo, TargetInfo, SessionInfo, StorageStatus, DiskInfo } from './useIscsiManagerTypes'
export type { IscsiManager, LocalNicInfo, LocalDiskInfo, TargetInfo, SessionInfo, StorageStatus, DiskInfo } // re-export for backward compatibility

export function useIscsiManager(deviceIP: string): IscsiManager {
  const { t } = useTranslation()
  const { addTask, openPanel } = useTaskQueue()
  const [localNics, setLocalNics] = useState<LocalNicInfo[]>([])
  const [selectedLocalIP, setSelectedLocalIP] = useState('')
  const [localDisks, setLocalDisks] = useState<LocalDiskInfo[]>([])
  const [selectedDisk, setSelectedDisk] = useState('')
  const [targetIP, setTargetIP] = useState('')
  const [targets, setTargets] = useState<TargetInfo[]>([])
  const [sessions, setSessions] = useState<SessionInfo[]>([])
  const [diskImages, setDiskImages] = useState<DiskInfo[]>([])
  const [storageMode, setStorageMode] = useState('local')
  const [storageStatus, setStorageStatus] = useState<StorageStatus | null>(null)
  const [loading, setLoading] = useState(false)
  const [partitionDevice, setPartitionDevice] = useState('')
  const [partitionResult, setPartitionResult] = useState<string[]>([])
  const selectedLocalIPRef = useRef(selectedLocalIP)
  selectedLocalIPRef.current = selectedLocalIP
  const currentDeviceRef = useRef(deviceIP)
  currentDeviceRef.current = deviceIP

  // 挂载状态轮询：挂载任务提交后任务需较长时间完成，轮询 storage 状态直到生效
  const mountPollRef = useRef<number | null>(null)
  const stopMountPolling = useCallback(() => {
    if (mountPollRef.current) { clearInterval(mountPollRef.current); mountPollRef.current = null }
  }, [])

  useEffect(() => {
    const load = async () => {
      try {
        const list = await IscsiService.ListLocalNics() as LocalNicInfo[]
        setLocalNics(list || [])
        if (list?.length > 0 && !selectedLocalIPRef.current) setSelectedLocalIP(list[0].ip)
      } catch (e) { console.warn('[iSCSI] ListLocalNics failed:', e) }
    }
    load()
  }, [])

  useEffect(() => {
    const load = async () => {
      try {
        const list = await IscsiService.ListLocalDisks() as LocalDiskInfo[]
        setLocalDisks(list || [])
        if (list?.length > 0 && !selectedDisk) setSelectedDisk(list[0].drive)
      } catch (e) { console.warn('[iSCSI] ListLocalDisks failed:', e) }
    }
    load()
  }, [])

  useEffect(() => {
    if (selectedLocalIP) setTargetIP(selectedLocalIP)
  }, [selectedLocalIP])

  useEffect(() => {
    setTargets([])
    setPartitionDevice('')
    setPartitionResult([])
    // 切换设备时立即清除上一台设备的挂载状态，防止短暂残留
    setSessions([])
    setStorageStatus(null)
    setStorageMode('local')
    stopMountPolling()
  }, [deviceIP, stopMountPolling])

  const refreshSessions = useCallback(async () => {
    if (!deviceIP) return
    const ip = deviceIP
    try {
      const list = await IscsiService.ListIscsi(ip) as SessionInfo[]
      if (currentDeviceRef.current !== ip) return
      console.log('[iSCSI] refreshSessions deviceIP=%s got=%d', ip, list?.length ?? 0)
      setSessions(list || [])
    } catch (e) {
      if (currentDeviceRef.current !== ip) return
      console.warn('[iSCSI] refreshSessions failed:', e)
      setSessions([])
    }
  }, [deviceIP])

  const refreshDisks = useCallback(async () => {
    try {
      const list = await IscsiService.ListDiskImages('') as DiskInfo[]
      setDiskImages(list || [])
    } catch (e) { console.warn('[iSCSI] ListDisks failed:', e) }
  }, [])

  const refreshStorageMode = useCallback(async (): Promise<StorageStatus | null> => {
    if (!deviceIP) return null
    const ip = deviceIP
    try {
      const status = await IscsiService.GetIscsiStatus(ip) as StorageStatus
      if (currentDeviceRef.current !== ip) return null
      console.log('[iSCSI] refreshStorageMode deviceIP=%s mode=%s mounted=%v', ip, status?.storageMode, status?.isMounted)
      setStorageStatus(status || null)
      setStorageMode(status?.storageMode || 'local')
      return status || null
    } catch (e) {
      if (currentDeviceRef.current !== ip) return null
      console.warn('[iSCSI] refreshStorageMode failed:', e)
      setStorageStatus(null)
      setStorageMode('local')
      return null
    }
  }, [deviceIP])

  // 启动有界轮询：每 5s 刷新一次存储状态，挂载生效或超 5 分钟后停止
  const startMountPolling = useCallback(() => {
    stopMountPolling()
    const ip = deviceIP
    if (!ip) return
    let ticks = 0
    mountPollRef.current = window.setInterval(async () => {
      if (currentDeviceRef.current !== ip) { stopMountPolling(); return }
      ticks++
      if (ticks > 60) { stopMountPolling(); return }
      const status = await refreshStorageMode()
      if (status?.isMounted) stopMountPolling()
    }, 5000)
  }, [deviceIP, refreshStorageMode, stopMountPolling])
  useEffect(() => stopMountPolling, [stopMountPolling])

  useEffect(() => { console.log('[iSCSI] refreshSessions effect triggered'); refreshSessions() }, [refreshSessions])
  useEffect(() => { refreshDisks() }, [refreshDisks])
  useEffect(() => { refreshStorageMode() }, [refreshStorageMode])

  const discoverTargets = async () => {
    if (!targetIP.trim()) { showToast(t('diskless:toast.enterTargetIp'), 'error'); return }
    setLoading(true)
    try {
      // 本地 Target Manager 数据是权威来源
      const disks = await IscsiService.ListDiskImages('') as DiskInfo[]
      setDiskImages(disks || [])
      const validIQNs = new Set((disks || []).filter(d => d.iqn).map(d => d.iqn))

      // discover 可能失败（无 IQN 时 iscsiadm 返回 exit 21），不阻断流程
      let filtered: TargetInfo[] = []
      try {
        const result = await IscsiService.DiscoverIscsi(deviceIP, `${targetIP.trim()}:3260`) as TargetInfo[]
        filtered = (result || []).filter(tk => validIQNs.has(tk.iqn))
      } catch {
        // discover 失败时用本地磁盘数据构造 target 列表
        filtered = (disks || []).filter(d => d.iqn).map(d => ({ iqn: d.iqn, portal: `${targetIP.trim()}:3260` }))
      }
      setTargets(filtered)
      if (!filtered.length && !(disks || []).length) showToast(t('diskless:toast.noTargetFound'))
    } catch (e: any) { showToast(t('diskless:toast.loadFailed', { error: e }), 'error') }
    finally { setLoading(false) }
  }

  const mountIscsi = async (iqn: string, portal: string, autoInit = false): Promise<boolean> => {
    setLoading(true)
    try {
      // 通过任务队列提交挂载任务（后端 single handler 执行，有进度）
      const taskId = await addTask({
        type: 'iscsiMount',
        deviceIps: [deviceIP],
        targets: [{ deviceIp: deviceIP, portal, iqn, autoInit, localIP: selectedLocalIP }],
        total: 1,
      })
      openPanel(taskId)
      showToast(t('diskless:toast.mountStarted'))
      // 启动有界轮询：挂载任务耗时较长，完成后 UI 自动更新挂载状态
      startMountPolling()
      // 异步刷新确认（任务完成后后端会推送状态更新）
      refreshSessions()
      refreshStorageMode()
      return true
    } catch (e: any) { showToast(t('diskless:toast.mountFailed', { error: e }), 'error'); return false }
    finally { setLoading(false) }
  }

  const umountIscsi = async (portal: string, iqn: string): Promise<boolean> => {
    setLoading(true)
    try {
      // 通过任务队列提交卸载任务（后端 single handler 执行，有进度）
      const taskId = await addTask({
        type: 'iscsiUmount',
        deviceIps: [deviceIP],
        targets: [{ deviceIp: deviceIP, portal, iqn, force: true }],
        total: 1,
      })
      openPanel(taskId)
      showToast(t('diskless:toast.unmountStarted'))
      stopMountPolling()
      // 乐观更新：立即清除挂载状态
      setStorageMode('local')
      setStorageStatus(null)
      setSessions([])
      // 异步刷新确认
      refreshSessions()
      refreshStorageMode()
      return true
    } catch (e: any) { showToast(t('diskless:toast.unmountFailed', { error: e }), 'error'); return false }
    finally { setLoading(false) }
  }

  // 远程卸载：磁盘被其他设备占用，提交卸载任务到占用方设备（不做本机状态乐观更新）
  const remoteUmountIscsi = async (targetDeviceIp: string, portal: string, iqn: string): Promise<boolean> => {
    if (!targetDeviceIp) return false
    try {
      const taskId = await addTask({
        type: 'iscsiUmount',
        deviceIps: [targetDeviceIp],
        targets: [{ deviceIp: targetDeviceIp, portal, iqn, force: true }],
        total: 1,
      })
      openPanel(taskId)
      showToast(t('diskless:toast.unmountStarted'))
      return true
    } catch (e: any) { showToast(t('diskless:toast.unmountFailed', { error: e }), 'error'); return false }
  }

  const createDisk = async (name: string, size: number, storageDir: string, diskType: string = 'fixed') => {
    if (!name.trim()) { showToast(t('diskless:toast.enterDiskName'), 'error'); return }
    if (size <= 0) { showToast(t('diskless:toast.enterValidDiskSize'), 'error'); return }
    setLoading(true)
    try {
      const result = await IscsiService.CreateDiskImage(deviceIP, '', name.trim(), size, diskType, storageDir) as any
      showToast(t('diskless:toast.diskCreated', { lun: result?.lun ?? '-' }))
      refreshDisks()
    } catch (e: any) { showToast(t('diskless:toast.createFailed', { error: e }), 'error') }
    finally { setLoading(false) }
  }

  const deleteDisk = async (name: string, force: boolean = false) => {
    if (!name.trim()) { showToast(t('diskless:toast.enterDiskName'), 'error'); return }
    setLoading(true)
    try {
      await IscsiService.DeleteDiskImage(deviceIP, '', name.trim(), force)
      showToast(t('diskless:toast.diskDeleted'))
      refreshDisks()
    } catch (e: any) { showToast(t('diskless:toast.deleteFailed', { error: e }), 'error') }
    finally { setLoading(false) }
  }

  const handlePartition = async (device: string, label: string) => {
    if (!device.trim()) { showToast(t('diskless:toast.enterBlockDevicePath'), 'error'); return }
    setLoading(true)
    try {
      const result = await IscsiService.PartitionIscsi(deviceIP, device.trim(), label) as string[]
      setPartitionResult(result || [])
      if (result?.length === 1) setPartitionDevice(result[0])
      showToast(t('diskless:toast.partitionSuccess', { partitions: result?.join(', ') || t('diskless:toast.partitionNoResult') }))
    } catch (e: any) { showToast(t('diskless:toast.partitionFailed', { error: e }), 'error') }
    finally { setLoading(false) }
  }

  const handleFormat = async (partition: string, fsType: string) => {
    if (!partition.trim()) { showToast(t('diskless:toast.enterPartitionPath'), 'error'); return }
    setLoading(true)
    try {
      await IscsiService.FormatIscsi(deviceIP, partition.trim(), fsType)
      showToast(t('diskless:toast.formatSuccess'))
    } catch (e: any) { showToast(t('diskless:toast.formatFailed', { error: e }), 'error') }
    finally { setLoading(false) }
  }

  return {
    localNics, selectedLocalIP, setSelectedLocalIP,
    localDisks, selectedDisk, setSelectedDisk,
    targetIP, setTargetIP,
    targets, sessions, diskImages, storageMode, storageStatus,
    loading,
    refreshSessions, refreshDisks, refreshStorageMode,
    discoverTargets, mountIscsi, umountIscsi, remoteUmountIscsi,
    createDisk, deleteDisk,
    partitionDevice, setPartitionDevice,
    partitionResult,
    handlePartition, handleFormat,
  }
}
