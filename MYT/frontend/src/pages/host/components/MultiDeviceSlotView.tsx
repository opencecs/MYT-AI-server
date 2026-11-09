import { useState, useMemo, useRef, useLayoutEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { gsap } from 'gsap'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { AndroidInfo, DeviceState, MirrorItem, SlotExpiry } from '../../../types/index'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getDeviceId } from '../../../utils/deviceId'
import { useDeviceNotes, useAndroidNotes } from '../../../hooks/useNoteSync'
import { useSlotTags, useSlotTagFilter, slotTagKey, type SlotTagControl } from '../../../hooks/useSlotTags'
import SlotView from './SlotView'
import TagFilterBar from './TagFilterBar'
import SlotTableHeader from './slotDetail/SlotTableHeader'
import { COL_FLAT, FLAT_PAD } from './slotDetail/slotViewUtils'
import { isStatusVisible } from '../../../utils/containerStatus'

interface Props {
  /** 勾选设备（已按设备列表顺序） */
  devices: DeviceState[]
  /** 全量设备列表：透传给 SlotView 的弹窗（如「移动云机」需要选目标设备），不能用勾选子集 */
  allDevices: DeviceState[]
  /** deviceId → 该设备下的云机（含已关机，坑位表需要全量） */
  androidGroups: Record<string, AndroidInfo[]>
  slotExpiry: Record<string, SlotExpiry[]>
  /** deviceId → 坑位数（页面级并行获取，避免每个 SlotView 各发一次 IPC） */
  slotCountMap: Record<string, number>
  mirrors: MirrorItem[]
  selectedIds: Set<string>
  onToggleSelect: (id: string) => void
  onSetSelectedIds: (ids: Set<string>) => void
  /** 宿主层提升的标签控制包（与 effectiveAndroids 的标签收窄同源）；不传时组件自持。 */
  tagControl?: SlotTagControl
  /**
   * 设置「云机展示=全部」：分区可见集合放行关机实例（可见集合含关机 → 分区头统计/
   * 全选覆盖关机行），并放宽 SlotView 的 onlyRunning（关机行照常渲染，行内自带开机按钮）。
   * 不传/false = 仅运行中（云机 tab 默认）。
   */
  showOffline?: boolean
}

/**
 * 多设备坑位视图：勾选多台设备时，每台设备一个可折叠分区，分区内复用单设备 SlotView。
 *
 * 关键取舍：
 * 1. **折叠态不渲染 SlotView** —— 勾选 115 台设备若全渲染 = 上千个坑位区块 + 上万 DOM 节点，必卡；
 *    只有展开的设备才建 DOM，开销与「展开数」成正比而非「勾选数」。
 * 2. **默认展开策略**：该设备有 running/starting 云机才默认展开，其余收起（与坑位内折叠同一套逻辑：
 *    多选设备时用户真正关心的是「哪些设备在跑东西」）。
 * 3. 用户手动折叠/展开过的设备以用户意图为准，不再被自动策略改写。
 * 4. **只列运行中的云机**（`onlyRunning`）：云机 tab 是多设备总览，关心的是「现在在跑什么」；
 *    已关机/已创建的实例连同其空坑位分组一起不渲染（2026-09-24 用户要求）。
 *    主机 tab（`MaintenancePage`）直连 `SlotView` 且不传该开关，保持全量 —— 那里要能启动已关机实例。
 */
