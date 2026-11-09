import { useState, useRef, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Loader2, Search, X } from 'lucide-react'
import { AndroidInfo } from '../../../../types/index'
import { showToast } from '../../../../components/Toast'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import { useAndroidNotes } from '../../../../hooks/useNoteSync'
import { containerDisplayName } from '../../../../utils/containerName'
import { statusLabel, statusTagCls } from '../../../../utils/containerStatus'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import { runSwitchOrStartTask } from './slotActions'

interface Props {
  androids: AndroidInfo[]   // 该坑位所有云机
  slot: number
  onClose: () => void
}

// SwitchAndroidModal 切换云机：展示该坑位所有云机，选中一个开机。
// 若当前有云机运行，先停掉再启动选中的（同坑位只能运行一个）。
// 关机→开机流程走任务队列，有进度反馈 + 可取消。
function SwitchAndroidModal({ androids, slot, onClose }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const { mode } = useDeployMode()
  const { addTask, updateTask, openPanel } = useTaskQueue()
  const [loading, setLoading] = useState<string | null>(null) // 正在操作的云机 name
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [search, setSearch] = useState('')

  const running = androids.find(a => a.status === 'running')

  // 搜索过滤（名称 + 备注，前端过滤；运行/关机都搜）
  const filtered = useMemo(() => {
    const kw = search.trim().toLowerCase()
    if (!kw) return androids
    return androids.filter(a =>
      (notes[a.id] || a.name).toLowerCase().includes(kw) || a.name.toLowerCase().includes(kw)
    )
  }, [androids, search, notes])

  // 搜索后选中项被滤掉 → 清空选中态（避免底部主按钮指向看不见的行）
  useEffect(() => {
    if (selectedId && !filtered.some(a => a.id === selectedId)) setSelectedId(null)
  }, [filtered, selectedId])

  const selected = androids.find(a => a.id === selectedId) ?? null

  // 键盘：Enter 确认（选中非运行项）、Esc 关闭（loading 中不允许——任务已在跑，关了会失去进度入口）
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { if (!loading) onClose() }
      else if (e.key === 'Enter' && selected && selected.status !== 'running' && !loading) {
        void handleSwitch(selected)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, loading, onClose])

  const handleSwitch = async (target: AndroidInfo) => {
    if (loading) return
    setLoading(target.name)
    const ok = await runSwitchOrStartTask(mode, addTask, updateTask, target, running, {
      stop: t('host:slotView.switchStepStop', { name: running?.name ?? '' }),
      start: t('host:slotView.switchStepStart', { name: target.name }),
    })
    if (ok) {
      showToast(t('host:toast.switchAndroidSuccess'), 'success')
      onClose()
    } else {
      showToast(t('host:toast.switchAndroidFailed'), 'error')
    }
    setLoading(null)
    openPanel()
  }

  // 行点击：只选中（执行走底部「开机」按钮或 Enter——防止误触，2026-10-15 用户要求）
  const onRowClick = (a: AndroidInfo) => {
    if (loading || a.status === 'running') return
    setSelectedId(a.id)
  }

  return (
    <div
      className="fixed inset-0 bg-black/30 backdrop-blur-[2px] flex items-center justify-center z-50 modal-overlay"
      ref={rootRef}
      onClick={() => { if (!loading) onClose() }}
    >
      <div
        className="bg-white rounded-xl border border-gray-200/70 w-[440px] flex flex-col modal-content overflow-hidden
                   shadow-[0_12px_40px_-8px_rgba(0,0,0,0.18),0_4px_12px_rgba(0,0,0,0.06)]"
        onClick={e => e.stopPropagation()}
      >
        {/* 头部：标题 + 关闭，底部分割线 */}
        <div className="px-4 py-3 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-[15px] font-semibold text-gray-800">{t('host:slotView.switchAndroidTitle', { slot })}</h2>
          <button
            onClick={() => { if (!loading) onClose() }}
            className="text-gray-400 hover:text-gray-600 w-6 h-6 flex items-center justify-center rounded-md hover:bg-gray-100 transition-colors duration-150"
          >
            <X size={14} />
          </button>
        </div>

        {/* 搜索框行 */}
        <div className="px-4 pt-3 pb-2">
          <div className="relative">
            <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
            <input
              type="text"
              value={search}
              onChange={e => setSearch(e.target.value)}
              placeholder={t('host:slotView.switchSearchPlaceholder')}
              className="w-full text-sm pl-8 pr-3 py-2 bg-white border border-gray-200 rounded-md
                         focus:outline-none focus:border-blue-400 placeholder:text-gray-300 transition-colors"
            />
          </div>
        </div>

        {/* 列表：卡片式行，选中蓝框高亮 + 勾选角标 */}
        <div className="px-4 pb-3 max-h-[55vh] overflow-y-auto space-y-2" role="listbox" aria-label={t('host:slotView.switchAndroid')}>
          {filtered.length === 0 && (
            <div className="flex items-center justify-center py-8 text-gray-300 gap-1.5">
              <Search size={14} />
              <span className="text-xs">{search ? t('host:screenshot.noMatch') : t('host:slotView.idle')}</span>
            </div>
          )}
          {filtered.map(a => {
            const isRunning = a.status === 'running'
            const isSelected = selectedId === a.id
            const isLoading = loading === a.name
            const disabled = loading !== null
            return (
              <div
                key={a.id}
                role="option"
                aria-selected={isSelected}
                tabIndex={disabled ? -1 : 0}
                onClick={() => onRowClick(a)}
                className={`relative flex items-center gap-2.5 px-3.5 py-2.5 rounded-lg border-2 cursor-pointer
                            transition-all duration-150
                  ${isSelected
                    ? 'border-blue-400 bg-blue-50/60'
                    : isRunning
                      ? 'border-gray-100 bg-gray-50/50'
                      : 'border-gray-100 bg-white hover:border-blue-200 hover:bg-blue-50/30'}
                  ${disabled && !isLoading ? 'opacity-50 pointer-events-none' : ''}`}
              >
                {/* 选中角标（右上角蓝色圆 + 白勾） */}
                {isSelected && (
                  <span className="absolute -top-1.5 -right-1.5 w-4 h-4 rounded-full bg-blue-500 flex items-center justify-center shadow-sm">
                    <Check size={10} className="text-white" strokeWidth={3} />
                  </span>
                )}
                {/* 名称（14px 主信息；显示名段与三个视图统一，hover title 显示全名） */}
                <span
                  title={notes[a.id] || a.name}
                  className={`flex-1 min-w-0 truncate text-sm font-medium ${isSelected ? 'text-blue-700' : 'text-gray-800'}`}
                >
                  {notes[a.id] || containerDisplayName(a.name, a.deviceId, a.indexNum)}
                </span>
                {/* 状态 tag（浅底深字，Element 风格） */}
                <span className={`shrink-0 text-xs px-2 py-0.5 rounded font-medium whitespace-nowrap ${statusTagCls(a.status)}`}>
                  {isLoading ? t('host:actionBar.starting') : statusLabel(a.status, t)}
                </span>
              </div>
            )
          })}
        </div>

        {/* 底部：分割线 + 右对齐 取消 / 开机 */}
        <div className="flex items-center justify-between px-4 py-3 border-t border-gray-100 bg-gray-50/50">
          <span className="text-xs text-gray-400 truncate">
            {selected && selected.status !== 'running'
              ? t('host:slotView.switchSelectedHint', { name: notes[selected.id] || selected.name })
              : running
                ? t('host:slotView.switchRunningHint', { name: notes[running.id] || running.name })
                : ''}
          </span>
          <div className="flex items-center gap-2 shrink-0">
            <button
              onClick={() => { if (!loading) onClose() }}
              disabled={!!loading}
              className="px-3.5 py-1.5 text-sm text-gray-600 bg-white border border-gray-200 rounded-lg hover:bg-gray-50
                         disabled:opacity-50 disabled:cursor-not-allowed transition-colors duration-150"
            >
              {t('button.cancel')}
            </button>
            <button
              onClick={() => { if (selected && selected.status !== 'running') void handleSwitch(selected) }}
              disabled={!selected || selected.status === 'running' || !!loading}
              className="inline-flex items-center gap-1.5 px-4 py-1.5 text-sm font-medium bg-blue-500 text-white rounded-lg
                         hover:bg-blue-600 active:scale-[0.98] disabled:opacity-40 disabled:cursor-not-allowed
                         transition-all duration-150"
            >
              {loading && <Loader2 size={13} className="animate-spin" />}
              {loading ? t('host:actionBar.starting') : t('host:slotView.start')}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

export default SwitchAndroidModal
