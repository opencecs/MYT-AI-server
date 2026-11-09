import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, RefreshCw } from 'lucide-react'
import * as PaymentService from '../../../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import { DeviceState, ViewMode, AndroidInfo } from '../../../types/index'
import ConfirmDialog from '../../../components/ConfirmDialog'
import DeviceNoteModal from '../../../components/DeviceNoteModal'
import DeviceInfoModal from '../../../components/DeviceInfoModal'
import { useDeviceOpsMenu } from '../../../hooks/useDeviceOpsMenu'
import { useAuth } from '../../../hooks/useAuth'
import { useNavigation } from '../../../hooks/useNavigation'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { useDeviceNotes } from '../../../hooks/useNoteSync'
import { useDeviceGroups, UNGROUPED_ID } from '../../../hooks/useDeviceGroups'
import { useShowOfflineAndroids } from '../../../hooks/useShowOfflineAndroids'
import { getDeviceId } from '../../../utils/deviceId'
import DeviceListItem from './DeviceListItem'
import DeviceGroupTree from './DeviceGroupTree'
import CreateGroupModal from '../../groupcontrol/CreateGroupModal'
import AuthDialog from './AuthDialog'
import BindDialog from './BindDialog'
import UnbindDialog from './UnbindDialog'
import AddDeviceDialog from './AddDeviceDialog'
interface Props {
  devices: DeviceState[]
  selectedDevice: DeviceState | null
  selectedDeviceIds: Set<string>
  viewMode: ViewMode
  scanning: boolean
  pendingDevices: DeviceState[]
  lowSdkIds?: Set<string>
  onSelect: (device: DeviceState) => void
  onToggleSelect: (deviceId: string) => void
  onSelectAll: () => void
  onDeselectAll: () => void
  onDelete: (device: DeviceState) => void
  onDeleteDevices: (deviceIDs: string[]) => void
  onAddPendingDevices: (deviceIDs: string[]) => void
  onAddAllPendingDevices: () => void
  onClearPendingDevices: () => void
  onCreateInstance: (device: DeviceState) => void
  onRefresh: () => void
  /** 设备 ID → 该设备下的云机列表（侧栏设备行展开容器子列表用） */
  androidGroups: Record<string, AndroidInfo[]>
  /** 侧栏展开的设备 ID 集合 */
  expandedDeviceIds: Set<string>
  onToggleDeviceExpand: (deviceId: string) => void
  /** 容器勾选切换（侧栏过滤器）：写入 sidebarAndroidIds + 同步批量勾选；联动设备勾选在行组件侧 */
  onToggleAndroidSelect: (androidId: string) => void
  /** 设备已勾选时点容器行 → 从该设备全选中排除此容器（摘除设备勾选 + 清容器级勾选） */
  onExcludeAndroidFromDevice: (androidId: string, deviceId: string) => void
  /** 侧栏过滤器勾选集合（决定右侧展示范围；与右侧批量勾选集合分离） */
  selectedAndroidIds: Set<string>
  /** 分组全选（三态 checkbox）：select=true 勾选该组全部（过滤可见）设备；不传则不渲染 */
  onSelectGroupDevices?: (deviceIds: string[], select: boolean) => void
  /** 单选模式（ROM 等单主机页面）：点行只选中高亮，不进多选/全选/批量删除链路。 */
  singleSelect?: boolean
}

