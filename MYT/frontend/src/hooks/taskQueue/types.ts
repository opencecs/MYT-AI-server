/** 任务状态（queued 为后端任务队列的初始排队态） */
export type TaskStatus = 'pending' | 'queued' | 'running' | 'completed' | 'failed' | 'canceled'

/** 任务类型 */
export type TaskType =
  | 'downloadImage'    // 下载镜像
  | 'uploadImage'      // 批量上传镜像
  | 'uploadFile'       // 批量上传文件
  | 'create'           // 批量创建云机
  | 'copy'             // 复制云机
  | 'restart'          // 批量重启
  | 'start'            // 批量开机
  | 'reset'            // 批量重置
  | 'shutdown'         // 批量关机
  | 'delete'           // 批量删除
  | 'switchModel'      // 批量切换机型
  | 'updateImage'      // 批量更新镜像
  | 'cleanDisk'        // 清理磁盘
  | 'uninstallAgent'   // 卸载智能体
  | 'installVpn'       // 安装 VPN 客户端
  | 'uninstallVpn'     // 卸载 VPN 客户端
  | 'startVpn'         // 启动 VPN 客户端
  | 'stopVpn'          // 停止 VPN 客户端
  | 'closeS5'          // 批量关闭 S5 代理
  | 'shake'            // 批量摇一摇
  | 'importAndroid'   // 导入安卓
  | 'exportAndroid'   // 导出安卓
  | 'moveAndroid'     // 跨设备移动安卓
  | 'sdkUpgrade'      // SDK 升级
  | 'sdkInstall'      // SDK 指定版本安装（客户端上传 + 轮询确认版本变化）
  | 'cleanLog'         // 日志清理
  | 'cleanCache'       // 缓存清理
  | 'cleanImage'       // 镜像清理
  | 'formatDisk'       // 格式化磁盘（设备级清盘）
  | 'installMytPanel'   // 安装魔云互联
  | 'uninstallMytPanel' // 卸载魔云互联
  | 'installTunnel'     // 安装公网穿透（服务器 frps + 设备 frpc）
  | 'uninstallTunnel'   // 卸载公网穿透
  | 'switchAndroid'    // 切换云机（停当前+启选中）
  | 'deleteDeviceModel' // 批量删除盒子本地机型
  | 'iscsiMount'        // 无盘挂载
  | 'iscsiUmount'      // 无盘卸载
  | 'teboxInstall'     // tebox SDK 安装（SSH + install-sdk.sh）
  | 'superSdkInstall'  // 超级 SDK 安装（v2 主机：镜像拉取 + myt_sdk 容器 + 81 回验）

/** 任务类型 → i18n key */
export const taskTypeLabel: Record<TaskType, string> = {
  downloadImage: 'taskQueue.type.downloadImage',
  uploadImage: 'taskQueue.type.uploadImage',
  uploadFile: 'taskQueue.type.uploadFile',
  create: 'taskQueue.type.create',
  copy: 'taskQueue.type.copy',
  restart: 'taskQueue.type.restart',
  start: 'taskQueue.type.start',
  reset: 'taskQueue.type.reset',
  shutdown: 'taskQueue.type.shutdown',
  delete: 'taskQueue.type.delete',
  switchModel: 'taskQueue.type.switchModel',
  updateImage: 'taskQueue.type.updateImage',
  cleanDisk: 'taskQueue.type.cleanDisk',
  uninstallAgent: 'taskQueue.type.uninstallAgent',
  installVpn: 'taskQueue.type.installVpn',
  uninstallVpn: 'taskQueue.type.uninstallVpn',
  startVpn: 'taskQueue.type.startVpn',
  stopVpn: 'taskQueue.type.stopVpn',
  closeS5: 'taskQueue.type.closeS5',
  sdkUpgrade: 'taskQueue.type.sdkUpgrade',
  sdkInstall: 'taskQueue.type.sdkInstall',
  cleanLog: 'taskQueue.type.cleanLog',
  cleanCache: 'taskQueue.type.cleanCache',
  cleanImage: 'taskQueue.type.cleanImage',
  formatDisk: 'taskQueue.type.formatDisk',
  installMytPanel: 'taskQueue.type.installMytPanel',
  uninstallMytPanel: 'taskQueue.type.uninstallMytPanel',
  installTunnel: 'taskQueue.type.installTunnel',
  uninstallTunnel: 'taskQueue.type.uninstallTunnel',
  switchAndroid: 'taskQueue.type.switchAndroid',
  deleteDeviceModel: 'taskQueue.type.deleteDeviceModel',
  iscsiMount: 'taskQueue.type.iscsiMount',
  iscsiUmount: 'taskQueue.type.iscsiUmount',
  teboxInstall: 'taskQueue.type.teboxInstall',
  superSdkInstall: 'taskQueue.type.superSdkInstall',
  shake: 'taskQueue.type.shake',
  importAndroid: 'taskQueue.type.importAndroid',
  exportAndroid: 'taskQueue.type.exportAndroid',
  moveAndroid: 'taskQueue.type.moveAndroid',
}

