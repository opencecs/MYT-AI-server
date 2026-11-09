import { useState, useEffect, useMemo, useRef, memo, useCallback } from 'react'
import { Events } from '@wailsio/runtime'
import { useTranslation } from 'react-i18next'
import { ClipboardList, Eraser } from 'lucide-react'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as MirrorService from '../../../bindings/github.com/moyunteng/myt/mirrorservicewrapper'
import * as SlotService from '../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import * as ScreenshotService from '../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import { DeviceState, AndroidInfo, MirrorItem, SlotExpiry, SlotInfo, Orientation, ViewMode } from '../../types/index'
import { jsonEqual, toSlotExpiryList } from '../host/hostManagementUtils'
import DeviceSidebar from '../../components/DeviceSidebar'
import { useDeployMode } from '../../hooks/useDeployMode'
import { useCardScale } from '../../hooks/useCardScale'
import { useShowOfflineAndroids } from '../../hooks/useShowOfflineAndroids'
import { useSdkVersions } from '../../hooks/useSdkVersions'
import { useSingleCastScale } from '../../hooks/useSingleCastScale'
import { useDeviceAndroids } from '../../hooks/useDeviceAndroids'
import { getDeviceId } from '../../utils/deviceId'
import { isStatusVisible } from '../../utils/containerStatus'
import SlotView from '../host/components/SlotView'
import ScreenshotView from '../host/components/ScreenshotView'
import ActionBar from '../host/components/ActionBar'
import CreateInstanceModal from '../host/create/CreateInstanceModal'
import { DeviceInfoBar } from './DeviceInfoBar'
import { MaintenanceModal } from './MaintenanceModal'
import BatchOpsModal from './BatchOpsModal'
import BindDialog from '../host/components/BindDialog'
import UnbindDialog from '../host/components/UnbindDialog'
import { useAuth } from '../../hooks/useAuth'
import { useNavigation, useActiveTab } from '../../hooks/useNavigation'
import type { BindActions } from '../../utils/bindLabel'
import type { ModalType } from './maintenanceTypes'

/**
 * 维护页（UI「主机」tab）：默认截图模式（单设备卡片网格），可切到坑位模式看全量坑位表。
 * 两种模式共用 ActionBar 批量工具条（设备 API/固件版本/SDK 升级/密码操作并入工具条第一行左侧 infoSlot）。
 * 设备级维护操作（信息/重启/魔云互联/镜像清理/日志清理/格式化）在设备列表右键菜单；
 * 批量操作（升级 SDK / 移除设备）走侧栏标题行「批量操作」按钮弹窗。云机级日志/缓存清理经行内按钮注入坑位表（仅运行中实例）。
 */
