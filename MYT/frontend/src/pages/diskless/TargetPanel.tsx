import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { IscsiManager } from './useIscsiManager'
import ConfirmDialog from '../../components/ConfirmDialog'
import { formatMB, formatBytes, nextDiskName, storageLabel, useDraggable } from './targetPanelUtils'

interface Props {
  target: {
    status: { running: boolean; iqn: string; portal: string }
    disks: { name: string; lun: number; size_gb: number; iqn: string; storagePath: string }[]
    loading: boolean
    refreshDisks: () => Promise<void>
    createDisk: (name: string, sizeGB: number, storageDir: string) => Promise<void>
    deleteDisk: (name: string, force?: boolean) => Promise<void>
    resizeDisk: (deviceIP: string, name: string, newSizeGB: number) => Promise<boolean>
  }
  mgr: IscsiManager
  occupiedDisks: Record<string, string>
  onRefreshOccupied: () => void
  /** 当前选中的云设备 IP（占用匹配用，勿与本机网卡 IP 混淆） */
  deviceIP: string
}

export function Spinner() {
  return <svg className="animate-spin h-3.5 w-3.5" viewBox="0 0 24 24" fill="none"><circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" /><path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" /></svg>
}

export function TargetPanel({ target, mgr, occupiedDisks, onRefreshOccupied, deviceIP }: Props) {
  const { t } = useTranslation()
  const [showCreate, setShowCreate] = useState(false)
  const [diskName, setDiskName] = useState('')
  const [diskSize, setDiskSize] = useState(10)
  const [storageDir, setStorageDir] = useState('')
  const [resizing, setResizing] = useState<string | null>(null)
  const [resizeSize, setResizeSize] = useState(0)
  const [confirm, setConfirm] = useState<{ title: string; message: string; onConfirm: () => void } | null>(null)
  const drag = useDraggable()

  const { disks, loading } = target
  const mounted = mgr.storageStatus?.isMounted
  const st = mgr.storageStatus
  const usedPct = st && st.totalMB > 0 ? (st.usedMB / st.totalMB * 100) : 0

  const defaultStorageDrive = mgr.localDisks.length > 0
    ? mgr.localDisks.reduce((best, d) => d.freeBytes > best.freeBytes ? d : best, mgr.localDisks[0])
    : null

  const handleCreate = () => {
    if (!diskName.trim()) return
    const dir = storageDir || (defaultStorageDrive ? defaultStorageDrive.drive : '')
    target.createDisk(diskName.trim(), diskSize, dir)
    setDiskName('')
    setDiskSize(10)
    setShowCreate(false)
  }

  const handleResize = async () => {
    if (!resizing || resizeSize <= 0) return
    // 联动设备 = 占用（挂载）该盘的云设备：occupiedDisks[iqn] 即挂载者 IP；
    // 无人挂载时为空 → 只扩 Target 端。注意不能传本机网卡 IP（那是 portal 地址，不是设备地址）
    const disk = disks.find(d => d.name === resizing)
    const resizeDeviceIP = disk ? occupiedDisks[disk.iqn] || '' : ''
    const ok = await target.resizeDisk(resizeDeviceIP, resizing, resizeSize)
    if (!ok) return
    await mgr.refreshStorageMode()
    await mgr.refreshSessions()
    onRefreshOccupied()
    setResizing(null)
  }

  const handleDelete = (name: string, iqn: string) => {
    const occupiedBy = occupiedDisks[iqn]
    if (occupiedBy) {
      setConfirm({
        title: t('diskless:target.forceDeleteTitle'),
        message: t('diskless:target.forceDeleteConfirm', { device: occupiedBy }),
        onConfirm: () => { target.deleteDisk(name, true); setConfirm(null) },
      })
      return
    }
    setConfirm({
      title: t('diskless:target.deleteDiskTitle'),
      message: t('diskless:target.deleteDiskConfirm', { name }),
      onConfirm: () => { target.deleteDisk(name); setConfirm(null) },
    })
  }

  const handleUnmount = () => {
    // 优先从 sessions 获取 IQN/portal，兜底从 occupiedDisks 推导
    let iqn = ''
    let portal = ''
    const s = mgr.sessions[0]
    if (s) {
      iqn = s.iqn
      portal = s.portal
    } else {
      // sessions 为空时，从 occupiedDisks 找当前选中设备占用的 IQN。
      // occupiedDisks 的 value 是挂载该盘的云设备 IP，必须与设备 IP 匹配——
      // 匹配不到就提示无法卸载，绝不退化取任意盘（可能是其他设备挂载的）
      for (const [q, ip] of Object.entries(occupiedDisks)) {
        if (deviceIP && ip === deviceIP) {
          iqn = q
          break
        }
      }
      if (iqn) portal = `${mgr.targetIP}:3260`
    }
    if (!iqn) {
      setConfirm({
        title: t('diskless:target.cannotUnmountTitle'),
        message: t('diskless:target.cannotUnmountMsg'),
        onConfirm: () => setConfirm(null),
      })
      return
    }
    const occupiedBy = occupiedDisks[iqn]
    setConfirm({
      title: t('diskless:target.unmountTitle'),
      message: occupiedBy
        ? t('diskless:target.unmountConfirmOccupied', { device: occupiedBy })
        : t('diskless:target.unmountConfirm'),
      onConfirm: () => { setConfirm(null); mgr.umountIscsi(portal, iqn).then(ok => { if (ok) onRefreshOccupied() }) },
    })
  }

  const selectedDriveInfo = mgr.localDisks.find(d => d.drive === (storageDir || defaultStorageDrive?.drive))
  const notEnoughSpace = selectedDriveInfo && diskSize > 0 && (diskSize * 1024 * 1024 * 1024) > selectedDriveInfo.freeBytes

  return (
    <div className="flex-1 flex flex-col overflow-hidden min-h-0">
      <div className="flex items-center gap-2 mb-3 shrink-0">
        <div className={`w-2 h-2 rounded-full ${target.status.running ? 'bg-green-500' : 'bg-gray-300'}`} />
        <span className="text-xs text-gray-600">{target.status.running ? t('diskless:target.storageRunning') : t('diskless:target.storageStopped')}</span>
        {target.status.portal && (
          <span className="text-[10px] text-gray-400 font-mono">{target.status.portal}</span>
        )}
      </div>

      <div className="flex-1 grid grid-cols-2 gap-4 min-h-0">
        {/* 左栏：虚拟磁盘 */}
        <div className="border border-gray-200 rounded-lg overflow-hidden flex flex-col min-h-0">
          <div className="px-4 py-2.5 bg-gray-50 border-b border-gray-200 flex items-center justify-between shrink-0">
            <span className="text-xs font-medium text-gray-700">{t('diskless:target.virtualDisks')}</span>
            <button onClick={() => { setShowCreate(true); setDiskName(nextDiskName(disks)); setStorageDir(defaultStorageDrive ? defaultStorageDrive.drive : '') }} className="px-2.5 py-1 text-xs rounded-md bg-blue-500 text-white hover:bg-blue-600 cursor-pointer">+ {t('diskless:target.create')}</button>
          </div>

          <div className="flex-1 overflow-auto min-h-0">
            {disks.length === 0 ? (
              <div className="px-4 py-8 text-center text-xs text-gray-400">{t('diskless:target.noDisks')}</div>
            ) : (
              <div className="divide-y divide-gray-100">
                {disks.map(d => {
                  const occupiedBy = occupiedDisks[d.iqn]
                  // 当前设备已挂载此磁盘（从 sessions 中判断，更可靠）
                  const currentSession = mgr.sessions.find(s => s.iqn === d.iqn)
                  const isSelfMounted = !!currentSession
                  return (
                    <div key={d.name} className="px-4 py-3 flex items-center justify-between hover:bg-gray-50/50">
                      <div className="flex items-center gap-3">
                        <div className="w-9 h-9 rounded-lg flex items-center justify-center text-xs font-bold bg-blue-50 text-blue-500">{d.lun}</div>
                        <div>
                          <div className="text-sm text-gray-700 font-medium">{d.name}</div>
                          <div className="flex items-center gap-2 text-xs text-gray-400 mt-0.5">
                            <span>{d.size_gb % 1 ? d.size_gb.toFixed(1) : d.size_gb} GB</span>
                            <span className="text-gray-300">|</span>
                            <span>{storageLabel(d.storagePath) || t('diskless:target.default')}</span>
                            {isSelfMounted && <><span className="text-gray-300">|</span><span className="text-green-600">{t('diskless:target.mounted')}</span></>}
                            {!isSelfMounted && occupiedBy && <><span className="text-gray-300">|</span><span className="text-orange-500">{t('diskless:target.inUseBy', { device: occupiedBy })}</span></>}
                          </div>
                        </div>
                      </div>
                      <div className="flex items-center gap-1.5">
                        {resizing === d.name ? (
                          <>
                            <input type="number" value={resizeSize} onChange={e => setResizeSize(Number(e.target.value))} min={d.size_gb + 1} className="w-20 text-xs border border-gray-200 rounded-md px-2 py-1 focus:outline-none focus:border-blue-400" />
                            <span className="text-xs text-gray-400">GB</span>
                            <button onClick={handleResize} disabled={loading} className="px-2.5 py-1 text-xs rounded-md bg-blue-500 text-white hover:bg-blue-600 disabled:opacity-50 cursor-pointer">{t('button.confirm')}</button>
                            <button onClick={() => setResizing(null)} className="px-2.5 py-1 text-xs rounded-md border border-gray-200 text-gray-500 hover:bg-gray-50 cursor-pointer">{t('button.cancel')}</button>
                          </>
                        ) : (
                          <>
                            {!isSelfMounted && occupiedBy && (
                              <button
                                onClick={() => {
                                  setConfirm({
                                    title: t('diskless:target.remoteUnmountTitle'),
                                    message: t('diskless:target.remoteUnmountConfirm', { device: occupiedBy }),
                                    onConfirm: async () => {
                                      setConfirm(null)
                                      // 远程卸载走任务队列（占用方设备执行，有进度、可查历史）
                                      const ok = await mgr.remoteUmountIscsi(occupiedBy, `${mgr.targetIP}:3260`, d.iqn)
                                      if (ok) {
                                        onRefreshOccupied()
                                        mgr.refreshSessions()
                                      }
                                    },
                                  })
                                }}
                                disabled={loading}
                                className="px-2.5 py-1 text-xs rounded-md border border-orange-200 text-orange-600 hover:bg-orange-50 disabled:opacity-50 cursor-pointer"
                              >{t('diskless:target.remoteUnmount')}</button>
                            )}
                            <button onClick={() => { setResizing(d.name); setResizeSize(d.size_gb + 10) }} disabled={loading} className="px-2.5 py-1 text-xs rounded-md border border-gray-200 text-gray-600 hover:bg-gray-50 disabled:opacity-50 cursor-pointer">{t('diskless:target.expand')}</button>
                            <button
                              onClick={() => handleDelete(d.name, d.iqn)}
                              disabled={loading || isSelfMounted}
                              className={`px-2.5 py-1 text-xs rounded-md border cursor-pointer ${isSelfMounted ? 'border-gray-100 text-gray-300 cursor-not-allowed' : occupiedBy ? 'border-orange-200 text-orange-600 hover:bg-orange-50' : 'border-red-200 text-red-500 hover:bg-red-50'}`}
                              title={isSelfMounted ? t('diskless:target.selfMountedTooltip') : occupiedBy ? t('diskless:target.inUseTooltip', { device: occupiedBy }) : ''}
                            >{occupiedBy ? t('diskless:target.forceDelete') : t('diskless:target.delete')}</button>
                          </>
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        </div>

        {/* 右栏：设备挂载 */}
        <div className="border border-gray-200 rounded-lg overflow-hidden flex flex-col min-h-0">
          <div className="px-4 py-2.5 bg-gray-50 border-b border-gray-200 flex items-center justify-between shrink-0">
            <span className="text-xs font-medium text-gray-700">{t('diskless:target.deviceMount')}</span>
            {mounted ? (
              <span className="flex items-center gap-1 text-[10px] text-green-600"><span className="w-1.5 h-1.5 rounded-full bg-green-500" />{t('diskless:target.connected')}</span>
            ) : (
              <span className="text-[10px] text-gray-400">{t('diskless:target.notConnected')}</span>
            )}
          </div>

          <div className="flex-1 flex flex-col min-h-0">
            {mounted && st ? (
              <div className="px-4 py-4 flex-1 flex flex-col gap-4">
                <div>
                  <div className="flex items-baseline justify-between mb-1.5">
                    <span className="text-lg font-semibold text-gray-800">{formatMB(st.availMB)} <span className="text-xs font-normal text-gray-400">{t('diskless:target.available')}</span></span>
                    <span className="text-[10px] text-gray-400">{formatMB(st.totalMB)} {t('diskless:target.total')}</span>
                  </div>
                  <div className="h-2 bg-gray-100 rounded-full overflow-hidden">
                    <div
                      className={`h-full rounded-full ${usedPct > 80 ? 'bg-red-400' : usedPct > 50 ? 'bg-yellow-400' : 'bg-green-400'}`}
                      style={{ width: `${usedPct.toFixed(1)}%` }}
                    />
                  </div>
                  <div className="mt-1 text-[10px] text-gray-400">{t('diskless:target.used', { used: formatMB(st.usedMB) })}</div>
                </div>

                <div className="grid grid-cols-2 gap-2 text-[11px]">
                  <div className="bg-gray-50 rounded px-3 py-2">
                    <div className="text-gray-400 text-[10px]">{t('diskless:target.blockDevice')}</div>
                    <div className="text-gray-700 font-mono">{st.device}</div>
                  </div>
                  <div className="bg-gray-50 rounded px-3 py-2">
                    <div className="text-gray-400 text-[10px]">{t('diskless:target.mountPoint')}</div>
                    <div className="text-gray-700 font-mono">{st.mountPoint}</div>
                  </div>
                </div>

                <button
                  onClick={handleUnmount}
                  disabled={mgr.loading}
                  className="w-full py-2 text-xs rounded-lg border border-red-200 text-red-600 hover:bg-red-50 disabled:opacity-50 cursor-pointer flex items-center justify-center gap-2"
                >
                  {mgr.loading && <Spinner />}
                  {mgr.loading ? t('diskless:target.unmounting') : t('diskless:target.unmount')}
                </button>
              </div>
            ) : (
              <div className="flex-1 flex flex-col items-center justify-center py-8 gap-2">
                <div className="w-10 h-10 rounded-full bg-gray-100 flex items-center justify-center">
                  <svg className="w-5 h-5 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                    <path strokeLinecap="round" strokeLinejoin="round" d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.622l1.757-1.757a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244" />
                  </svg>
                </div>
                <div className="text-xs text-gray-400">{t('diskless:target.notMounted')}</div>
                <div className="text-[10px] text-gray-300">{t('diskless:target.notMountedHint')}</div>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* 创建磁盘弹窗 - 点背景不关，可拖拽 */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div ref={drag.panelRef} className="bg-white rounded-lg shadow-xl w-[400px]" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-3 border-b border-gray-200 cursor-move select-none" onMouseDown={drag.onMouseDown}>
              <h3 className="text-sm font-semibold text-gray-800">{t('diskless:target.createDiskTitle')}</h3>
              <button onClick={() => setShowCreate(false)} className="text-gray-400 hover:text-gray-600 cursor-pointer text-lg leading-none">x</button>
            </div>
            <div className="px-5 py-4 space-y-4">
              <div>
                <label className="block text-xs text-gray-500 mb-1">{t('diskless:target.diskName')}</label>
                <input value={diskName} onChange={e => setDiskName(e.target.value)} className="w-full text-xs border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400" />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">{t('diskless:target.diskSize')}</label>
                <input type="number" value={diskSize} onChange={e => setDiskSize(Number(e.target.value))} min={1} className="w-full text-xs border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400" />
                {notEnoughSpace && <div className="text-[10px] text-red-500 mt-1">{t('diskless:target.notEnoughSpace', { available: formatBytes(selectedDriveInfo!.freeBytes) })}</div>}
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">{t('diskless:target.storageLocation')}</label>
                <select value={storageDir || (defaultStorageDrive?.drive ?? '')} onChange={e => setStorageDir(e.target.value)} className="w-full text-xs border border-gray-200 rounded-lg px-3 py-2 focus:outline-none focus:border-blue-400">
                  {mgr.localDisks.map(d => {
                    const pct = d.totalBytes > 0 ? ((d.totalBytes - d.freeBytes) / d.totalBytes * 100) : 0
                    return (
                      <option key={d.drive} value={d.drive}>
                        {d.drive} {d.label ? `(${d.label})` : ''} — {formatBytes(d.freeBytes)} {t('diskless:target.availableLabel')} / {formatBytes(d.totalBytes)} {pct > 80 ? t('diskless:target.spaceInsufficient') : ''}
                      </option>
                    )
                  })}
                </select>
              </div>
            </div>
            <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-gray-200">
              <button onClick={() => setShowCreate(false)} className="px-4 py-1.5 text-xs rounded-lg border border-gray-200 text-gray-600 hover:bg-gray-50 cursor-pointer">
                {t('button.cancel')}
              </button>
              <button onClick={handleCreate} disabled={loading || !diskName.trim() || diskSize <= 0 || notEnoughSpace} className="px-4 py-1.5 text-xs rounded-lg bg-blue-500 text-white hover:bg-blue-600 disabled:opacity-50 cursor-pointer">
                {loading ? t('status.loading') : t('button.create')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 二次确认弹窗 */}
      {confirm && (
        <ConfirmDialog
          title={confirm.title}
          message={confirm.message}
          confirmLabel={t('button.confirm')}
          onConfirm={confirm.onConfirm}
          onCancel={() => setConfirm(null)}
        />
      )}
    </div>
  )
}
