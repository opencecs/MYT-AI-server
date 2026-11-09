import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useModalEnter } from '../../hooks/useModalAnimation'
import ConfirmDialog from '../../components/ConfirmDialog'
import { MIN_SDK_VERSION } from '../../hooks/useSdkVersions'
import { useDeployMode } from '../../hooks/useDeployMode'
import { useBatchOps, rowKey } from './useBatchOps'
import { makeArithmeticChallenge } from '../../utils/arithmeticChallenge'

interface Props {
  /** 设备 SDK 版本缓存（useSdkVersions：WS hello 实时推送 + 页面挂载批量查询兜底） */
  sdkVersions: Record<string, number>
  onClose: () => void
}

/** 批量操作弹窗：主机列表（多选）+ 批量升级 SDK + 批量移除设备。 */
export default function BatchOpsModal({ sdkVersions, onClose }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  // 确认弹窗的算术题只在弹窗首次挂载时生成一次（惰性初始化），与 FormatDiskContent
  // 同款语义——内联调用会在每次渲染时重生成，导致用户输入到一半题目被换掉。
  const [challenge] = useState(() => makeArithmeticChallenge())
  const {
    loading, rows, checked, submitting, confirmRemove, confirmFormatDisk,
    setConfirmRemove, setConfirmFormatDisk, toggle, toggleAll, selectOffline, selectAuthPending,
    upgradableChecked, onlineCheckedCount, submitUpgrade, submitRemove, submitFormatDisk,
  } = useBatchOps(sdkVersions, onClose)

  const stateLabel = (state: number) =>
    state === 0 ? t('status.online') : state === 2 ? t('status.authFailed') : t('status.offline')
  const stateDot = (state: number) =>
    state === 0 ? 'bg-green-500' : state === 2 ? 'bg-amber-500' : 'bg-red-400'

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[520px] max-h-[75vh] flex flex-col modal-content">
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200">
          <h3 className="text-sm font-semibold text-gray-800">{t('maintenance:batchOps.title')}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        <div className="flex-1 overflow-y-auto p-4 min-h-[160px]">
          {loading ? (
            <div className="flex items-center justify-center h-32 text-xs text-gray-400">{t('maintenance:batchOps.loading')}</div>
          ) : rows.length === 0 ? (
            <div className="flex items-center justify-center h-32 text-xs text-gray-400">{t('maintenance:batchOps.empty')}</div>
          ) : (
            <>
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-3">
                  <button onClick={toggleAll} className="text-xs text-blue-500 hover:text-blue-600">
                    {checked.size === rows.length ? t('button.deselectAll') : t('button.selectAll')}
                  </button>
                  <button onClick={selectOffline} className="text-xs text-blue-500 hover:text-blue-600">
                    {t('maintenance:batchOps.selectOffline')}
                  </button>
                  <button onClick={selectAuthPending} className="text-xs text-blue-500 hover:text-blue-600">
                    {t('maintenance:batchOps.selectAuthPending')}
                  </button>
                </div>
                <span className="text-[11px] text-gray-400">{t('maintenance:batchOps.selectedCount', { count: checked.size })}</span>
              </div>
              <div className="border border-gray-100 rounded-lg divide-y divide-gray-50 max-h-[42vh] overflow-y-auto">
                {rows.map(row => {
                  const id = rowKey(row)
                  const on = checked.has(id)
                  return (
                    <label key={id} className={`flex items-center gap-2.5 px-3 py-2 cursor-pointer transition-colors ${on ? 'bg-blue-50/60' : 'hover:bg-gray-50'}`}>
                      <input
                        type="checkbox"
                        checked={on}
                        onChange={() => toggle(id)}
                        className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500 focus:ring-blue-400"
                      />
                      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${stateDot(row.device.state)}`} />
                      <span className="text-xs text-gray-800 truncate flex-1" title={row.ip}>{row.ip}</span>
                      <span className="text-[10px] text-gray-400 shrink-0">{stateLabel(row.device.state)}</span>
                      {/* v2 模式隐藏版本徽标：V2 SDK 版本是 string，int 化无意义且与
                          「版本只在版本管理弹窗展示」口径一致（useSdkVersions v2 不查） */}
                      {mode !== 'v2' && (
                        <span className={`text-[10px] shrink-0 ${row.upgradable ? 'text-amber-600' : 'text-gray-400'}`}>
                          v{row.current || '?'}
                          {row.upgradable
                            ? row.latest != null ? ` → v${row.latest}` : ` → ≥${MIN_SDK_VERSION}`
                            : ''}
                        </span>
                      )}
                    </label>
                  )
                })}
              </div>
            </>
          )}
        </div>

        <div className="flex justify-end gap-2 px-4 py-3 border-t border-gray-200">
          <button onClick={onClose} className="px-3 py-1.5 text-xs bg-gray-100 text-gray-600 rounded hover:bg-gray-200">{t('button.cancel')}</button>
          <button
            onClick={() => setConfirmRemove(true)}
            disabled={loading || submitting || checked.size === 0}
            className="px-3 py-1.5 text-xs bg-red-500 text-white rounded hover:bg-red-600 disabled:opacity-40"
          >
            {t('maintenance:batchOps.remove', { count: checked.size })}
          </button>
          <button
            onClick={() => setConfirmFormatDisk(true)}
            disabled={loading || submitting || onlineCheckedCount === 0}
            className="px-3 py-1.5 text-xs bg-red-500 text-white rounded hover:bg-red-600 disabled:opacity-40"
          >
            {t('maintenance:batchOps.format', { count: onlineCheckedCount })}
          </button>
          <button
            onClick={submitUpgrade}
            disabled={loading || submitting || upgradableChecked === 0}
            className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-40"
          >
            {submitting ? t('maintenance:batchOps.submitting') : t('maintenance:batchOps.upgrade', { count: upgradableChecked })}
          </button>
        </div>
      </div>

      {confirmRemove && (
        <ConfirmDialog
          title={t('maintenance:batchOps.removeTitle')}
          message={t('maintenance:batchOps.removeMsg', { count: checked.size })}
          confirmLabel={t('button.delete')}
          onConfirm={submitRemove}
          onCancel={() => setConfirmRemove(false)}
        />
      )}

      {confirmFormatDisk && (
        <ConfirmDialog
          title={t('maintenance:batchOps.formatTitle')}
          message={t('maintenance:batchOps.formatMsg', { count: onlineCheckedCount })}
          confirmLabel={t('maintenance:format.confirmFormat')}
          challenge={challenge}
          onConfirm={submitFormatDisk}
          onCancel={() => setConfirmFormatDisk(false)}
        />
      )}
    </div>
  )
}
