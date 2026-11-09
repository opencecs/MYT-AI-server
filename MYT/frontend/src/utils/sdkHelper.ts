import * as ScreenshotService from '../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import { emitSlotStatus } from './wailsEvent'
import { showToast } from '../components/Toast'
import { getErrorMessage } from './error'
import i18n from '../i18n'

type SlotStatus = 'starting' | 'stopped' | 'resetting' | 'restarting' | 'removing'

// 操作 → 乐观状态：关机类 → stopped；删除 → removing；重置 → resetting；
// 重启 → restarting；其余（开机等） → starting。
// label 来自 i18n，中英文都要兜住（zh: 关机/停止/删除/重置/重启；en: stop/shutdown/delete/reset/restart）
const statusAfterOp = (label: string): SlotStatus => {
  if (/stop|shutdown|关机|停止/i.test(label)) return 'stopped'
  if (/delete|remove|删除/i.test(label)) return 'removing'
  if (/reset|重置/i.test(label)) return 'resetting'
  if (/restart|重启/i.test(label)) return 'restarting'
  return 'starting'
}

export const doSdk = (label: string, fn: () => Promise<void>, deviceIP?: string, indexNum?: number) => async () => {
  console.log(`[sdk] ${label} deviceIP=${deviceIP} indexNum=${indexNum}`)
  try {
    await fn()
    console.log(`[sdk] ${label} done`)
    if (deviceIP && indexNum !== undefined) {
      ScreenshotService.InvalidateSlot(deviceIP, indexNum).catch(() => {})
      // 立即通知前端设置过渡状态，不等安卓列表刷新
      emitSlotStatus(`${deviceIP}_${indexNum}`, statusAfterOp(label))
    }
    // 成功也要有反馈（摇一摇这类无可见效果的操作，成功后毫无感知）
    showToast(String(i18n.t('host:toast.opSuccess', { op: label })), 'success')
  } catch (err) {
    // 失败必须有反馈（此前静默吞掉，摇一摇等点了一下毫无反应）
    console.error(`[sdk] ${label} failed:`, err)
    showToast(String(i18n.t('host:toast.opFailed', { op: label, error: getErrorMessage(err) })), 'error')
  }
}
