import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as ScreenshotService from '../../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import { AndroidInfo } from '../../../../types/index'
import { getDeviceId } from '../../../../utils/deviceId'
import { emitSlotStatus } from '../../../../utils/wailsEvent'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { useTaskQueue, waitForTaskEnd } from '../../../../hooks/taskQueue/useTaskQueue'

/** 开机/切换云机走任务队列。
 *  - 仅开机（type='start'）：有后端 handler，由后端 executor 执行，前端只提交并等待结果
 *    （⚠️ 前端再直调 StartAndroid 会与后端执行双发）
 *  - 切换（type='switchAndroid'）：无后端 handler，前端编排执行——先停当前运行中云机，再启选中云机
 *  返回 true 表示成功提交并执行完毕。 */
export async function runSwitchOrStartTask(
  mode: ReturnType<typeof useDeployMode>['mode'],
  addTask: ReturnType<typeof useTaskQueue>['addTask'],
  updateTask: ReturnType<typeof useTaskQueue>['updateTask'],
  target: AndroidInfo,
  running?: AndroidInfo,
  labels: { start?: string; stop?: string } = {},
): Promise<boolean> {
  const targetIP = getDeviceId(target, mode)

  // 空标识符防护：设备信息未就绪（ip/device_id 尚未填充）时不执行，避免"设备标识符为空"失败
  if (!targetIP) {
    return false
  }

  const steps: { label: string; fn: () => Promise<unknown>; deviceIp: string; name: string }[] = []
  if (running && running.name !== target.name) {
    const runningIP = getDeviceId(running, mode)
    steps.push({
      label: labels.stop || `Stop ${running.name}`,
      fn: () => SdkApiService.StopAndroid(runningIP, running.name),
      deviceIp: runningIP, name: running.name,
    })
  }
  if (target.status !== 'running') {
    steps.push({
      label: labels.start || `Start ${target.name}`,
      fn: () => SdkApiService.StartAndroid(targetIP, target.name),
      deviceIp: targetIP, name: target.name,
    })
  }
  if (steps.length === 0) return true // 已是目标运行中

  const deviceIps = [targetIP]
  const type = running && running.name !== target.name ? 'switchAndroid' : 'start'
  const taskId = await addTask({
    type,
    targets: steps.map(s => ({ deviceIp: s.deviceIp, name: s.name, label: s.label })),
    deviceIps,
    total: steps.length,
  })

  // 乐观状态（与 useBatchActions.runSdkBatch 对齐）：提交后立即置过渡状态 + 清旧截图，
  // 不等后端执行+查询链——公网模式该链秒级延迟，期间 UI 停留旧状态表现为"点了没反应"。
  // 意图保护（statusIntent 30s）防执行期间的旧状态回灌。
  if (running && running.name !== target.name) {
    const runningIP = getDeviceId(running, mode)
    ScreenshotService.InvalidateSlot(runningIP, running.indexNum).catch(() => {})
    emitSlotStatus(`${runningIP}_${running.indexNum}`, 'stopped')
  }
  if (target.status !== 'running') {
    ScreenshotService.InvalidateSlot(targetIP, target.indexNum).catch(() => {})
    emitSlotStatus(`${targetIP}_${target.indexNum}`, 'starting')
  }

  // start 有后端 handler（后端 executor 执行）；switchAndroid 无 handler（前端编排执行）
  if (type === 'switchAndroid') {
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    let completed = 0, failed = 0
    for (const s of steps) {
      try {
        await s.fn()
        completed++
      } catch {
        failed++
      }
      const progress = Math.round((completed + failed) / steps.length * 100)
      updateTask(taskId, { progress, completed, failed, status: 'running' })
      if (failed > 0) break
    }
    const ok = failed === 0
    updateTask(taskId, { status: ok ? 'completed' : 'failed', progress: 100, completed, failed, endTime: Date.now() })
    return ok
  }

  // 后端执行：等待终态
  const task = await waitForTaskEnd(taskId)
  return !!task && task.status === 'completed'
}
