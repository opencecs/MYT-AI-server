import { useMemo, useRef, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import * as DiscoverService from '../../../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import type { DeviceState } from '../../../../types/index'
import ScanRadar from './ScanRadar'
import AddDeviceRow from './AddDeviceRow'
import { showToast } from '../../../../components/Toast'

type ScanPhase = 'idle' | 'scanning' | 'done'

interface Props {
  pendingDevices: DeviceState[]
  onAddPendingDevices: (deviceIDs: string[]) => void
  onAddAllPendingDevices: () => void
  onClearPendingDevices: () => void
  onClose: () => void
  /** 切换到手动添加 Tab（扫描无结果时的引导） */
  onGoManual: () => void
  /** 通知父级扫描进行中（禁用关闭/切换的强约束暂不需要，仅用于 Tab 标记） */
  onScanningChange?: (scanning: boolean) => void
  /** 扫描触发覆盖（默认走本地 UDP Discover；v2 模式传入 V2 网段扫描） */
  onScan?: () => Promise<unknown>
  /** 待添加行显示「未装超级 SDK」标注（v2 准入放宽语义） */
  showSdkBadge?: boolean
  /** 扫描区提示文案覆盖（v2 模式提示 2375 端口语义） */
  zoneHint?: string
  /** 无结果提示文案覆盖 */
  noneFoundHint?: string
}

/**
 * 自动发现 Tab：扫描状态区（空闲/扫描中/完成，随 pendinglist:updated 事件流式更新）
 * + 待添加列表（搜索/多选/全选）+ 底部操作。扫描计数 = 本轮新增（基线差值）。
 */
function AddDeviceScanTab({ pendingDevices, onAddPendingDevices, onAddAllPendingDevices, onClearPendingDevices, onClose, onGoManual, onScanningChange, onScan, showSdkBadge, zoneHint, noneFoundHint }: Props) {
  const { t } = useTranslation()
  const [phase, setPhase] = useState<ScanPhase>('idle')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [searchText, setSearchText] = useState('')
  const scanBaseRef = useRef(0)

  const liveNew = pendingDevices.length - scanBaseRef.current
  const foundNew = phase === 'done' ? liveNew : 0

  // 按 IP 数值排序 + 搜索过滤（IP / 型号 / 编号）
  const filteredDevices = useMemo(() => {
    const sorted = [...pendingDevices].sort((a, b) => a.ip.localeCompare(b.ip, undefined, { numeric: true }))
    const kw = searchText.trim().toLowerCase()
    if (!kw) return sorted
    return sorted.filter(d =>
      d.ip.toLowerCase().includes(kw) ||
      (d.device_type || '').toLowerCase().includes(kw) ||
      (d.device_name || '').toLowerCase().includes(kw) ||
      d.device_id.toLowerCase().includes(kw),
    )
  }, [pendingDevices, searchText])

  const toggleSelect = useCallback((id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const startScan = useCallback(async () => {
    scanBaseRef.current = pendingDevices.length
    setSelected(new Set())
    setPhase('scanning')
    onScanningChange?.(true)
    let busy = false
    try {
      if (onScan) {
        await onScan()
      } else {
        await DiscoverService.Discover()
      }
    } catch (err) {
      // 后端防重入（扫描/状态刷新进行中）是明确错误，toast 提示并回到空闲，
      // 不落"没有设备"；其余（网络错误等）保持原行为：事件流不更新，置回完成态
      const msg = err instanceof Error ? err.message : String(err ?? '')
      if (msg.includes('稍后再试')) {
        busy = true
        showToast(t('host:addDevice.scanBusy'), 'warning')
      }
    } finally {
      setPhase(busy ? 'idle' : 'done')
      onScanningChange?.(false)
    }
  }, [pendingDevices.length, onScanningChange, onScan, t])

  const handleIgnore = () => {
    onClearPendingDevices()
    setSelected(new Set())
    setPhase('idle')
    scanBaseRef.current = 0
  }

  const handleAddSelected = () => {
    if (selected.size === 0) return
    onAddPendingDevices([...selected])
    setSelected(new Set())
    if (pendingDevices.length <= selected.size) onClose()
  }

  const handleAddAll = () => {
    onAddAllPendingDevices()
    setSelected(new Set())
    if (pendingDevices.length > 0) onClose()
  }

  const allSelected = filteredDevices.length > 0 && filteredDevices.every(d => selected.has(d.device_id))

  return (
    <div>
      {/* 扫描状态区 */}
      <div className={`add-scan-zone ${phase}`}>
        {phase !== 'done' ? (
          <div className="flex flex-col items-center justify-center py-7 px-5 text-center">
            <ScanRadar active={phase === 'scanning'} blipCount={liveNew} />
            <div className="text-[15px] font-semibold text-gray-800 mt-3.5">
              {phase === 'scanning' ? t('host:addDevice.scanningTitle') : t('host:addDevice.scanZoneTitle')}
            </div>
            <p className="text-xs text-gray-400 mt-1.5 leading-relaxed">
              {phase === 'scanning'
                ? <span>{t('host:addDevice.foundLive', { count: Math.max(liveNew, 0) })}</span>
                : (zoneHint ?? t('host:addDevice.scanZoneHint'))}
            </p>
            {phase === 'idle' && (
              <button
                onClick={startScan}
                className="mt-4 h-9 px-5 rounded-lg bg-blue-600 text-white text-sm font-medium
                  hover:bg-blue-700 active:translate-y-px transition-all inline-flex items-center gap-2"
              >
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="2" /><path d="M16.24 7.76a6 6 0 0 1 0 8.49" /><path d="M7.76 16.24a6 6 0 0 1 0-8.49" /><path d="M19.07 4.93a10 10 0 0 1 0 14.14" /><path d="M4.93 19.07a10 10 0 0 1 0-14.14" />
                </svg>
                {t('host:addDevice.startScan')}
              </button>
            )}
          </div>
        ) : (
          <div className="flex items-center gap-2.5 flex-wrap px-3.5 py-2.5">
            {foundNew > 0 ? (
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#059669" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" className="shrink-0">
                <polyline points="20 6 9 17 4 12" />
              </svg>
            ) : (
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#9ca3af" strokeWidth="2" strokeLinecap="round" className="shrink-0">
                <circle cx="12" cy="12" r="9" /><line x1="12" y1="8" x2="12" y2="12.5" /><line x1="12" y1="16" x2="12.01" y2="16" />
              </svg>
            )}
            <span className="text-[13px] font-medium text-gray-700 flex-1 min-w-0">
              {foundNew > 0
                ? t('host:addDevice.scanComplete', { count: foundNew })
                : t('host:addDevice.noneFoundTitle')}
            </span>
            <button onClick={startScan} className="text-xs text-blue-600 font-medium hover:bg-blue-50 rounded-md px-2 py-1">
              {t('host:addDevice.rescan')}
            </button>
            {foundNew === 0 && (
              <button onClick={onGoManual} className="text-xs text-blue-600 font-medium hover:bg-blue-50 rounded-md px-2 py-1">
                {t('host:addDevice.goManual')}
              </button>
            )}
            {foundNew === 0 && (
              <span className="w-full text-xs text-gray-400 leading-relaxed">{noneFoundHint ?? t('host:addDevice.noneFoundHint')}</span>
            )}
          </div>
        )}
      </div>

      {/* 结果列表 + 底部操作（有待添加设备或正在扫描时显示；
          phase==='done' 时即使 pending 已清空也保留，让「完成/无结果」状态可见） */}
      {(pendingDevices.length > 0 || phase === 'scanning' || phase === 'done') && (
        <>
          <div className="flex items-center gap-2.5 mt-3.5 mb-2">
            <span className="text-[13px] text-gray-500 flex-1">
              {t('host:addDevice.foundDevices', { count: pendingDevices.length })}
            </span>
            <label className="flex items-center gap-1.5 h-[30px] px-2.5 rounded-lg border border-gray-200 bg-white
              focus-within:border-blue-500 focus-within:ring-[3px] focus-within:ring-blue-500/15 transition-shadow w-[168px]">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="#9ca3af" strokeWidth="2" strokeLinecap="round" className="shrink-0">
                <circle cx="11" cy="11" r="7" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
              </svg>
              <input
                value={searchText}
                onChange={e => setSearchText(e.target.value)}
                placeholder={t('host:addDevice.searchPlaceholder')}
                className="w-full text-xs bg-transparent outline-none placeholder:text-gray-300"
              />
            </label>
            <button
              onClick={() => setSelected(allSelected ? new Set() : new Set(filteredDevices.map(d => d.device_id)))}
              className="text-xs text-blue-600 font-medium hover:bg-blue-50 rounded-md px-2 py-1"
            >
              {allSelected ? t('button.deselectAll') : t('button.selectAll')}
            </button>
          </div>

          <div className="max-h-72 overflow-y-auto scrollbar-thin border border-gray-100 rounded-xl bg-gray-50/60">
            {filteredDevices.length > 0 ? filteredDevices.map(d => (
              <AddDeviceRow key={d.device_id} device={d} checked={selected.has(d.device_id)} onToggle={toggleSelect} showSdkBadge={showSdkBadge} />
            )) : phase === 'scanning' ? (
              <div className="py-7 text-center text-xs text-gray-400">{t('host:addDevice.waitingReplies')}</div>
            ) : (
              <div className="py-6 text-center text-xs text-gray-400">{t('host:addDevice.searchEmpty')}</div>
            )}
          </div>

          <div className="flex items-center gap-2 mt-3.5 pt-3.5 border-t border-gray-100">
            <button onClick={handleIgnore} className="h-8.5 px-2.5 text-[13px] text-gray-400 hover:text-gray-600 hover:bg-gray-50 rounded-lg transition-colors">
              {t('host:addDevice.ignore')}
            </button>
            <span className="flex-1" />
            <button onClick={handleAddAll} className="h-8.5 px-4 text-[13px] text-gray-600 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors">
              {t('host:addDevice.addAll')}
            </button>
            <button
              onClick={handleAddSelected}
              disabled={selected.size === 0}
              className="h-8.5 px-4 text-[13px] font-medium text-white bg-blue-600 rounded-lg
                hover:bg-blue-700 active:translate-y-px transition-all disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {selected.size > 0 ? `${t('host:addDevice.addSelected')} (${selected.size})` : t('host:addDevice.addSelected')}
            </button>
          </div>
        </>
      )}
    </div>
  )
}

export default AddDeviceScanTab