function MultiDeviceSlotView({ devices, allDevices, androidGroups, slotExpiry, slotCountMap, mirrors, selectedIds, onToggleSelect, onSetSelectedIds, tagControl, showOffline = false }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { notes } = useDeviceNotes()
  // 云机（容器）备注：搜索用（key = 容器 id），与截图模式 ScreenshotView 同源
  const { notes: androidNotes } = useAndroidNotes()
  // 坑位标签（key = 设备ID#坑位号）：与 SlotView / 截图模式同款。
  // 云机 tab 由宿主提升传入（tagControl），未传时组件自持。
  const localTag = useSlotTags()
  const { tags: slotTags } = tagControl
    ? { tags: tagControl.slotTags }
    : localTag
  // 标签筛选（选中集合 / 全局标签 / 失效标签自动收敛）：与截图模式共用同一套语义。
  // hook 必须无条件调用（React 规则），传入 tagControl 时其返回值仅被忽略。
  const localFilter = useSlotTagFilter(slotTags)
  const { allTags, selectedTags, toggleTag, clearTags, hasFilterBar } =
    tagControl ? tagControl.filter : localFilter
  // 实例搜索词：与截图模式同款（trim + 小写，匹配实例名 / IP / 云机备注）
  const [searchKw, setSearchKw] = useState('')
  // 用户手动切换过折叠状态的设备（一旦动过，自动策略不再介入）
  const [userDeviceExpanded, setUserDeviceExpanded] = useState<Map<string, boolean>>(new Map())
  // 设备分区折叠动画的 DOM 挂载集合：展开才挂载、收起动画播完才卸载
  // （保住「折叠态不渲染 SlotView」的性能铁律——勾选上百台时上千坑位 DOM 不能常驻）
  const [mountedIds, setMountedIds] = useState<Set<string>>(new Set())
  const bodyRefs = useRef(new Map<string, HTMLDivElement>())
  // 待播「展开」动画的分区：新挂载的内容在下一次渲染后由 layout effect 播 fromTo
  const pendingOpenRef = useRef(new Set<string>())
  // 展开意图的同步镜像：收起动画的 onComplete 要读「此刻」的意图——
  // 动画途中反悔再展开时，state 闭包还是旧值，会误卸载内容
  const intentRef = useRef<Map<string, boolean>>(new Map())

  // 过滤已保证每个分区至少有一台开机云机 → 默认展开，只有用户手动折叠过才收起
  const isExpanded = (device: DeviceState): boolean =>
    userDeviceExpanded.get(device.device_id) ?? true

  const toggleDevice = (deviceId: string) => {
    const device = devices.find(d => d.device_id === deviceId)
    const willExpand = device ? !isExpanded(device) : true
    setUserDeviceExpanded(prev => {
      const next = new Map(prev)
      next.set(deviceId, willExpand)
      return next
    })
    intentRef.current.set(deviceId, willExpand)
    const unmount = () => setMountedIds(prev => {
      const next = new Set(prev)
      next.delete(deviceId)
      return next
    })
    const el = bodyRefs.current.get(deviceId)
    if (willExpand) {
      if (el) {
        // 收起动画途中反悔：内容还在挂，直接反向播展开（overwrite:'auto' 杀掉收起 tween，
        // 被杀 tween 的 onComplete 不会触发 → 不会误卸载）
        gsap.to(el, { height: 'auto', autoAlpha: 1, duration: 0.22, ease: 'power2.out', overwrite: 'auto', clearProps: 'height' })
      } else {
        pendingOpenRef.current.add(deviceId)
        setMountedIds(prev => new Set(prev).add(deviceId))
      }
    } else if (el) {
      gsap.to(el, {
        height: 0, autoAlpha: 0, duration: 0.15, ease: 'power2.in', overwrite: 'auto',
        // 播完才卸载内容；若动画期间被重新展开（意图已翻回）则保留挂载
        onComplete: () => { if (!(intentRef.current.get(deviceId) ?? true)) unmount() },
      })
    } else {
      unmount()
    }
  }

  // 分区头全选：只作用于「当前看得见」的云机（运行中 + 命中标签筛选）。
  // 与截图模式的分组全选语义一致（那里传的也是该组可见卡片 ids）——
  // 看不见的云机不该被批量操作波及。
  const toggleDeviceSelect = (ids: string[]) => {
    if (ids.length === 0) return
    const allSelected = ids.every(id => selectedIds.has(id))
    const next = new Set(selectedIds)
    if (allSelected) for (const id of ids) next.delete(id)
    else for (const id of ids) next.add(id)
    onSetSelectedIds(next)
  }

  const groups = useMemo(
    () => devices
      .map(device => {
        const all = androidGroups[device.device_id] ?? []
        // 标签筛选：命中任一选中标签即保留（或语义，与截图模式一致）
        const androids = selectedTags.size === 0 ? all : all.filter(a => {
          const tags = slotTags[slotTagKey(a.deviceId, a.indexNum)] ?? []
          return tags.some(tag => selectedTags.has(tag))
        })
        // 实例搜索：trim + 小写，匹配实例名 / IP / 备注（androidNotes，key = 容器 id）
        const kw = searchKw.trim().toLowerCase()
        const searched = kw
          ? androids.filter(a =>
              a.name.toLowerCase().includes(kw) ||
              a.ip.toLowerCase().includes(kw) ||
              (androidNotes[a.id] || '').toLowerCase().includes(kw))
          : androids
        // 列表模式只渲染运行中/启动中的云机（SlotView 的 onlyRunning），「可见集合」据此再收一次，
        // 分区头的统计 / 全选 / 复选框都只认它。
        // showOffline（设置「云机展示=全部」）：全状态进可见集合（分区头统计/全选覆盖关机行）；
        // 关闭时仅开机相关可见，created 同样不展示（isStatusVisible，见 containerStatus.ts 口径）
        const visible = searched.filter(a => isStatusVisible(a.status, showOffline))
        return { device, androids: searched, visible }
      })
      // 只展示「至少有一台可见云机」的设备分区：没开机的设备整块不渲染（连分区头都不留）。
      // 勾选上百台设备时绝大多数设备没有开机云机，全出分区头 = 一屏全是空分区（2026-09-22 用户要求）。
      // 被过滤掉的设备若之后有云机开机，会重新出现，且用户此前的折叠意图仍然生效。
      .filter(g => g.visible.length > 0),
    [devices, androidGroups, selectedTags, slotTags, searchKw, androidNotes],
  )

  // 新出现的分区（如某设备的云机刚开机）默认展开 → 直接挂载不播动画（同 SlotView 首屏瞬时落位）。
  // 用 layout effect：挂载发生在绘制前，避免首帧内容闪空
  useLayoutEffect(() => {
    setMountedIds(prev => {
      const next = new Set(prev)
      let changed = false
      for (const g of groups) {
        const id = g.device.device_id
        if (!next.has(id) && isExpanded(g.device)) {
          next.add(id)
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [groups])

  // 待播「展开」动画：toggleDevice 新挂载的内容在渲染后从 0 高度展开。
  // clearProps:'height' —— gsap 的 height:'auto' 动画结束会固化显式 px，内容变化（截图心跳）后不自适应
  useLayoutEffect(() => {
    for (const id of [...pendingOpenRef.current]) {
      const el = bodyRefs.current.get(id)
      if (!el) continue
      pendingOpenRef.current.delete(id)
      gsap.fromTo(el, { height: 0, autoAlpha: 0 }, {
        height: 'auto', autoAlpha: 1, duration: 0.22, ease: 'power2.out', overwrite: 'auto', clearProps: 'height',
      })
    }
  })

  // 实例搜索框：样式与截图模式同款；正常态与空态都渲染（空态下用户可重新输入）
  const searchBox = (
    <input
      type="text"
      className="w-56 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-700 outline-none placeholder:text-gray-400 focus:border-blue-400"
      placeholder={t('host:screenshot.searchInstance')}
      value={searchKw}
      onChange={e => setSearchKw(e.target.value)}
    />
  )

  // 空态三种：未勾选设备 / 勾选但无开机云机 / 有开机云机但被标签筛掉。
  // 筛选栏在空态也要渲染——否则筛空后没有取消筛选的入口，用户只能切模式自救。
  if (devices.length === 0 || groups.length === 0) {
    return (
      <div className="h-full flex flex-col">
        {/* 筛空时筛选栏仍要渲染（用 hasFilterBar 而非 allTags.length>0）：
            标签被删光但筛选仍激活时，这里是用户唯一的「清除筛选」入口 */}
        <div className="flex items-center gap-3 px-1 pb-3 pt-1">{searchBox}</div>
        {hasFilterBar && (
          <TagFilterBar allTags={allTags} selected={selectedTags} onToggle={toggleTag} onClear={clearTags} />
        )}
        <div className="flex-1 flex flex-col items-center justify-center text-gray-400 gap-2">
          {devices.length === 0
            ? t('host:empty.selectDeviceForSlot')
            : selectedTags.size > 0
              ? t('host:screenshot.noMatch')
              : t('host:empty.noRunningOnSelected')}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3 px-1 pb-3 pt-1">{searchBox}</div>
      {hasFilterBar && (
        <TagFilterBar allTags={allTags} selected={selectedTags} onToggle={toggleTag} onClear={clearTags} />
      )}
      <div>
        {/* 全表唯一列头（吸顶）：「表头 → IP 分区 → 云机行」三段式，各设备实例传 hideHeader */}
        <SlotTableHeader grid={COL_FLAT} pad={FLAT_PAD} flat />
        <div className="space-y-3">
        {groups.map(({ device, androids, visible }) => {
        const expanded = isExpanded(device)
        const deviceKey = getDeviceId(device, mode) || device.ip
        const note = notes[device.device_id]
        // 分区全选 = 该设备可见容器全量（关机恢复可选，与坑位分组全选同口径）
        const selectableIds = visible.map(a => a.id)
        const allSelected = selectableIds.length > 0 && selectableIds.every(id => selectedIds.has(id))
        const someSelected = selectableIds.some(id => selectedIds.has(id)) && !allSelected
        return (
          // 设备分区不画卡片边框（整表连成一体），分区头带底下细分隔线区分段落
          <div key={device.device_id}>
            {/* 设备分区头：IP/备注 + 运行统计 + 全选 + 折叠 */}
            <div className="flex items-center gap-2 px-4 py-2 bg-gray-50/70 border-b border-gray-100">
              <button
                onClick={() => toggleDevice(device.device_id)}
                className="flex items-center gap-1 text-xs font-medium text-gray-600 hover:text-gray-900 transition-colors cursor-pointer select-none"
                title={expanded ? t('host:multiSlot.collapseDevice') : t('host:multiSlot.expandDevice')}
              >
                {expanded ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                <span className="truncate max-w-[280px]">{note || deviceKey}</span>
              </button>
              {device.state !== 0 && (
                <span className="text-[10px] px-1.5 py-px rounded bg-red-50 text-red-500">{t('status.offline')}</span>
              )}
              <span className="text-[11px] text-gray-400">
                {t('host:multiSlot.summary', { total: androids.length, running: visible.length })}
              </span>
              <div className="flex-1" />
              {selectableIds.length > 0 && (
                <input
                  type="checkbox"
                  checked={allSelected}
                  ref={el => { if (el) el.indeterminate = someSelected }}
                  onChange={() => toggleDeviceSelect(selectableIds)}
                  className="w-3.5 h-3.5 border-gray-300 text-blue-500 focus:ring-blue-400 cursor-pointer"
                  title={allSelected ? t('host:screenshot.clickToDeselect') : t('host:screenshot.clickToSelect')}
                />
              )}
            </div>
            {mountedIds.has(device.device_id) && (
              <div
                ref={el => {
                  if (el) bodyRefs.current.set(device.device_id, el)
                  else bodyRefs.current.delete(device.device_id)
                }}
              >
                <SlotView
                  device={device}
                  devices={allDevices}
                  androids={androids}
                  slotExpiry={slotExpiry[device.device_id] ?? []}
                  mirrors={mirrors}
                  slotCount={slotCountMap[device.device_id]}
                  selectedIds={selectedIds}
                  onToggleSelect={onToggleSelect}
                  onSetSelectedIds={onSetSelectedIds}
                  onlyRunning={!showOffline}
                  flat
                  hideHeader
                />
              </div>
            )}
          </div>
        )
        })}
        </div>
      </div>
    </div>
  )
}

export default MultiDeviceSlotView
