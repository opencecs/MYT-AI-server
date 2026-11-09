import { useState, useEffect, useRef, useCallback } from 'react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import { useTranslation } from 'react-i18next'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as AgentService from '../../../bindings/github.com/moyunteng/myt/agentservicewrapper'
import { showToast } from '../../components/Toast'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import { useDeployMode } from '../../hooks/useDeployMode'
import { useDeviceNotes } from '../../hooks/useNoteSync'
import { getDeviceId } from '../../utils/deviceId'

/** 带超时的 Promise，超时后 reject，用于防止 Wails 调用卡住导致 loading 永久转 */
function withTimeout<T>(p: Promise<T>, ms: number, label: string): Promise<T> {
  return Promise.race([
    p,
    new Promise<T>((_, rej) => setTimeout(() => rej(new Error(label + ' timeout')), ms)),
  ])
}

/** 并发受限映射：结果顺序与 items 一致；fn 需自行捕获单台失败（不抛出），避免中断其他设备 */
async function mapWithConcurrency<T, R>(items: readonly T[], limit: number, fn: (item: T) => Promise<R>): Promise<R[]> {
  const results = new Array<R>(items.length)
  let next = 0
  const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (next < items.length) {
      const i = next++
      results[i] = await fn(items[i])
    }
  })
  await Promise.all(workers)
  return results
}
import type { DeviceState } from '../../types/index'

gsap.registerPlugin(useGSAP)

interface DeviceRow {
  ip: string
  deviceId: string
  ident: string
  displayName: string
  selected: boolean
  agentInstalled: boolean
  agentVersion: string
  agentStatus: string
  agentLoading: boolean
}

interface Props {
  onClose: () => void
}

/**
 * 旧版 Agent 清理弹窗（智能体页）：勾选在线设备并批量卸载设备端旧版 Hermes Agent。
 * 原「VPN 客户端」tab 已迁移到主机右键设备的「公网穿透」弹窗（penetration/VpnSection）。
 */
