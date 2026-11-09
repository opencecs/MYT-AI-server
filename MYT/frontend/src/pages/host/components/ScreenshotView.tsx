import { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Events, System } from '@wailsio/runtime'
import * as ScreenshotService from '../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import * as SlotService from '../../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import { AndroidInfo, SlotExpiry, Orientation, DeviceState, MirrorItem } from '../../../types/index'
import { ScreenshotCard, ScreenshotCardProps } from './ScreenshotCard'
import { ScreenshotGroup, GroupCardData } from './ScreenshotGroup'
import StoppedAndroidCard from './screenshotDetail/StoppedAndroidCard'
import TagFilterBar from './TagFilterBar'
import TagFilterDropdown from './TagFilterDropdown'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getDeviceId } from '../../../utils/deviceId'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { useSlotTags, useSlotTagFilter, saveAndroidTags, collectAndroidTags, type SlotTagControl } from '../../../hooks/useSlotTags'
import SlotTagEditor from '../../../components/SlotTagEditor'
import { showToast } from '../../../components/Toast'
import { compareIP } from '../../../utils/ipCompare'
import { writeScreenshotLru } from './screenshotLru'
import { collectMissingScreenshotKeys, useScreenshotOnDemand } from './screenshotOnDemand'
import { useVisibleScreenshotKeys } from './screenshotDetail/useVisibleScreenshotKeys'
import { useDominantAspect } from './screenshotDetail/useDominantAspect'
import { cardMinWidthPx, observeWidth } from './screenshotDetail/cardGridWidth'
import { markCastExit } from '../../../utils/castGuard'
import { onEvent } from '../../../utils/wailsEvent'
import { isRunnableStatus, isRunningStatus, cardStatusFromAndroid, type CardStatus } from '../../../utils/containerStatus'
import Select from '../../../components/Select'

/**
 * 截图卡片布局（由宿主固定，不做切换）：
 *  device = 按设备分组（设备头 + 组内网格 + 坑位占位），主机 tab 用；
 *  flat   = 不分类平铺（全部实例连续排列成一张网格，无设备头/无占位；运行中整体在前，
 *           其内部按设备聚块），云机 tab 用。
 */
type ScreenshotLayout = 'device' | 'flat'

interface Props {
  androids: AndroidInfo[]
  /** 完整云机列表（含非 running，用于切换云机弹窗展示该坑位所有容器）。 */
  allAndroids: AndroidInfo[]
  devices: DeviceState[]
  slotExpiry: Record<string, SlotExpiry[]>
  mirrors: MirrorItem[]
  orientation: Orientation
  onOrientationChange: (o: Orientation) => void
  hasSelectedDevices: boolean
  selectedAndroidIds: Set<string>
  onToggleAndroidSelect: (id: string) => void
  onToggleGroupSelect: (ids: string[]) => void
  onReadyKeysChange: (keys: Set<string>) => void
  cardScale: number
  onCardScaleChange: (v: number) => void
  /** 单台投屏窗口大小（%，50~100）；不传则不显示该滑条（MaintenancePage 等场景）。 */
  singleCastScale?: number
  onSingleCastScaleChange?: (v: number) => void
  /** 隐藏「实例名搜索框」（主机 tab 不做实例搜索，2026-09-26 用户要求删除）。 */
  hideSearch?: boolean
  /** 单设备宿主（主机 tab）隐藏「设备下拉筛选」——该场景下设备只有一台，下拉无意义。 */
  hideDeviceFilter?: boolean
  /** 隐藏「标签筛选行」（主机 tab 不做标签筛选：单设备场景下筛选价值低，标签仅作展示与编辑）。 */
  hideTagFilter?: boolean
  /** 卡片布局，由宿主固定（云机 tab = flat，主机 tab = device），组件内不做切换。 */
  layout?: ScreenshotLayout
  /**
   * 「云机展示=全部」设置（宿主层从 useShowOfflineAndroids 读入）：
   * flat 布局据此决定同坑位多容器是否去重——false 只显示每坑位优先级最高的一台
   * （现状）；true 全量渲染（同坑位多台各出一张卡，关机的走 StoppedAndroidCard）。
   */
  showOffline?: boolean
  /**
   * 宿主层提升的标签控制包（标签数据 + 筛选状态）。云机 tab 必传——
   * effectiveAndroids 的标签收窄/全选/批量操作与此同源；不传时组件自持
   * （MaintenancePage 单设备场景，该场景 hideTagFilter=true，无筛选）。
   */
  tagControl?: SlotTagControl
  /** 打开「创建云机」弹窗（宿主层持有弹窗状态）；不传则卡片不显示创建按钮。 */
  onCreateInstance?: (device: DeviceState, slot: number) => void
}

