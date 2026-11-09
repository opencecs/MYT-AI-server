import { useTranslation } from 'react-i18next'
import type { ConfirmInfo } from './actionBarTypes'

interface BatchActionFns {
  doBatchRestart: () => void
  doBatchStart: () => void
  doBatchShutdown: () => void
  doBatchReset: () => void
  doBatchDelete: () => void
  doBatchCloseProxy: () => void
  doBatchShake: () => void
  runBatchScreenCast: () => void
  runResetCasting: () => void
}

/**
 * useBatchConfirms 把 9 个 confirmBatch* 闭包抽成 hook。
 * 每个 confirm 只是构造 ConfirmInfo 触发确认弹窗，确认后再执行对应批量动作。
 * 抽出后 ActionBar 主组件不再含一长串 confirm 闭包，职责更清晰。
 */
export function useBatchConfirms(
  setConfirm: (c: ConfirmInfo | null) => void,
  count: number,
  fns: BatchActionFns,
) {
  const { t } = useTranslation()
  const wrap = (key: keyof typeof labels, fn: () => void, danger?: boolean) => () =>
    setConfirm({
      title: t(labels[key].title),
      message: key === 'batchCast' ? t(labels[key].message) : t(labels[key].message, { count }),
      confirmLabel: t(labels[key].confirm),
      danger,
      onConfirm: () => { setConfirm(null); fn() },
    })

  const labels = {
    batchCast: { title: 'host:confirm.batchCast.title', message: 'host:confirm.batchCast.message', confirm: 'host:confirm.batchCast.confirm' },
    resetCast: { title: 'host:confirm.resetCast.title', message: 'host:confirm.resetCast.message', confirm: 'host:confirm.resetCast.confirm' },
    batchRestart: { title: 'host:confirm.batchRestart.title', message: 'host:confirm.batchRestart.message', confirm: 'host:confirm.batchRestart.confirm' },
    batchShutdown: { title: 'host:confirm.batchShutdown.title', message: 'host:confirm.batchShutdown.message', confirm: 'host:confirm.batchShutdown.confirm' },
    batchStart: { title: 'host:confirm.batchStart.title', message: 'host:confirm.batchStart.message', confirm: 'host:confirm.batchStart.confirm' },
    batchReset: { title: 'host:confirm.batchReset.title', message: 'host:confirm.batchReset.message', confirm: 'host:confirm.batchReset.confirm' },
    batchDelete: { title: 'host:confirm.batchDelete.title', message: 'host:confirm.batchDelete.message', confirm: 'host:confirm.batchDelete.confirm' },
    batchCloseS5: { title: 'host:confirm.batchCloseS5.title', message: 'host:confirm.batchCloseS5.message', confirm: 'host:confirm.batchCloseS5.confirm' },
    batchShake: { title: 'host:confirm.batchShake.title', message: 'host:confirm.batchShake.message', confirm: 'host:confirm.batchShake.confirm' },
  }

  return {
    confirmBatchCast: wrap('batchCast', fns.runBatchScreenCast),
    confirmResetCasting: wrap('resetCast', fns.runResetCasting, true),
    confirmBatchRestart: wrap('batchRestart', fns.doBatchRestart),
    confirmBatchStart: wrap('batchStart', fns.doBatchStart),
    confirmBatchShutdown: wrap('batchShutdown', fns.doBatchShutdown, true),
    confirmBatchReset: wrap('batchReset', fns.doBatchReset, true),
    confirmBatchDelete: wrap('batchDelete', fns.doBatchDelete, true),
    confirmBatchCloseProxy: wrap('batchCloseS5', fns.doBatchCloseProxy),
    confirmBatchShake: wrap('batchShake', fns.doBatchShake),
  }
}
