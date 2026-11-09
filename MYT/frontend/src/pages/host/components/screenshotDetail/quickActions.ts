import { Cast, TerminalSquare, Pencil, RotateCcw, Power } from 'lucide-react'
import type { TFunction } from 'i18next'
import type { CardMenuItem } from './cardMenuTypes'

/**
 * 右键菜单顶部「Win11 风格五快捷操作」（投屏/终端/重命名/重启/关机）的共享构建器。
 * ScreenshotCard（卡片右键）与 SlotContextMenu（坑位表「...」）两处双写收敛于此——
 * 五项的 icon/label/v4Hide/结构完全同源，之前两份手写数组在改禁用口径时容易漂移。
 *
 * 差异参数化：castDisabled / terminalDisabled 由调用方按各自上下文传入（坑位表
 * status 恒 'screenshot'，卡片按真实截图状态）；hideRename 由部署模式（v2 砍）与
 * 调用方标记共同决定；onCast/onTerminal/onRename 由调用方注入（卡片走 setState，
 * 坑位表走 setter 回调）。v4Hide 标记沿用既有过滤约定（V4 无端点项隐藏）。
 */
export interface QuickActionsParams {
  t: TFunction
  castDisabled: boolean
  terminalDisabled: boolean
  /** v2 模式砍重命名；无重命名入口的调用方也置 true 隐藏 */
  hideRename: boolean
  onCast: () => void
  onTerminal: () => void
  onRename?: () => void
  onRestart: () => void
  onStop: () => void
}

/** 五项快捷按钮（顺序固定：投屏/终端/重命名/重启/关机），返回后调用方可再过滤 v4Hide。 */
export function buildQuickActions(p: QuickActionsParams): CardMenuItem[] {
  const t = p.t
  const c = t('host:screenshot.contextMenu', { returnObjects: true }) as Record<string, string>
  return [
    { icon: Cast, label: c.cast, disabled: p.castDisabled, onClick: p.onCast },
    { icon: TerminalSquare, label: c.terminal, disabled: p.terminalDisabled, onClick: p.onTerminal },
    ...(p.hideRename || !p.onRename ? [] : [{ icon: Pencil, label: c.rename, onClick: p.onRename, v4Hide: true }]),
    { icon: RotateCcw, label: c.restart, onClick: p.onRestart },
    { icon: Power, label: c.stop, onClick: p.onStop },
  ]
}
