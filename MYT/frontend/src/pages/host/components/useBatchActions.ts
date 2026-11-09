import { useTranslation } from 'react-i18next'
import { getDeviceId } from '../../../utils/deviceId'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { emitSlotStatus } from '../../../utils/wailsEvent'

type SlotStatus = 'starting' | 'stopped' | 'resetting' | 'restarting' | 'removing'
import * as ScreenshotService from '../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import { AndroidInfo } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import { useTaskQueue, waitForTaskEnd } from '../../../hooks/taskQueue/useTaskQueue'
import { TaskType } from '../../../hooks/taskQueue/types'

interface BatchActionsArgs {
  batchTargets: AndroidInfo[]
  setBatching: (s: string) => void
}

/**
 * useBatchActions 抽出 ActionBar 的批量操作逻辑。
 * 所有操作统一提交到任务队列，由**后端 executor 执行**（hasHandler=true 的类型：
 * restart/start/shutdown/delete/reset/closeS5/shake），前端只负责提交、乐观状态提示
 * 和终态收尾（toast）。⚠️ 不要在前端再直调 SDK 执行同一操作——会与后端执行双发
 * （历史上 delete 双发导致设备端大量 "No such container / removal already in progress"）。
 */
export function useBatchActions({ batchTargets, setBatching }: BatchActionsArgs) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { addTask } = useTaskQueue()

  // SDK 批量操作（重启/关机/删除等）：提交后后端 5 路并发执行，前端等终态收尾。
  // 走 batching 状态：执行中 ActionBar 全部批量按钮禁用（disabled={!!batching}）
  const runSdkBatch = async (type: TaskType, label: string) => {
    if (batchTargets.length === 0) return
    setBatching(label)
    try {
      const targets = batchTargets.map(a => ({ deviceIp: getDeviceId(a, mode), name: a.name }))
      const deviceIps = [...new Set(batchTargets.map(a => getDeviceId(a, mode)))]
      const taskId = await addTask({ type, targets, deviceIps, total: batchTargets.length })
      // 乐观状态提示（实际状态由后端执行 + androidlist 刷新驱动）：
      // 关机→stopped、删除→removing、重置→resetting、重启→restarting、开机→starting
      const slotStatus: SlotStatus =
        type === 'shutdown' ? 'stopped'
        : type === 'delete' ? 'removing'
        : type === 'reset' ? 'resetting'
        : type === 'restart' ? 'restarting'
        : 'starting'
      for (const a of batchTargets) {
        const id = getDeviceId(a, mode)
        ScreenshotService.InvalidateSlot(id, a.indexNum).catch(() => {})
        emitSlotStatus(`${id}_${a.indexNum}`, slotStatus)
      }
      const task = await waitForTaskEnd(taskId)
      if (!task) return // 超时：任务面板仍可见，不额外 toast
      if (task.status === 'completed') showToast(t('host:toast.batchCompleted', { label }), 'success')
      else if (task.status === 'failed') showToast(t('host:toast.batchFailedWithError', { label, error: task.error || `${task.failed}/${task.total}` }), 'error')
    } finally {
      setBatching('')
    }
  }

  // 队列型批量操作（reset/closeS5/shake）：同样后端 executor 执行（hasHandler）
  const runBatchViaQueue = async (type: TaskType, label: string, statusHint?: 'starting' | 'stopped' | 'resetting') => {
    if (batchTargets.length === 0) return
    const targets = batchTargets.map(a => ({ deviceIp: getDeviceId(a, mode), name: a.name }))
    const deviceIps = [...new Set(batchTargets.map(a => getDeviceId(a, mode)))]
    const taskId = await addTask({ type, targets, deviceIps, total: batchTargets.length })
    if (statusHint) {
      for (const a of batchTargets) emitSlotStatus(`${getDeviceId(a, mode)}_${a.indexNum}`, statusHint)
    }
    const task = await waitForTaskEnd(taskId)
    if (!task) return
    if (task.status === 'failed') showToast(t('host:toast.batchFailed', { count: task.failed, label }), 'error')
  }

  const doBatchRestart = () => runSdkBatch('restart', t('host:actionBar.restart'))
  const doBatchStart = () => runSdkBatch('start', t('host:actionBar.start'))
  const doBatchShutdown = () => runSdkBatch('shutdown', t('host:actionBar.stop'))
  const doBatchDelete = () => runSdkBatch('delete', t('host:actionBar.delete'))
  const doBatchReset = () => runBatchViaQueue('reset', t('host:actionBar.reset'), 'resetting')
  const doBatchCloseProxy = () => runBatchViaQueue('closeS5', t('host:actionBar.closeS5Proxy'))
  const doBatchShake = () => runBatchViaQueue('shake', t('host:actionBar.shake'))

  return {
    runBatchViaQueue, runSdkBatch,
    doBatchRestart, doBatchStart, doBatchShutdown, doBatchDelete, doBatchReset, doBatchCloseProxy, doBatchShake,
  }
}
