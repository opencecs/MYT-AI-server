import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo } from '../../../../types/index'
import { emitSlotStatus } from '../../../../utils/wailsEvent'
import * as ScreenshotService from '../../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import { AskFn } from '../../../../hooks/useConfirm'
import { useTaskQueue, waitForTaskEnd } from '../../../../hooks/taskQueue/useTaskQueue'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../utils/deviceId'
import { showToast } from '../../../../components/Toast'
import { useModalEnter } from '../../../../hooks/useModalAnimation'

interface Props {
  android: AndroidInfo
  ask: AskFn
  onClose: () => void
}

const optCls = 'w-full px-4 py-3 text-left text-sm rounded-lg border hover:border-blue-300 cursor-pointer'

// 重置容器二选一：重置并开机 / 仅重置不开机。
// 与 ActionBar 批量重置一致：提交 reset 任务到任务队列（后端 executor 执行），
// 前端只做乐观状态提示与终态接收——不直调 SDK，避免与任务双发。
function ResetModal({ android, ask, onClose }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { addTask } = useTaskQueue()

  const doReset = (start: boolean) => {
    const id = getDeviceId(android, mode)
    const label = t('host:actionBar.reset')
    ask(t('host:reset.confirmTitle'), t('host:reset.confirmMessage'), async () => {
      const taskId = await addTask({ type: 'reset', targets: [{ deviceIp: id, name: android.name, start }], deviceIps: [id], total: 1 })
      // 乐观状态提示（实际状态由后端执行 + WS container/boot 事件驱动）
      ScreenshotService.InvalidateSlot(id, android.indexNum).catch(() => {})
      // 乐观状态提示（实际状态由后端执行 + WS container/boot 事件驱动）：
      // 重置后无论是否接开机，先显示「重置中」（resetting），后续由 start/running 事件推进
      emitSlotStatus(`${id}_${android.indexNum}`, 'resetting')
      const task = await waitForTaskEnd(taskId)
      if (!task) return // 超时：任务面板仍可见，不额外 toast
      if (task.status === 'completed') showToast(t('host:toast.batchCompleted', { label }), 'success')
      else if (task.status === 'failed') showToast(t('host:toast.batchFailedWithError', { label, error: task.error || `${task.failed}/${task.total}` }), 'error')
    })
    onClose()
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[380px] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-base font-semibold text-gray-800">{t('host:reset.title')}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="p-6 space-y-2.5">
          <button onClick={() => doReset(true)} className={`${optCls} border-blue-200 bg-blue-50/50`}>
            <div className="text-gray-700 font-medium">{t('host:reset.resetAndStart')}</div>
            <div className="text-xs text-gray-400 mt-0.5">{t('host:reset.resetAndStartHint')}</div>
          </button>
          <button onClick={() => doReset(false)} className={`${optCls} border-gray-200`}>
            <div className="text-gray-700 font-medium">{t('host:reset.resetOnly')}</div>
            <div className="text-xs text-gray-400 mt-0.5">{t('host:reset.resetOnlyHint')}</div>
          </button>
        </div>
      </div>
    </div>
  )
}

export default ResetModal
