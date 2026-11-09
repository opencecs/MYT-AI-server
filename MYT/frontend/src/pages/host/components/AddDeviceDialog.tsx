import { useState, useRef, useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useModalEnter } from '../../../hooks/useModalAnimation'
import { useDeployMode } from '../../../hooks/useDeployMode'
import * as V2Service from '../../../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import * as V4Service from '../../../../bindings/github.com/moyunteng/myt/v4servicewrapper'
import type { DeviceState } from '../../../types/index'
import AddDeviceScanTab from './addDevice/AddDeviceScanTab'
import AddDeviceManualTab from './addDevice/AddDeviceManualTab'
import AddDeviceV2Tab from './addDevice/AddDeviceV2Tab'

interface AddDeviceDialogProps {
  visible: boolean
  onClose: () => void
  pendingDevices: DeviceState[]
  onAddPendingDevices: (deviceIDs: string[]) => void
  onAddAllPendingDevices: () => void
  onClearPendingDevices: () => void
}

type Tab = 'manual' | 'auto'

/**
 * 添加设备弹窗壳：头部 + 分段 Tab + 两个子 Tab。
 * 两个 Tab 常驻挂载（hidden 切换），切换不丢扫描进度 / 已输入的 IP 芯片。
 * 信息架构与旧版一致：自动发现（扫描 → 待添加列表）、手动添加（IP 芯片输入）。
 * v2 模式：同样两 Tab，但扫描走 V2 网段 2375 探测、手动添加走 V2 主机探测，
 * 语义与本地一致（扫描 → 待添加 → 确认入列）。
 * v4 模式：扫描走 V4 专属发现（UDP 7678 广播 + 8088 /health 验证），手动添加走
 * 8088 /health 定向探测；两个 Tab 与 local 同构，语义一致（扫描 → 待添加 → 确认入列）。
 */
function AddDeviceDialog({
  visible, onClose,
  pendingDevices, onAddPendingDevices, onAddAllPendingDevices, onClearPendingDevices,
}: AddDeviceDialogProps) {
  const { t } = useTranslation()
  const { mode: deployMode } = useDeployMode()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [tab, setTab] = useState<Tab>('auto')

  const goManual = useCallback(() => setTab('manual'), [])

  // 打开即清空待添加列表（用户口径：每次打开全新开始，不留缓存）。
  // 后端清空后会 emit pendinglist:updated，pendingDevices 随事件归零，
  // 结果区随之收起；扫描产生的设备由扫描重新写入。
  // 同时重置 Tab：本组件常驻挂载（靠 visible 控制显隐），useState 初值只在首次
  // 挂载生效，所以每次「打开」都落回「自动发现」（local/public/v2/v4 四种模式
  // 都具备扫描能力）。打开期间用户自己切 Tab 不受影响（effect 只在 visible 变化时跑）。
  useEffect(() => {
    if (visible) {
      onClearPendingDevices()
      setTab('auto')
    }
  }, [visible, onClearPendingDevices])

  if (!visible) return null

  const isV2 = deployMode === 'v2'
  const isV4 = deployMode === 'v4'
  // 扫描入口：v2 = 81 dc_api 网段扫描，v4 = 7678 广播 + 8088 /health，其余（local/public）
  // 走 AddDeviceScanTab 内默认的本地 UDP Discover
  const scanEntry = isV2 ? () => V2Service.ScanV2Hosts() : isV4 ? () => V4Service.ScanV4Hosts() : undefined

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div
        className="bg-white rounded-xl shadow-xl w-[520px] max-w-[calc(100vw-32px)] modal-content"
        onClick={e => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="flex items-center gap-3 px-5 pt-4 pb-3.5">
          <div className="w-8.5 h-8.5 rounded-lg bg-blue-50 text-blue-600 flex items-center justify-center shrink-0">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
              <rect x="2" y="3" width="20" height="14" rx="2" /><line x1="8" y1="21" x2="16" y2="21" /><line x1="12" y1="17" x2="12" y2="21" />
            </svg>
          </div>
          <div className="flex-1 min-w-0">
            <div className="text-[15px] font-semibold text-gray-800">{t('host:addDevice.title')}</div>
            <div className="text-xs text-gray-400 mt-0.5">
              {isV2 ? t('host:addDevice.v2.subtitle') : isV4 ? t('host:addDevice.v4.subtitle') : t('host:addDevice.subtitle')}
            </div>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 hover:bg-gray-50 rounded-lg w-7 h-7 flex items-center justify-center transition-colors">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        {/* 分段 Tab（滑动指示器） */}
        <div className="px-5">
          <div className="add-seg" data-tab={tab}>
            <div className="add-seg__ind" />
            <button className={tab === 'auto' ? 'add-seg__btn active' : 'add-seg__btn'} onClick={() => setTab('auto')}>
              {t('host:addDevice.autoDiscover')}
            </button>
            <button className={tab === 'manual' ? 'add-seg__btn active' : 'add-seg__btn'} onClick={() => setTab('manual')}>
              {t('host:addDevice.manualAdd')}
            </button>
          </div>
        </div>

        <div className="p-5">
          <div hidden={tab !== 'auto'}>
            <AddDeviceScanTab
              pendingDevices={pendingDevices}
              onAddPendingDevices={onAddPendingDevices}
              onAddAllPendingDevices={onAddAllPendingDevices}
              onClearPendingDevices={onClearPendingDevices}
              onClose={onClose}
              onGoManual={goManual}
              onScan={scanEntry}
              showSdkBadge={isV2}
              zoneHint={
                isV2 ? t('host:addDevice.v2.scanZoneHint')
                  : isV4 ? t('host:addDevice.v4.scanZoneHint')
                    : undefined
              }
              noneFoundHint={
                isV2 ? t('host:addDevice.v2.noneFoundHint')
                  : isV4 ? t('host:addDevice.v4.noneFoundHint')
                    : undefined
              }
            />
          </div>
          <div hidden={tab !== 'manual'}>
            {isV2 ? <AddDeviceV2Tab onClose={onClose} /> : <AddDeviceManualTab onClose={onClose} />}
          </div>
        </div>
      </div>
    </div>
  )
}

export default AddDeviceDialog
