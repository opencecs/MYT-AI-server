import { ShieldCheck, AlertTriangle, Lock, Loader2 } from 'lucide-react'

/**
 * 主机绑定状态 → 徽章描述（文案 key + 图标 + 样式）。
 *
 * 状态值来自 PaymentService.GetDeviceBindStatus：
 * 0=未绑定 / 1=已绑定当前账号 / 2=被其他账号绑定；
 * undefined = 尚未查询到（查询中），不能当作未绑定渲染。
 */
export interface BindBadgeInfo {
  /** payment 命名空间的文案 key */
  textKey: string
  icon: typeof ShieldCheck
  /** 徽章容器样式（浅底 + 深字 + 描边） */
  cls: string
  /** 查询中：图标转圈 */
  spinning: boolean
}

export function bindBadgeInfo(status: number | undefined): BindBadgeInfo {
  switch (status) {
    case 1:
      return { textKey: 'bound', icon: ShieldCheck, cls: 'text-green-600 bg-green-50 border-green-200', spinning: false }
    case 2:
      return { textKey: 'boundByOther', icon: AlertTriangle, cls: 'text-amber-600 bg-amber-50 border-amber-200', spinning: false }
    case 0:
      return { textKey: 'unbound', icon: Lock, cls: 'text-gray-400 bg-gray-50 border-gray-200', spinning: false }
    default:
      return { textKey: 'querying', icon: Loader2, cls: 'text-gray-400 bg-gray-50 border-gray-200', spinning: true }
  }
}
