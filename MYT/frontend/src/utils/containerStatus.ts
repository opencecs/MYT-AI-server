/**
 * 容器状态共享工具：可见性谓词 / i18n 文案 / tag 配色，三处同源。
 *
 * 背景：`running || starting` 谓词散落 7 处（useHostManagement / MaintenancePage /
 * MultiDeviceSlotView / DeviceAndroidList / DeviceListItem / ScreenshotView / SlotView），
 * 状态标签文案与配色也在 slotViewUtils / SwitchAndroidModal 各写一份——
 * 补全状态全量支持（restarting/resetting/removing/dead/paused）时必然演化分叉，
 * 收拢到一处。
 *
 * 状态可见性口径（「云机展示=全部」开关）：
 * - showOffline=true（全部）：所有状态可见——每个状态在视图都有 tag/文案区分。
 * - showOffline=false（默认，仅「与开机相关」）：running / starting / restarting /
 *   resetting 可见；关机类 exited / dead / removing **与 created 均不展示**。
 *   created 仅在 showOffline=true 时可见（2026-10-17 曾放行、随后按用户最新拍板回退：
 *   created 不放行——否则关闭「云机展示」时新建的未开机云机会常驻冒出）。
 *   创建过程可见性由 starting 覆盖：创建后立即 start 的场景（标准流程）一进入
 *   starting 即显示；仅停留在 created 的窗口期（未发 start）不可见。
 * - paused 归一为 starting（设备端 pause 后 ADB 仍可用，UI 按「暂离运行」理解）。
 */

/** 与开机相关的状态（showOffline=false 时的可见集合） */
const RUNNABLE_STATUSES = new Set(['running', 'starting', 'restarting', 'resetting'])

/** 过渡中（操作进行中）的状态：不可再发起开机/重启等新操作 */
const TRANSIENT_STATUSES = new Set(['starting', 'restarting', 'resetting', 'removing'])

/** 状态归一：paused → starting，其余小写透传 */
export function normalizeStatus(s: string | undefined): string {
  const v = (s || '').toLowerCase()
  return v === 'paused' ? 'starting' : v
}

/** 是否「与开机相关」（running/starting/restarting/resetting；paused 归一 starting） */
export function isRunnableStatus(s: string | undefined): boolean {
  return RUNNABLE_STATUSES.has(normalizeStatus(s))
}

/**
 * 展示可见性（「云机展示」开关的统一谓词，6 处过滤同源——useHostManagement /
 * DeviceListItem / DeviceAndroidList / MultiDeviceSlotView / SlotView onlyRunning /
 * MaintenancePage）：showOffline=true 全状态可见；false 时仅开机相关可见，
 * created 同样不可见（见文件头口径）。
 */
export function isStatusVisible(s: string | undefined, showOffline: boolean): boolean {
  return showOffline || isRunnableStatus(s)
}

/** 是否过渡中（操作进行中，禁再发新操作） */
export function isTransientStatus(s: string | undefined): boolean {
  return TRANSIENT_STATUSES.has(normalizeStatus(s))
}

/** 是否精确运行中（ADB 等需要真跑着的能力按钮用；restarting 中不给） */
export function isRunningStatus(s: string | undefined): boolean {
  return normalizeStatus(s) === 'running'
}

/**
 * 截图卡片「截图区中央」的状态值（ScreenshotCard 的 status prop）。
 * 之前散落在 ScreenshotCard / ScreenshotGroup / ScreenshotView / CardActionBar 四处
 * 各写一份 union，扩展中间态（created/restarting/resetting/removing）时必然分叉，收拢到此。
 */
export type CardStatus =
  | 'loading'      // running 容器截图未就绪（兜底「加载中」）
  | 'starting'     // 开机中
  | 'screenshot'   // 截图已就绪
  | 'stopped'      // 已关机（占位样式）
  | 'created'      // 已创建（未开机）
  | 'restarting'   // 重启中
  | 'resetting'    // 重置中
  | 'removing'     // 删除中

/**
 * 容器真实状态 → 截图区卡片状态的兜底推导（statusMap 无该 key 的截图记录时用）。
 * 过渡中状态按真实状态透传（截图区显示「重启中/重置中/删除中」等文案而非一律「加载中」，
 * 2026-10-17 用户反馈）；running 无截图 / exited / dead 等其余状态兜底 loading
 * （exited/dead 不会走到 ScreenshotCard——分别由占位卡/关机卡渲染，此处只是防御）。
 */
export function cardStatusFromAndroid(s: string | undefined): CardStatus {
  switch (normalizeStatus(s)) {
    case 'starting': return 'starting'
    case 'created': return 'created'
    case 'restarting': return 'restarting'
    case 'resetting': return 'resetting'
    case 'removing': return 'removing'
    default: return 'loading'
  }
}

/**
 * 状态文案（i18n，host:slotView.status* 命名空间）。未知状态返回原文（兜底显示）。
 */
export function statusLabel(s: string | undefined, t: (k: string) => string): string {
  switch (normalizeStatus(s)) {
    case 'created': return t('host:slotView.statusCreated')
    case 'running': return t('host:slotView.statusRunning')
    case 'starting': return t('host:slotView.statusStarting')
    case 'resetting': return t('host:slotView.statusResetting')
    case 'restarting': return t('host:slotView.statusRestarting')
    case 'removing': return t('host:slotView.statusRemoving')
    case 'exited': return t('host:slotView.statusStopped')
    case 'dead': return t('host:slotView.statusDead')
    default: return s || ''
  }
}

/**
 * 状态 tag 配色（Element 风格浅底深字，沿用 slotViewUtils.statusBadge 体系）：
 * 绿=running / 蓝=starting,resetting,restarting / 蓝=created / 灰=exited /
 * 琥珀=removing / 红=dead。未知状态灰。
 */
export function statusTagCls(s: string | undefined): string {
  switch (normalizeStatus(s)) {
    case 'running': return 'bg-green-50 text-green-600'
    case 'starting':
    case 'resetting':
    case 'restarting': return 'bg-blue-50 text-blue-500'
    case 'created': return 'bg-blue-50 text-blue-500'
    case 'exited': return 'bg-gray-100 text-gray-500'
    case 'removing': return 'bg-amber-50 text-amber-600'
    case 'dead': return 'bg-red-50 text-red-500'
    default: return 'bg-gray-50 text-gray-500'
  }
}
