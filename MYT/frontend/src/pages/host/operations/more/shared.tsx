import { ReactNode } from 'react'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo, MirrorItem } from '../../../../types/index'
import { showToast } from '../../../../components/Toast'
import i18n from '../../../../i18n'
import { isEmulatorMirrorType } from '../../../mirror/constants'
import { getErrorMessage } from '../../../../utils/error'

export { SdkApiService, showToast, getErrorMessage }
export type { AndroidInfo, MirrorItem }

export const inputCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200 focus:border-blue-400 focus:outline-none'
export const selectCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200'
export const btnCls = 'px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50'
export const btnSecondaryCls = 'px-3 py-1.5 text-xs bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200 disabled:opacity-50'
export const preCls = 'bg-gray-50 rounded-lg border border-gray-100 p-2 overflow-auto text-xs font-mono'

export function isContainerImage(imageUrl: string, mirrors: MirrorItem[]): boolean {
  if (!imageUrl) return true
  const m = mirrors.find(it => it.url === imageUrl)
  if (!m) return true
  return !isEmulatorMirrorType(m)
}

export async function runBatch(
  androids: AndroidInfo[],
  mirrors: MirrorItem[],
  fn: (a: AndroidInfo) => Promise<unknown>,
  successLabel: string,
): Promise<{ success: number; failed: number; skipped: number }> {
  let success = 0, failed = 0, skipped = 0
  const errors: string[] = []
  for (const a of androids) {
    try { await fn(a); success++ } catch (e) { failed++; errors.push(`${a.name}: ${getErrorMessage(e)}`) }
  }
  const parts = [i18n.t('host:moreOps.runBatchSuccess', { success })]
  if (failed > 0) parts.push(i18n.t('host:moreOps.runBatchFailed', { failed }))
  const firstErr = errors[0] || ''
  showToast(i18n.t('host:moreOps.runBatchResult', { label: successLabel, parts: parts.join(', ') }) + (firstErr ? `\n${firstErr}` : ''), failed > 0 ? 'error' : 'success')
  if (errors.length > 0) console.warn(`[runBatch] ${successLabel} 失败详情:`, errors.join('\n'))
  return { success, failed, skipped }
}

export function OpBlock({ label, hint, children }: {
  label: string
  hint?: string
  children: ReactNode
}) {
  return (
    <div className="rounded-lg border border-gray-100 p-3 space-y-2">
      <div className="flex items-baseline justify-between">
        <span className="text-xs font-medium text-gray-700">{label}</span>
        {hint && <span className="text-[10px] text-gray-400 font-mono">{hint}</span>}
      </div>
      {children}
    </div>
  )
}
