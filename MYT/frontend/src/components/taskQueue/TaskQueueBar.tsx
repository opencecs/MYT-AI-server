import { barGrad, tq, type BarTone } from './taskQueueTheme'

/**
 * 4px 细轨进度条：内凹轨道（inset 影）+ 渐变填充。
 * 进行中的定值进度叠加流光（.tq-sheen，reduced-motion 降级见 style.css）；
 * indet（不定进度）为 30% 宽色带左右滑动。
 */
export default function Bar({ pct, tone = 'run', indet }: { pct: number; tone?: BarTone; indet?: boolean }) {
  return (
    <div className="h-1 rounded-full overflow-hidden mt-2" style={{ background: tq.track, boxShadow: 'inset 0 1px 2px rgba(22,26,33,.07)' }}>
      {indet ? (
        <div className="h-full w-[30%] rounded-full animate-[tqslide_1.5s_ease-in-out_infinite]" style={{ background: `linear-gradient(90deg,transparent,${tq.run},transparent)` }} />
      ) : (
        <div className="relative h-full rounded-full overflow-hidden transition-[width] duration-500 ease-out" style={{ width: `${Math.min(100, Math.max(0, pct))}%`, background: barGrad[tone] }}>
          {tone === 'run' && <span className="tq-sheen" />}
        </div>
      )}
    </div>
  )
}
