import { useState, useMemo, memo } from 'react'
import { useTranslation } from 'react-i18next'
import { useDeployMode } from '../../hooks/useDeployMode'
import { useOnlineDevices } from '../../hooks/useOnlineDevices'
import { useStreamManager } from './useStreamManager'
import ForwardStreamTab from './ForwardStreamTab'
import P2PStreamTab from './P2PStreamTab'
import CameraStreamTab from './camera/CameraStreamTab'
import StreamGuideTab from './StreamGuideTab'
import DistributionPanel from './distribution/DistributionPanel'
import DistributeDialog from './distribution/DistributeDialog'
import type { StreamTab, Distribution } from './streamTypes'

const ALL_TABS: { key: StreamTab; label: string }[] = [
  { key: 'forward', label: 'stream:tab.forward' },
  { key: 'p2p', label: 'stream:tab.p2p' },
  { key: 'camera', label: 'stream:tab.camera' },
  { key: 'guide', label: 'stream:tab.guide' },
]

// 公网模式不可用的 Tab（客户端无公网 IP，设备无法拉流 + exe 直连设备不可行）
const PUBLIC_HIDDEN_TABS: StreamTab[] = ['forward', 'p2p']

function StreamManagement() {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { devices } = useOnlineDevices()
  const [activeTab, setActiveTab] = useState<StreamTab>('forward')
  const mgr = useStreamManager()
  // 分发弹窗：新建（streamName 预填 / 空）与「修改流」（record 预填）共用
  const [distributeTarget, setDistributeTarget] = useState<{ streamName?: string; record?: Distribution } | null>(null)

  const tabs = useMemo(() => {
    if (mode === 'public') {
      return ALL_TABS.filter(tab => !PUBLIC_HIDDEN_TABS.includes(tab.key))
    }
    return ALL_TABS
  }, [mode])

  // 公网模式默认选 camera（forward/p2p 被隐藏）
  const effectiveTab = tabs.some(tab => tab.key === activeTab) ? activeTab : (tabs[0]?.key ?? 'guide')
  // forward/p2p 带右侧分发管理面板（camera 自带预览面板）
  const showPanel = effectiveTab === 'forward' || effectiveTab === 'p2p'

  return (
    <div className="flex h-full flex-col p-5">
      <div className="flex gap-1 border-b border-gray-200 mb-4">
        {tabs.map(tab => (
          <button key={tab.key} onClick={() => setActiveTab(tab.key)}
            className={`px-4 py-2 text-xs border-b-2 transition-colors ${
              effectiveTab === tab.key
                ? 'text-blue-500 border-blue-500 font-medium'
                : 'text-gray-500 border-transparent hover:text-gray-700'
            }`}>
            {t(tab.label)}
          </button>
        ))}
      </div>
      <div className="flex-1 min-h-0 flex gap-3">
        <div className="flex-1 min-w-0 flex flex-col">
          {effectiveTab === 'forward' && (
            <ForwardStreamTab mgr={mgr} onDistribute={(name) => setDistributeTarget({ streamName: name })} />
          )}
          {effectiveTab === 'p2p' && <P2PStreamTab mgr={mgr} devices={devices} />}
          {effectiveTab === 'camera' && <CameraStreamTab mgr={mgr} devices={devices} />}
          {effectiveTab === 'guide' && <StreamGuideTab />}
        </div>
        {showPanel && (
          <DistributionPanel mgr={mgr} devices={devices}
            onDistribute={() => setDistributeTarget({})}
            onEdit={(record) => setDistributeTarget({ record })} />
        )}
      </div>
      {distributeTarget && (
        <DistributeDialog mgr={mgr} devices={devices} initial={distributeTarget}
          onClose={() => setDistributeTarget(null)} />
      )}
    </div>
  )
}

export default memo(StreamManagement)
