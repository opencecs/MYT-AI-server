import { memo } from 'react'
import { useTranslation } from 'react-i18next'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { statusLabel } from '../../../../utils/containerStatus'
import CardActionBar from './CardActionBar'

interface Props {
  /** offline = 关机实例占位（按 androidStatus 显示真实状态文案）；empty = 真空坑位（「空坑位」） */
  kind: 'offline' | 'empty'
  slotIdx: number
  /** 占位区宽高比（宽/高）：与正常卡同源（宿主下发的本页主流截图比例），保证等高 */
  aspectRatio: number
  scale: number
  /** 实例真实容器状态（offline 时用于显示「异常退出/已创建/已关机」等区分文案） */
  androidStatus?: string
  /** 打开「创建云机」弹窗（宿主层持有）；不传则按钮条只剩切换/为空 */
  onCreate?: () => void
  /** 点击占位/切换：弹「切换实例」（pc-client 既有交互，保留） */
  onOpen: () => void
}

/**
 * 坑位占位卡（关机实例 / 真空坑位），与 ScreenshotCard 完全同构：
 * 同一外框（白底/边框/圆角/hover 上浮）+ 同一 header 布局（勾选圈位置留空 + #坑位号 +
 * 实例名位置占位）+ 占位区（#f0f2f5 底 #909399 字 max(8px,14px*scale)）+ 同一按钮条布局
 * （按钮自然宽度靠左，不 flex-1 撑满——与正常卡按钮同尺寸同位置，只是没有 ADB/切换）。
 * 样式值来自 CloudManagement.vue。
 */
const SlotPlaceholderCard = memo(function SlotPlaceholderCard({ kind, slotIdx, aspectRatio, scale = 1, androidStatus = 'stopped', onCreate, onOpen }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const font = Math.max(7, Math.round(11 * scale))
  const padY = Math.max(2, Math.round(4 * scale))
  const padX = Math.max(4, Math.round(8 * scale))
  const placeholderFont = Math.max(8, Math.round(14 * scale))

  return (
    <div
      className="rounded-lg border border-gray-200 bg-white shadow-sm overflow-hidden transition-all duration-200 hover:shadow-lg hover:-translate-y-0.5"
      style={{ borderRadius: `calc(8px * ${scale})` }}
    >
      {/* header：与正常卡同构——左侧坑位号（勾选圈位置留空，占位卡不参与勾选）、
          实例名位置为空（无实例名）、右侧无 tag（状态/到期 tag 已按用户要求移除） */}
      <div className="flex items-center gap-1.5 bg-gray-50 border-b border-gray-200 font-semibold text-gray-600"
        style={{ fontSize: font, padding: `${padY}px ${padX}px` }}>
        <span className="text-[10px] font-normal text-gray-400">#{slotIdx}</span>
        <span className="flex-1" />
      </div>
      {/* 占位区：点击弹「切换实例」（源码空坑位无点击交互，pc-client 保留既有入口） */}
      <div
        className="flex items-center justify-center cursor-pointer"
        style={{ background: '#f0f2f5', color: '#909399', fontSize: placeholderFont, aspectRatio: String(aspectRatio) }}
        onClick={onOpen}
        title={t('host:slotView.switchAndroid')}
      >
        {kind === 'offline' ? statusLabel(androidStatus, t) : t('host:slotView.emptySlot')}
      </div>
      {/* 标签行已删（2026-10-10 用户要求）：标签展示/编辑已在 header（icon+数量 chip），
          正常卡（ScreenshotCard）也无此行——两卡段落结构（header/截图区/按钮条）天然等高 */}
      {/* 按钮条：与正常卡同布局——创建 + 切换（empty 也显示切换：2026-10-10 用户要求
          与关机占位一致；点击均弹「切换实例」，CardActionBar 按状态显隐） */}
      <CardActionBar
        status="stopped"
        androidStatus={kind === 'offline' ? androidStatus : ''}
        running={false}
        hasInstance
        v2Mode={mode === 'v2'}
        scale={scale}
        onCreate={onCreate}
        onAdb={() => {}}
        onSwitch={onOpen}
      />
    </div>
  )
})

export default SlotPlaceholderCard
