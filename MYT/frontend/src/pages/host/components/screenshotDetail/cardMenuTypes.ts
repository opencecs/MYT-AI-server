import type { MenuItemConfig } from '../../../../components/AndroidContextMenu'

/**
 * 带 v4Hide 扩展的菜单项/组类型（V4 无对应 SDK 端点的项在 V4 下隐藏）。
 * ScreenshotCard（卡片右键）与 SlotContextMenu（坑位表）共用——两处的菜单
 * 结构同源，v4Hide 过滤约定一致（见各自渲染处的 filter）。
 */
export type CardMenuItem = MenuItemConfig & { v4Hide?: boolean }
export type CardMenuGroup = { items: CardMenuItem[]; danger?: boolean }
