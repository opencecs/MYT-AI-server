import { useState, useEffect, useMemo, useRef } from 'react'
import { Events } from '@wailsio/runtime'
import { useTranslation } from 'react-i18next'
import { Archive } from 'lucide-react'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { DeviceState } from '../../types/index'
import DeviceSidebar from '../../components/DeviceSidebar'
import { useDeployMode } from '../../hooks/useDeployMode'
import { getDeviceId } from '../../utils/deviceId'
import { PhoneModelBackupTab } from './tabs/PhoneModelBackupTab'
import { AndroidBackupTab } from './tabs/AndroidBackupTab'

type BackupTabId = 'phoneModel' | 'android'

// 备份 Tab 页：左侧设备列表 + 右侧二级 Tab（备份机型 / 备份云机）
function BackupManagement() {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [devices, setDevices] = useState<DeviceState[]>([])
  const [selectedIP, setSelectedIP] = useState('')
  const selectedIPRef = useRef(selectedIP)
  selectedIPRef.current = selectedIP
  const [subTab, setSubTab] = useState<BackupTabId>('phoneModel')

  useEffect(() => {
    const load = async () => {
      try {
        const list = await DiscoverService.GetDeviceList() as DeviceState[]
        // 在线优先 + state 0（在线）/1（离线）均展示，与 DeviceSidebar 一致
        const visible = list.filter(d => d.state === 0 || d.state === 1 || d.state === 2)
        setDevices(visible)
        const online = visible.filter(d => d.state === 0)
        const first = online.length > 0 ? online[0] : (visible.length > 0 ? visible[0] : null)
        if (first && !selectedIPRef.current) setSelectedIP(getDeviceId(first, mode))
        if (selectedIPRef.current && !visible.find(d => getDeviceId(d, mode) === selectedIPRef.current) && first) {
          setSelectedIP(getDeviceId(first, mode))
        }
      } catch {}
    }
    load()
    const off = Events.On('devicelist:updated', (ev: any) => {
      const list = (ev.data ?? ev) as DeviceState[]
      if (Array.isArray(list)) {
        const visible = list.filter(d => d.state === 0 || d.state === 1 || d.state === 2)
        setDevices(visible)
        // 默认选第一台在线：选中失效时自动补位
        const online = visible.filter(d => d.state === 0)
        const first = online.length > 0 ? online[0] : (visible.length > 0 ? visible[0] : null)
        if (first) {
          setSelectedIP(prev => {
            const still = visible.find(d => getDeviceId(d, mode) === prev)
            if (prev && still) return prev
            return getDeviceId(first, mode)
          })
        }
      }
    })
    return () => { off() }
  }, [mode])

  const selectedDevice = useMemo(() => devices.find(d => getDeviceId(d, mode) === selectedIP), [devices, selectedIP, mode])

  const subTabs: { id: BackupTabId; label: string }[] = [
    { id: 'phoneModel', label: t('backup:subTab.phoneModel') },
    { id: 'android', label: t('backup:subTab.android') },
  ]

  return (
    <div className="flex h-full">
      <DeviceSidebar selectedIP={selectedIP} onSelect={setSelectedIP} />
      <div className="flex-1 flex flex-col overflow-hidden min-w-0">
        {/* 顶部二级 Tab */}
        <div className="h-10 shrink-0 flex items-center gap-1 px-4 bg-gray-50 border-b border-gray-200">
          <Archive size={14} className="text-gray-400 mr-1" />
          {subTabs.map(st => (
            <button
              key={st.id}
              onClick={() => setSubTab(st.id)}
              className={`h-7 px-3 text-xs rounded-md transition-colors ${
                subTab === st.id ? 'bg-blue-500 text-white' : 'text-gray-500 hover:bg-gray-100 hover:text-gray-700'
              }`}
            >
              {st.label}
            </button>
          ))}
        </div>

        {/* 内容区 */}
        <div className="flex-1 overflow-auto min-h-0">
          {!selectedDevice ? (
            <div className="flex items-center justify-center h-full text-gray-400 text-sm">
              {t('backup:selectDevice')}
            </div>
          ) : subTab === 'phoneModel' ? (
            <PhoneModelBackupTab device={selectedDevice} />
          ) : (
            <AndroidBackupTab device={selectedDevice} />
          )}
        </div>
      </div>
    </div>
  )
}

export default BackupManagement
