import type { DiskInfo } from './useIscsiTarget'

export type { DiskInfo }

export interface LocalNicInfo {
  name: string
  ip: string
  mac: string
}

export interface LocalDiskInfo {
  drive: string
  label: string
  totalBytes: number
  freeBytes: number
}

export interface TargetInfo {
  iqn: string
  portal: string
}

export interface SessionInfo {
  iqn: string
  portal: string
  device: string
  localIP: string
}

export interface StorageStatus {
  storageMode: string
  mountPoint: string
  device: string
  isMounted: boolean
  totalMB: number
  usedMB: number
  availMB: number
}

export interface IscsiManager {
  localNics: LocalNicInfo[]
  selectedLocalIP: string
  setSelectedLocalIP: (ip: string) => void
  localDisks: LocalDiskInfo[]
  selectedDisk: string
  setSelectedDisk: (d: string) => void
  targetIP: string
  setTargetIP: (ip: string) => void
  targets: TargetInfo[]
  sessions: SessionInfo[]
  diskImages: DiskInfo[]
  storageMode: string
  storageStatus: StorageStatus | null
  loading: boolean
  refreshSessions: () => Promise<void>
  refreshDisks: () => Promise<void>
  refreshStorageMode: () => Promise<StorageStatus | null>
  discoverTargets: () => Promise<void>
  /** 提交挂载任务（任务队列）；返回任务是否提交成功 */
  mountIscsi: (iqn: string, portal: string, autoInit?: boolean) => Promise<boolean>
  /** 提交本机卸载任务（任务队列）；返回任务是否提交成功 */
  umountIscsi: (portal: string, iqn: string) => Promise<boolean>
  /** 提交远程卸载任务（磁盘被其他设备占用时）；返回任务是否提交成功 */
  remoteUmountIscsi: (targetDeviceIp: string, portal: string, iqn: string) => Promise<boolean>
  createDisk: (name: string, size: number, storageDir: string) => Promise<void>
  deleteDisk: (name: string, force?: boolean) => Promise<void>
  partitionDevice: string
  setPartitionDevice: (d: string) => void
  partitionResult: string[]
  handlePartition: (device: string, label: string) => Promise<void>
  handleFormat: (partition: string, fsType: string) => Promise<void>
}
