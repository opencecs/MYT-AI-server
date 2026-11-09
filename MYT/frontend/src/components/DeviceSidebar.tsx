import { useState, useEffect, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Events } from '@wailsio/runtime'
import { ListChecks, Plus, RefreshCw } from 'lucide-react'
import * as DiscoverService from '../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as DeviceDetailService from '../../bindings/github.com/moyunteng/myt/devicedetailservicewrapper'
import { DeviceState } from '../types/index'
import type { BindActions } from '../utils/bindLabel'
import { useDeployMode } from '../hooks/useDeployMode'
import { getDeviceId } from '../utils/deviceId'
import { useDeviceNotes } from '../hooks/useNoteSync'
import { mergeDeviceList } from '../utils/deviceMerge'
import { useDeviceGroups, UNGROUPED_ID } from '../hooks/useDeviceGroups'
import { DeviceItem } from './DeviceItem'
import DeviceGroupTree from '../pages/host/components/DeviceGroupTree'
import CreateGroupModal from '../pages/groupcontrol/CreateGroupModal'
import DeviceNoteModal from './DeviceNoteModal'
import DeviceInfoModal from './DeviceInfoModal'
import ConfirmDialog from './ConfirmDialog'
import { useDeviceOpsMenu } from '../hooks/useDeviceOpsMenu'
import { useDeviceDiscovery } from '../hooks/useDeviceDiscovery'
import AddDeviceDialog from '../pages/host/components/AddDeviceDialog'

interface Props {
  selectedIP: string
  onSelect: (ip: string) => void
  sdkVersions?: Record<string, number>
  /** 标题行「批量操作」图标按钮：打开批量操作弹窗（升级 SDK / 移除设备；不传则不显示） */
  onOpenBatchOps?: () => void
  /** 设备行「+」新建云机（不传则不显示 + 入口；仅主机页传入，另三页共用方零影响） */
  onCreateInstance?: (device: DeviceState) => void
  /** 隐藏搜索框（仅主机页传入，2026-09-25 用户要求；search 恒空 → 过滤直通） */
  hideSearch?: boolean
  /** 「添加设备 / 刷新」按钮（仅主机页传入；样式与云机 tab 的 DeviceList 一致） */
  showDeviceActions?: boolean
  /**
   * 绑定状态徽章的动作包（仅主机 Tab 传入；宿主须 useMemo 稳定引用，否则 DeviceItem
   * memo 全部失效）。不传则设备卡片不显示绑定徽章。
   */
  bindActions?: BindActions
}