function ScreenshotView({ androids, allAndroids, devices, slotExpiry, mirrors, orientation, hasSelectedDevices, selectedAndroidIds, onToggleAndroidSelect, onToggleGroupSelect, onReadyKeysChange, cardScale, onCardScaleChange, singleCastScale, onSingleCastScaleChange, hideSearch, hideDeviceFilter, hideTagFilter, layout = 'device', tagControl, showOffline = false, onCreateInstance }: Props) {
  const { t } = useTranslation()
  const { mode: deployMode } = useDeployMode()
  const { notes, setNoteLocal } = useAndroidNotes()
  const [screenshotMap, setScreenshotMap] = useState<Record<string, string>>({})
  // 截图版本号渲染镜像（versionRef 的 state 副本）：驱动 imageUrl（asset server 二进制
  // 通道）换 URL —— 版本号变化 → URL 变化 → 浏览器按 URL 取新图/缓存管理。
  // 与 versionRef 同轮更新（写 versionRef 的地方同步写这里）。
  const [versionMap, setVersionMap] = useState<Record<string, number>>({})
  // 截图二进制 URL（asset server 通道，见 device_service.go / screenshot_http.go）：
  // img src 直连 asset server 的 /screenshots/{key}?v={version}，版本号在 URL 里
  // 做缓存失效控制（URL 变 → 浏览器取新图；旧 URL 的缓存自然失效）。版本号取
  // versionMap（versionRef 渲染镜像）；无版本号记录 = 后端无图 → null（回落 dataUrl）。
  // asset server 虚拟域名按平台分支（vendor：assetserver_windows.go / assetserver_linux.go /
  // assetserver_darwin.go）：Windows WebView2 = http://wails.localhost，
  // Linux(WebKitGTK)/macOS(WKWebView) = wails://localhost。
  const imageUrlOf = useCallback((key: string): string | null => {
    const ver = versionMap[key]
    if (!ver) return null
    const base = System.IsWindows() ? 'http://wails.localhost' : 'wails://localhost'
    return `${base}/screenshots/${encodeURIComponent(key)}?v=${ver}`
  }, [versionMap])
  const [statusMap, setStatusMap] = useState<Record<string, CardStatus>>({})
  const [slotCountMap, setSlotCountMap] = useState<Record<string, number>>({}) // deviceId → 坑位数
  const versionRef = useRef<Record<string, number>>({})
  // screenshotMap 的权威副本：非渲染集旧图清理需要「读当前缓存 → 算新缓存 + 清理名单」
  // 一次成型，放进 setState 更新函数里算会带副作用（StrictMode 下更新函数会被调用两次）。
  // 所有写入都经由 applyScreenshotUpdates 走这里，state 只作渲染镜像。
  const screenshotCacheRef = useRef<Record<string, string>>({})
  // 截图请求的 key 级 in-flight 集合：poll 的批拉与「按需补拉」（screenshotOnDemand）
  // 共用同一份，保证同一张图不会同时被两条通道各请求一次。
  const inFlightKeysRef = useRef<Set<string>>(new Set())
  /**
   * 写截图缓存（见 screenshotLru.ts）。updates 中空值表示删除该 key。
   *
   * 清理（evicted）**故意不动 versionRef**，也不动 statusMap：清理只表示「本地没图了」，
   * 后端该版本仍在。清掉版本号会让下一轮 poll 把它判为「未同步」→ 全量重拉，
   * 这就是「每轮都全量取图 + 卡片闪加载中」的抖动来源。缺图改由渲染侧按需补拉
   * （下方 useScreenshotOnDemand）定向解决，一张卡补一张图。
   * 用 useCallback 固定身份：poll effect 与本组件调用的按需补拉 hook 都依赖它。
   */
  // 当前渲染中的运行卡 key 集合（renderedRunningKeys 每次 render 后回填）。写缓存时
  // 作为保护集传入：清理只丢「未被渲染」的旧图（取消勾选/下线残留），正在显示的图
  // 永不淘汰、无上限（用户拍板，见 screenshotLru.ts）。用 ref 中转而非直接依赖 state：
  // applyScreenshotUpdates 必须保持身份稳定（poll effect 与按需补拉 hook 都依赖它），
  // 闭包里读 ref 拿到的永远是最新一轮的渲染集。
  const renderedKeysRef = useRef<Set<string>>(new Set())
  const applyScreenshotUpdates = useCallback((updates: Record<string, string | null | undefined>) => {
    const { next } = writeScreenshotLru(screenshotCacheRef.current, updates, renderedKeysRef.current)
    screenshotCacheRef.current = next
    setScreenshotMap(next)
  }, [])
  // 兜底定时轮询用的 poll 引用（poll 定义在下方 effect 闭包内，通过 ref 供定时器触发）
  const pollRef = useRef<() => void>(() => {})
  // 筛选状态：选中的设备 key（'' = 全部设备），实例名搜索词
  const [selectedDeviceKey, setSelectedDeviceKey] = useState<string>('')
  const [searchKw, setSearchKw] = useState<string>('')
  // 坑位标签（key = 设备ID#坑位号）：与 SlotView 同款，组件内自持 + 事件同步。
  // 云机 tab 由宿主提升传入（tagControl，与 effectiveAndroids 的标签收窄同源）；
  // 未传时自持（MaintenancePage 单设备场景）。
  // 注意：仅筛选数据源；筛选判定已改为设备端 android.tags（useSlotTags 的本地映射
  // 保留仅供「全局已有标签」聚合，写入走 saveAndroidTags 设备端接口）。
  const localTag = useSlotTags()
  const { tags: slotTags } =
    tagControl
      ? { tags: tagControl.slotTags }
      : localTag
  // 标签筛选（选中集合 / 全局标签 / 失效标签自动收敛）：与列表模式共用同一套语义。
  // hook 必须无条件调用（React 规则），传入 tagControl 时其返回值仅被忽略。
  // 2026-10-27：筛选数据源改为「当前视图容器」的 android.tags 去重（标签唯一权威来源
  // 是设备端容器，本地坑位映射已停止消费）；allTags 聚合 / 失效收敛 / 判定同源，
  // 否则选中一个仅存在于容器 tags 的标签会被收敛 effect 误删（勾选后立即弹回）。
  // 标签乐观回填已下沉 Go 端（SetAndroidLabels 成功后补丁缓存并推
  // androidlist:updated），前端 setAndroidList 内容比对命中 → androids props 换新 →
  // 卡片（memo 按 android.tags 内容浅比）与浮层即时收敛，无需本地版本号（2026-10-30：
  // 旧 androidListVersion 方案只 bump 本地 state、props 引用不变，memo 跳过重渲染，
  // 清空标签后 UI 停留旧值）。
  const viewContainerTags = useMemo(
    () => collectAndroidTags(layout === 'flat' ? androids : allAndroids),
    [layout, androids, allAndroids],
  )
  const localFilter = useSlotTagFilter(slotTags, viewContainerTags)
  const { allTags, selectedTags, toggleTag, clearTags, hasFilterBar } =
    tagControl ? tagControl.filter : localFilter
  // 标签编辑浮层目标
  const [tagEditor, setTagEditor] = useState<{ indexNum: number; x: number; y: number; android: AndroidInfo } | null>(null)

  // 投屏退出守卫：Go 端发射 screencast:exited（窗口 X 关闭/显式停止/刷新替换都会发），
  // 记录时刻供 ScreenshotCard 丢弃关窗后 500ms 内的杂散点击（误开下层卡片投屏）
  useEffect(() => {
    const off = onEvent('screencast:exited', () => markCastExit())
    return off
  }, [])

  // 截图状态变化时通知父组件哪些实例就绪。
  // 必须做内容比对后再上报：setReadyKeys 写的是 HostManagement 的 state，
  // 无条件传新 Set 会让父页每轮（截图 1s 一轮）全量重渲染，并连带把
  // androids 引用换掉 → 本组件 groupedAndroids/filteredGroups 全部重算。
  const readyKeysRef = useRef<Set<string>>(new Set())
  // 模式切换时父组件会自行把 readyKeys 清空（useHostManagement 的 deployMode effect），
  // 那是本组件看不到的一次外部改动 —— 必须同步重置比对基线，否则新模式下若就绪集合
  // 恰好与旧模式相同，会被判为"无变化"而漏报，批量投屏按钮永远点不动。
  useEffect(() => {
    readyKeysRef.current = new Set()
  }, [deployMode])
  useEffect(() => {
    const ready = new Set<string>()
    for (const [key, status] of Object.entries(statusMap)) {
      if (status === 'screenshot') ready.add(key)
    }
    const prev = readyKeysRef.current
    if (prev.size === ready.size && [...ready].every(k => prev.has(k))) return
    readyKeysRef.current = ready
    onReadyKeysChange(ready)
    // 依赖含 deployMode：模式切换后强制重评估一次，配合上面的基线重置补报，
    // 保证即使 statusMap 未发生迁移也不会把就绪集合漏报给父组件。
  }, [statusMap, onReadyKeysChange, deployMode])

  // 版本号对比：纯事件驱动，后端按轮合批推送。
  // in-flight 守卫：一轮内的多个事件只跑一次实际查询，结束后补跑一次收敛迟到变化。
  // 不加守卫时 N 张图变化 → N 个并发 poll，且 versionRef 在取回数据前不更新，
  // 每个 poll 都把同样的 key 判为未同步 → N 次全量取图（O(N²) IPC 数据量），
  // 设备多+画面持续变化时内存被 IPC 载荷与临时字符串打爆。
  useEffect(() => {
    let polling = false
    let pending = false
    const poll = async () => {
      if (polling) { pending = true; return }
      polling = true
      try {
        const versions = await ScreenshotService.GetScreenshotVersions() as Record<string, number>
        if (!versions) return
        const changed: string[] = []
        for (const [key, ver] of Object.entries(versions)) {
          if (versionRef.current[key] !== ver) changed.push(key)
        }
        const pruned: string[] = []
        for (const key of Object.keys(versionRef.current)) {
          if (!(key in versions)) { delete versionRef.current[key]; pruned.push(key) }
        }
        if (pruned.length > 0) {
          // 后端缓存已无此 key（取消勾选/清理）：同步释放截图与状态，避免残留
          const removals: Record<string, null> = {}
          for (const key of pruned) removals[key] = null
          applyScreenshotUpdates(removals)
          setStatusMap(prev => { const next = { ...prev }; for (const key of pruned) delete next[key]; return next })
        }
        if (changed.length > 0) {
          // 双通道分流（99 台卡顿根因修复：dataURL 通道退役，imageUrl 二进制通道接管）：
          //  - 已有版本号的 key → 纯 URL 通道：只更新版本号镜像（URL 的 v 参数变了 →
          //    img src 变 → 浏览器按新 URL 取图），零 IPC 载荷、不写 screenshotMap
          //    （dataUrl 静止保留，仅在 URL 通道未建立时兜底）；
          //  - 无版本号的 key（首图/后端缓存刚建立）→ GetScreenshots 拉 dataURL 兜底
          //    一次并记下版本号，之后该 key 永久走 URL 通道。
          const urlKeys: string[] = []
          const inFlight = inFlightKeysRef.current
          // 与按需补拉共享 key 级 in-flight 守卫：正被按需通道请求的 key 本轮跳过
          // （否则同一张图两份 IPC 载荷；跳过也不丢数据，下一轮事件会再收敛）。
          const fetchKeys: string[] = []
          for (const key of changed) {
            if (versionRef.current[key]) urlKeys.push(key)
            else if (!inFlight.has(key)) fetchKeys.push(key)
          }
          if (urlKeys.length > 0) {
            // 版本号镜像批量化：循环只收集，循环后一次 setState（逐 key setState 会让
            // N 张图变化 → N 次重渲染）
            const nextVersions: Record<string, number> = {}
            for (const key of urlKeys) {
              versionRef.current[key] = versions[key]
              nextVersions[key] = versions[key]
            }
            setVersionMap(prev => ({ ...prev, ...nextVersions }))
          }
          if (fetchKeys.length > 0) {
            const keys = fetchKeys
            for (const key of keys) inFlight.add(key)
            let data: Record<string, string> | null | undefined
            try {
              data = await ScreenshotService.GetScreenshots(keys) as Record<string, string>
            } finally {
              for (const key of keys) inFlight.delete(key)
            }
            // 走 LRU 写入（写入即「最近使用」；渲染中的图永不淘汰，非渲染旧图被清理）
            const updates: Record<string, string | null> = {}
            for (const key of keys) updates[key] = data?.[key] ?? null
            applyScreenshotUpdates(updates)
            setStatusMap(prev => {
              const next = { ...prev }
              for (const key of keys) {
                if (data?.[key]) next[key] = 'screenshot'
                else if (!prev[key]) next[key] = 'loading'
              }
              return next
            })
            const gotVersions: Record<string, number> = {}
            for (const key of keys) {
              if (data?.[key]) {
                versionRef.current[key] = versions[key]
                gotVersions[key] = versions[key]
              }
            }
            if (Object.keys(gotVersions).length > 0) {
              // 版本号镜像批量化（见 versionMap 声明处注释）
              setVersionMap(prev => ({ ...prev, ...gotVersions }))
            }
          }
        }
      } catch { /* 失败不致命：下一轮事件会重试 */ } finally {
        polling = false
        if (pending) { pending = false; poll() }
      }
    }
    poll()
    pollRef.current = poll
    const off1 = Events.On('screenshot:updated', () => { poll() })
    // 回调收到的是事件对象而非裸 key（Wails v3 语义），与 GroupControlPage 同款取法
    const off2 = Events.On('screenshot:invalidated', (ev: any) => {
      const key = ev?.data ?? ev
      delete versionRef.current[key]
      setVersionMap(prev => {
        if (!(key in prev)) return prev
        const next = { ...prev }; delete next[key]; return next
      })
      applyScreenshotUpdates({ [key]: null })
      setStatusMap(prev => { const next = { ...prev }; delete next[key]; return next })
    })
    return () => { off1(); off2() }
    // applyScreenshotUpdates 只读写 ref（缓存镜像/版本号）并调 setState，身份变化无副作用，
    // 故不列入依赖（组件每轮截图推送都会重渲染，列进去会让事件订阅反复解绑重绑）。
  }, [])

  // 兜底定时轮询：截图刷新纯靠 screenshot:updated 事件驱动，事件流断了
  // （后端轮询协程异常/事件丢失）无截图的卡片会永久卡在「开机中」。每 10s
  // 检查一次：只要还有 running 卡片没有 'screenshot' 记录就触发一次 poll，
  // 复用上方 in-flight/pending 守卫（不绕过限流），有图后条件自然失效。
  const needsFallbackRef = useRef(false)
  useEffect(() => {
    needsFallbackRef.current = androids.some(a =>
      isRunningStatus(a.status) &&
      statusMap[`${a.ip}_${a.indexNum}`] !== 'screenshot'
    )
  }, [androids, statusMap])
  useEffect(() => {
    const timer = setInterval(() => {
      if (needsFallbackRef.current) pollRef.current()
    }, 10_000)
    return () => clearInterval(timer)
  }, [])

  // 设备分组 key：本地模式=设备 IP，公网模式=设备 ID。
  // 用 IP/ID 而非 deviceId，因为标题要显示 IP（本地）/ID（公网），与 DeviceListItem 一致。
  const groupKeyOf = (a: AndroidInfo): string => deployMode === 'public' ? a.deviceId : a.ip

  // 按设备分组：deviceKey → 实例列表。组内按坑位号升序，保证关机实例占住自己的坑位位置。
  // 组间按分组键排序：本地模式按 IP 数值序（compareIP），公网模式按设备 ID 字符串序。
  const groupedAndroids = useMemo(() => {
    const map = new Map<string, AndroidInfo[]>()
    for (const a of androids) {
      const k = groupKeyOf(a)
      const arr = map.get(k)
      if (arr) arr.push(a)
      else map.set(k, [a])
    }
    for (const arr of map.values()) arr.sort((a, b) => a.indexNum - b.indexNum)
    // 对分组键排序，避免后端 androidlist:updated 推送顺序变化导致设备分组乱跳
    const sortedEntries = [...map.entries()].sort(([keyA], [keyB]) =>
      deployMode === 'public' ? (keyA < keyB ? -1 : keyA > keyB ? 1 : 0) : compareIP(keyA, keyB)
    )
    return new Map(sortedEntries)
  }, [androids, deployMode])

  // devices.find 改 Map（99 台场景：每次渲染对每台卡做 O(n) 线性查找 → O(1)）
  const deviceById = useMemo(() => new Map(devices.map(d => [d.device_id, d])), [devices])

  // 下拉框设备选项：有实例的设备列表（去重，保持顺序）。
  const deviceOptions = useMemo(() => Array.from(groupedAndroids.keys()), [groupedAndroids])

  // 设备顺序表（groupKey → 序号）：flat 布局「按设备聚块」的第一比较键。
  // 直接取 groupedAndroids 的键序（local 按 IP 数值序 / public 按设备 ID 字符串序，
  // 与 hostManagementUtils.compareAndroidByDeviceThenSlot 同口径），不再重复实现设备比较。
  const deviceOrder = useMemo(() => {
    const order = new Map<string, number>()
    let i = 0
    for (const key of groupedAndroids.keys()) order.set(key, i++)
    return order
  }, [groupedAndroids])

  // 坑位数只取决于「参与分组的设备集合」，与截图内容无关：
  // 依赖必须用设备 ID 签名，不能挂 groupedAndroids 引用——截图每推一轮，
  // 本组件就重渲染并算出新的 Map，旧实现于是每轮再发一轮 GetSlotCount，
  // 且在 for 循环里串行 await（115 台 = 115 次串行跨进程往返），
  // 这是"设备一多就卡"的主因之一。改签名依赖 + 并行 + 内容比对后不再自激。
  const slotCountDeviceIds = useMemo(() => {
    const ids = new Set<string>()
    for (const group of groupedAndroids.values()) {
      const id = group[0]?.deviceId
      if (id) ids.add(id)
    }
    return [...ids].sort()
  }, [groupedAndroids])
  const slotCountSignature = slotCountDeviceIds.join(',')
  useEffect(() => {
    if (!slotCountSignature) return
    let cancelled = false
    const ids = slotCountSignature.split(',')
    Promise.all(ids.map(async id => {
      try {
        return [id, await SlotService.GetSlotCount(id) as number] as const
      } catch {
        return null // 单台失败不致命：该设备不补空坑位占位
      }
    })).then(pairs => {
      if (cancelled) return
      const next: Record<string, number> = {}
      for (const p of pairs) if (p) next[p[0]] = p[1]
      setSlotCountMap(prev => {
        const prevIds = Object.keys(prev)
        if (prevIds.length !== Object.keys(next).length) return next
        for (const id of prevIds) if (prev[id] !== next[id]) return next
        return prev
      })
    })
    return () => { cancelled = true }
  }, [slotCountSignature])

  // 切换设备或实例集变化时，若选中的设备已不在列表，重置为全部。
  useEffect(() => {
    if (selectedDeviceKey && !deviceOptions.includes(selectedDeviceKey)) {
      setSelectedDeviceKey('')
    }
  }, [deviceOptions, selectedDeviceKey])

  // 最终展示的分组。三种布局：
  //  device = 按设备分组（每台设备一组，含坑位占位）
  //  tag    = 按标签分组（一台云机可出现在多个标签组；不打标签的归入「未打标签」）
  //  flat   = 不分类平铺（调用方按设备顺序连续排列）
  // 标签筛选（selectedTags）与实例名搜索（searchKw）对三种布局一致生效。
  const filteredGroups = useMemo(() => {
    const kw = searchKw.trim().toLowerCase()
    const tagFilterActive = selectedTags.size > 0

    const passTagFilter = (a: AndroidInfo) => {
      if (!tagFilterActive) return true
      const list = a.tags ?? []
      return list.some(tag => selectedTags.has(tag))
    }

    // 收集一组云机的卡片 + 坑位占位（一坑位一卡、开机优先、按坑位去重）
    const collect = (list: AndroidInfo[], deviceId: string, withSlots: boolean) => {
      const total = slotCountMap[deviceId] || 0
      const cards: GroupCardData[] = []
      const runningSlots = new Set<number>()   // 已有开机实例的坑位
      const usedSlots = new Set<number>()      // 有实例（开/关机）的坑位，避免被补全到末尾
      const offlineSlotSet = new Set<number>()  // 仅关机实例的坑位（一坑位一占位，去重）
      const offlineStatus = new Map<number, string>() // 坑位 → 占位实例的容器状态（占位卡显示真实状态文案）
      for (const a of list) {
        // 与坑位模式（SlotView）一致：超出设备坑位数的实例不渲染
        // （设备 API 可能返回非标准容器，如测试容器；total=0 表示坑位数未加载，不过滤避免闪空）
        if (total > 0 && a.indexNum > total) continue
        usedSlots.add(a.indexNum)
        // 搜索：实例名 / 备注（androidNotes，key = 容器 id）命中任一即保留
        if (kw && !a.name.toLowerCase().includes(kw) && !(notes[a.id] || '').toLowerCase().includes(kw)) continue
        if (!passTagFilter(a)) continue
        // starting 也按开机渲染：开机操作后乐观状态是 starting（状态意图保护期间），
        // 若不算 running 会把启动中的实例渲染成关机占位卡，"启动中"卡片消失
        const isRunning = isRunnableStatus(a.status)
        if (isRunning) {
          // 同坑位多条数据防御（后端/迁移中间态万一重复）：一坑位一卡，保留第一条并留诊断线索
          if (runningSlots.has(a.indexNum)) {
            console.warn('[ScreenshotView] 同坑位重复实例，已跳过:', deviceId, 'slot', a.indexNum, a.id, a.name, a.status)
            continue
          }
          runningSlots.add(a.indexNum)
          const key = `${a.ip}_${a.indexNum}`
          // statusMap 无值时按容器真实状态推导（与 flat 分支同语义，cardStatusFromAndroid：
          // starting→starting、created→created、restarting/resetting/removing→对应态，
          // running 无截图兜底 loading「加载中」）
          cards.push({
            android: a,
            // 缺图（本地无缓存，如 poll 失败写 null）= 这里为 null，卡片显示「加载中」；对应的定向补拉由
            // 上方 missingScreenshotKeys → useScreenshotOnDemand 负责（渲染期不发请求）
            dataUrl: screenshotMap[key] || null,
            imageUrl: imageUrlOf(key),
            status: statusMap[key] ?? cardStatusFromAndroid(a.status),
            note: notes[a.id] || '',
          })
        } else {
          // 仅当该坑位没有开机实例时才记为关机占位（一坑位一格，开机优先）；
          // 记录占位实例状态（dead「异常退出」/ created「已创建」等占位卡文案用）
          if (!runningSlots.has(a.indexNum)) {
            offlineSlotSet.add(a.indexNum)
            offlineStatus.set(a.indexNum, a.status)
          }
        }
      }
      // 有开机实例的坑位从关机占位中剔除（避免同坑位既显示开机卡又显示占位）
      for (const s of runningSlots) offlineSlotSet.delete(s)
      const offlineSlots = [...offlineSlotSet].sort((a, b) => a - b)
      // 补全真空闲坑位占位：搜索或标签筛选激活时不补——用户在看具体实例，占位卡只会干扰
      const emptySlots: number[] = []
      if (withSlots && !kw && !tagFilterActive) {
        for (let i = 1; i <= total; i++) {
          if (!usedSlots.has(i)) emptySlots.push(i)
        }
      }
      return { cards, emptySlots, offlineSlots, offlineStatus }
    }

    type Row = { deviceKey: string; cards: GroupCardData[]; emptySlots: number[]; offlineSlots: number[]; offlineStatus: Map<number, string>; deviceId: string }
    const result: Row[] = []

    for (const [deviceKey, groupAndroids] of groupedAndroids) {
      if (selectedDeviceKey && deviceKey !== selectedDeviceKey) continue
      const deviceId = groupAndroids[0]?.deviceId || ''
      const { cards, emptySlots, offlineSlots, offlineStatus } = collect(groupAndroids, deviceId, true)
      if (cards.length > 0 || emptySlots.length > 0 || offlineSlots.length > 0) {
        result.push({ deviceKey, cards, emptySlots, offlineSlots, offlineStatus, deviceId })
      }
    }
    return result
  }, [groupedAndroids, selectedDeviceKey, searchKw, screenshotMap, statusMap, notes, slotCountMap, selectedTags, imageUrlOf])

  // 平铺布局（云机 Tab）的可见集合：直接对 androids 过滤（搜索/标签），**不按坑位对齐**——
  // running/starting 出正常卡，关机实例出关机卡（StoppedAndroidCard）。
  // 排序：**① 运行中（running/starting）优先 → ② 按设备聚块 → ③ 坑位号升序 → ④ 显示名
  // （备注||名称）**。即所有开机卡整体排在前面，关机卡排在后面；两个区段内部各自按设备
  // 聚块（不同主机的卡绝不相邻交错）、再按坑位号、再按名称。设备序沿用 groupedAndroids
  // 的键序（local 按 IP 数值序 / public 按设备 ID 字符串序，与
  // compareAndroidByDeviceThenSlot 同口径），见 deviceOrder。
  // device 布局（主机 Tab）走 filteredGroups 的 collect()（坑位对齐 + 空坑位补全），两布局各管各的。
  //
  // 同坑位去重（一坑位一卡）：创建云机会在已有 running 容器的坑位上再建一个 created
  // 容器（「选中坑位已有运行实例，新建云机默认不开机」），Go 侧 Merge 不去重（备份
  // 场景同坑位多容器合法），flat 若不去重会把同坑位的多条全渲染成多张 #4 卡
  // （2026-10-15「创建坑位4 出现 4 个坑位4 在启动」的根因之一）。去重优先级：
  // running/starting > created > stopped/exited——用户关心的是「这个坑位现在的状态」。
  // **仅「云机展示=运行中」时去重**：showOffline=true（「全部」）用户显式要求看到所有
  // 坑位下的所有云机（含同坑位的关机备份实例），去重会把这些卡丢掉（2026-10-17 bug）。
  const visibleFlat = useMemo(() => {
    if (layout !== 'flat') return []
    const kw = searchKw.trim().toLowerCase()
    const tagFilterActive = selectedTags.size > 0
    const displayName = (a: AndroidInfo) => notes[a.id] || a.name
    const isRunning = (a: AndroidInfo) => isRunnableStatus(a.status)
    // 去重优先级：running/starting > created > stopped/exited——用户关心的是「这个坑位现在的状态」
    const priority = (a: AndroidInfo) => (isRunning(a) ? 3 : a.status === 'created' ? 2 : 1)
    const filtered = androids.filter(a => {
      // 搜索：实例名 / 备注（androidNotes，key = 容器 id）命中任一即保留
      if (kw && !a.name.toLowerCase().includes(kw) && !(notes[a.id] || '').toLowerCase().includes(kw)) return false
      if (!tagFilterActive) return true
      const list = a.tags ?? []
      return list.some(tag => selectedTags.has(tag))
    })
    // 去重：同 deviceId+indexNum 只保留优先级最高的一条（并列时保留先出现的，稳定）。
    // showOffline=true 跳过去重：「全部」是用户显式要求看到同坑位的所有容器
    // （含关机备份实例各出一张卡，2026-10-17 bug）；false（默认「运行中」）必须去重——
    // 创建云机场景同坑位会同时存在 running（原）+ created（新）合法数据，
    // 不去重会短暂闪出多张卡（2026-10-17 二次回归）。
    const best = new Map<string, AndroidInfo>()
    if (!showOffline) {
      for (const a of filtered) {
        const k = `${a.deviceId}#${a.indexNum}`
        const prev = best.get(k)
        if (!prev || priority(a) > priority(prev)) best.set(k, a)
      }
    }
    const deduped = showOffline ? filtered : [...best.values()]
    // 排序（两分支共用同一比较器）：
    //   ① isRunning 降序——运行中（running/starting）的卡整体排前，关机卡整体排后；
    //   ② deviceOrder 升序——同状态内按设备聚块，不同主机的卡不相邻交错；
    //   ③ indexNum 升序——设备内的坑位自然顺序；
    //   ④ 显示名（备注||名称）localeCompare。
    // 设备键与 groupedAndroids 同口径（groupKeyOf），序号取 deviceOrder；未知键（理论上
    // 不会出现）排最后并按 key 兜底。
    return deduped.sort((a, b) => {
      const aRun = isRunning(a)
      const bRun = isRunning(b)
      if (aRun !== bRun) return aRun ? -1 : 1
      const aKey = groupKeyOf(a)
      const bKey = groupKeyOf(b)
      if (aKey !== bKey) {
        const aOrder = deviceOrder.get(aKey)
        const bOrder = deviceOrder.get(bKey)
        if (aOrder !== undefined && bOrder !== undefined && aOrder !== bOrder) return aOrder - bOrder
        if (aOrder === undefined && bOrder !== undefined) return 1
        if (aOrder !== undefined && bOrder === undefined) return -1
        return aKey < bKey ? -1 : aKey > bKey ? 1 : 0
      }
      if (a.indexNum !== b.indexNum) return a.indexNum - b.indexNum
      return displayName(a).localeCompare(displayName(b), 'zh-Hans-CN')
    })
  }, [layout, androids, searchKw, selectedTags, notes, showOffline, deployMode, deviceOrder])

  // ── 按需补拉（缓存缺图的缺口，见 screenshotOnDemand.ts）────────────────────
  // 「当前实际渲染的运行中卡片」key：device 布局取 filteredGroups 的 cards（已过搜索/标签
  // 筛选、仅含开机实例），flat 布局取 visibleFlat 里的运行中项。与下面两处渲染分支同源，
  // 保证只补「用户真看得到的卡」，而不是把所有缺失的 key 一次性拉回来。
  const renderedRunningKeys = useMemo(() => {
    if (layout === 'flat') {
      return visibleFlat.filter(a => isRunnableStatus(a.status)).map(a => `${a.ip}_${a.indexNum}`)
    }
    const keys: string[] = []
    for (const group of filteredGroups) {
      for (const card of group.cards) keys.push(`${card.android.ip}_${card.android.indexNum}`)
    }
    return keys
  }, [layout, visibleFlat, filteredGroups])
  // 渲染集回填 ref（每次 render 同步）：applyScreenshotUpdates 的写入从 ref 读
  // 「当前正在渲染的 key」作保护集，见上方 renderedKeysRef 注释。
  useEffect(() => {
    renderedKeysRef.current = new Set(renderedRunningKeys)
  }, [renderedRunningKeys])

  // 卡片公共 props（除每张卡片独立的 android/dataUrl/status/note/selected 外）。
  const isPortrait = orientation === 'portrait'
  // 截图区容器比例 = **本页主流截图比例**（useDominantAspect 按 <img> onLoad 的真实尺寸统计），
  // 图片一律 object-cover：比例与容器一致时零黑边零裁切，个别不同分辨率的 ROM 极轻微裁切。
  // 为什么不能写死 9:16：V4(tobox) 的 ROM 是 9:20（720x1600），9:20 的图放进 9:16 容器用
  // object-contain 会按高度铺满、宽度不足 → 左右各一条黑边（用户反馈的「左右黑边」）。
  // 横屏展示的是同一批竖屏截图的 CSS 旋转，容器比例取竖屏比例的倒数（宽/高）。
  // 兜底按模式给：V4 用 9:20（主流 tobox ROM），其余用 9:16（V3 的 720x1280 一族）。
  const fallbackPortraitRatio = deployMode === 'v4' ? 9 / 20 : 9 / 16
  const [portraitRatio, reportImageSize] = useDominantAspect(fallbackPortraitRatio, deployMode)
  const aspectRatio = isPortrait ? portraitRatio : 1 / portraitRatio
  // 卡片宽度映射：scale = 滑条值/100（范围 25~150 step 5，默认 80，源码 el-slider）。
  // 竖屏列宽沿用 android-client CloudManagement.vue 的 150px 基准；横屏**不能**沿用固定的
  // 200px 基准——横屏容器比例是竖屏比例的倒数，同一列宽下截图区高度只剩竖屏的
  // portraitRatio² 倍（V4 9:20 ≈ 1/4.9），即「切横屏后卡片小一大圈」的根因。
  // 横屏改为「按目标高度反推列宽 + 按可用宽度封顶」，推导见 screenshotDetail/cardGridWidth.ts。
  const scale = cardScale / 100
  // 网格可用宽度（0 = 尚未测量到）：横屏列宽封顶用。挂在滚动容器上——device 布局的
  // 各分组网格与 flat 布局的平铺网格都在这一层之内，宽度同源。
  const gridRef = useRef<HTMLDivElement | null>(null)
  const [gridWidth, setGridWidth] = useState(0)
  useEffect(() => observeWidth(gridRef.current, setGridWidth), [androids.length])
  const cardMinPx = cardMinWidthPx({ isPortrait, portraitRatio, scale, availablePx: gridWidth })

  // ── 视口激活（仅 flat 布局，2026-11 视口优化）─────────────────────────────
  // IntersectionObserver 观察 flat 网格卡片（data-key 标记，见渲染分支），维护
  // 「视口内（含 200px 预加载带）」的截图 key 集合。两个消费点：
  //  1) renderedKeysRef（LRU 保护集）只放视口内 key —— 视口外旧图被
  //     writeScreenshotLru 清理（内存随视口收敛，几百张卡不再全量常驻）；
  //  2) 按需补拉只补视口内缺图（missingScreenshotKeys 的 filter）——滚入时再拉，
  //     滚入瞬间显示上一张回落图/加载中（ScreenshotCard 回落机制已覆盖，不闪）。
  // visibleKeys 为空集（observer 尚未首次回调）时保护集退回全集：避免首帧保护集
  // 为空把刚取回的图全清掉；首次 intersection 回调几乎立即到达，空窗可忽略。
  // device 布局不做（卡片少：单设备场景，保护集本来就不大，见回传说明）。
  const flatGridRef = useRef<HTMLDivElement | null>(null)
  const visibleKeys = useVisibleScreenshotKeys(gridRef, flatGridRef, [layout === 'flat', visibleFlat.length, cardMinPx])
  useEffect(() => {
    if (layout !== 'flat') return
    const viewport = visibleKeys.size > 0 ? visibleKeys : null
    const guarded = new Set<string>()
    for (const key of renderedRunningKeys) {
      if (viewport === null || viewport.has(key)) guarded.add(key)
    }
    renderedKeysRef.current = guarded
  }, [layout, renderedRunningKeys, visibleKeys])

  // ── 按需补拉（缓存缺图的缺口，见 screenshotOnDemand.ts）────────────────────
  // statusMap 已是 'screenshot'（后端确有截图）但本地无图（poll 失败写 null 等）→ 需要定向补拉。
  // flat 布局：只补视口内缺图（visibleKeys 过滤）——视口外滚入时 visibleKeys 变化 →
  // 本 memo 重算 → 自然补拉。渲染期只负责「算出缺什么」，请求统一在 effect 里发
  // （合批 + in-flight 去重 + 冷却）
  const missingScreenshotKeys = useMemo(
    () => collectMissingScreenshotKeys(
      renderedRunningKeys.filter(k => layout !== 'flat' || visibleKeys.has(k)),
      statusMap,
      screenshotMap,
    ),
    [layout, renderedRunningKeys, visibleKeys, statusMap, screenshotMap],
  )
  useScreenshotOnDemand({
    missingKeys: missingScreenshotKeys,
    inFlightKeys: inFlightKeysRef,
    applyScreenshots: applyScreenshotUpdates,
  })

  const updateNote = useCallback((id: string, value: string) => {
    setNoteLocal(id, value)
  }, [setNoteLocal])

  // 必须 useCallback：cardCommon 是 ScreenshotGroup/ScreenshotCard 的 memo 比较依据，
  // 内联箭头函数会让 cardCommon 每轮 render 都是新引用 → 截图每秒一轮时全部卡片重渲染
  const openTagEditor = useCallback((x: number, y: number, android: AndroidInfo) => {
    setTagEditor({ indexNum: android.indexNum, x, y, android })
  }, [])

  const openCreateInstance = useCallback((device: DeviceState, slot: number) => {
    onCreateInstance?.(device, slot)
  }, [onCreateInstance])

  const cardCommon: Omit<ScreenshotCardProps, 'android' | 'dataUrl' | 'status' | 'note' | 'selected' | 'deviceInfo' | 'slotExpiry' | 'deviceId'> = useMemo(() => ({
    androids,
    allAndroids,
    devices,
    mirrors,
    aspectRatio,
    isPortrait,
    onNoteChange: updateNote,
    onToggleSelect: onToggleAndroidSelect,
    clickToCast: true,
    // 标签 chip 显示源已切 android.tags（设备端同源，见 CardHeader），不再传 slotTags
    onEditTags: openTagEditor,
    onCreateInstance: openCreateInstance,
    onImageLoad: reportImageSize,
    scale,
  }), [androids, allAndroids, devices, mirrors, aspectRatio, isPortrait, onToggleAndroidSelect, updateNote, openTagEditor, openCreateInstance, reportImageSize, scale])

  if (androids.length === 0) {
    return <div className="text-gray-400 text-center mt-10">{hasSelectedDevices ? t('host:empty.noInstances') : t('host:empty.selectDeviceForScreenshot')}</div>
  }

  return (
    <div className="flex flex-col h-full">
      {/* 筛选区：设备下拉框（单选+全部，单设备宿主隐藏）+ 实例名搜索框 */}
      <div className="flex items-center gap-3 px-1 pb-3 pt-1">
        {!hideDeviceFilter && (
          <Select
            value={selectedDeviceKey}
            onChange={setSelectedDeviceKey}
            placeholder={t('host:screenshot.allDevices')}
            options={[
              { value: '', label: t('host:screenshot.allDevices') },
              ...deviceOptions.map(k => ({ value: k, label: k })),
            ]}
            className="w-[200px]"
          />
        )}
        {!hideSearch && (
          <>
            <input
              type="text"
              className="w-56 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-700 outline-none placeholder:text-gray-400 focus:border-blue-400"
              placeholder={t('host:screenshot.searchInstance')}
              value={searchKw}
              onChange={e => setSearchKw(e.target.value)}
            />
            {/* 标签下拉多选（2026-10-27，云机 Tab flat 布局）：紧挨搜索框右侧。
                数据源 = 当前视图容器的 android.tags 去重；筛选语义仍是宿主的
                selectedTags（或语义）。可见性口径与原平铺行一致：无标签可筛且
                无选中时按钮隐藏，避免「点了没东西」的死入口。 */}
            {layout === 'flat' && (hasFilterBar || selectedTags.size > 0) && (
              <TagFilterDropdown
                allTags={allTags}
                selected={selectedTags}
                onToggle={toggleTag}
                onSelectAll={() => { for (const tag of allTags) if (!selectedTags.has(tag)) toggleTag(tag) }}
                onClear={clearTags}
                /* 打开即拉一次设备端标签字典（GET /android/tags）：视图容器集合未覆盖
                   的标签（其他客户端打的 / 事件未带回的）也进下拉选项。 */
                onOpen={tagControl?.refreshTagDict}
              />
            )}
          </>
        )}
        {/* 窗口大小滑动条（从 ActionBar 移到筛选区）+ 单台投屏大小（持久化到 Go config）*/}
        <div className="ml-auto flex items-center gap-2">
          <span className="text-[11px] text-gray-400 whitespace-nowrap" title={t('host:actionBar.windowSizeTitle')}>{t('host:actionBar.windowSize')}</span>
          <input
            type="range" min={25} max={150} step={5}
            value={cardScale}
            onChange={e => onCardScaleChange(Number(e.target.value))}
            className="w-24 h-1.5 accent-blue-500 cursor-pointer"
          />
          <span className="text-[11px] text-gray-500 w-8 text-right">{cardScale}%</span>
          {singleCastScale !== undefined && onSingleCastScaleChange && (
            <>
              <span className="text-[11px] text-gray-400 whitespace-nowrap ml-2" title={t('host:actionBar.singleCastSizeTitle')}>{t('host:actionBar.singleCastSize')}</span>
              <input
                type="range" min={35} max={100} step={1}
                value={singleCastScale}
                onChange={e => onSingleCastScaleChange(Number(e.target.value))}
                className="w-24 h-1.5 accent-blue-500 cursor-pointer"
              />
              <span className="text-[11px] text-gray-500 w-8 text-right">{singleCastScale}%</span>
            </>
          )}
        </div>
      </div>

      {/* 标签筛选行（device 布局 = 主机 Tab 保留平铺 chips；flat 已改为搜索框右侧下拉）。 */}
      {layout === 'device' && !hideTagFilter && hasFilterBar && (
        <TagFilterBar allTags={allTags} selected={selectedTags} onToggle={toggleTag} onClear={clearTags} />
      )}

      {/* 实例列表：layout=device 按设备分组（每组设备头+全选+坑位占位）；flat 不分类平铺（连续网格） */}
      <div ref={gridRef} className="flex-1 overflow-auto p-1">
        {filteredGroups.length === 0 && visibleFlat.length === 0 ? (
          <div className="text-gray-400 text-center mt-10">{t('host:screenshot.noMatch')}</div>
        ) : layout === 'device' ? (
          filteredGroups.map(({ deviceKey, cards, emptySlots, offlineSlots, offlineStatus, deviceId }) => (
            <ScreenshotGroup
              key={deviceKey}
              groupTitle={deviceKey}
              cards={cards}
              emptySlots={emptySlots}
              offlineSlots={offlineSlots}
              offlineStatus={offlineStatus}
              groupDeviceId={deviceId}
              cardCommon={cardCommon}
              deviceById={deviceById}
              slotExpiry={slotExpiry}
              selectedAndroidIds={selectedAndroidIds}
              onToggleAndroidSelect={onToggleAndroidSelect}
              onToggleGroupSelect={onToggleGroupSelect}
              onNoteChange={updateNote}
              cardMinPx={cardMinPx}
              cardScaleVar={scale}
            />
          ))
        ) : (
          // 不分类平铺：全部实例连续排列成一张网格，
          // **不按坑位对齐**（2026-10-15 用户要求：去掉坑位混排/占位对齐）——
          // running/starting 出正常卡（ScreenshotCard），关机实例出关机卡（StoppedAndroidCard）。
          // 顺序由 visibleFlat 排序决定（运行中整体在前，各自内部按设备聚块，见上方比较器注释）。
          // 视口激活（仅 flat）：每张卡外套 data-key 容器供 IntersectionObserver 观察，
          // 视口内的 key 进 LRU 保护集 + 按需补拉（见 renderedKeysRef/missingScreenshotKeys）。
          // ScreenshotCard 根是 fragment（多子元素），不能直接做观察目标；容器自身无样式，
          // 不影响网格布局（grid 子元素由它顶替，内部卡撑满）。
          <div
            ref={flatGridRef}
            className="grid gap-3"
            style={{ ['--card-scale' as string]: scale, gridTemplateColumns: `repeat(auto-fill, minmax(min(${cardMinPx}px, 100%), 1fr))`, gap: `calc(10px * var(--card-scale))` }}
          >
            {visibleFlat.map(a => {
              const isRunning = isRunnableStatus(a.status)
              if (isRunning) {
                const key = `${a.ip}_${a.indexNum}`
                // statusMap 无值时按容器真实状态推导（cardStatusFromAndroid，与 device
                // 分支同语义）：starting/created/restarting/resetting/removing 各显对应文案，
                // 不再一律兜底 starting/loading——兜底 starting 会让「截图未就绪的 running
                // 容器」全部显示启动中 spinner（多台设备同坑位时即「N 个坑位N 在启动」的视觉来源）。
                const cardStatus: CardStatus = statusMap[key] ?? cardStatusFromAndroid(a.status)
                // 同 device 分支：缺图（本地无缓存）时 dataUrl=null → 卡片「加载中」，
                // 补拉走上方 missingScreenshotKeys 的按需通道，这里不发请求。
                return (
                  <div key={a.id} data-key={key}>
                  <ScreenshotCard
                    {...cardCommon}
                    compact
                    android={a}
                    deviceId={a.deviceId}
                    deviceInfo={deviceById.get(a.deviceId)}
                    slotExpiry={slotExpiry[a.deviceId] ?? []}
                    dataUrl={screenshotMap[key] || null}
                    imageUrl={imageUrlOf(key)}
                    status={cardStatus}
                    note={notes[a.id] || ''}
                    selected={selectedAndroidIds.has(a.id)}
                    onToggleSelect={onToggleAndroidSelect}
                    onNoteChange={updateNote}
                  />
                  </div>
                )
              }
              return (
                <StoppedAndroidCard
                  key={a.id}
                  android={a}
                  allAndroids={androids}
                  selected={selectedAndroidIds.has(a.id)}
                  onToggleSelect={onToggleAndroidSelect}
                  scale={scale}
                  aspectRatio={aspectRatio}
                  note={notes[a.id] || ''}
                  mirrors={mirrors}
                />
              )
            })}
          </div>
        )}
      </div>

      {tagEditor && (
        <SlotTagEditor
          x={tagEditor.x}
          y={tagEditor.y}
          /* 浮层持有的 android 是打开时的快照：保存成功后权威推送会替换 androids 里的
             条目，快照的 tags 不再更新 → 新标签「加了没反应」。按 id 找回最新条目取 tags。 */
          tags={androids.find(a => a.id === tagEditor.android.id)?.tags ?? tagEditor.android.tags ?? []}
          allTags={allTags}
          onSave={async (next) => {
            // 写设备端 /android/label（local 直连 / public 网关透传 / v2 走翻译层）；
            // Go 端 SetAndroidLabels 成功后统一乐观回填缓存并推 androidlist:updated
            // （androids props 换新 → 卡片/浮层即时收敛），同时 notifyAndroidsChanged
            // 触发容器列表查询，设备端真实 tags 秒级回灌
            // 标识必须与 RenameModal 同款走 getDeviceId：local 模式传 IP 直连，
            // 传 device_id（序列号）会 dial http://<序列号>:8000 → no such host
            const deviceIdent = getDeviceId({ ip: tagEditor.android.ip, device_id: tagEditor.android.deviceId }, deployMode)
            try {
              await saveAndroidTags(deviceIdent, [tagEditor.android.name], next)
              showToast(t('host:toast.tagSaveSuccess'))
            } catch (e) {
              showToast(t('host:toast.tagSaveFailed', { error: String(e) }), 'error')
            }
          }}
          onClose={() => setTagEditor(null)}
        />
      )}
    </div>
  )
}

export default ScreenshotView
