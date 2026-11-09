import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo, MirrorItem } from '../../../../types/index'
import { parseImage, buildMirrorLookup } from '../slotDetail/slotViewUtils'
import { formatOsVer } from '../../../mirror/constants'

/** 悬停信息卡字段行：[标签, 值] */
type TipRow = [string, string]

interface TipProps {
  android: AndroidInfo
  note: string
  mirrors: MirrorItem[]
  /** 光标位置（视口坐标，信息卡内部再偏移并夹紧到视口内） */
  x: number
  y: number
  /** 可选状态行（如关机卡传「已关机」；运行卡不传则不显示该行） */
  statusLabel?: string
  /** 可选坑位号行（关机卡补充展示；运行卡不传则不显示该行） */
  slot?: number
}

/**
 * 截图卡片悬停信息卡：鼠标停在卡片上 ~300ms 后在光标旁弹出云机基本信息
 * （名称/备注/镜像/安卓版本/机型）。pointer-events-none 保证不阻挡
 * 点击投屏等既有交互；fixed 定位不被滚动容器裁剪。
 * 镜像/版本推导与坑位模式表格一致：镜像列表命中优先，未命中回退 image 原串 + tag 解析。
 */
export default function ScreenshotInfoTip({ android, note, mirrors, x, y, statusLabel, slot }: TipProps) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ x: x + 12, y: y + 16 })
  // 首帧渲染后按实际尺寸夹紧到视口内（一帧内修正，视觉无感）
  useEffect(() => {
    const el = ref.current
    if (!el) return
    setPos({
      x: Math.max(4, Math.min(x + 12, window.innerWidth - el.offsetWidth - 4)),
      y: Math.max(4, Math.min(y + 16, window.innerHeight - el.offsetHeight - 4)),
    })
  }, [x, y])

  const matched = buildMirrorLookup(mirrors).get(android.image)
  const fallback = parseImage(android.image)
  const osVer = formatOsVer(matched?.os_ver || fallback.ver) || '-'

  const rows: TipRow[] = [
    [t('host:screenshot.hoverInfo.name'), android.name],
    ...(note ? [[t('host:screenshot.hoverInfo.note'), note] as TipRow] : []),
    ...(statusLabel ? [[t('host:screenshot.hoverInfo.status'), statusLabel] as TipRow] : []),
    ...(slot !== undefined ? [[t('host:screenshot.hoverInfo.slot'), `#${slot}`] as TipRow] : []),
    [t('host:screenshot.hoverInfo.image'), matched?.name || android.image],
    [t('host:screenshot.hoverInfo.androidVersion'), osVer],
    [t('host:screenshot.hoverInfo.model'), android.modelPath || '-'],
  ]

  return (
    <div ref={ref}
      className="fixed z-40 pointer-events-none bg-gray-900/90 backdrop-blur text-white rounded-lg shadow-xl px-3 py-2 text-xs space-y-1 min-w-[140px] max-w-[280px]"
      style={{ left: pos.x, top: pos.y }}>
      {rows.map(([label, value]) => (
        <div key={label} className="flex gap-3">
          <span className="text-gray-400 shrink-0">{label}</span>
          <span className="truncate">{value}</span>
        </div>
      ))}
    </div>
  )
}

/**
 * 悬停检测 hook：延迟显示（避免鼠标扫过卡片时信息卡连环闪烁），
 * 位置在显示瞬间快照（此后鼠标在卡片内移动不再跟随——否则父组件
 * 每秒重渲染会读到最新坐标导致信息卡间歇跳动）；null = 隐藏。
 * 返回的 handlers 挂到卡片外层元素。
 */
export function useInfoTipHover(delay = 300) {
  const [tipPos, setTipPos] = useState<{ x: number; y: number } | null>(null)
  const timer = useRef<number | null>(null)
  const posRef = useRef({ x: 0, y: 0 })
  useEffect(() => () => {
    if (timer.current !== null) window.clearTimeout(timer.current)
  }, [])

  const handlers = {
    onMouseEnter: () => {
      if (timer.current !== null) window.clearTimeout(timer.current)
      timer.current = window.setTimeout(() => setTipPos({ ...posRef.current }), delay)
    },
    onMouseMove: (e: React.MouseEvent) => {
      posRef.current = { x: e.clientX, y: e.clientY }
    },
    onMouseLeave: () => {
      if (timer.current !== null) { window.clearTimeout(timer.current); timer.current = null }
      setTipPos(null)
    },
  }
  return { tipPos, handlers }
}
