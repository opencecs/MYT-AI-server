import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, ClipboardList, Smartphone, HardDrive, Monitor, Globe } from 'lucide-react'
import { useModalEnter } from '../../hooks/useModalAnimation'
import { MytPanelContent } from './modals/MytPanelContent'
import { TunnelSection } from './modals/TunnelSection'
import { PenetrationPanel } from './modals/penetration/PenetrationPanel'
import { DockerContent } from './modals/DockerContent'
import { LogContent } from './modals/LogContent'
import { CacheContent } from './modals/CacheContent'
import { FormatDiskContent } from './modals/FormatDiskContent'
import type { ModalType } from './maintenanceTypes'

const modalTitleKeys: Record<string, string> = {
  mytPanel: 'maintenance:mytPanel', tunnel: 'maintenance:tunnel.title', penetration: 'maintenance:penetration.title', docker: 'maintenance:dockerCleanup',
  log: 'maintenance:logCleanup', cache: 'maintenance:cacheCleanup', formatDisk: 'maintenance:formatDisk',
}
const modalIcons: Record<string, React.ComponentType<{ className?: string }>> = {
  mytPanel: Monitor, tunnel: Globe, penetration: Globe, docker: Trash2, log: ClipboardList, cache: Smartphone, formatDisk: HardDrive,
}

/** 维护操作弹窗外壳：维护页云机卡片 + 设备右键菜单共用。 */
export function MaintenanceModal({ type, deviceIP, androidName, logMode, onClose }: {
  type: Exclude<ModalType, null>
  deviceIP: string
  androidName: string
  logMode: 'device' | 'android'
  onClose: () => void
}) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const Icon = modalIcons[type]
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef}>
      <div className={`bg-white rounded-lg shadow-xl max-h-[80vh] flex flex-col modal-content ${type === 'penetration' ? 'w-[900px] max-w-[calc(100vw-32px)]' : 'w-[520px]'}`}>
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200">
          <h3 className="text-sm font-semibold text-gray-800 flex items-center gap-1.5">
            <Icon className="w-4 h-4 text-gray-500" />
            {t(modalTitleKeys[type])}
          </h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 cursor-pointer text-lg leading-none">×</button>
        </div>
        <div className="flex-1 overflow-y-auto p-4">
          {type === 'mytPanel' && <MytPanelContent deviceIP={deviceIP} onClose={onClose} />}
          {type === 'tunnel' && <TunnelSection deviceIP={deviceIP} onClose={onClose} />}
          {type === 'penetration' && <PenetrationPanel deviceIP={deviceIP} onClose={onClose} />}
          {type === 'docker' && <DockerContent deviceIP={deviceIP} onClose={onClose} />}
          {type === 'log' && <LogContent deviceIP={deviceIP} androidName={androidName} mode={logMode} onClose={onClose} />}
          {type === 'cache' && <CacheContent deviceIP={deviceIP} androidName={androidName} onClose={onClose} />}
          {type === 'formatDisk' && <FormatDiskContent deviceIP={deviceIP} onClose={onClose} />}
        </div>
      </div>
    </div>
  )
}