function DeviceList({
  devices, selectedDevice, selectedDeviceIds, viewMode,
  scanning, pendingDevices, lowSdkIds, onSelect, onToggleSelect, onSelectAll, onDeselectAll,
  onDelete, onDeleteDevices, onAddPendingDevices, onAddAllPendingDevices, onClearPendingDevices,
  onCreateInstance, onRefresh,
  androidGroups, expandedDeviceIds, onToggleDeviceExpand, onToggleAndroidSelect,
  onExcludeAndroidFromDevice,
  selectedAndroidIds,
  onSelectGroupDevices,
  singleSelect = false,
}: Props) {
  const { t } = useTranslation()
  const [deleteTarget, setDeleteTarget] = useState<DeviceState | null>(null)
  const [deleteGroupTarget, setDeleteGroupTarget] = useState<string | null>(null)
  const [offlineDeleteConfirm, setOfflineDeleteConfirm] = useState(false)
  const [authTarget, setAuthTarget] = useState<DeviceState | null>(null)
  const [bindTarget, setBindTarget] = useState<DeviceState | null>(null)
  const [unbindTargets, setUnbindTargets] = useState<DeviceState[]>([])
  const [showAddDevice, setShowAddDevice] = useState(false)
  const [showCreateGroup, setShowCreateGroup] = useState(false)
  const [username, setUsername] = useState('')
  // 备注展示（搜索/名称）+ 右键菜单最新值；编辑保存由 DeviceNoteModal 内部完成
  const { notes } = useDeviceNotes()
  const [search, setSearch] = useState('')
  const { loggedIn } = useAuth()
  const { mode: deployMode, modeLoading } = useDeployMode()
  // 「云机展示=全部」设置（模块级 store 共享，与 useHostManagement 同源）：透传给
  // DeviceListItem 的展开区容器列表与三态勾选口径
  const [showOffline] = useShowOfflineAndroids()
  const navigate = useNavigation()
  // 右键菜单：设置备注 / 设备信息 弹窗目标
  const [noteTarget, setNoteTarget] = useState<DeviceState | null>(null)
  const [infoTarget, setInfoTarget] = useState<DeviceState | null>(null)
  // 右键菜单：维护操作（重启/魔云互联/镜像清理/日志清理/格式化）
  const ops = useDeviceOpsMenu()

  const {
    groups, expandedGroups,
    createGroup, deleteGroup, renameGroup,
    addDeviceToGroup, removeDeviceFromGroup, batchMoveDevicesToGroup,
    toggleGroupExpand,
  } = useDeviceGroups()

  // 两种模式都多选（2026-09-22）：截图模式按勾选显示卡片，坑位模式按勾选设备分区渲染
  // （MultiDeviceSlotView）。此前坑位模式左侧没有复选框，用户得先切到截图模式勾选再切回坑位模式，
  // 与右侧「按勾选设备分区」的交互割裂。
  const isMultiSelect = !singleSelect && (viewMode === 'screenshot' || viewMode === 'slot')

  useEffect(() => {
    if (loggedIn) {
      PaymentService.GetUsername().then((u: string) => setUsername(u)).catch(() => {})
    } else {
      setUsername('')
    }
  }, [loggedIn])

  // 在线含「认证失败」(state===2)：设备可达，仅待认证；离线仍为纯离线，与「删除所有离线设备」(删 state===1) 自洽
  const onlineCount = devices.filter(d => d.state === 0 || d.state === 2).length
  const offlineCount = devices.filter(d => d.state === 1).length
  const authFailCount = devices.filter(d => d.state === 2).length
  const allDevicesSelected = devices.length > 0 && devices.every(d => selectedDeviceIds.has(d.device_id))

  const keyword = search.trim().toLowerCase()
  const matched = keyword
    ? devices.filter(d => d.ip.toLowerCase().includes(keyword) || d.device_id.toLowerCase().includes(keyword) || (notes[d.device_id] || '').toLowerCase().includes(keyword))
    : devices
  // 在线在上，离线在下
  const filteredDevices = [...matched].sort((a, b) => {
    if (a.state === 0 && b.state !== 0) return -1
    if (a.state !== 0 && b.state === 0) return 1
    return 0
  })

  const handleCreateGroup = async (name: string) => {
    await createGroup(name)
    setShowCreateGroup(false)
  }

  // renderCard 的回调全部 useCallback 稳定引用：DeviceListItem memo 生效的前提。
  // 几百台设备时，内联箭头函数会让每次列表刷新都重渲染全部行（2026-09-05）。
  // 绑定徽章随 2026-09-27 设备行单行化移除后，设备行的 onBind/onUnbind 通道已删；
  // 主机 Tab 的绑定徽章现由 MaintenancePage 侧栏设备卡片（DeviceItem）渲染（utils/bindLabel.ts）。
  const handleNavigateToPayment = useCallback(() => navigate('payment'), [navigate])

  const renderCard = useCallback((device: DeviceState) => (
    <DeviceListItem
      device={device}
      isChecked={selectedDeviceIds.has(device.device_id)}
      isSingleSelected={selectedDevice?.device_id === device.device_id}
      isMultiSelect={isMultiSelect}
      lowSdk={lowSdkIds?.has(device.device_id) ?? false}
      note={notes[device.device_id]}
      androids={androidGroups[device.device_id]}
      isExpanded={expandedDeviceIds.has(device.device_id)}
      onToggleExpand={onToggleDeviceExpand}
      checkedAndroidIds={selectedAndroidIds}
      onToggleAndroidSelect={onToggleAndroidSelect}
      onExcludeAndroidFromDevice={onExcludeAndroidFromDevice}
      onSelect={onSelect}
      onToggleSelect={onToggleSelect}
      onCreateInstance={onCreateInstance}
      onAuth={setAuthTarget}
      onDelete={setDeleteTarget}
      onNavigateToPayment={handleNavigateToPayment}
      loggedIn={loggedIn}
      showOffline={showOffline}
    />
  ), [selectedDeviceIds, selectedDevice, isMultiSelect, lowSdkIds, notes, androidGroups,
      expandedDeviceIds, onToggleDeviceExpand, onToggleAndroidSelect, onExcludeAndroidFromDevice,
      selectedAndroidIds, singleSelect,
      onSelect, onToggleSelect, onCreateInstance,
      loggedIn, handleNavigateToPayment, showOffline])

  return (
    // w-72（288px）：行内常驻元素多（勾选框 14 + 箭头 16 + 徽标 20 + 容器数 ~24 +
    // + 按钮 20 + 告警⚠ 13 + gap-2×6 ≈ 175px），w-60/w-64 时 IP（~88px）必被
    // truncate；加宽后名称/IP 区约 113px。主区 flex-1 弹性自适应，无碍。
    <div className="w-72 bg-white border-r border-gray-200 flex flex-col shrink-0">
      <div className="px-3 pt-3 pb-2 border-b border-gray-100">
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm font-medium text-gray-700">{t('deviceList.title')}</span>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setShowCreateGroup(true)}
              className="px-2 py-1.5 gap-1 text-xs text-gray-500 hover:text-gray-700 hover:bg-gray-100 rounded transition-colors flex items-center"
              title={t('groupcontrol:newGroup')}
              aria-label={t('groupcontrol:newGroup')}
            >
              <Plus size={14} />
              {t('groupcontrol:newGroupShort')}
            </button>
            <button
              onClick={onRefresh}
              disabled={scanning}
              className="p-1.5 text-gray-500 hover:text-gray-700 hover:bg-gray-100 rounded transition-colors disabled:opacity-50 flex items-center"
              title={t('button.refresh')}
            >
              <RefreshCw size={14} className={scanning ? 'animate-spin' : ''} />
            </button>
          </div>
        </div>
        {deployMode !== 'public' && (
          <button
            onClick={() => setShowAddDevice(true)}
            className="w-full text-xs px-2.5 py-1.5 bg-blue-500 text-white rounded-lg hover:bg-blue-600 transition-colors flex items-center justify-center gap-1"
          >
            <Plus size={13} />
            {t('host:deviceList.addDevice')}
          </button>
        )}
      </div>

      <div className="px-3 py-2 flex items-center gap-3 border-b border-gray-100 text-[11px]">
        <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-green-500" />{t('status.online')} {onlineCount}</span>
        <span className="flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-red-400" />{t('status.offline')} {offlineCount}</span>
        <span className="text-gray-400">{t('host:deviceList.total')} {devices.length}</span>
        {offlineCount > 0 && (
          <button
            onClick={() => setOfflineDeleteConfirm(true)}
            className="ml-auto text-red-400 hover:text-red-500"
          >
            {t('host:deviceList.deleteOffline')}
          </button>
        )}
      </div>

      <div className="px-3 py-2 border-b border-gray-100">
        <div className="relative">
          <svg className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
          <input
            type="text"
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder={t('host:deviceList.searchPlaceholder')}
            className="w-full text-xs pl-7 pr-2 py-1.5 bg-gray-50 border border-gray-200 rounded-md focus:outline-none focus:border-blue-400 placeholder:text-gray-300"
          />
        </div>
      </div>

      <div className="flex-1 overflow-y-auto overflow-x-hidden scrollbar-hide">
        {modeLoading ? (
          // 切模式后到收到新模式首个 devicelist:updated 之前：显示加载中，
          // 不渲染上一模式的残留列表（设备列表随模式隔离，事件到达前旧数据无意义）
          <div className="h-full flex flex-col items-center justify-center gap-2">
            <div className="w-8 h-8 border-4 border-blue-100 border-t-blue-500 rounded-full animate-spin"></div>
            <div className="text-xs text-gray-400">{t('deployMode.loadingDevices')}</div>
          </div>
        ) : (
          <DeviceGroupTree
          groups={groups}
          expandedGroups={expandedGroups}
          devices={filteredDevices}
          selectedDeviceIds={selectedDeviceIds}
          ungroupedId={UNGROUPED_ID}
          onToggleExpand={toggleGroupExpand}
          onRenameGroup={renameGroup}
          onDeleteGroup={setDeleteGroupTarget}
          onMoveDevices={batchMoveDevicesToGroup}
          onAddToGroup={addDeviceToGroup}
          onRemoveFromGroup={removeDeviceFromGroup}
          onSetNote={setNoteTarget}
          onDeviceInfo={setInfoTarget}
          onRebootDevice={ops.onRebootDevice}
          onMytPanel={ops.onMytPanel}
          onTunnel={ops.onTunnel}
          onDockerCleanup={ops.onDockerCleanup}
          onLogCleanup={ops.onLogCleanup}
          onFormatDisk={ops.onFormatDisk}
          onTeboxInstall={ops.onTeboxInstall}
          notes={notes}
          onSelectGroupDevices={onSelectGroupDevices}
          renderCard={renderCard}
          />
        )}
      </div>

      <div className="px-2 py-2 border-t border-gray-100 flex items-center justify-between text-[10px] text-gray-400">
        {isMultiSelect && devices.length > 0 ? (
          <div className="flex items-center gap-1.5 shrink-0">
            <span>{t('host:deviceList.selectedCount', { count: selectedDeviceIds.size })}</span>
            <button
              onClick={allDevicesSelected ? onDeselectAll : onSelectAll}
              className="text-blue-500 hover:text-blue-600"
            >
              {allDevicesSelected ? t('button.deselectAll') : t('button.selectAll')}
            </button>
          </div>
        ) : <span />}
        <div className="flex items-center gap-2">
          {authFailCount > 0 && (
            <span className="flex items-center gap-1 text-amber-600">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
              {t('deviceList.authFailCount', { count: authFailCount })}
            </span>
          )}
        </div>
      </div>

      {deleteTarget && (
        <ConfirmDialog
          title={t('host:deviceList.confirmDeleteTitle')}
          message={t('host:deviceList.confirmDeleteMessage', { ip: deleteTarget.ip || deleteTarget.device_id })}
          confirmLabel={t('button.delete')}
          onConfirm={() => { onDelete(deleteTarget); setDeleteTarget(null) }}
          onCancel={() => setDeleteTarget(null)}
        />
      )}

      {deleteGroupTarget && (
        <ConfirmDialog
          title={t('host:deviceList.confirmDeleteGroupTitle')}
          message={t('host:deviceList.confirmDeleteGroupMessage')}
          confirmLabel={t('button.delete')}
          onConfirm={() => { deleteGroup(deleteGroupTarget); setDeleteGroupTarget(null) }}
          onCancel={() => setDeleteGroupTarget(null)}
        />
      )}

      {offlineDeleteConfirm && (
        <ConfirmDialog
          title={t('host:deviceList.deleteAllOfflineTitle')}
          message={t('host:deviceList.deleteAllOfflineMessage', { count: offlineCount })}
          confirmLabel={t('button.delete')}
          onConfirm={() => {
            const offlineIds = devices.filter(d => d.state === 1).map(d => d.device_id)
            onDeleteDevices(offlineIds)
            setOfflineDeleteConfirm(false)
          }}
          onCancel={() => setOfflineDeleteConfirm(false)}
        />
      )}

      <AuthDialog
        device={authTarget}
        onClose={() => setAuthTarget(null)}
      />

      <BindDialog
        device={bindTarget}
        username={username}
        onClose={() => setBindTarget(null)}
      />

      <UnbindDialog
        devices={unbindTargets}
        username={username}
        onClose={() => setUnbindTargets([])}
      />

      <AddDeviceDialog
        visible={showAddDevice}
        onClose={() => setShowAddDevice(false)}
        pendingDevices={pendingDevices}
        onAddPendingDevices={onAddPendingDevices}
        onAddAllPendingDevices={onAddAllPendingDevices}
        onClearPendingDevices={onClearPendingDevices}
      />

      <CreateGroupModal
        visible={showCreateGroup}
        onClose={() => setShowCreateGroup(false)}
        onSubmit={handleCreateGroup}
      />

      {noteTarget && (
        <DeviceNoteModal
          device={noteTarget}
          note={notes[noteTarget.device_id]}
          onClose={() => setNoteTarget(null)}
        />
      )}

      {infoTarget && (
        <DeviceInfoModal
          deviceIP={getDeviceId(infoTarget, deployMode)}
          onClose={() => setInfoTarget(null)}
        />
      )}

      {ops.dialogs}
    </div>
  )
}

export default DeviceList
