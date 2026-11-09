import { useState } from 'react'

export interface ConfirmAction {
  title: string
  message: string
  onConfirm: () => void | Promise<void>
}

/** ask 的独立签名：供常驻宿主（渲染 ConfirmDialog 的组件）经 props 下传给子组件（弹窗/右键菜单）。
 *  子组件绝不能自己 useConfirm——弹窗类子组件 ask 后即卸载、菜单类不渲染 ConfirmDialog，
 *  state 会随实例销毁/无人消费，确认框永不出现（坑位右键静默失败事故的根因）。 */
export type AskFn = (title: string, msg: string, fn: () => void | Promise<void>) => void

export function useConfirm() {
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(null)
  const ask: AskFn = (title, msg, fn) =>
    setConfirmAction({ title, message: msg, onConfirm: fn })
  return { confirmAction, setConfirmAction, ask }
}