function AgentCleanupModal({ onClose }: Props) {
  const { t } = useTranslation()
  const [devices, setDevices] = useState<DeviceRow[]>([])
  const { mode } = useDeployMode()
  const { notes: deviceNotes } = useDeviceNotes()
  const deviceNotesRef = useRef(deviceNotes)
  deviceNotesRef.current = deviceNotes
  const [operating, setOperating] = useState(false)
  const [search, setSearch] = useState('')
  const containerRef = useRef<HTMLDivElement>(null)
  const { addTask } = useTaskQueue()

  useGSAP(() => {
    gsap.fromTo('.tools-modal', { scale: 0.94, opacity: 0 }, { scale: 1, opacity: 1, duration: 0.35, ease: 'power2.out' })
    gsap.fromTo('.tools-row', { x: -12, opacity: 0 }, { x: 0, opacity: 1, duration: 0.3, stagger: 0.04, ease: 'power2.out', delay: 0.1 })
  }, { scope: containerRef })

  useEffect(() => {
    const load = async () => {
      try {
        const list = await DiscoverService.GetDeviceList() as DeviceState[]
        const online = list.filter((d) => d.state === 0)
        setDevices(online.map((d) => {
          const ident = getDeviceId(d, mode)
          const note = deviceNotesRef.current[d.device_id] || ''
          const displayName = d.ip || note || d.device_id
          return {
          ip: d.ip, deviceId: d.device_id, ident, displayName, selected: true,
          agentInstalled: false, agentVersion: '', agentStatus: '', agentLoading: true,
        }
        }))
        await mapWithConcurrency(online, 5, async (dev) => {
          const devIdent = getDeviceId(dev, mode)
          const patch = (fields: Partial<DeviceRow>) =>
            setDevices((prev) => prev.map((d) => d.ident === devIdent ? { ...d, ...fields } : d))
          try {
            const s = await withTimeout(AgentService.GetAgentStatus(devIdent), 15000, 'agent') as { installed: boolean; version: string; status: string }
            patch({ agentInstalled: s.installed, agentVersion: s.version, agentStatus: s.status, agentLoading: false })
          } catch { patch({ agentLoading: false }) }
        })
      } catch { showToast(t('agent:deviceTools.toast.getDevicesFailed'), 'error') }
    }
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const selectedDevices = devices.filter((d) => d.selected)

  const keyword = search.trim().toLowerCase()
  const filteredDevices = keyword ? devices.filter((d) => d.displayName.toLowerCase().includes(keyword) || d.ident.toLowerCase().includes(keyword)) : devices
  const filteredSelected = filteredDevices.filter((d) => d.selected).length
  const filteredAllSelected = filteredDevices.length > 0 && filteredSelected === filteredDevices.length

  const toggleSelect = (ident: string) => {
    setDevices((prev) => prev.map((d) => d.ident === ident ? { ...d, selected: !d.selected } : d))
  }
  const toggleAll = () => {
    if (filteredAllSelected) {
      // 取消全选：取消所有设备的选中状态
      setDevices((prev) => prev.map((d) => ({ ...d, selected: false })))
    } else {
      // 全选：只选中当前筛选结果
      setDevices((prev) => prev.map((d) => keyword && !(d.displayName.toLowerCase().includes(keyword) || d.ident.toLowerCase().includes(keyword)) ? d : { ...d, selected: true }))
    }
  }

  const handleAgentUninstall = useCallback(async () => {
    const targets = selectedDevices.filter((d) => d.agentInstalled)
    if (targets.length === 0) { showToast(t('agent:deviceTools.toast.noAgentInstalled')); return }
    setOperating(true)
    for (const d of targets) {
      addTask({ type: 'uninstallAgent', deviceIps: [d.ident], targets: [{ deviceIP: d.ident }], total: 1 })
      try { await AgentService.UninstallAgent(d.ident) } catch (e) { showToast(t('agent:deviceTools.toast.uninstallFailed', { ip: d.displayName, error: e }), 'error') }
    }
    showToast(t('agent:deviceTools.toast.cleanupStarted', { count: targets.length }))
    setOperating(false)
    onClose()
  }, [selectedDevices, addTask, onClose, t])

  return (
    <div className="fixed inset-0 z-[80] flex items-center justify-center bg-black/30" onClick={onClose}>
      <div ref={containerRef} className="tools-modal bg-white border border-gray-200 rounded-xl shadow-xl w-[600px] max-w-[calc(100vw-32px)] max-h-[80vh] flex flex-col" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
          <h3 className="text-sm font-semibold text-gray-800">{t('agent:deviceTools.title')}</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">×</button>
        </div>

        {/* Select bar + Search */}
        <div className="px-5 py-2.5 border-b border-gray-100 flex items-center gap-3 bg-gray-50/50">
          <button onClick={toggleAll} disabled={filteredDevices.length === 0}
            className="text-xs text-blue-500 hover:text-blue-600 disabled:text-gray-300 disabled:cursor-not-allowed">
            {filteredAllSelected ? t('button.deselectAll') : t('button.selectAll')}
          </button>
          <span className="text-[11px] text-gray-400">
            {t('agent:deviceTools.onlineSelected', { total: devices.length, selected: selectedDevices.length })}{keyword && filteredDevices.length < devices.length ? t('agent:deviceTools.filtered', { count: filteredDevices.length }) : ''}
          </span>
          <div className="flex-1" />
          <div className="relative w-40">
            <svg className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t('agent:deviceTools.searchIp')}
              className="w-full text-xs pl-7 pr-2 py-1.5 bg-white border border-gray-200 rounded-md focus:outline-none focus:border-blue-400 placeholder:text-gray-300"
            />
          </div>
        </div>

        {/* Device list */}
        <div className="flex-1 overflow-y-auto py-1">
          {devices.length === 0 ? (
            <div className="py-12 text-center text-sm text-gray-400">{t('agent:deviceTools.loading')}</div>
          ) : filteredDevices.length === 0 ? (
            <div className="py-12 text-center text-sm text-gray-400">{t('agent:deviceTools.noMatch')}</div>
          ) : (
            filteredDevices.map((d) => (
              <div key={d.deviceId}
                className={`tools-row flex items-center gap-3 px-5 py-2.5 border-b border-gray-50 hover:bg-gray-50 cursor-pointer`}
                onClick={() => toggleSelect(d.ident)}
              >
                <input type="checkbox" checked={d.selected} readOnly
                  className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500 focus:ring-blue-400/30 bg-white pointer-events-none" />
                <span className="text-sm text-gray-800 font-mono">{d.displayName}</span>
                <div className="ml-auto flex items-center gap-2">
                  {d.agentLoading ? <span className="text-[10px] text-gray-400">{t('agent:deviceTools.querying')}</span>
                  : d.agentInstalled ? (
                    <>
                      <span className="text-[10px] text-gray-500">v{d.agentVersion}</span>
                      <span className={`text-[10px] px-1.5 py-0.5 rounded ${d.agentStatus === 'running' ? 'bg-green-50 text-green-600' : 'bg-gray-100 text-gray-500'}`}>
                        {d.agentStatus === 'running' ? t('status.running') : t('status.stopped')}
                      </span>
                    </>
                  ) : <span className="text-[10px] text-gray-300">{t('status.notInstalled')}</span>}
                </div>
              </div>
            ))
          )}
        </div>

        {/* Action bar */}
        <div className="px-5 py-4 border-t border-gray-100">
          <div className="flex items-center gap-3">
            <p className="text-xs text-gray-500 flex-1">
              {t('agent:deviceTools.cleanDesc')}
            </p>
            <button onClick={handleAgentUninstall} disabled={selectedDevices.filter((d) => d.agentInstalled).length === 0 || operating}
              className="px-4 py-1.5 text-xs rounded-lg bg-red-500 text-white hover:bg-red-600 disabled:bg-gray-100 disabled:text-gray-400 disabled:cursor-not-allowed transition-colors">
              {operating ? t('agent:deviceTools.initiating') : t('agent:deviceTools.cleanCount', { count: selectedDevices.filter((d) => d.agentInstalled).length })}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

export default AgentCleanupModal
