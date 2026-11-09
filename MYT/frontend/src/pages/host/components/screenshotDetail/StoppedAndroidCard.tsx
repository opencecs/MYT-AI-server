import { memo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo } from '../../../../types/index'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import { useConfirm } from '../../../../hooks/useConfirm'
import ConfirmDialog from '../../../../components/ConfirmDialog'
import { runSwitchOrStartTask } from '../../operations/system/slotActions'
import { showToast } from '../../../../components/Toast'
import { containerDisplayName } from '../../../../utils/containerName'
import { statusLabel, isTransientStatus } from '../../../../utils/containerStatus'
import { MirrorItem } from '../../../../types/index'
import ScreenshotInfoTip, { useInfoTipHover } from './ScreenshotInfoTip'

interface Props {
  android: AndroidInfo
  selected: boolean
  /** 切换实例选中（header 勾选圈） */
  onToggleSelect: (id: string) => void
  /** 缩放系数（滑条值/100） */
  scale: number
  /** 占位区宽高比（宽/高）：与正常卡同源（宿主下发的本页主流截图比例），保证等高 */
  aspectRatio: number
  /**
   * 全量实例列表（本设备）：开机前检查同坑位是否已有运行中云机（坑位独占，硬开会挤占）。
   * ScreenshotView flat 分支传 androids（展示集合含关机实例，含 status 可判定）。
   */
  allAndroids?: AndroidInfo[]
  /** 实例备注（悬停信息卡展示） */
  note?: string
  /** 镜像列表（悬停信息卡展示镜像/安卓版本） */
  mirrors?: MirrorItem[]
}

/**
 * 关机云机卡（云机 Tab 平铺模式，设置「云机展示=全部」时显示）：
 * header 与正常卡同构（勾选圈 + #坑位号 + 实例名），灰底占位区按容器真实状态显示文案
 * （exited「已关机」/ dead「异常退出」/ created「已创建」）——
 * 点击占位区即开机（2026-10-16 用户要求：底部「开机」按钮条已砍）——走任务队列
 * type='start'（runSwitchOrStartTask，后端 executor 执行 + 前端乐观置 starting）。
 * 过渡中状态（resetting/removing）禁点开机（操作已在进行）；dead 可开机。
 * 开机前检查同坑位是否已有运行中云机（deviceId+indexNum 相同、id 不同）：
 * 有则弹确认告知冲突风险（坑位独占），确认后继续 start（不改 switch 语义），
 * 取消则什么都不做；无冲突直接开机。
 * 开机成功后 androidlist:updated 推送 running 状态，宿主重渲染自动换回正常卡。
 */
