// 任务队列状态色板集中定义：面板（TaskQueuePanel）与详情弹窗（GlobalTaskModal）共用，
// 防止两处色值各写一份产生漂移。四态语义：run 蓝 / ok 绿 / err 红 / warn 琥珀（部分失败），
// 中性色走冷灰 ink 系。状态相关的新配色一律加在这里，不允许散落到组件里。
import type { TaskStatus } from '../../hooks/taskQueue/types'

export const tq = {
  ink: '#161a21', // 主文本（冷黑）
  sub: '#5a6474', // 次文本
  mute: '#8f99a9', // 弱文本
  faint: '#c3cad6', // 最弱（未开始步骤、装饰）
  line: '#e6eaf1', // 发丝分隔线
  surface: '#f7f9fc', // 浅表面（头部渐变端、代码块底）
  track: '#edf0f5', // 进度轨
  run: '#2f6fd6',
  ok: '#1e8e52',
  err: '#d4483f',
  warn: '#b3801f',
  railIdle: '#ccd2dc', // 排队/取消状态条
} as const

export type BarTone = 'run' | 'ok' | 'err' | 'warn'

/** 进度条填充渐变（纵向顶亮底暗，细轨上的受光质感） */
export const barGrad: Record<BarTone, string> = {
  run: 'linear-gradient(180deg,#5b90e5,#2f6fd6)',
  ok: 'linear-gradient(180deg,#2fa369,#1e8e52)',
  err: 'linear-gradient(180deg,#e06a62,#d4483f)',
  warn: 'linear-gradient(180deg,#c99a3f,#b3801f)',
}

/** 行首状态色条 */
export function barColor(status: TaskStatus, failed: number): string {
  if (status === 'failed') return tq.err
  if (status === 'completed') return tq.ok
  if (status === 'canceled') return tq.railIdle
  if (status === 'running') return failed > 0 ? tq.warn : tq.run
  return tq.railIdle // queued / pending
}

/** 状态 pill：圆点 + 文字、浅底色、无描边 */
export function pillCls(status: TaskStatus): string {
  if (status === 'completed') return 'text-[#1e8e52] bg-[#e9f5ee]'
  if (status === 'failed') return 'text-[#d4483f] bg-[#fdeeef]'
  if (status === 'running') return 'text-[#2f6fd6] bg-[#e9f1fc]'
  return 'text-[#8f99a9] bg-[#f0f2f6]' // queued / pending / canceled
}

/** pill 内的状态圆点（跟随 currentColor） */
export const pillDotCls = 'w-[5px] h-[5px] rounded-full bg-current shrink-0'
