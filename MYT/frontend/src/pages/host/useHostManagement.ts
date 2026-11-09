import { useState, useCallback, useRef, useMemo, useEffect } from 'react'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { DeviceState, ViewMode, AndroidInfo, SlotExpiry, Orientation, MirrorItem } from '../../types/index'
import { USE_MOCK, MOCK_DEVICES, MOCK_ANDROIDS, MOCK_SLOT_EXPIRY } from '../../utils/mockData'
import { cast } from '../../utils/cast'
import { useDeployMode } from '../../hooks/useDeployMode'
import { useShowOfflineAndroids } from '../../hooks/useShowOfflineAndroids'
import { useSlotTags, useSlotTagFilter, collectAndroidTags, fetchAndroidTagDict, type SlotTagControl } from '../../hooks/useSlotTags'
import { getDeviceId } from '../../utils/deviceId'
import { compareAndroidByDeviceThenSlot, computeViewScopeAndroids } from './hostManagementUtils'
import { isStatusVisible } from '../../utils/containerStatus'
import { useDeviceDiscovery } from '../../hooks/useDeviceDiscovery'
import { useHostEvents } from './useHostEvents'

export function useHostManagement() {
  const { mode: deployMode } = useDeployMode()
  // 「云机展示：运行中 / 全部」设置（设置页开关，模块级 store 共享）：开启后关机云机
  // 也进展示/操作集合（视图层渲染为关机占位卡）；默认关闭保持现状（仅 running/starting）。
  const [showOffline, setShowOffline] = useShowOfflineAndroids()
  const [devices, setDevices] = useState<DeviceState[]>(USE_MOCK ? MOCK_DEVICES : [])
  const [selectedDevice, setSelectedDevice] = useState<DeviceState | null>(() => {
    if (!USE_MOCK) return null
    return MOCK_DEVICES.find(d => d.state === 0) || null
  })
  const [selectedDeviceIds, setSelectedDeviceIds] = useState<Set<string>>(new Set())
  const [viewMode, setViewMode] = useState<ViewMode>('screenshot')

  const handleViewModeChange = (mode: ViewMode) => {
    // 勾选集合（selectedDeviceIds）跨模式沿用：两种模式的左侧列表都是多选，右侧按勾选渲染
    // （截图模式出卡片、坑位模式出设备分区）。
    // 容器级勾选（批量勾选 + 侧栏过滤器 sidebarAndroidIds）也跨模式保留（2026-09-26）：
    // 侧栏过滤器是用户显式输入，决定右侧展示范围，切换视图清掉会让展示范围意外扩大；
    // 批量勾选沿用也无歧义（两模式容器口径一致：都是 running/starting）。
    setViewMode(mode)
  }
  const [orientation, setOrientation] = useState<Orientation>('portrait')
  const [createTarget, setCreateTarget] = useState<DeviceState | null>(null)
  const [androidList, setAndroidList] = useState<AndroidInfo[]>(USE_MOCK ? MOCK_ANDROIDS : [])
  const [slotExpiry, setSlotExpiry] = useState<Record<string, SlotExpiry[]>>(USE_MOCK ? MOCK_SLOT_EXPIRY : {})
  const [selectedAndroidIds, setSelectedAndroidIds] = useState<Set<string>>(new Set())
  // 侧栏设备行的展开状态（展示该设备的运行中容器）。必须放本层而非 DeviceListItem
  // 内部：androidlist:updated / devicestats:updated 事件会重渲染整列表，状态放行内
  // 会被重挂载丢失；同时 useHostEvents 需要它把展开设备并入容器查询范围。
  const [expandedDeviceIds, setExpandedDeviceIds] = useState<Set<string>>(new Set())
  const [mirrors, setMirrors] = useState<MirrorItem[]>([])
  const [readyKeys, setReadyKeys] = useState<Set<string>>(new Set())
  const devicesRef = useRef<DeviceState[]>(devices)
  devicesRef.current = devices
  // 容器列表 ref：设备取消勾选时按 deviceId 清理 sidebar/批量勾选集合用。
  // 走 ref 而非依赖——androidList 高频事件刷新，直接进依赖会让 handleToggleSelect
  // 引用每秒变化，DeviceListItem 的 memo 全部失效（2026-09-05 卡顿修复的回退路径）。
  const androidListRef = useRef<AndroidInfo[]>(androidList)
  androidListRef.current = androidList

  // 待添加设备 + 设备发现/刷新（与主机 tab 的 DeviceSidebar 共用 useDeviceDiscovery）
  const {
    pendingDevices, scanning,
    refreshDevices: discoverDevices,
    addPendingDevices: handleAddPendingDevices,
    addAllPendingDevices: handleAddAllPendingDevices,
    clearPendingDevices: handleClearPendingDevices,
  } = useDeviceDiscovery()

  // 事件订阅 + 初始拉取（6 个 effect）抽到 useHostEvents，主 hook 只保留状态与操作
  useHostEvents({
    selectedDeviceId: selectedDevice?.device_id,
    viewMode,
    selectedDeviceIds,
    expandedDeviceIds,
    setDevices, setSelectedDevice, setSelectedDeviceIds,
    setAndroidList, setSlotExpiry,
    setMirrors,
  })

  // 模式切换时立即清空容器列表和就绪状态（Go 端也会清，但前端立即清避免旧数据闪烁）
  useEffect(() => {
    setAndroidList([])
    setReadyKeys(new Set())
  }, [deployMode])

  // v4 首次进入自动勾选主机：V4 的语义是「一台主机下所有实例平铺」，用户进来就该看到
  // 实例，而不是先面对「请勾选设备」的空面板（V3 的勾选是跨会话持久化的用户意图，
  // V4 主机的 order 一般全为 0）。只做一次——用户之后主动取消全选必须被尊重。
  const v4AutoSelectedRef = useRef(false)
  useEffect(() => {
    if (deployMode !== 'v4') {
      v4AutoSelectedRef.current = false
      return
    }
    if (v4AutoSelectedRef.current || devices.length === 0) return
    v4AutoSelectedRef.current = true
    if (selectedDeviceIds.size > 0) return
    DiscoverService.SelectAllDevices()
      .then(list => {
        const dl = cast<DeviceState[]>(list)
        if (Array.isArray(dl)) setSelectedDeviceIds(new Set(dl.filter(d => d.order > 0).map(d => d.device_id)))
      })
      .catch(() => {})
  }, [deployMode, devices.length, selectedDeviceIds.size])

  const handleDeleteDevice = (device: DeviceState) => {
    if (USE_MOCK) {
      setDevices(prev => prev.filter(d => d.device_id !== device.device_id))
      setAndroidList(prev => prev.filter(a => a.deviceId !== device.device_id))
      return
    }
    DiscoverService.DeleteDevice(device.device_id).then(() => {
      DiscoverService.GetDeviceList().then(list => {
        const dl = cast<DeviceState[]>(list)
        if (Array.isArray(dl)) setDevices(prev => prev.length === dl.length && JSON.stringify(prev) === JSON.stringify(dl) ? prev : dl)
      }).catch(() => {})
      DiscoverService.GetAndroidList().then(al => {
        const list = cast<AndroidInfo[]>(al ?? [])
        setAndroidList(prev => prev.length === list.length && JSON.stringify(prev) === JSON.stringify(list) ? prev : list)
      }).catch(() => {})
    }).catch(console.error)
    setSelectedDeviceIds(prev => {
      const next = new Set(prev)
      next.delete(device.device_id)
      return next
    })
    if (selectedDevice?.device_id === device.device_id) {
      setSelectedDevice(null)
    }
  }

  // useCallback：DeviceListItem memo 生效前提（回调引用跨渲染稳定）
  const handleSelectDevice = useCallback((device: DeviceState) => {
    setSelectedDevice(device)
  }, [])

  // 取消设备勾选时清理该设备的容器级状态（sidebar 过滤器 + 批量勾选集合）。
  // 注意声明位置：sidebarAndroidIds 状态定义在本文件后段，此处在 TDZ 风险之外
  // （函数体运行时才访问 setter），放 handleToggleSelect 之前保证依赖求值不报错。
  const clearAndroidSelectionForDevice = useCallback((deviceId: string) => {
    const androidIds = androidListRef.current
      .filter(a => a.deviceId === deviceId)
      .map(a => a.id)
    if (androidIds.length === 0) return
    const removeFrom = (prev: Set<string>) => {
      let changed = false
      const next = new Set(prev)
      for (const id of androidIds) { if (next.delete(id)) changed = true }
      return changed ? next : prev
    }
    setSidebarAndroidIds(removeFrom)
    setSelectedAndroidIds(removeFrom)
  }, [])

  // 设备勾选切换：toggle order 链路（唯一事实源）。**勾选/取消两个方向**都同步清理
  // 该设备的容器级状态（sidebar 过滤器 + 批量勾选集合）：
  // - 取消勾选：设备已不在选择范围，残留会让展开区容器「视觉还勾着」、右侧按旧
  //   过滤器收窄（与设备未勾选矛盾，2026-09-27 修复）。
  // - 勾选：设备级语义是「该设备全部容器进范围」，若此前勾过该设备的部分容器
  //   （sidebar 过滤器残留），viewScopeAndroids 的收窄规则会继续压住设备级全量——
  //   右侧仍只显示旧过滤器命中的子集、设备框三态被 checkedCount>0 拉住不变
  //   （2026-10-28 修复：点设备框全勾后右侧不刷新、勾选框不变）。
  const handleToggleSelect = useCallback((deviceId: string) => {
    setSelectedDeviceIds(prev => {
      const next = new Set(prev)
      if (next.has(deviceId)) {
        next.delete(deviceId)
      } else {
        next.add(deviceId)
      }
      if (!USE_MOCK) {
        const maxOrder = Math.max(0, ...devicesRef.current.filter(d => next.has(d.device_id)).map(d => d.order))
        let order = maxOrder
        DiscoverService.ToggleDeviceOrder(deviceId, next.has(deviceId) ? ++order : 0).catch(console.error)
      }
      return next
    })
    // 勾选/取消都清：取消是移除矛盾残留，勾选是解除过滤器对设备级全量的压制（见上）
    clearAndroidSelectionForDevice(deviceId)
  }, [clearAndroidSelectionForDevice])

  // 全选/全不选：先乐观改写（瞬时反馈），再用 Go 返回的权威列表收敛。
  // 权威收敛这步不能省：勾选的唯一事实源是 Go 的 order 字段，只靠乐观改写的话，
  // 写入期间任何一次 devicelist:updated 快照都会按"还没写完的 order"把勾选挤掉
  // （就是"全选后离线/401 设备自动取消"的复发路径）。Go 侧已改为一次锁内批量写，
  // 返回的列表必然是写完之后的状态。
  const applySelectionFromOrder = (list: DeviceState[]) => {
    setSelectedDeviceIds(new Set(list.filter(d => d.order > 0).map(d => d.device_id)))
  }

  const handleSelectAll = () => {
    // 全选=列表全部设备（含离线/待认证），与单选取舍一致；不可达设备在批量
    // 执行时按台失败回报，不在勾选阶段偷偷过滤。
    setSelectedDeviceIds(new Set(devices.map(d => d.device_id)))
    if (USE_MOCK) return
    DiscoverService.SelectAllDevices()
      .then(list => {
        const dl = cast<DeviceState[]>(list)
        if (Array.isArray(dl)) applySelectionFromOrder(dl)
      })
      .catch(console.error)
  }

  const handleDeselectAll = () => {
    setSelectedDeviceIds(new Set())
    if (USE_MOCK) return
    DiscoverService.DeselectAll()
      .then(list => {
        const dl = cast<DeviceState[]>(list)
        if (Array.isArray(dl)) applySelectionFromOrder(dl)
      })
      .catch(console.error)
  }

  // 勾选设备中「正在跑」的云机（running/starting）—— 云机 tab 两种模式的可操作集合同一口径：
  // 截图模式出卡片、列表模式出坑位分区，都是这一批；ActionBar 的「N / M」与「全选」也以它为分母。
  // 「全选」回调要把它作为 useCallback 依赖，必须声明在回调之前：依赖数组在 render
  // 期立即求值，放在后面会命中 TDZ（"Cannot access before initialization"）。
  // 含 starting（开机乐观状态）：ScreenshotView 的渲染分支按 running/starting 出卡。
  // 若这里只滤 running，开机中的实例会被整体剔除——坑位被 emptySlots 补成占位卡，
  // 展开侧栏设备行的容器列表（同 DeviceGroupTree 的 expandedGroups 范式）
  const handleToggleDeviceExpand = useCallback((deviceId: string) => {
    setExpandedDeviceIds(prev => {
      const next = new Set(prev)
      if (next.has(deviceId)) next.delete(deviceId)
      else next.add(deviceId)
      return next
    })
  }, [])

  // 开关机状态迁移窗口内同一坑位"占位卡/开机卡"交替闪现（重复坑位观感的来源）。
  // 勾选设备的运行容器（设备级语义：勾设备 = 其全部运行容器进范围）。
  // showOffline（设置「云机展示=全部」）时全状态进集合；关闭时仅开机相关可见
  // （isStatusVisible，见 containerStatus.ts 口径——created 关闭时同样不可见）。
  // 状态谓词集中一处（statusVisible）：三处过滤同口径，避免各写一遍演化分叉。
  const statusVisible = useCallback(
    (a: AndroidInfo) => isStatusVisible(a.status, showOffline),
    [showOffline],
  )
  const deviceVisibleAndroids = useMemo(() =>
    androidList.filter(a => selectedDeviceIds.has(a.deviceId) && statusVisible(a))
      .sort((a, b) => compareAndroidByDeviceThenSlot(a, b, deployMode)),
    [androidList, selectedDeviceIds, deployMode, statusVisible]
  )

  // 侧栏展开区的容器勾选 = **过滤器**：决定右侧展示哪些容器（「以左侧选中的为准」）。
  // 与 selectedAndroidIds（右侧批量操作勾选）分离——同一集合承担双重职责会导致：
  // 右侧「取消全选」清空勾选 → 过滤器失效 → 展示反而扩大到设备级全量（2026-09-26 反向 bug）。
  // 语义：某设备下 sidebar 勾了任一容器 → 该设备只展示被勾选的；一个没勾 → 设备级全量。
  const [sidebarAndroidIds, setSidebarAndroidIds] = useState<Set<string>>(new Set())

  // 有效容器集合（右侧两视图的展示集合，ActionBar N/M 分母同源）。
  // **不依赖设备级勾选前置**（2026-09-26 取消联动后）：设备勾选与容器勾选完全独立——
  //   勾选设备 → 全量 running/starting 容器；未勾选设备 → 仅 sidebar 过滤器命中的容器。
  //   两者并集即展示范围（仅勾容器未勾设备的容器必须在展示/操作范围内）。
  // 收窄语义：某设备下 sidebar 命中任一容器 → 该设备只展示被勾选的那些（勾选设备同样收窄）。
  // 标签筛选状态提升到本层（2026-09-27）：截图/列表两视图共用一份（同一套标签体系 +
  // 同款「命中任一选中标签」或语义），effectiveAndroids 追加标签收窄——否则标签筛选后
  // 「全选」会按未筛选的全量收窄前集合收，把被标签筛掉的云机也选进来（同源 bug）。
  // 视图范围（未做标签筛选）：设备勾选 ∪ sidebar 过滤器命中（状态可见）。
  // 从 effectiveAndroids 拆出（2026-10-27）：标签筛选数据源（containerTags）必须
  // 聚合自「未做标签筛选」的范围——若从标签筛选后的集合聚合，会形成
  // tagFilter → effectiveAndroids → containerTags → tagFilter 的循环依赖，
  // 且筛选激活时下拉列表会缩到只剩命中标签（选中其他标签随即被失效收敛误删）。
  // 语义与原 effectiveAndroids 的设备/容器勾选收窄完全一致（仅去掉标签收窄）。
  const viewScopeAndroids = useMemo(
    () => computeViewScopeAndroids(
      androidList, selectedDeviceIds, sidebarAndroidIds, statusVisible,
      (a, b) => compareAndroidByDeviceThenSlot(a, b, cast<'local' | 'public'>(deployMode)),
    ),
    [androidList, sidebarAndroidIds, selectedDeviceIds, deployMode, statusVisible],
  )

  // 标签筛选数据源（2026-10-27）：视图范围容器的 android.tags 去重。
  // 标签唯一权威来源是设备端容器（本地坑位映射 slot_tags.json 已停止消费），
  // allTags 聚合 / 失效收敛 / 筛选判定必须同源，否则选中仅存在于容器 tags 的标签
  // 会被收敛 effect 误删（勾选后立即弹回）。
  // slotTags 仍传给 tagControl（视图层标签编辑浮层的兜底建议源，不再参与筛选判定）。
  const { tags: slotTags, setTagsLocal: setSlotTagsLocal } = useSlotTags()
  // 设备端标签字典（GET /android/tags）：标签筛选下拉打开时拉一次的兜底源——
  // 视图容器集合未覆盖的标签（如其他客户端打的、或事件/轮询尚未带回的新标签）
  // 也进下拉选项。失败忽略（建议源，不阻塞筛选）。
  const [tagDict, setTagDict] = useState<string[]>([])
  const tagDictInFlight = useRef(false)
  const refreshTagDict = useCallback(() => {
    if (tagDictInFlight.current) return
    const idents = new Set<string>()
    for (const a of viewScopeAndroids) {
      const ident = getDeviceId({ ip: a.ip, device_id: a.deviceId }, deployMode)
      if (ident) idents.add(ident)
    }
    if (idents.size === 0) return
    tagDictInFlight.current = true
    fetchAndroidTagDict([...idents])
      .then(setTagDict)
      .catch(() => {})
      .finally(() => { tagDictInFlight.current = false })
  }, [viewScopeAndroids, deployMode])
  const containerTags = useMemo(() => {
    const set = new Set(collectAndroidTags(viewScopeAndroids))
    for (const tag of tagDict) set.add(tag)
    return [...set].sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
  }, [viewScopeAndroids, tagDict])
  const tagFilter = useSlotTagFilter(slotTags, containerTags)
  const tagControl: SlotTagControl = useMemo(
    () => ({ slotTags, setSlotTagsLocal, filter: tagFilter, refreshTagDict }),
    [slotTags, setSlotTagsLocal, tagFilter, refreshTagDict],
  )
  const selectedTags = tagFilter.selectedTags

  // 有效容器集合（右侧两视图的展示集合，ActionBar N/M 分母同源）。
  // **不依赖设备级勾选前置**（2026-09-26 取消联动后）：设备勾选与容器勾选完全独立——
  //   勾选设备 → 全量 running/starting 容器；未勾选设备 → 仅 sidebar 过滤器命中的容器。
  //   两者并集即展示范围（仅勾容器未勾设备的容器必须在展示/操作范围内）。
  // 收窄语义：某设备下 sidebar 命中任一容器 → 该设备只展示被勾选的那些（勾选设备同样收窄）。
  // 标签筛选状态提升到本层（2026-09-27）：截图/列表两视图共用一份（同一套标签体系 +
  // 同款「命中任一选中标签」或语义），effectiveAndroids 追加标签收窄——否则标签筛选后
  // 「全选」会按未筛选的全量收窄前集合收，把被标签筛掉的云机也选进来（同源 bug）。
  // 判定改用 android.tags（2026-10-27，与筛选数据源同源；原 slotTags[slotTagKey] 查的
  // 是已停止消费的本地坑位映射，容器标签筛不动它）。
  const effectiveAndroids = useMemo(() => {
    const tagFilterActive = selectedTags.size > 0
    if (!tagFilterActive) return viewScopeAndroids
    return viewScopeAndroids.filter(a => (a.tags ?? []).some(tag => selectedTags.has(tag)))
  }, [viewScopeAndroids, selectedTags])

  // 以下选择类回调会作为 props 一路传到每张截图卡片，ScreenshotCard 的 memo
  // 比较器按引用判定 onToggleSelect——不包 useCallback 就是每轮 render 全部卡片
  // memo 失效（截图每秒推一轮，等于整页持续重渲染）。
  const handleToggleAndroidSelect = useCallback((id: string) => {
    setSelectedAndroidIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  // 全选 = 当前展示集合（effectiveAndroids）全量：点完必然 N === M。
  // 关机云机恢复参与全选/勾选（勾选源头不再按 isRunnableStatus 收口）：上传等批量
  // 操作有 Go 侧逐台护栏兜底（死容器进 failed 明细不毒整组），安全性不靠勾选闸门。
  // ref 声明在下方 selectedAndroids 附近——useCallback 依赖数组在 render 期立即求值，
  // 闭包引用后置 ref 与同文件 handleToggleDeviceExpand 的既有范式一致（见该处注释）。
  // 只写批量勾选集合，**不动 sidebar 过滤器**——展示范围永远由左侧勾选驱动。
  const handleSelectAllAndroids = useCallback(() => {
    setSelectedAndroidIds(new Set(effectiveIdsRef.current))
  }, [])

  // 取消全选 = 清空批量勾选：右侧卡片勾选态全部清掉，但 **sidebar 过滤器不动**——
  // 展示范围保持「左侧勾选的那几个」，不会反弹回设备级全量（2026-09-26 反向 bug 的修复点）。
  const handleDeselectAllAndroids = useCallback(() => {
    setSelectedAndroidIds(new Set())
  }, [])

  // 侧栏展开区容器勾选切换（过滤器）：写入 sidebarAndroidIds；同时同步批量勾选集合
  // （勾上 → 加入批量集合，用户勾它通常就是要操作它；取消 → 从批量集合移除，避免
  // 「过滤器里看不到却被批量操作打到」）。联动设备勾选由 DeviceListItem 侧处理。
  const handleToggleSidebarAndroid = useCallback((id: string) => {
    let adding = true
    setSidebarAndroidIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) { next.delete(id); adding = false }
      else next.add(id)
      return next
    })
    // 批量勾选同步：读旧值判断方向（updater 必须纯，副作用放外面）
    setSelectedAndroidIds(prev => {
      const has = prev.has(id)
      // adding 时若已在批量集合则不动；取消时必移除
      if (adding && !has) { const n = new Set(prev); n.add(id); return n }
      if (!adding) { const n = new Set(prev); n.delete(id); return n }
      return prev
    })
  }, [])

  // 设备已勾选状态下点容器行 = 从该设备的全选中排除此容器（DeviceListItem 分派）。
  // 设备级勾选是 order 链路的「整台设备」粒度，无单容器维度——侧栏/右侧的展示
  // 集合无法表达「设备已勾但少一台容器」，因此摘除整台设备勾选 + 清掉该设备的
  // 容器级勾选残留，视觉与语义一致：设备变空、容器行取消、该容器退出批量集合。
  const handleExcludeAndroidFromDevice = useCallback((androidId: string, deviceId: string) => {
    setSelectedDeviceIds(prev => {
      if (!prev.has(deviceId)) return prev
      const next = new Set(prev)
      next.delete(deviceId)
      if (!USE_MOCK) {
        DiscoverService.ToggleDeviceOrder(deviceId, 0).catch(console.error)
      }
      return next
    })
    clearAndroidSelectionForDevice(deviceId)
  }, [clearAndroidSelectionForDevice])

  // 分组全选切换：该组所有实例 id 都已选中→全不选(移除)，否则→全选(加入)。
  // 用于截图页分组标题的全选框。
  const handleToggleGroupSelect = useCallback((ids: string[]) => {
    if (ids.length === 0) return
    setSelectedAndroidIds(prev => {
      const next = new Set(prev)
      const allSelected = ids.every(id => next.has(id))
      if (allSelected) {
        for (const id of ids) next.delete(id)
      } else {
        for (const id of ids) next.add(id)
      }
      return next
    })
  }, [])

  // 分组设备全选/取消全选（云机 tab 侧栏分组树的三态 checkbox）：
  // - 乐观改写 selectedDeviceIds（瞬时反馈）；
  // - 一次 IPC 批量落 Go order（SetDevicesOrder，整组几十台不再逐台 Toggle）；
  // - 勾选/取消两个方向都清理该组设备的容器级状态（同 handleToggleSelect 的理由）；
  // - 用 Go 返回的权威列表收敛（order 是勾选唯一事实源，乐观改写期间的心跳快照
  //   会按「还没写完的 order」挤掉勾选——全选后离线设备自动取消的同款复发路径）。
  const handleToggleGroupDevices = useCallback((deviceIds: string[], select: boolean) => {
    if (deviceIds.length === 0) return
    setSelectedDeviceIds(prev => {
      const next = new Set(prev)
      if (select) for (const id of deviceIds) next.add(id)
      else for (const id of deviceIds) next.delete(id)
      return next
    })
    for (const id of deviceIds) clearAndroidSelectionForDevice(id)
    if (USE_MOCK) return
    DiscoverService.SetDevicesOrder(deviceIds, select)
      .then(list => {
        const dl = cast<DeviceState[]>(list)
        if (Array.isArray(dl)) applySelectionFromOrder(dl)
      })
      .catch(console.error)
  }, [clearAndroidSelectionForDevice])

  const handleDeleteDevices = useCallback(async (deviceIDs: string[]) => {
    if (USE_MOCK) {
      setDevices(prev => prev.filter(d => !deviceIDs.includes(d.device_id)))
      return
    }
    await DiscoverService.DeleteDevices(JSON.stringify(deviceIDs))
    DiscoverService.GetDeviceList().then(list => {
      const dl = cast<DeviceState[]>(list)
      if (Array.isArray(dl)) setDevices(prev => prev.length === dl.length && JSON.stringify(prev) === JSON.stringify(dl) ? prev : dl)
    }).catch(() => {})
    DiscoverService.GetAndroidList().then(al => {
      const list = cast<AndroidInfo[]>(al ?? [])
      setAndroidList(prev => prev.length === list.length && JSON.stringify(prev) === JSON.stringify(list) ? prev : list)
    }).catch(() => {})
    setSelectedDeviceIds(prev => {
      const next = new Set(prev)
      deviceIDs.forEach(id => next.delete(id))
      return next
    })
    if (selectedDevice && deviceIDs.includes(selectedDevice.device_id)) {
      setSelectedDevice(null)
    }
  }, [selectedDevice])

  // 批量操作目标 = 「有效容器集合」中已勾选的云机：与右侧展示、ActionBar 的 N/M
  // 分母同一份集合（effectiveAndroids），已勾选后被关机的云机自动掉出目标，
  // 不会出现"操作打到看不见的云机"。
  // 关机云机恢复可选（勾选源头闸门撤除，2026-11 回滚）：selectedAndroids 直接从
  // effectiveAndroids 收口。死容器安全性由 Go 侧护栏兜底（批量上传逐台返回
  // 「已关机」failed 明细不毒整组）；ADB 批量（唯一无逐台兜底的路径）由 ActionBar
  // executeAdb 在执行前按 isRunnableStatus 过滤被选集合并 toast 跳过。
  const selectedAndroids = useMemo(
    () => effectiveAndroids.filter(a => selectedAndroidIds.has(a.id)),
    [effectiveAndroids, selectedAndroidIds],
  )

  // 选中集合收敛（悬空 id 清理）：容器列表变化时把**已不存在**的容器从勾选集合中
  // 剔除（Set 单调累积会残留悬空 id——删除/移动/换设备后旧 id 永远留着，下一次
  // 「全选」外的任何操作都会带着它们）。**不剔除关机**：关机云机仍可勾选（见上），
  // 清理只会把它从集合里踢掉（用户明明勾着却被静默取消）。
  // 与 onSlotStatus 乐观状态天然联动：容器删除时 androidList 即时更新 → 本 effect
  // 同帧清理选中（单一挂点覆盖批量删除/单台删除/设备端自发移除）。
  // 引用稳定范式：集合无变化时返回 prev，避免高频事件每秒重建 Set（卡顿教训同款）。
  const effectiveIdsRef = useRef<string[]>([])
  effectiveIdsRef.current = effectiveAndroids.map(a => a.id)
  useEffect(() => {
    setSelectedAndroidIds(prev => {
      if (prev.size === 0) return prev
      const live = new Set(effectiveIdsRef.current)
      let changed = false
      const next = new Set<string>()
      for (const id of prev) {
        if (live.has(id)) next.add(id)
        else changed = true
      }
      if (!changed) return prev
      return next
    })
  }, [androidList])

  // 第三层数据：设备 ID → 该设备下的云机列表（按坑位号排序）。
  // 只包含有云机的设备 —— 侧栏据此决定是否显示展开箭头（无云机的设备不显示 0 的可展开项）。
  const androidGroups = useMemo(() => {
    const map: Record<string, AndroidInfo[]> = {}
    for (const a of androidList) {
      const arr = map[a.deviceId]
      if (arr) arr.push(a)
      else map[a.deviceId] = [a]
    }
    for (const arr of Object.values(map)) arr.sort((x, y) => x.indexNum - y.indexNum)
    return map
  }, [androidList])

  return {
    devices,
    scanning,
    selectedDevice,
    selectedDeviceIds,
    viewMode,
    handleViewModeChange,
    orientation,
    setOrientation,
    createTarget,
    setCreateTarget,
    androidList,
    slotExpiry,
    selectedAndroidIds,
    // 侧栏展开的设备（容器子列表）；useHostEvents 据此扩大容器查询范围
    expandedDeviceIds,
    handleToggleDeviceExpand,
    mirrors,
    readyKeys,
    setReadyKeys,
    discoverDevices,
    handleSelectDevice,
    handleToggleSelect,
    handleExcludeAndroidFromDevice,
    handleSelectAll,
    handleDeselectAll,
    handleDeleteDevice,
    handleToggleAndroidSelect,
    handleSelectAllAndroids,
    handleDeselectAllAndroids,
    handleToggleGroupSelect,
    handleToggleGroupDevices,
    handleSetSelectedAndroidIds: setSelectedAndroidIds,
    pendingDevices,
    handleAddPendingDevices,
    handleAddAllPendingDevices,
    handleClearPendingDevices,
    handleDeleteDevices,
    // 勾选设备的 running/starting 容器（设备级语义，不含仅勾容器的未勾设备）
    deviceVisibleAndroids,
    // 有效容器集合（由侧栏过滤器收窄）：右侧视图展示集合
    effectiveAndroids,
    selectedAndroids,
    // 侧栏过滤器勾选集合（决定右侧展示范围）+ 其切换回调
    sidebarAndroidIds,
    handleToggleSidebarAndroid,
    // 设备 ID → 该设备下的云机列表（多设备坑位视图按设备分区时用）
    androidGroups,
    // 坑位标签控制包（标签数据 + 筛选状态）：下发给两视图，与 effectiveAndroids 同源
    tagControl,
    // 「云机展示=全部」设置：视图层据此放宽 onlyRunning / 渲染关机占位卡
    showOffline,
    setShowOffline,
  }
}