const StoppedAndroidCard = memo(function StoppedAndroidCard({ android, selected, onToggleSelect, scale, aspectRatio, allAndroids = [], note = '', mirrors = [] }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { addTask, updateTask } = useTaskQueue()
  const { confirmAction, setConfirmAction, ask } = useConfirm()
  const [starting, setStarting] = useState(false)
  const infoTip = useInfoTipHover()

  const font = Math.max(7, Math.round(11 * scale))
  const padY = Math.max(2, Math.round(4 * scale))
  const padX = Math.max(4, Math.round(8 * scale))
  const placeholderFont = Math.max(8, Math.round(14 * scale))

  const handleStart = async () => {
    if (starting) return
    // 过渡中（resetting/removing 等）禁点：操作已在进行，再发起 start 会与设备端冲突
    if (isTransientStatus(android.status)) return
    // 同坑位冲突检查：deviceId + indexNum 相同、id 不同的其他 running 实例
    // （坑位独占：硬开会导致两台挤同一坑位，设备端行为不可控——告知风险后允许继续）
    const conflict = allAndroids.find(a =>
      a.id !== android.id && a.deviceId === android.deviceId &&
      a.indexNum === android.indexNum && a.status === 'running'
    )
    if (conflict) {
      ask(
        t('host:confirm.startConflict.title'),
        t('host:confirm.startConflict.message', { slot: android.indexNum, name: conflict.name }),
        () => doStart(),
      )
      return
    }
    await doStart()
  }

  const doStart = async () => {
    if (starting) return
    setStarting(true)
    try {
      const ok = await runSwitchOrStartTask(mode, addTask, updateTask, android, undefined, {
        start: t('host:slotView.switchStepStart', { name: android.name }),
      })
      if (!ok) showToast(t('host:toast.startFailed'), 'error')
    } finally {
      setStarting(false)
    }
  }

  // 占位区文案：按容器真实状态显示（dead「异常退出」/ created「已创建」/ exited「已关机」），
  // 不再硬编码「已关机」；本地 starting（点击后提交中）优先显示「开机中」
  const statusText = starting ? t('host:screenshot.starting') : statusLabel(android.status, t)
  // 可点开机：过渡中（resetting/removing/restarting）禁点；dead/created/exited 可开机
  const canStart = !starting && !isTransientStatus(android.status)

  return (
    <>
      <div
        className="rounded-lg border border-gray-200 bg-white shadow-sm overflow-hidden transition-all duration-200 hover:shadow-lg hover:-translate-y-0.5"
        style={{ ['--card-scale' as string]: scale, borderRadius: `calc(8px * var(--card-scale))` }}
        onMouseEnter={infoTip.handlers.onMouseEnter} onMouseMove={infoTip.handlers.onMouseMove} onMouseLeave={infoTip.handlers.onMouseLeave}
      >
      {/* header：与正常卡（CardHeader）同构——勾选圈 + #坑位号 + 实例名（无标签 chip：
          标签编辑仅运行中可用，关机只读展示时正常卡也常无 chip，这里保持简洁）。
          关机云机恢复可勾选（勾选源头闸门撤除）：header 点击切换选中，与正常卡同交互。 */}
      <div
        className="flex items-center gap-1 bg-gray-50 border-b border-gray-200 font-semibold text-gray-600 cursor-pointer"
        style={{ fontSize: font, padding: `${padY}px ${padX}px` }}
        onClick={e => { e.stopPropagation(); onToggleSelect(android.id) }}
        title={selected ? t('host:screenshot.clickToDeselect') : t('host:screenshot.clickToSelect')}
      >
        {selected ? (
          <span className="w-3 h-3 rounded-full bg-blue-500 flex items-center justify-center shadow shadow-blue-500/30">
            <svg className="w-2 h-2 text-white" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3"><polyline points="20 6 9 17 4 12" /></svg>
          </span>
        ) : (
          <span className="w-3 h-3 rounded-full bg-white/70 backdrop-blur-sm border border-gray-300/80 shadow-sm" />
        )}
        <span className="text-[10px] font-normal text-gray-400">#{android.indexNum}</span>
        {/* 名称：备注优先（note || containerDisplayName），与正常卡 CardHeader 同规则；
            hover title 恒为容器全名 android.name */}
        <span className="flex-1 min-w-0 truncate font-semibold text-gray-700" title={android.name}>{note || containerDisplayName(android.name, android.deviceId, android.indexNum)}</span>
      </div>
      {/* 占位区：按容器真实状态显示文案（不再硬编码「已关机」），点击即开机（底部按钮条
          已砍，这是唯一开机入口；starting/过渡中禁点防重复提交；hover 变色 + title 提示可点） */}
      <div
        className={`flex flex-col items-center justify-center gap-1 transition-colors ${canStart ? 'cursor-pointer hover:bg-[#e8ebf0]' : 'cursor-not-allowed opacity-60'}`}
        style={{ background: starting ? '#f0f2f5' : undefined, color: '#909399', fontSize: placeholderFont, aspectRatio: String(aspectRatio) }}
        onClick={e => { e.stopPropagation(); if (canStart) handleStart() }}
        title={canStart ? t('host:slotView.start') : statusText}
      >
        <svg className="w-5 h-5 text-gray-300" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="12" cy="12" r="10" /><line x1="4.93" y1="4.93" x2="19.07" y2="19.07" />
        </svg>
        <span className="text-xs">{statusText}</span>
      </div>
      </div>
      {/* 悬停信息卡（与运行卡一致）：渲染在卡片 div 之外（卡片有 overflow-hidden +
          hover translate transform，信息卡放里面会被裁剪且 fixed 相对卡片定位跑偏），
          fixed + pointer-events-none，不影响布局与点击 */}
      {infoTip.tipPos && (
        <ScreenshotInfoTip android={android} note={note} mirrors={mirrors}
          statusLabel={statusLabel(android.status, t)} slot={android.indexNum}
          x={infoTip.tipPos.x} y={infoTip.tipPos.y} />
      )}
      {/* 同坑位冲突确认（坑位独占：确认后继续 start，取消则不动作） */}
      {confirmAction && (
        <ConfirmDialog
          title={confirmAction.title}
          message={confirmAction.message}
          onConfirm={confirmAction.onConfirm}
          onCancel={() => setConfirmAction(null)}
        />
      )}
    </>
  )
})

export default StoppedAndroidCard
