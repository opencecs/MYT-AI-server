import { memo } from 'react'
import { useTranslation } from 'react-i18next'
import { isRunningStatus, type CardStatus } from '../../../../utils/containerStatus'

interface Props {
  /** 截图读取状态（判断「切换」可用性：v2 模式下无此功能，直接隐藏） */
  status: CardStatus
  /** 容器状态（'running' 时才显示 ADB 按钮，与 android-client 语义一致） */
  androidStatus: string
  running: boolean
  /** 该坑位存在实例（非空坑位）→ 显示「切换」 */
  hasInstance: boolean
  /** v2 模式：无切换云机能力（与右键菜单同款裁剪） */
  v2Mode: boolean
  /**
   * 云机 Tab 紧凑场景：隐藏「创建」与「切换」（坑位/创建是主机侧概念，云机 Tab 不需要），
   * 只保留 ADB；主机 Tab（device 布局）不传，保持三按钮。
   */
  compact?: boolean
  /** 缩放系数（滑条值/100）：字号/间距随其缩放（android-client el-button--small 同源） */
  scale?: number
  /** 打开创建云机弹窗（宿主层持有 CreateInstanceModal）；不传则隐藏创建按钮 */
  onCreate?: () => void
  onAdb: () => void
  /** 「切换」= 实例切换（SwitchAndroidModal：同坑位所有云机，停当前开选中） */
  onSwitch: () => void
}

/** 截图卡片底部操作条：创建 / ADB / 切换（样式值来自 CloudManagement.vue
 *  .cloud-machine-info-small / .el-button--small：#f8f9fa 底、#e9ecef 顶边、
 *  字号 max(8px,12px*scale)、padding 4px*scale × 8px*scale、间距 8px*scale、
 *  容器 padding 10px*scale）。按钮 flex-1 均分 + nowrap，窄卡 overflow-x-auto 兜底。
 *  compact（云机 Tab）：只显示 ADB；无任何可见按钮时整条不渲染（不留空灰条）。 */
const CardActionBar = memo(function CardActionBar({ status, androidStatus, running, hasInstance, v2Mode, compact = false, scale = 1, onCreate, onAdb, onSwitch }: Props) {
  const { t } = useTranslation()
  // ADB 仅精确 running 可用（restarting/starting 中不给——ADB 还没就绪）
  const showAdb = isRunningStatus(androidStatus)
  const showSwitch = !compact && hasInstance && !v2Mode
  const showCreate = !compact && !!onCreate
  // 全部按钮不可见 → 不渲染按钮条（避免空灰条）
  if (!showAdb && !showSwitch && !showCreate) return null
  // 源码公式（CloudManagement.vue L2446-2472），下限防极小缩放时不可读
  const btnFont = Math.max(8, Math.round(12 * scale))
  const btnPadY = Math.max(2, Math.round(4 * scale))
  const btnPadX = Math.max(4, Math.round(8 * scale))
  const barGap = Math.max(4, Math.round(8 * scale))
  const barPad = Math.max(4, Math.round(10 * scale))

  const btnCls = 'whitespace-nowrap rounded bg-blue-500 text-white hover:brightness-110 transition-all cursor-pointer'
  const btnStyle = { fontSize: btnFont, padding: `${btnPadY}px ${btnPadX}px` }

  // 对齐规则：全部居中（2026-10-11 用户要求：1/2/3 个按钮都 justify-center，
  // 关机占位卡「创建+切换」两按钮也居中，与云机 Tab 单 ADB 居中一致）

  return (
    <div className={`flex items-center justify-center bg-[#f8f9fa] border-t border-[#e9ecef] overflow-x-auto`} style={{ gap: barGap, padding: barPad }}>
      {showCreate && (
        <button
          onClick={e => { e.stopPropagation(); onCreate() }}
          className={btnCls}
          style={btnStyle}
          title={t('button.create')}
        >
          {t('button.create')}
        </button>
      )}
      {showAdb && (
        <button
          onClick={e => { e.stopPropagation(); onAdb() }}
          className={btnCls}
          style={{ ...btnStyle, background: '#67c23a', color: '#fff' }}
          title="ADB"
        >
          ADB
        </button>
      )}
      {showSwitch && (
        <button
          onClick={e => { e.stopPropagation(); onSwitch() }}
          disabled={status === 'starting'}
          className={btnCls + ' disabled:opacity-40 disabled:cursor-not-allowed'}
          style={btnStyle}
          title={t('host:slotView.switchAndroid')}
        >
          {t('button.switch')}
        </button>
      )}
    </div>
  )
})

export default CardActionBar
