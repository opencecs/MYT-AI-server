import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { showToast } from '../../../components/Toast'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'
import { makeArithmeticChallenge } from '../../../utils/arithmeticChallenge'
import type { ConfirmChallenge } from '../../../components/ConfirmDialog'

/**
 * 格式化磁盘（设备级清盘）——单层确认弹窗：
 * 目标 IP + 红色后果清单 + 算术题（答对才能点「继续」，直接下发任务）。
 * 原两层结构（内容弹窗 + ConfirmDialog 第二层）已合一：算术确认取代二次确认，
 * 确认按钮在答案不匹配时置灰，误触风险由「后果清单 + 算术题」双重承担。
 * 执行交给任务队列（formatDisk 任务类型，后端 handler 执行），不再在前端直调 SDK。
 */
export function FormatDiskContent({ deviceIP, onClose }: { deviceIP: string; onClose?: () => void }) {
  const { t } = useTranslation()
  const { addTask, openPanel } = useTaskQueue()
  const [challenge, setChallenge] = useState<ConfirmChallenge>(makeArithmeticChallenge)
  const [answer, setAnswer] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const answerOk = answer.trim() === challenge.expectedAnswer

  const handleFormat = async () => {
    if (!answerOk || submitting) return
    setSubmitting(true)
    try {
      const taskId = await addTask({
        type: 'formatDisk',
        targets: [{ deviceIp: deviceIP }],
        deviceIps: [deviceIP],
        total: 1,
      })
      if (taskId) {
        showToast(t('maintenance:format.triggered'), 'success')
        openPanel(taskId)
        onClose?.()
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="space-y-3">
      {/* 目标设备单独一行：避免像之前那样把 IP 埋在长段落中间 */}
      <div className="flex items-center justify-between text-xs bg-gray-50 border border-gray-200 rounded-lg px-3 py-2">
        <span className="text-gray-500">{t('maintenance:format.target')}</span>
        <span className="font-mono text-gray-800">{deviceIP}</span>
      </div>

      <div className="text-xs bg-red-50 border border-red-200 rounded-lg px-3 py-2">
        <div className="flex items-center gap-1.5 font-medium text-red-700">
          <AlertTriangle size={13} />
          {t('maintenance:format.consequenceTitle')}
        </div>
        <ul className="mt-1 space-y-0.5 text-red-600">
          <li>• {t('maintenance:format.consequence1')}</li>
          <li>• {t('maintenance:format.consequence2')}</li>
          <li>• {t('maintenance:format.consequence3')}</li>
        </ul>
      </div>

      {/* 算术确认：题目挂载时生成一次；「换一题」可换题并清空输入（防止瞎蒙后残留答案） */}
      <div className="text-xs border border-gray-200 rounded-lg px-3 py-2">
        <div className="flex items-center justify-between mb-1.5">
          <span className="text-gray-600">{t('maintenance:format.challengeHint')}</span>
          <button
            onClick={() => { setChallenge(makeArithmeticChallenge()); setAnswer('') }}
            className="text-blue-500 hover:text-blue-600"
          >
            {t('maintenance:format.challengeRefresh')}
          </button>
        </div>
        <div className="flex items-center gap-2">
          <span className="font-mono font-semibold text-gray-800 text-sm">{challenge.question}</span>
          <input
            type="text"
            autoFocus
            value={answer}
            onChange={e => setAnswer(e.target.value)}
            placeholder={t('maintenance:format.challengePlaceholder')}
            className="w-24 px-2 py-1 text-xs border border-gray-300 rounded focus:outline-none focus:ring-1 focus:ring-blue-400"
          />
        </div>
      </div>

      <div className="flex justify-end gap-2">
        <button onClick={onClose} className="px-3 py-1.5 text-xs bg-white border border-gray-200 rounded hover:bg-gray-50 cursor-pointer">
          {t('button.cancel')}
        </button>
        <button
          onClick={handleFormat}
          disabled={!answerOk || submitting}
          className="px-3 py-1.5 text-xs bg-red-500 text-white rounded hover:bg-red-600 disabled:opacity-40 disabled:hover:bg-red-500 cursor-pointer disabled:cursor-not-allowed"
        >
          {t('maintenance:format.continue')}
        </button>
      </div>
    </div>
  )
}
