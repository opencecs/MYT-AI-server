import { useRef } from 'react'
import { AdbExecution } from './groupTypes'
import { ADB_SHORTCUTS } from './groupConstants'
import { showToast } from '../../components/Toast'
import { useTranslation } from 'react-i18next'
import { useModalEnter } from '../../hooks/useModalAnimation'

interface Props {
  visible: boolean
  targetCount: number
  adbCommand: string
  onAdbCommandChange: (cmd: string) => void
  adbExecution: AdbExecution
  onExecute: (cmd: string) => void
  onClose: () => void
}

function BatchAdbPanel({ visible, targetCount, adbCommand, onAdbCommandChange, adbExecution, onExecute, onClose }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  if (!visible) return null

  const handleShortcut = (command: string) => {
    onAdbCommandChange(command)
  }

  const handleExecute = () => {
    if (!adbCommand.trim()) {
      showToast(t('groupcontrol:enterCommand'), 'error')
      return
    }
    if (targetCount === 0) {
      showToast(t('groupcontrol:selectDeviceFirst'), 'error')
      return
    }
    onExecute(adbCommand)
  }

  const successCount = adbExecution.results.filter(r => r.success).length

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-xl shadow-xl w-[520px] max-w-[calc(100vw-32px)] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        {/* 标题栏 */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-100">
          <h3 className="text-sm font-medium text-gray-700">{t('groupcontrol:batchAdbTitle')}</h3>
          <div className="flex items-center gap-3">
            <span className="text-xs text-gray-400">{t('groupcontrol:targetCount', { count: targetCount })}</span>
            <button onClick={onClose} className="text-gray-400 hover:text-gray-600 transition-colors">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
        </div>

        {/* 命令输入 */}
        <div className="px-5 pt-4 pb-2">
          <div className="flex gap-2">
            <input
              type="text"
              placeholder={t('groupcontrol:adbPlaceholder')}
              value={adbCommand}
              onChange={e => onAdbCommandChange(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') handleExecute() }}
              autoFocus
              className="flex-1 px-3 py-2 text-xs bg-gray-50 border border-gray-200 rounded-lg focus:outline-none focus:border-blue-400 font-mono"
            />
            <button
              onClick={handleExecute}
              disabled={adbExecution.running || targetCount === 0 || !adbCommand.trim()}
              className="px-4 py-2 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 disabled:cursor-not-allowed transition-colors shrink-0"
            >
              {adbExecution.running ? t('groupcontrol:executing') : t('groupcontrol:execute')}
            </button>
          </div>
        </div>

        {/* 快捷命令 */}
        <div className="px-5 pb-3">
          <div className="flex flex-wrap gap-1">
            {ADB_SHORTCUTS.map(s => (
              <button
                key={s.label}
                onClick={() => handleShortcut(s.command)}
                className="px-2 py-1 text-[11px] bg-gray-50 border border-gray-200 rounded hover:bg-gray-100 text-gray-600 transition-colors"
              >
                {s.label && t(s.label)}
              </button>
            ))}
          </div>
        </div>

        {/* 执行结果 */}
        {adbExecution.results.length > 0 && (
          <div className="flex-1 min-h-0 px-5 pb-4">
            <div className="border border-gray-200 rounded-lg overflow-hidden flex flex-col max-h-[40vh]">
              <div className="bg-gray-50 px-3 py-1.5 flex items-center justify-between shrink-0">
                <span className="text-[11px] text-gray-500 font-medium">
                  {t('groupcontrol:executionResult')} <span className="font-mono">{adbExecution.command}</span>
                </span>
                <span className="text-[11px] text-gray-400">
                  {t('groupcontrol:successCount', { success: successCount, total: adbExecution.results.length })}
                </span>
              </div>
              <div className="overflow-y-auto flex-1">
                {adbExecution.results.map((r, i) => (
                  <div
                    key={i}
                    className={`px-3 py-1.5 text-xs border-t border-gray-100 ${
                      r.success ? 'text-gray-600' : 'text-red-500'
                    }`}
                  >
                    <div className="flex items-center gap-2">
                      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${r.success ? 'bg-green-400' : 'bg-red-400'}`} />
                      <span className="font-mono truncate">{r.device_id}</span>
                      {!r.success && r.error && (
                        <span className="text-[11px] text-red-400 truncate ml-auto">{r.error}</span>
                      )}
                    </div>
                    {r.success && r.output && (
                      <pre className="mt-1 ml-3.5 text-[11px] text-gray-500 font-mono whitespace-pre-wrap break-all bg-gray-50 rounded p-1.5 max-h-20 overflow-y-auto">{r.output}</pre>
                    )}
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export default BatchAdbPanel