function MaintenancePage() {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [devices, setDevices] = useState<DeviceState[]>([])
  const { sdkVersions } = useSdkVersions(devices)
  const [selectedIP, setSelectedIP] = useState('')
  const selectedIPRef = useRef(selectedIP)
  selectedIPRef.current = selectedIP
  // 批量操作弹窗（设备列表侧栏标题行「批量操作」按钮打开）
  const [showBatchOps, setShowBatchOps] = useState(false)
  const [singleCastScale, setSingleCastScale] = useSingleCastScale() // 单台投屏窗口大小（localStorage + Go config 落档）

  // 云机清理弹窗：log（android 模式）/ cache
  const [modalType, setModalType] = useState<ModalType>(null)
  const [modalAndroid, setModalAndroid] = useState('')

  // 绑定/解绑弹窗目标（设备卡片徽章点击）：绑定后 Go 侧回写 bind_status 缓存但不广播
  // devicelist:updated，故成功后显式重拉一次设备列表刷新徽章文案。
  const [bindTarget, setBindTarget] = useState<DeviceState | null>(null)
  const [unbindTargets, setUnbindTargets] = useState<DeviceState[]>([])
  const { loggedIn, username } = useAuth()
  const navigate = useNavigation()

  /** 拉取设备列表并落 state（首屏加载与绑定/解绑后刷新共用同一口径）。 */
  const reloadDevices = useCallback(async () => {
    try {
      const list = await DiscoverService.GetDeviceList() as DeviceState[]
      // 口径与侧栏 DeviceSidebar 一致：在线/离线/待认证都进列表
      // （此前只留 0/1，新添加的「待认证」设备进不了页面级 devices →
      // 侧栏看得见、选中它时右侧却显示「请选择设备」，2026-09-27 统一）
      const visible = list.filter(d => d.state === 0 || d.state === 1 || d.state === 2)
      setDevices(visible)
      if (visible.length > 0 && !selectedIPRef.current) setSelectedIP(getDeviceId(visible[0], mode))
      if (selectedIPRef.current && !visible.find(d => getDeviceId(d, mode) === selectedIPRef.current) && visible.length > 0) setSelectedIP(getDeviceId(visible[0], mode))
    } catch {}
  }, [mode])

  useEffect(() => {
    reloadDevices()
    const off = Events.On('devicelist:updated', (ev: any) => {
      const list = ev.data ?? ev
      if (Array.isArray(list)) setDevices(list.filter((d: DeviceState) => d.state === 0 || d.state === 1 || d.state === 2))
    })
    return () => { off() }
  }, [reloadDevices])

  const selectedDevice = useMemo(() => devices.find(d => getDeviceId(d, mode) === selectedIP), [devices, selectedIP])

  // 把当前设备同步给截图服务（控制后台截图轮询范围）：主机 tab 是单选设备，
  // 截图模式要抓的就是这台设备的坑位——不告诉截图服务，截图卡片永远出不来
  // （云机 tab 由 useHostEvents 做同一件事；这里主机 tab 没有那个 hook，需自带）。
  // 仅在主机 tab 可见时写入：全局截图集合是云机 tab 与本页共享的，本页 keep-alive
  // 失活后若仍无条件重发，会覆盖云机 tab 的勾选集合（跨页面污染，2026-10-09 修复）。
  // 失活时不主动交还集合（本页无从得知云机 tab 的当前勾选），由云机 tab 激活时
  // useHostEvents 的「回到 host 无条件重发」兜底恢复。
  const activeTab = useActiveTab()
  useEffect(() => {
    if (activeTab !== 'maintenance') return
    if (!selectedDevice) return
    ScreenshotService.SetSelectedDevices([selectedDevice.device_id]).catch(() => {})
  }, [activeTab, selectedDevice?.device_id])

  // 坑位表数据：云机列表（全部坑位含已关机，事件驱动刷新）
  const androids = useDeviceAndroids(selectedDevice?.device_id ?? '')

  // 镜像列表：读 Go 缓存（StartAutoRefresh 每 10 分钟后台维护，零外网请求），
  // 缓存为空时兜底拉一次（启动竞态：云机 tab 的 FetchMirrors 可能尚未返回）
  const [mirrors, setMirrors] = useState<MirrorItem[]>([])
  useEffect(() => {
    MirrorService.GetCachedMirrors().then((m: MirrorItem[]) => {
      if (Array.isArray(m) && m.length > 0) setMirrors(m)
      else MirrorService.FetchMirrors().then((r) => setMirrors(r ?? [])).catch(() => {})
    }).catch(() => {})
  }, [])

  // 坑位授权状态：与云机 tab 坑位模式同款数据源（useHostEvents）——
  // 1) 选中设备时 QuerySlotInfo 主动查一次（实时同步，Go 侧结果同时落缓存）
  // 2) 3s 轮询 GetSlotExpiry 缓存（本地读零外网），合入其他页面/后台 30 分钟兜底任务的更新
  const [slotExpiry, setSlotExpiry] = useState<SlotExpiry[]>([])
  useEffect(() => {
    if (!selectedDevice) { setSlotExpiry([]); return }
    const deviceId = selectedDevice.device_id
    // 切换设备先清空：避免上个设备的授权状态错挂到新设备的坑位头
    setSlotExpiry([])
    SlotService.QuerySlotInfo([deviceId]).then((results: { slots: SlotInfo[] }[]) => {
      if (results?.[0]?.slots) {
        setSlotExpiry(toSlotExpiryList(results[0].slots))
      }
    }).catch(() => {})
    // 轮询合并语义同 useHostEvents：新数据 state 未定义时保留旧值，缓存缺项不丢
    const fetchCached = () => {
      DiscoverService.GetSlotExpiry().then(se => {
        setSlotExpiry(prev => {
          const incoming = (se as Record<string, SlotExpiry[] | undefined>)?.[deviceId] ?? []
          const oldMap = new Map(prev.map(s => [s.index, s]))
          const merged = incoming.map(s => {
            if (s.state === undefined) {
              const old = oldMap.get(s.index)
              if (old && old.state !== undefined) return old
            }
            return s
          })
          for (const s of prev) {
            if (!merged.some(m => m.index === s.index)) merged.push(s)
          }
          return jsonEqual(prev, merged) ? prev : merged
        })
      }).catch(() => {})
    }
    fetchCached()
    const interval = setInterval(fetchCached, 3000)
    return () => clearInterval(interval)
  }, [selectedDevice?.device_id])

  // 创建云机弹窗：目标设备由左侧设备行的「+」决定，可能是任意在线设备（不必等于当前查看设备）。
  // 弹窗内的坑位授权状态（坑位边框/圆点颜色）只认 slotExpiry prop，而页面级 slotExpiry 只跟
  // 当前查看设备——对另一台设备点「+」会张冠李戴，故按目标设备单独查一次。
  const [createTarget, setCreateTarget] = useState<DeviceState | null>(null)
  const [createSlot, setCreateSlot] = useState<number | undefined>(undefined)
  const [createSlotExpiry, setCreateSlotExpiry] = useState<SlotExpiry[]>([])
  // 坑位表空坑位「创建」：当前查看设备即目标设备，传入来源坑位让弹窗默认选中
  const handleCreateInSlot = useCallback((slot: number) => {
    if (!selectedDevice) return
    setCreateSlot(slot)
    setCreateTarget(selectedDevice)
  }, [selectedDevice])
  useEffect(() => {
    const id = createTarget?.device_id
    if (!id) { setCreateSlotExpiry([]); return }
    let cancelled = false
    SlotService.QuerySlotInfo([id]).then((results: { slots: SlotInfo[] }[]) => {
      if (!cancelled && results?.[0]?.slots) setCreateSlotExpiry(toSlotExpiryList(results[0].slots))
    }).catch(() => {})
    return () => { cancelled = true }
  }, [createTarget?.device_id])

  // 行内注入「日志清理/缓存清理」（原云机卡片按钮的等价入口）：仅运行中实例可清理
  const openCleanModal = useCallback((type: ModalType, name: string) => {
    setModalAndroid(name); setModalType(type)
  }, [])
  const renderRowActions = useCallback((a: AndroidInfo) =>
    a.status === 'running' ? (
      <>
        <button onClick={(e) => { e.stopPropagation(); openCleanModal('log', a.name) }}
          className="text-gray-400 hover:text-gray-700 hover:bg-gray-200 rounded p-1 transition-colors cursor-pointer"
          title={t('maintenance:logCleanup')}>
          <ClipboardList size={14} />
        </button>
        <button onClick={(e) => { e.stopPropagation(); openCleanModal('cache', a.name) }}
          className="text-gray-400 hover:text-gray-700 hover:bg-gray-200 rounded p-1 transition-colors cursor-pointer"
          title={t('maintenance:cacheCleanup')}>
          <Eraser size={14} />
        </button>
      </>
    ) : null
  , [t, openCleanModal])

  // 云机勾选（提升到页面级：SlotView 复选框与 ActionBar 批量操作共用同一集合）
  const [selectedAndroidIds, setSelectedAndroidIds] = useState<Set<string>>(new Set())
  // 坑位表实例搜索词：与云机 tab 列表模式同款（实例名 / IP）
  const [slotSearchKw, setSlotSearchKw] = useState('')
  const handleToggleAndroidSelect = useCallback((id: string) => {
    setSelectedAndroidIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])
  const handleDeselectAllAndroids = useCallback(() => {
    setSelectedAndroidIds(new Set())
  }, [])
  // 分组全选切换：截图模式的设备头全选框用（该组所有实例 id 都已选中→全不选，否则→全选）
  const handleToggleGroupSelect = useCallback((ids: string[]) => {
    if (ids.length === 0) return
    setSelectedAndroidIds(prev => {
      const next = new Set(prev)
      const allSelected = ids.every(id => next.has(id))
      if (allSelected) for (const id of ids) next.delete(id)
      else for (const id of ids) next.add(id)
      return next
    })
  }, [])
  // 切换设备清空勾选：另一台设备的实例 id 残留会让批量操作打到不存在的目标
  useEffect(() => {
    setSelectedAndroidIds(new Set())
  }, [selectedDevice?.device_id])

  const selectedAndroids = useMemo(
    () => androids.filter(a => selectedAndroidIds.has(a.id)).sort((a, b) => a.indexNum - b.indexNum),
    [androids, selectedAndroidIds],
  )

  // 批量投屏就绪集合：坑位模式以事件驱动 androidList 的 running 态为准
  // （云机 tab 用截图就绪 key；此处语义等价——投屏要求实例在运行）。
  // 键统一为 `${a.ip}_${index}`（与 useBatchProjection 消费端及截图体系一致；
  // getDeviceId 在 v2 下返回序列号，会与消费端错位）
  const slotReadyKeys = useMemo(() => {
    const keys = new Set<string>()
    for (const a of androids) {
      if (a.status === 'running') keys.add(`${a.ip}_${a.indexNum}`)
    }
    return keys
  }, [androids])

  // 截图模式就绪集合：由 ScreenshotView 回传（有画面才可投屏），与坑位模式的 running 判定并存，
  // 按当前模式二选一喂给 ActionBar —— 两种模式下「可投屏」的判据本就不同
  const [screenshotReadyKeys, setScreenshotReadyKeys] = useState<Set<string>>(new Set())
  // 默认截图模式（与云机 tab 一致）：维护页单设备，进来先看画面比先看坑位表更常用；
  // 坑位模式仍可在 ActionBar 切换（模式选择不是持久化的，每次进页面都回到默认）
  const [viewMode, setViewMode] = useState<ViewMode>('screenshot')
  const [cardScale, setCardScale] = useCardScale() // 缩放滑条持久化（localStorage，android-client 同款）
  const readyKeys = viewMode === 'screenshot' ? screenshotReadyKeys : slotReadyKeys

  // 截图模式的卡片来源：该设备运行中（含开机中）的云机；坑位模式用全量（含已关机）。
  // 设置「云机展示=全部」时关机云机也进截图模式（ScreenshotView 渲染为关机占位卡）；
  // ActionBar 分母/全选同源同步。
  const [showOffline] = useShowOfflineAndroids()
  const runningAndroids = useMemo(
    () => androids.filter(a => isStatusVisible(a.status, showOffline)),
    [androids, showOffline],
  )

  // ActionBar 要求 Record 形态（云机 tab 全设备 map）；主机 tab 单设备，构造同构 map
  const slotExpiryMap = useMemo(() => (
    selectedDevice ? { [selectedDevice.device_id]: slotExpiry } : {}
  ), [selectedDevice, slotExpiry])

  // 与 ActionBar 分母同源：列表（坑位）模式用全量 androids，截图模式用 runningAndroids
  const handleSelectAllAndroids = useCallback(() => {
    const visible = viewMode === 'slot' ? androids : runningAndroids
    setSelectedAndroidIds(new Set(visible.map(a => a.id)))
  }, [viewMode, androids, runningAndroids])

  const [orientation, setOrientation] = useState<Orientation>('portrait')

  // 设备卡片绑定徽章的动作包：必须 useMemo 稳定引用——它是 DeviceItem 的 memo
  // 比较依据之一，内联对象会让侧栏每张卡片每轮重渲染（心跳 2 次/秒）。
  const handleNavigateToPayment = useCallback(() => navigate('payment'), [navigate])
  const handleUnbind = useCallback((device: DeviceState) => setUnbindTargets([device]), [])
  const bindActions = useMemo<BindActions>(() => ({
    loggedIn,
    onNavigateToPayment: handleNavigateToPayment,
    onBind: setBindTarget,
    onUnbind: handleUnbind,
  }), [loggedIn, handleNavigateToPayment, handleUnbind])

  return (
    <div className="flex h-full">
      <DeviceSidebar
        selectedIP={selectedIP}
        onSelect={setSelectedIP}
        sdkVersions={sdkVersions}
        onOpenBatchOps={() => setShowBatchOps(true)}
        onCreateInstance={setCreateTarget}
        showDeviceActions
        bindActions={bindActions}
      />
      <div className="flex-1 flex flex-col overflow-hidden">
        {!selectedDevice ? (
          <div className="flex items-center justify-center h-full text-gray-400 text-sm">{t('maintenance:selectDevice')}</div>
        ) : (
          <>
            <ActionBar
              viewMode={viewMode}
              onViewModeChange={(m) => { setViewMode(m); setSelectedAndroidIds(new Set()) }}
              orientation={orientation}
              onOrientationChange={setOrientation}
              infoSlot={<DeviceInfoBar device={selectedDevice} />}
              selectedAndroids={selectedAndroids}
              allAndroids={runningAndroids}
              slotAndroids={androids}
              mirrors={mirrors}
              devices={devices}
              readyKeys={readyKeys}
              slotExpiry={slotExpiryMap}
              onSelectAllAndroids={handleSelectAllAndroids}
              onDeselectAllAndroids={handleDeselectAllAndroids}
              hasSelectedDevices
            />
            <div className="flex-1 overflow-auto p-5 pt-4">
              {selectedDevice.state !== 0 ? (
                <div className="flex items-center justify-center h-full text-gray-400 text-sm">
                  {t('host:empty.deviceOffline')}
                </div>
              ) : viewMode === 'screenshot' ? (
                <ScreenshotView
                  androids={runningAndroids}
                  allAndroids={androids}
                  devices={[selectedDevice]}
                  slotExpiry={slotExpiryMap}
                  mirrors={mirrors}
                  orientation={orientation}
                  onOrientationChange={setOrientation}
                  hasSelectedDevices
                  selectedAndroidIds={selectedAndroidIds}
                  onToggleAndroidSelect={handleToggleAndroidSelect}
                  onToggleGroupSelect={handleToggleGroupSelect}
                  onReadyKeysChange={setScreenshotReadyKeys}
                  cardScale={cardScale}
                  onCardScaleChange={setCardScale}
                  singleCastScale={singleCastScale}
                  onSingleCastScaleChange={setSingleCastScale}
                  hideDeviceFilter
                  hideTagFilter
                  layout="device"
                  onCreateInstance={(d, slot) => { setCreateSlot(slot); setCreateTarget(d) }}
                />
              ) : (
                <>
                  {/* 实例搜索行：与云机 tab 列表模式同款（单台设备容器列表，标签条无独立数据源故不加） */}
                  <div className="flex items-center gap-3 px-1 pb-3 pt-1">
                    <input
                      type="text"
                      className="w-56 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-700 outline-none placeholder:text-gray-400 focus:border-blue-400"
                      placeholder={t('host:screenshot.searchInstance')}
                      value={slotSearchKw}
                      onChange={e => setSlotSearchKw(e.target.value)}
                    />
                  </div>
                  <SlotView
                    device={selectedDevice}
                    devices={devices}
                    androids={androids}
                    slotExpiry={slotExpiry}
                    mirrors={mirrors}
                    selectedIds={selectedAndroidIds}
                    onToggleSelect={handleToggleAndroidSelect}
                    onSetSelectedIds={setSelectedAndroidIds}
                    rowActions={renderRowActions}
                    searchKw={slotSearchKw}
                    onCreateInSlot={handleCreateInSlot}
                  />
                </>
              )}
            </div>
          </>
        )}
      </div>
      {/* 绑定/解绑弹窗（设备卡片徽章触发）：成功后 Go 已回写 bind_status 缓存，
          这里再拉一次列表把新状态刷进 devices（徽章文案随之更新）。 */}
      <BindDialog
        device={bindTarget}
        username={username}
        onClose={() => setBindTarget(null)}
        onSuccess={reloadDevices}
      />
      <UnbindDialog
        devices={unbindTargets}
        username={username}
        onClose={() => setUnbindTargets([])}
        onSuccess={reloadDevices}
      />
      {modalType && selectedDevice && (
        <MaintenanceModal
          type={modalType}
          deviceIP={getDeviceId(selectedDevice, mode)}
          androidName={modalAndroid}
          logMode="android"
          onClose={() => setModalType(null)}
        />
      )}
      {showBatchOps && (
        <BatchOpsModal sdkVersions={sdkVersions} onClose={() => setShowBatchOps(false)} />
      )}
      {/* 创建云机：创建成功后云机列表由 androidlist:updated 事件自动刷新（useDeviceAndroids） */}
      {createTarget && (
        <CreateInstanceModal
          device={createTarget}
          slotExpiry={createSlotExpiry}
          defaultSlot={createSlot}
          onClose={() => { setCreateTarget(null); setCreateSlot(undefined) }}
          onCreated={() => { setCreateTarget(null); setCreateSlot(undefined) }}
        />
      )}
    </div>
  )
}

export default memo(MaintenancePage)
