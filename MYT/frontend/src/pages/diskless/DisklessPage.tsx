import { useState, useEffect, useMemo, useRef, memo } from 'react'
import { useTranslation } from 'react-i18next'
import { Events } from '@wailsio/runtime'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as IscsiService from '../../../bindings/github.com/moyunteng/myt/iscsiservicewrapper'
import * as SdkApiService from '../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { DeviceState } from '../../types/index'

import DeviceSidebar from '../../components/DeviceSidebar'
import { useIscsiManager } from './useIscsiManager'
import { useIscsiTarget } from './useIscsiTarget'
import { IscsiWizardModal } from './IscsiWizardModal'
import { TargetPanel } from './TargetPanel'

interface OccupiedDisk {
  iqn: string
  deviceIP: string
}

function DisklessPage() {
  const { t } = useTranslation()
  const [devices, setDevices] = useState<DeviceState[]>([])
  const [selectedIP, setSelectedIP] = useState('')
  const [showWizard, setShowWizard] = useState(false)
  const [occupiedDisks, setOccupiedDisks] = useState<Record<string, string>>({})
  const selectedIPRef = useRef(selectedIP)
  selectedIPRef.current = selectedIP

  const refreshOccupiedDisks = async () => {
    try {
      const list = await IscsiService.GetOccupiedDisks() as OccupiedDisk[]
      const map: Record<string, string> = {}
      if (Array.isArray(list)) {
        for (const d of list) map[d.iqn] = d.deviceIP
      }
      setOccupiedDisks(map)
    } catch (e) { console.warn('[iSCSI] GetOccupiedDisks failed:', e); setOccupiedDisks({}) }
  }

  useEffect(() => {
    const load = async () => {
      try {
        const list = await DiscoverService.GetDeviceList() as DeviceState[]
        const online = list.filter(d => d.state === 0)
        setDevices(online)
        if (online.length > 0 && !selectedIPRef.current) setSelectedIP(online[0].ip)
        if (selectedIPRef.current && !online.find(d => d.ip === selectedIPRef.current) && online.length > 0) setSelectedIP(online[0].ip)
      } catch {}
    }
    load()
    const off = Events.On('devicelist:updated', (ev: any) => {
      const list = ev.data ?? ev
      if (Array.isArray(list)) setDevices(list.filter((d: DeviceState) => d.state === 0))
    })
    return () => { off() }
  }, [])

  const selectedDevice = useMemo(() => devices.find(d => d.ip === selectedIP), [devices, selectedIP])
  const [sdkVersion, setSdkVersion] = useState(0)
  useEffect(() => {
    if (!selectedIP) { setSdkVersion(0); return }
    let cancelled = false
    SdkApiService.GetAPIVersionInfo(selectedIP).then((info: any) => {
      if (cancelled) return
      const v = info?.currentVersion ?? 0
      console.log('[iSCSI] sdkVersion:', v)
      setSdkVersion(v)
    }).catch(() => { if (!cancelled) setSdkVersion(0) })
    return () => { cancelled = true }
  }, [selectedIP])
  const mgr = useIscsiManager(selectedIP)
  const target = useIscsiTarget()

  useEffect(() => { refreshOccupiedDisks() }, [selectedIP, mgr.storageStatus?.isMounted])
  useEffect(() => { const ti = setInterval(refreshOccupiedDisks, 5000); return () => clearInterval(ti) }, [selectedIP])

  const handleRefresh = () => {
    target.refreshStatus()
    target.refreshDisks()
    mgr.refreshSessions()
    mgr.refreshStorageMode()
    refreshOccupiedDisks()
  }

  return (
    <div className="flex h-full">
      <DeviceSidebar selectedIP={selectedIP} onSelect={setSelectedIP} />
      <div className="flex-1 flex flex-col overflow-hidden p-5">
        {!selectedDevice ? (
          <div className="flex-1 flex flex-col items-center justify-center gap-2">
            <div className="w-12 h-12 rounded-full bg-gray-100 flex items-center justify-center">
              <svg className="w-6 h-6 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3" />
              </svg>
            </div>
            <div className="text-sm text-gray-400">{t('diskless:page.selectDevice')}</div>
          </div>
        ) : sdkVersion < 126 ? (
          <div className="flex-1 flex flex-col items-center justify-center gap-2">
            <div className="w-12 h-12 rounded-full bg-gray-100 flex items-center justify-center">
              <svg className="w-6 h-6 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
              </svg>
            </div>
            <div className="text-sm text-gray-400">{t('diskless:page.sdkVersionTooLow', { version: selectedDevice.version || t('diskless:page.unknownVersion') })}</div>
            <div className="text-xs text-gray-300">{t('diskless:page.sdkUpgradeHint')}</div>
          </div>
        ) : (
          <>
            <div className="flex items-center gap-3 mb-4 shrink-0">
              <h2 className="text-base font-semibold text-gray-800">{selectedDevice.ip}</h2>
              <span className="text-[10px] text-gray-400 bg-gray-100 px-1.5 py-0.5 rounded">{selectedDevice.device_type.toUpperCase()}</span>
              {mgr.storageStatus?.isMounted && (
                <span className="flex items-center gap-1 text-[10px] text-green-600 bg-green-50 px-1.5 py-0.5 rounded">
                  <span className="w-1.5 h-1.5 rounded-full bg-green-500" />
                  {t('diskless:page.disklessMode')}
                </span>
              )}
              <div className="flex-1" />
              <button onClick={handleRefresh} className="px-2.5 py-1 text-xs rounded-lg border border-gray-200 text-gray-500 hover:bg-gray-50 cursor-pointer">
                {t('button.refresh')}
              </button>
              {!mgr.storageStatus?.isMounted && (
                <button onClick={() => setShowWizard(true)} className="px-3 py-1 text-xs rounded-lg bg-blue-500 text-white hover:bg-blue-600 cursor-pointer">
                  {t('diskless:page.configureStorage')}
                </button>
              )}
            </div>
            <TargetPanel target={target} mgr={mgr} occupiedDisks={occupiedDisks} onRefreshOccupied={refreshOccupiedDisks} deviceIP={selectedIP} />
          </>
        )}
      </div>
      {showWizard && selectedDevice && (
        <IscsiWizardModal mgr={mgr} occupiedDisks={occupiedDisks} onClose={() => setShowWizard(false)} />
      )}
    </div>
  )
}

export default memo(DisklessPage)
