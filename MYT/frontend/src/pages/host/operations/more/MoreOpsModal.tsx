import { useState, useRef} from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo, MirrorItem } from '../../../../types/index'
import { isContainerImage } from './shared'
import InfoTab from './tabs/InfoTab'
import DeviceTab from './tabs/DeviceTab'
import RootTab from './tabs/RootTab'
import CommTab from './tabs/CommTab'
import AppTab from './tabs/AppTab'
import DeviceInfoTab from './tabs/DeviceInfoTab'
import AppDisguiseTab from './tabs/AppDisguiseTab'
import V2ExtraTab from './tabs/V2ExtraTab'
import MediaTab from './tabs/MediaTab'
import LocationTab from './tabs/LocationTab'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import { useAndroidNotes } from '../../../../hooks/useNoteSync'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
  onClose: () => void
}

type TabKey = 'info' | 'device' | 'root' | 'comm' | 'app' | 'devinfo' | 'disguise' | 'media' | 'location' | 'v2extra'

// V3（容器）tab 列表
const v3Tabs: { key: TabKey; labelKey: string }[] = [
  { key: 'info', labelKey: 'host:moreOps.tabs.info' },
  { key: 'device', labelKey: 'host:moreOps.tabs.device' },
  { key: 'comm', labelKey: 'host:moreOps.tabs.comm' },
  { key: 'app', labelKey: 'host:moreOps.tabs.app' },
  { key: 'devinfo', labelKey: 'host:moreOps.tabs.devinfo' },
  { key: 'disguise', labelKey: 'host:moreOps.tabs.disguise' },
  { key: 'media', labelKey: 'host:moreOps.tabs.media' },
  { key: 'location', labelKey: 'host:moreOps.tabs.location' },
]

// V2（模拟器）tab 列表
const v2Tabs: { key: TabKey; labelKey: string }[] = [
  { key: 'root', labelKey: 'host:moreOps.tabs.root' },
  { key: 'comm', labelKey: 'host:moreOps.tabs.comm' },
  { key: 'v2extra', labelKey: 'host:moreOps.tabs.v2extra' },
  { key: 'devinfo', labelKey: 'host:moreOps.tabs.devinfo' },
  { key: 'media', labelKey: 'host:moreOps.tabs.media' },
  { key: 'location', labelKey: 'host:moreOps.tabs.location' },
]

function MoreOpsModal({ androids, mirrors, onClose }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const isV3 = isContainerImage(androids[0]?.image ?? '', mirrors)
  const tabs = isV3 ? v3Tabs : v2Tabs
  const [tab, setTab] = useState<TabKey>(tabs[0].key)
  const isBatch = androids.length > 1
  const primary = androids[0]

  // 确保 tab 在当前列表中（V2/V3 切换时可能持有无效 tab）
  const validTab = tabs.some(t => t.key === tab) ? tab : tabs[0].key

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[1180px] max-w-[96vw] h-[92vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <h2 className="text-base font-semibold text-gray-800">
              {t('host:moreOps.title')} · {isBatch ? t('host:moreOps.selectedCount', { count: androids.length }) : (primary ? (notes[primary.id] || primary.name) : '')}
            </h2>
            <span className={`text-[10px] px-1.5 py-0.5 rounded ${isV3 ? 'bg-blue-50 text-blue-500' : 'bg-emerald-50 text-emerald-500'}`}>
              {isV3 ? t('host:moreOps.container') : t('host:moreOps.emulator')}
            </span>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        <div className="flex border-b border-gray-100 overflow-x-auto">
          {tabs.map(tab => (
            <button
              key={tab.key}
              onClick={() => setTab(tab.key)}
              className={`px-3 py-2 text-xs font-medium whitespace-nowrap border-b-2 ${
                validTab === tab.key ? 'border-blue-500 text-blue-600' : 'border-transparent text-gray-500 hover:text-gray-700'
              }`}
            >
              {t(tab.labelKey)}
            </button>
          ))}
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4">
          {validTab === 'info' && <InfoTab androids={androids} mirrors={mirrors} />}
          {validTab === 'device' && <DeviceTab androids={androids} mirrors={mirrors} allContainer={true} />}
          {validTab === 'root' && <RootTab androids={androids} mirrors={mirrors} />}
          {validTab === 'comm' && <CommTab androids={androids} mirrors={mirrors} />}
          {validTab === 'app' && <AppTab androids={androids} mirrors={mirrors} allContainer={true} />}
          {validTab === 'devinfo' && <DeviceInfoTab androids={androids} mirrors={mirrors} />}
          {validTab === 'disguise' && <AppDisguiseTab androids={androids} mirrors={mirrors} />}
          {validTab === 'v2extra' && <V2ExtraTab androids={androids} mirrors={mirrors} />}
          {validTab === 'media' && <MediaTab androids={androids} mirrors={mirrors} />}
          {validTab === 'location' && <LocationTab androids={androids} mirrors={mirrors} />}
        </div>

        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('host:moreOps.close')}</button>
        </div>
      </div>
    </div>
  )
}

export default MoreOpsModal