function DeviceSidebar({ selectedIP, onSelect, sdkVersions, onOpenBatchOps, onCreateInstance, hideSearch, showDeviceActions, bindActions }: Props) {
  const { t } = useTranslation()
  const { mode, modeLoading } = useDeployMode()
  // idOf 稳定引用（useCallback）：DeviceItem memo 的 prop 之一——每次渲染新建
  // 会让全部卡片 memo 失效（心跳 2 次/秒 × 136 卡片全量重渲染 = 渲染进程打满）。
  const idOf = useCallback((d: DeviceState) => getDeviceId(d, mode) || d.ip, [mode])
  const [devices, setDevices] = useState<DeviceState[]>([])
  const [authTarget, setAuthTarget] = useState<DeviceState | null>(null)
  const [authPwd, setAuthPwd] = useState('')
  const [authLoading, setAuthLoading] = useState(false)
  const [authError, setAuthError] = useState('')
  const [search, setSearch] = useState('')
  const [showCreateGroup, setShowCreateGroup] = useState(false)
  const [showAddDevice, setShowAddDevice] = useState(false)
  // 待添加设备 + 设备发现/刷新（与云机 tab 的 useHostManagement 共用）
  const {
    pendingDevices, scanning, refreshDevices,
    addPendingDevices, addAllPendingDevices, clearPendingDevices,
  } = useDeviceDiscovery()
  // 备注展示（搜索/名称）+ 右键菜单最新值；编辑保存由 DeviceNoteModal 内部完成
  const { notes } = useDeviceNotes()
  // 右键菜单：设置备注 / 设备信息 弹窗目标
  const [noteTarget, setNoteTarget] = useState<DeviceState | null>(null)
  const [infoTarget, setInfoTarget] = useState<DeviceState | null>(null)
  // 删除分组确认弹窗目标（分组名；点垃圾桶先弹确认，确认才删——与云机 tab DeviceList 同构）
  const [deleteGroupTarget, setDeleteGroupTarget] = useState<string | null>(null)
  // 右键菜单：维护操作（重启/魔云互联/镜像清理/日志清理/格式化）
  const ops = useDeviceOpsMenu()
  const {
    groups, expandedGroups, toggleGroupExpand,
    createGroup, deleteGroup, renameGroup,
    addDeviceToGroup, removeDeviceFromGroup, batchMoveDevicesToGroup,
  } = useDeviceGroups()

  useEffect(() => {
    const load = async () => {
      try {
        const list = await DiscoverService.GetDeviceList() as DeviceState[]
        if (Array.isArray(list)) setDevices(list.filter(d => d.state === 0 || d.state === 1 || d.state === 2))
      } catch {}
    }
    load()
    // 引用稳定合并（与 useHostEvents.mergeDeviceData 同款）：事件 payload 是全新
    // JSON 对象，直接 setDevices 会让所有卡片引用全变 → memo 全失效 → 每次心跳
    // 全量重渲染。本侧栏被 maintenance/backup/vpc/diskless 四个常驻页面各挂一份
    // （App 全页面 display:none 不卸载），是 2026-09-18 客户端卡顿的主因。
    const apply = (list: DeviceState[]) => {
      if (!Array.isArray(list)) return
      setDevices(prev => mergeDeviceList(prev, list.filter(d => d.state === 0 || d.state === 1 || d.state === 2)))
    }
    const off = Events.On('devicelist:updated', (ev: any) => {
      apply((ev.data ?? ev) as DeviceState[])
    })
    // 指标心跳（磁盘/内存条刷新）；授权页不订阅本事件——心跳不触发授权重查
    const offStats = Events.On('devicestats:updated', (ev: any) => {
      apply((ev.data ?? ev) as DeviceState[])
    })
    return () => { off(); offStats() }
  }, [])

  const onlineCount = devices.filter(d => d.state === 0).length
  const offlineCount = devices.filter(d => d.state === 1).length
  const authFailCount = devices.filter(d => d.state === 2).length

  // 搜索过滤
  const keyword = search.trim().toLowerCase()
  const matched = keyword
    ? devices.filter(d => {
        const ipMatch = d.ip.toLowerCase().includes(keyword)
        const idMatch = (d.device_id || '').toLowerCase().includes(keyword)
        const noteMatch = (notes[d.device_id] || '').toLowerCase().includes(keyword)
        return ipMatch || idMatch || noteMatch
      })
    : devices

  // 在线在上，离线在下
  const sorted = [...matched].sort((a, b) => {
    if (a.state === 0 && b.state !== 0) return -1
    if (a.state !== 0 && b.state === 0) return 1
    return 0
  })

  // DeviceGroupTree 需要的 selectedDeviceIds（单选模式下用当前选中设备）
  const selectedDeviceIds = useMemo(() => {
    const selected = devices.find(d => idOf(d) === selectedIP)
    return new Set(selected ? [selected.device_id] : [])
  }, [devices, selectedIP, mode])

  // 设备卡片渲染：单击单选（UI「主机」tab 交互语义），勾选批量走 SDK 弹窗
  const renderCard = (device: DeviceState) => (
    <DeviceItem
      device={device}
      selectedIP={selectedIP}
      onSelect={onSelect}
      sdkVersions={sdkVersions}
      notes={notes}
      setAuthTarget={setAuthTarget}
      idOf={idOf}
      t={t}
      onCreateInstance={onCreateInstance}
      bindActions={bindActions}
    />
  )

  const handleVerifyAuth = async () => {
    if (!authTarget || !authPwd.trim()) return
    setAuthLoading(true); setAuthError('')
    try {
      const ok = await DeviceDetailService.VerifyDeviceAuth(authTarget.device_id, authTarget.ip, authPwd.trim())
      if (ok) { setAuthTarget(null); setAuthPwd(''); setAuthError('') }
      else { setAuthError(t('auth.authFailed')) }
    } catch { setAuthError(t('auth.requestFailed')) }
    finally { setAuthLoading(false) }
  }

  return (
    // w-64（256px）：标题行「设备列表 + 批量 + 刷新 + 分组」需 ~206px，w-60（216px 内宽）
    // 中文勉强放下但 en locale 临界会换行，加宽留余量；主区 flex-1 弹性自适应。
    <div className="w-64 bg-white border-r border-gray-200 flex flex-col shrink-0">
      <div className="p-3 border-b border-gray-100">
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm font-medium text-gray-700">{t('deviceList.title')}</span>
          <div className="flex items-center gap-1">
            {onOpenBatchOps && (
              <button
                onClick={onOpenBatchOps}
                className="px-2 py-1.5 gap-1 text-xs text-gray-500 hover:text-gray-700 hover:bg-gray-100 rounded transition-colors flex items-center"
                title={t('maintenance:batchOps.button')}
                aria-label={t('maintenance:batchOps.button')}
              >
                <ListChecks size={14} />
                {t('maintenance:batchOps.batch')}
              </button>
            )}
            {showDeviceActions && (
              <button
                onClick={refreshDevices}
                disabled={scanning}
                className="p-1.5 text-gray-500 hover:text-gray-700 hover:bg-gray-100 rounded transition-colors disabled:opacity-50 flex items-center"
                title={t('button.refresh')}
                aria-label={t('button.refresh')}
              >
                <RefreshCw size={14} className={scanning ? 'animate-spin' : ''} />
              </button>
            )}
            <button
              onClick={() => setShowCreateGroup(true)}
              className="px-2 py-1.5 gap-1 text-xs text-gray-500 hover:text-gray-700 hover:bg-gray-100 rounded transition-colors flex items-center"
              title={t('groupcontrol:newGroup')}
              aria-label={t('groupcontrol:newGroup')}
            >
              <Plus size={14} />
              {t('groupcontrol:newGroupShort')}
            </button>
          </div>
        </div>
        {showDeviceActions && mode !== 'public' && (
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
        <span className="text-gray-400">{t('deviceList.total')} {devices.length}</span>
      </div>
      {!hideSearch && (
        <div className="px-3 py-2 border-b border-gray-100">
          <div className="relative">
            <svg className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <input type="text" value={search} onChange={e => setSearch(e.target.value)} placeholder={t('deviceList.searchPlaceholder')}
              aria-label={t('deviceList.searchPlaceholder')}
              className="w-full text-xs pl-7 pr-2 py-1.5 bg-gray-50 border border-gray-200 rounded-md focus:outline-none focus:border-blue-400 placeholder:text-gray-300" />
          </div>
        </div>
      )}
      <div className="flex-1 overflow-y-auto overflow-x-hidden scrollbar-hide">
        {modeLoading ? (
          // 切模式后到收到新模式首个 devicelist:updated 之前：显示加载中，
          // 不渲染上一模式的残留列表（与云机 tab DeviceList 同款）
          <div className="h-full flex flex-col items-center justify-center gap-2">
            <div className="w-8 h-8 border-4 border-blue-100 border-t-blue-500 rounded-full animate-spin"></div>
            <div className="text-xs text-gray-400">{t('deployMode.loadingDevices')}</div>
          </div>
        ) : (
        <DeviceGroupTree
          groups={groups}
          expandedGroups={expandedGroups}
          devices={sorted}
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
          onSdkUpgrade={ops.onSdkUpgrade}
          sdkUpgradeV2={ops.sdkUpgradeV2}
          onTeboxInstall={ops.onTeboxInstall}
          notes={notes}
          renderCard={renderCard}
          />
        )}
      </div>
      {authFailCount > 0 && (
        <div className="px-3 py-2 border-t border-gray-100 flex items-center gap-1.5 shrink-0">
          <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
          <span className="text-[11px] text-amber-600">{t('deviceList.authFailCount', { count: authFailCount })}</span>
        </div>
      )}
      {authTarget && (
        <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50">
          <div className="bg-white rounded-2xl shadow-xl p-6 w-[320px] text-center" onClick={e => e.stopPropagation()}>
            <div className="w-12 h-12 rounded-full bg-gradient-to-br from-amber-400 to-orange-500 flex items-center justify-center mx-auto mb-4 shadow-lg shadow-amber-200/50">
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
            </div>
            <div className="flex items-center justify-between mb-1">
              <div className="text-base font-semibold text-gray-700">{t('auth.title')}</div>
              <button onClick={() => setAuthTarget(null)} aria-label={t('button.close')} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            </div>
            <div className="text-sm font-medium text-gray-800 bg-gray-50 rounded-lg px-3 py-2 mb-3">{authTarget.ip || idOf(authTarget)}</div>
            {authError && <div className="text-[11px] text-red-500 bg-red-50 rounded-lg px-3 py-2 mb-3">{authError}</div>}
            <input type="password" value={authPwd} onChange={e => setAuthPwd(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') handleVerifyAuth() }} placeholder={t('auth.enterPassword')} autoFocus
              className="w-full bg-gray-50 text-sm px-3 py-2 rounded-lg border border-gray-200 focus:border-amber-400 focus:outline-none mb-4" />
            <div className="flex gap-3">
              <button onClick={() => { setAuthTarget(null); setAuthPwd(''); setAuthError('') }} disabled={authLoading}
                className="flex-1 px-4 py-2 text-sm bg-gray-100 text-gray-600 rounded-lg hover:bg-gray-200 transition-colors disabled:opacity-50">{t('button.cancel')}</button>
              <button onClick={handleVerifyAuth} disabled={authLoading || !authPwd.trim()}
                className="flex-1 px-4 py-2 text-sm bg-gradient-to-r from-amber-500 to-orange-500 text-white rounded-lg hover:from-amber-600 hover:to-orange-600 transition-all shadow-md shadow-amber-200/50 disabled:opacity-50">{authLoading ? t('status.verifying') : t('button.confirm')}</button>
            </div>
          </div>
        </div>
      )}

      <CreateGroupModal
        visible={showCreateGroup}
        onClose={() => setShowCreateGroup(false)}
        onSubmit={async (name: string) => { await createGroup(name); setShowCreateGroup(false) }}
      />

      <AddDeviceDialog
        visible={showAddDevice}
        onClose={() => setShowAddDevice(false)}
        pendingDevices={pendingDevices}
        onAddPendingDevices={addPendingDevices}
        onAddAllPendingDevices={addAllPendingDevices}
        onClearPendingDevices={clearPendingDevices}
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
          deviceIP={getDeviceId(infoTarget, mode)}
          onClose={() => setInfoTarget(null)}
        />
      )}

      {ops.dialogs}

      {deleteGroupTarget && (
        <ConfirmDialog
          title={t('host:deviceList.confirmDeleteGroupTitle')}
          message={t('host:deviceList.confirmDeleteGroupMessage')}
          confirmLabel={t('button.delete')}
          onConfirm={() => { deleteGroup(deleteGroupTarget); setDeleteGroupTarget(null) }}
          onCancel={() => setDeleteGroupTarget(null)}
        />
      )}
    </div>
  )
}

export default DeviceSidebar