/** 任务状态 → i18n key */
export const taskStatusLabel: Record<TaskStatus, string> = {
  pending: 'taskQueue.status.pending',
  queued: 'taskQueue.status.queued',
  running: 'taskQueue.status.running',
  completed: 'taskQueue.status.completed',
  failed: 'taskQueue.status.failed',
  canceled: 'taskQueue.status.canceled',
}

/** 设备维度进度（镜像上传/文件上传） */
export interface DeviceProgress {
  total: number
  completed: number
  failed: number
  currentProgress?: number
  stage?: string
  error?: string
  /** 上传阶段：已传字节 */
  uploaded?: number
  /** 上传阶段：总字节 */
  totalBytes?: number
  /** 上传速度 (MB/s) */
  speed?: number
  /** 当前上传的文件名 */
  currentFile?: string
}

/** 复制云机日志条目 */
export interface CopyLog {
  current: number
  total: number
  name: string
  status: 'running' | 'success' | 'failed'
  message: string
}

/** 智能体卸载步骤 */
export interface AgentStep {
  index: number
  name: string
  status: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  message?: string
}

/** 任务条目 */
export interface TaskQueueItem {
  id: string
  type: TaskType
  status: TaskStatus
  total: number
  completed: number
  failed: number
  progress: number // 0-100
  targets: unknown[]
  deviceIps: string[]
  deviceProgress: Record<string, DeviceProgress>
  startTime: number | null
  endTime: number | null
  failedTargets: Array<{ deviceIp?: string; machineName?: string; slot?: number; error: string; [key: string]: unknown }>
  /** 已确认的失败数（点开详情后记录当时的 failed；角标 = failed > failedAck 的任务数）。随任务持久化，重启不丢 */
  failedAck?: number
  /** 任务级错误文案（后端 Task.Error，失败时非空） */
  error?: string

  // 创建云机：两阶段进度
  currentStep?: 'image' | 'create' | null
  imageProgress?: number | null
  // 创建云机：本地镜像 ID（取消时调 CancelAllUploads 中断推送）
  mirrorId?: string

  // 复制云机：流式日志
  copyLogs?: CopyLog[]

  // 清理磁盘：步骤
  totalSteps?: number
  currentStepIndex?: number
  steps?: string[]

  // 智能体：步骤进度
  agentSteps?: AgentStep[]
  agentAction?: 'install' | 'update' | 'uninstall'

  // 导出安卓：阶段进度（设备端 SSE 阶段 config/snapshot/.../pack，或 downloading）
  exportStage?: string
  exportMsg?: string
  exportProgress?: number

  // 导入安卓：阶段进度（设备端 SSE 阶段 upload/verify/extract/restore/start）
  importStage?: string
  importMsg?: string

  // 跨设备移动：阶段进度（preparing / exporting / transferring / cleanup）
  moveStage?: string
  moveMessage?: string

  // 元数据（各类型不同）
  [key: string]: unknown
}

/** 创建任务的输入参数 */
export interface CreateTaskInput {
  type: TaskType
  targets?: unknown[]
  deviceIps?: string[]
  total?: number
  [key: string]: unknown
}
