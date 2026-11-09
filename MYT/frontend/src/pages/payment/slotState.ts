/**
 * 实例（坑位）状态 → 视觉样式。
 *
 * 状态值语义与 Go 侧 GetSlotExpireInfo 一致：
 * 0=正常 / 1=即将到期 / 2=已过期 / -1=未开通（无记录）。
 * 配色对齐 pc-android-client 授权页的状态图例：蓝=正常、黄=即将到期、红=已过期、灰=无实例。
 */
export interface SlotStyle {
  /** 格子容器样式（浅底 + 描边 + 字色） */
  cell: string
  /** 状态小圆点颜色（图例用） */
  dot: string
}

export function slotStyle(state: number): SlotStyle {
  switch (state) {
    case 0:
      return { cell: 'bg-blue-50 border-blue-200 text-blue-600 hover:bg-blue-100', dot: 'bg-blue-500' }
    case 1:
      return { cell: 'bg-amber-50 border-amber-200 text-amber-600 hover:bg-amber-100', dot: 'bg-amber-500' }
    case 2:
      return { cell: 'bg-red-50 border-red-200 text-red-500 hover:bg-red-100', dot: 'bg-red-500' }
    default:
      return { cell: 'bg-gray-50 border-gray-200 text-gray-300 hover:bg-gray-100', dot: 'bg-gray-300' }
  }
}
