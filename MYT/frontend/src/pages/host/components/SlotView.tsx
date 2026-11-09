import { useState, useEffect, useRef, useMemo, memo } from 'react'
import { useTranslation } from 'react-i18next'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import * as DiscoverService from '../../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as SlotService from '../../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as ScreencastService from '../../../../bindings/github.com/moyunteng/myt/screencastservicewrapper'
import { DeviceState, AndroidInfo, SlotExpiry, MirrorItem } from '../../../types/index'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getDeviceId } from '../../../utils/deviceId'
import ConfirmDialog from '../../../components/ConfirmDialog'
import ImportModal from '../operations/data/ImportModal'
import { expireColor, expireLabel } from '../../../utils/slotHelpers'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { useSlotTags, saveAndroidTags } from '../../../hooks/useSlotTags'
import SlotTagChips from '../../../components/SlotTagChips'
import SlotTagEditor from '../../../components/SlotTagEditor'
import { useConfirm } from '../../../hooks/useConfirm'
import { useTaskQueue } from '../../../hooks/taskQueue/useTaskQueue'
import { runSwitchOrStartTask } from '../operations/system/slotActions'
import { showToast } from '../../../components/Toast'
import { useInlineEdit } from '../../../hooks/useInlineEdit'
import { containerDisplayName } from '../../../utils/containerName'
import { btnPrimary, btnDanger } from '../../../utils/buttonStyles'
import { isStatusVisible } from '../../../utils/containerStatus'
import { formatOsVer, isEmulatorMirrorType } from '../../mirror/constants'
import { SlotGroup, isMytNetwork, statusBadge, parseImage, buildMirrorLookup, COL, COL_WIDE, COL_FLAT, SLOT_CHILD_PAD, FLAT_PAD } from './slotDetail/slotViewUtils'
import SlotTableHeader from './slotDetail/SlotTableHeader'
import { MoreHorizontal, Power, ChevronDown, ChevronRight } from 'lucide-react'
import { SlotDialogs } from './slotDetail/SlotDialogs'
import { SlotContextMenu } from './slotDetail/SlotContextMenu'
import AndroidNoteModal from './screenshotDetail/AndroidNoteModal'

gsap.registerPlugin(useGSAP)

interface Props {
  device: DeviceState
  devices: DeviceState[]
  androids: AndroidInfo[]
  slotExpiry: SlotExpiry[]
  mirrors: MirrorItem[]
  /** 云机勾选集合。不传时组件内部自持（无 ActionBar 的宿主页面用，如主机 tab）。 */
  selectedIds?: Set<string>
  onToggleSelect?: (id: string) => void
  onSetSelectedIds?: (ids: Set<string>) => void
  /** 行尾扩展操作插槽（渲染在「开机/更多」按钮之前）。主机 tab 用于注入日志清理/缓存清理。 */
  rowActions?: (a: AndroidInfo) => React.ReactNode
  /** 坑位数（宿主批量传入时组件不再自行查询；多设备视图统一并行获取后分发） */
  slotCount?: number
  /**
   * 只渲染运行中/启动中的云机，并隐藏没有运行实例的坑位分组（云机 tab 的列表模式用）。
   *
   * 注意：只影响表格渲染；传给 SlotDialogs 的仍是**全量** androids ——
   * 「切换云机」要列出同坑位其它容器（含已关机），传过滤集会选不到目标。
   */
  onlyRunning?: boolean
  /**
   * 摊平渲染：不渲染坑位分组头，所有可见云机排成一张扁平列表，坑位号进「坑位」列。
   *
   * 用于云机 tab 的列表模式——那里每个坑位至多一台在跑（配合 onlyRunning），
   * 分组头下只有一行，层级纯属冗余（2026-09-24 用户要求）。
   * 代价：分组头上的「导入安卓」「按坑位批量删除」在此模式下不可用（前者走主机 tab）。
   */
  flat?: boolean
  /**
   * 隐藏列头：多设备列表模式（MultiDeviceSlotView）把列头统一渲染在整个列表最上层，
   * 各设备实例不再各渲染一份（2026-09-25 用户要求「表头/IP/云机」三段式）。
   */
  hideHeader?: boolean
  /** 实例搜索词（trim + 小写，匹配实例名 / IP）。不传时不过滤（主机 tab 不用搜索时零影响）。 */
  searchKw?: string
  /**
   * 空坑位「创建」入口：点击时回调坑位号（宿主层持有 CreateInstanceModal）。
   * 不传时空坑位只显示「空闲」占位（云机 tab 的 MultiDeviceSlotView 不传，行为不变）。
   */
  onCreateInSlot?: (slot: number) => void
}

function SlotView({ device, devices, androids, slotExpiry, mirrors, selectedIds: externalIds, onToggleSelect: externalToggle, onSetSelectedIds: externalSet, rowActions, slotCount: externalSlotCount, onlyRunning = false, flat = false, hideHeader = false, searchKw = '', onCreateInSlot }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const deviceId = getDeviceId(device, mode)
  // 勾选三件套可选注入：宿主页面（云机 tab 的 ActionBar）传外部状态；不传时本地自持
  const [localSelected, setLocalSelected] = useState<Set<string>>(new Set())
  const selectedIds = externalIds ?? localSelected
  const onToggleSelect = externalToggle ?? ((id: string) => setLocalSelected(prev => {
    const next = new Set(prev)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  }))
  const onSetSelectedIds = externalSet ?? setLocalSelected
  const [importTarget, setImportTarget] = useState<number | null>(null)
  // 右键/操作菜单 + 单机弹窗目标
  const [menuAndroid, setMenuAndroid] = useState<{ android: AndroidInfo; x: number; y: number } | null>(null)
  const [castTarget, setCastTarget] = useState<{ ip: string; slot: number } | null>(null)
  const [moveTarget, setMoveTarget] = useState<AndroidInfo | null>(null)
  const [exportTarget, setExportTarget] = useState<AndroidInfo | null>(null)
  const [copyTarget, setCopyTarget] = useState<AndroidInfo | null>(null)
  const [upgradeTarget, setUpgradeTarget] = useState<AndroidInfo | null>(null)
  const [terminalTarget, setTerminalTarget] = useState<AndroidInfo | null>(null)
  const [switchModelTarget, setSwitchModelTarget] = useState<AndroidInfo | null>(null)
  const [vpcTarget, setVpcTarget] = useState<AndroidInfo | null>(null)
  const [fileManagerTarget, setFileManagerTarget] = useState<AndroidInfo | null>(null)
  const [apiDetailTarget, setApiDetailTarget] = useState<AndroidInfo | null>(null)
  const [streamTarget, setStreamTarget] = useState<AndroidInfo | null>(null)
  const [renameTarget, setRenameTarget] = useState<AndroidInfo | null>(null)
  const [switchBackupTarget, setSwitchBackupTarget] = useState<AndroidInfo | null>(null)
  const [resetTarget, setResetTarget] = useState<AndroidInfo | null>(null)
  const [s5Target, setS5Target] = useState<AndroidInfo | null>(null)
  const [gpsTarget, setGpsTarget] = useState<AndroidInfo | null>(null)
  const [googleCertTarget, setGoogleCertTarget] = useState<AndroidInfo | null>(null)
  // V4 专属：设备档案弹窗（POST /android/configure，与卡片右键 DeviceProfileModal 同款）
  const [profileTarget, setProfileTarget] = useState<AndroidInfo | null>(null)
  const { confirmAction, setConfirmAction, ask } = useConfirm()
  const { addTask, updateTask } = useTaskQueue()
  const { notes, setNoteLocal } = useAndroidNotes()
  // 容器标签（设备端下发，随 androidlist:updated/WS 推送同步；写入走 saveAndroidTags
  // → POST /android/label，public 模式网关透传）。useSlotTags 的本地映射仅用于聚合
  // 「全局已有标签」快速复用。
  const { tags: slotTags } = useSlotTags()
  const allTags = useMemo(() => {
    const set = new Set<string>()
    for (const list of Object.values(slotTags)) for (const tag of list) set.add(tag)
    return [...set].sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
  }, [slotTags])
  // 标签编辑浮层目标（容器 + 触发坐标）
  const [tagEditor, setTagEditor] = useState<{ android: AndroidInfo; x: number; y: number } | null>(null)
  // 备注弹窗目标（右键「设置备注」入口，AndroidNoteModal）
  const [noteTarget, setNoteTarget] = useState<AndroidInfo | null>(null)
  // 备注落档（设备端 SetAndroidNote）+ 本地映射同步 + 投屏窗口标题同步；
  // 行内编辑（useInlineEdit）与备注弹窗（AndroidNoteModal）共用同一实现
  const persistNote = async (id: string, value: string) => {
    await DiscoverService.SetAndroidNote(id, value)
    setNoteLocal(id, value)
    // 实时更新投屏窗口标题
    const android = androids.find(a => a.id === id)
    if (android) {
      const title = value || android.name || ''
      ScreencastService.UpdateProjectionTitle(deviceId, android.indexNum, title).catch(() => {})
    }
  }
  const { editingId, editValue, setEditValue, startEdit: rawStartEdit, cancelEdit, saveNote: rawSaveNote } = useInlineEdit(persistNote)
  // V4 剪贴板：读本机剪贴板 → POST /android/clipboard（paste=true 直贴云机焦点输入框）。
  // readText 需要 WebView2 的 clipboard-read 权限，被拒/失败时 toast 明确提示。
  // 与卡片右键 ScreenshotCard.pasteLocalClipboard 同源实现。
  const pasteLocalClipboard = async (android: AndroidInfo) => {
    try {
      const text = await navigator.clipboard.readText()
      if (!text) { showToast(t('host:screenshot.contextMenu.clipboardEmpty'), 'error'); return }
      await SdkApiService.SetAndroidClipboard(getDeviceId(android, mode), android.name, text, true)
      showToast(t('host:toast.opSuccess', { op: t('host:screenshot.contextMenu.pasteClipboard') }), 'success')
    } catch (err) {
      showToast(String(t('host:screenshot.contextMenu.clipboardReadFailed', { error: String(err) })), 'error')
    }
  }
  const startEdit = (id: string, e: React.MouseEvent) => { rawStartEdit(id, e); setEditValue(notes[id] || '') }

  const [localSlotCount, setLocalSlotCount] = useState(12)
  // 坑位数可由宿主批量传入（多设备坑位视图统一并行获取，避免每个实例各发一次 IPC）
  const slotCount = externalSlotCount ?? localSlotCount

  // 坑位折叠：默认「没开机的收起、开机的不展开」——一台设备 12~24 个坑位里通常只有 1~3 台在跑，
  // 其余全是空坑位/关机占位，默认展开会让一屏大半是空白行（多选设备时更明显）。
  //
  // userSlotCollapsed 只记「用户手动点过」的坑位：一旦动过就以用户意图为准，自动策略不再介入
  // （与项目既有教训一致——自动合并/派生逻辑不得改写用户 UI 意图）。用户没动过的坑位则完全
  // 跟随云机状态派生，所以「关机自动收起、开机自动展开」都能即时生效。
  const [userSlotCollapsed, setUserSlotCollapsed] = useState<Map<number, boolean>>(new Map())
  // 坑位 body 容器 refs（容器常驻 DOM，折叠/展开走 gsap height+autoAlpha 动画）
  const slotBodyRefs = useRef(new Map<number, HTMLDivElement>())

  const mirrorLookup = buildMirrorLookup(mirrors)

  // onlyRunning（云机 tab 的列表模式）：只渲染运行中/启动中的云机（showOffline=false 时
  // 由 MultiDeviceSlotView 传 onlyRunning=!showOffline；created 关闭时同样不可见，见
  // isStatusVisible 口径）。
  // 过滤放在分组之前 —— 下游全部派生（坑位分组 / 折叠判定 / 分组勾选 / 统计 / 行渲染）
  // 自然都基于可见集合，无需各自再判一次。
  // 注意弹窗（SlotDialogs）与备注查找仍用**全量** androids，不受此过滤影响。
  // searchKw（主机 tab 列表模式）：实例名 / IP / 云机备注 搜索，写法与 MultiDeviceSlotView / ScreenshotView 同款。
  const visibleAndroids = useMemo(() => {
    let list = onlyRunning ? androids.filter(a => isStatusVisible(a.status, false)) : androids
    const kw = searchKw.trim().toLowerCase()
    if (kw) {
      list = list.filter(a =>
        a.name.toLowerCase().includes(kw) ||
        a.ip.toLowerCase().includes(kw) ||
        (notes[a.id] || '').toLowerCase().includes(kw))
    }
    return list
  }, [androids, onlyRunning, searchKw, notes])

  // useMemo：collapsedSlots 的依赖，必须引用稳定——否则每次渲染都是新数组，
  // 下游 useGSAP 的 dependencies 每帧都变（等于每帧重跑折叠动画逻辑）。
  const slotGroups: SlotGroup[] = useMemo(() => {
    const groups = Array.from({ length: slotCount }, (_, i) => {
      const idx = i + 1
      const grouped = visibleAndroids.filter(a => a.indexNum === idx)
      const expire = slotExpiry.find(s => s.index === idx)
      return { index: idx, androids: grouped, expireSlot: expire }
    })
    // onlyRunning 时丢掉没有运行实例的坑位：否则一屏全是「空闲」占位分组
    return onlyRunning ? groups.filter(g => g.androids.length > 0) : groups
  }, [slotCount, visibleAndroids, slotExpiry, onlyRunning])

  const collapsedSlots = useMemo(() => {
    const set = new Set<number>()
    for (const g of slotGroups) {
      const userValue = userSlotCollapsed.get(g.index)
      // 折叠判定与可见集合同口径（isStatusVisible）：同坑位仅 created 实例时
      // 若按 isRunnableStatus 判定会默认折叠，放行的 created 等于没显示
      const collapsed = userValue ?? !g.androids.some(a => isStatusVisible(a.status, false))
      if (collapsed) set.add(g.index)
    }
    return set
  }, [slotGroups, userSlotCollapsed])

  const toggleSlotCollapsed = (index: number) => {
    setUserSlotCollapsed(prev => {
      const next = new Map(prev)
      next.set(index, !collapsedSlots.has(index))
      return next
    })
  }

  // 切换设备重置用户折叠选择：坑位号相同的另一台设备不应继承上一台的展开/收起状态
  useEffect(() => {
    setUserSlotCollapsed(new Map())
  }, [device.device_id])

  // 折叠/展开动画（DeviceContextMenu 分组展开同款 height+autoAlpha）。
  // 只动「实际呈现态与目标不符」的坑位：避免把全部坑位高度反复固化为像素值
  // （height:'auto' 动画会显式设 px，内容变化后不自适应），也避免重复触发 tween。
  //
  // ⚠️ 收起态的「呈现」由渲染期的 CSS 类（body 的 h-0 invisible）兜底，gsap 只做过渡：
  // React StrictMode（开发模式）会把 effect 跑两遍（mount → 模拟卸载 → mount），
  // 模拟卸载时 useGSAP 的 cleanup 会 context.revert() 撤销 gsap 写过的 inline style，
  // 若收起态只靠 gsap 的 inline style，坑位就会停在被 revert 后的展开态
  // （2026-09-22 实机：dev 模式下未运行的坑位全部展开，生产构建不复现）。
  // 类名是 React 声明式写入的，revert 动不了它 —— 即使 gsap 一步没跑，收起态也成立。
  useGSAP(
    () => {
      for (const [index, el] of slotBodyRefs.current) {
        const collapsed = collapsedSlots.has(index)
        const target = collapsed ? 'collapsed' : 'open'
        // height 为 '0px' = 已收起；为空 = gsap 还没接管过（首屏，或被 revert 清掉）
        const applied = el.style.height === '0px' ? 'collapsed' : (el.style.height ? 'open' : null)
        // 已朝这个目标动过、且元素确实被 gsap 接管着（含动画进行中）→ 不重复触发，避免打断
        if (el.dataset.anim === target && applied !== null) continue
        el.dataset.anim = target
        const to = collapsed
          ? { height: 0, autoAlpha: 0 }
          : { height: 'auto', autoAlpha: 1 }
        if (!el.style.height) {
          // 瞬时落位：默认折叠的坑位若走 0.15s 收起动画，首屏会看到整片坑位「塌陷」一下
          gsap.set(el, to)
        } else if (collapsed) {
          gsap.to(el, { ...to, duration: 0.15, ease: 'power2.in', overwrite: 'auto' })
        } else {
          gsap.to(el, { ...to, duration: 0.22, ease: 'power2.out', overwrite: 'auto' })
        }
      }
    },
    { dependencies: [collapsedSlots] },
  )

  useEffect(() => {
    // 宿主已批量传入坑位数时不再各实例 IPC（多设备视图下 N 个实例 = N 次跨进程往返）
    if (externalSlotCount !== undefined) return
    SlotService.GetSlotCount(device.device_id).then((count: number) => {
      setLocalSlotCount(count)
    }).catch(() => {})
  }, [device.device_id, externalSlotCount])

  const saveNote = () => { rawSaveNote().catch(() => {}) }

  // 行含扩展操作时列网格取加宽版（操作列容纳注入按钮）
  const colClass = rowActions ? COL_WIDE : COL

  // 摊平模式（云机 tab 列表）：多一列「坑位」，行是顶层——不再嵌在坑位分组下，故用 px-4 而非子级缩进
  const rowGrid = flat ? COL_FLAT : colClass
  const rowPad = flat ? FLAT_PAD : SLOT_CHILD_PAD

  // 摊平模式下待渲染的行：slotGroups 本身按坑位号升序生成，flatMap 后即为「按坑位排序的扁平列表」
  const flatRows = useMemo(
    () => flat ? slotGroups.flatMap(g => g.androids.map(a => ({ a, group: g }))) : [],
    [flat, slotGroups],
  )

  /**
   * 单个云机行。分组渲染与摊平渲染共用同一份行 JSX——两处各写一遍必然演化分叉。
   * @param isFirst 是否所属容器内的首行（首行不加分隔上边框）
   * @param group 该行所属的坑位分组（摊平模式下用于取坑位号与到期状态）
   */
  const renderRow = (a: AndroidInfo, isFirst: boolean, group: SlotGroup) => {
    const matched = mirrorLookup.get(a.image)
    const fallback = parseImage(a.image)
    const mirrorName = matched?.name || fallback.mirror
    const osVer = formatOsVer(matched?.os_ver || fallback.ver)
    const isEmulator = matched ? isEmulatorMirrorType(matched) : false
    const note = notes[a.id]
    const checked = selectedIds.has(a.id)
    // 关机云机恢复可勾选（勾选源头闸门撤除）：上传等批量操作有 Go 侧护栏兜底，
    // ADB 批量由 ActionBar 执行前过滤跳过（见 batchSelectGate 注释）。
    // 标签绑容器（设备端 /android/label）：写入不再要求容器运行
    const rowTags = a.tags ?? []
    const expire = group.expireSlot
    return (
      <div key={a.id} className={`${rowGrid} ${rowPad} py-2 ${isFirst ? '' : 'border-t border-gray-50'} hover:bg-gray-50/50`}>
        <input type="checkbox" checked={checked} onChange={() => onToggleSelect(a.id)} aria-label={t('a11y.selectInstance', { name: a.name })} className={`w-3.5 h-3.5 border-gray-300 text-blue-500 focus:ring-blue-400 cursor-pointer`} title={t('host:screenshot.clickToSelect')} />
        {/* 摊平模式：坑位号 + 到期状态进列（原来挂在坑位分组头上） */}
        {flat && (
          <div className="flex items-center gap-1 justify-center min-w-0">
            <span className="text-xs text-gray-600">{a.indexNum}</span>
            {expire && expire.state !== undefined
              ? <span className={`text-[10px] whitespace-nowrap ${expireColor(expire.state)}`}>{expireLabel(expire.state)}</span>
              : <span className="text-[10px] text-gray-400 whitespace-nowrap">{t('host:slotView.unsubscribed')}</span>}
          </div>
        )}
        {editingId === a.id ? (
          <input type="text" value={editValue} onChange={(e) => setEditValue(e.target.value)}
            onBlur={saveNote} onKeyDown={(e) => { if (e.key === 'Enter') saveNote(); if (e.key === 'Escape') cancelEdit() }}
            onClick={(e) => e.stopPropagation()} autoFocus placeholder={note || a.name}
            className="text-sm px-1 py-0.5 w-full border border-blue-300 rounded outline-none focus:border-blue-500 bg-white" />
        ) : (
          <span onClick={(e) => startEdit(a.id, e)}
            className="text-sm truncate cursor-pointer text-gray-700 hover:text-blue-600 transition-colors"
            title={note ? a.name + ' - ' + note : t('host:deviceList.setNote')}>{note || containerDisplayName(a.name, a.deviceId, a.indexNum)}</span>
        )}
        <span className="text-xs text-gray-500 truncate text-center" title={a.image}>{mirrorName}</span>
        <div className="flex justify-center">
          {matched ? (
            isEmulator ? (
              <span className="text-[10px] px-1 py-px rounded bg-amber-50 text-amber-600">{t('host:slotView.emulator')}</span>
            ) : (
              <span className="text-[10px] px-1 py-px rounded bg-teal-50 text-teal-600">{t('host:slotView.container')}</span>
            )
          ) : (
            <span className="text-[10px] px-1 py-px rounded bg-gray-50 text-gray-400" title={t('host:slotView.unknownMirrorHint')}>{t('host:slotView.unknown')}</span>
          )}
        </div>
        <span className="text-xs text-gray-400 truncate text-center" title={osVer}>{osVer}</span>
        <div className="flex items-center gap-1 justify-center">{isMytNetwork(a.networkName) ? <span className="text-[10px] px-1 py-px rounded bg-blue-50 text-blue-500">{t('host:slotView.bridge')}</span> : <span className="text-[10px] px-1 py-px rounded bg-gray-50 text-gray-400">{t('host:slotView.nat')}</span>}</div>
        <span className="text-xs text-gray-500 truncate text-center" title={a.containerIP || '-'}>{a.containerIP || '-'}</span>
        <div className="flex justify-center">{statusBadge(a.status)}</div>
        {/* 标签列：点击开编辑浮层（写入设备端，不再限制运行中；停机容器标签同样可编辑） */}
        <div className="flex justify-center min-w-0">
          <button
            onClick={(e) => { e.stopPropagation(); setTagEditor({ android: a, x: e.clientX, y: e.clientY }) }}
            className="px-1 py-0.5 rounded hover:bg-gray-100 transition-colors cursor-pointer max-w-full"
            title={t('host:tags.setTags')}
          >
            {rowTags.length > 0
              ? <SlotTagChips tags={rowTags} max={2} />
              : <span className="text-[10px] text-gray-300">+ {t('host:slotView.colTags')}</span>}
          </button>
        </div>
        <span className="text-xs text-gray-400 truncate text-center" title={a.modelPath}>{a.modelPath || '-'}</span>
        <div className="flex justify-end gap-1">
          {rowActions?.(a)}
          {a.status !== 'running' && (
            <button onClick={(e) => {
              e.stopPropagation()
              ask(t('host:confirm.start.title'), t('host:confirm.start.message', { name: a.name }),
                async () => {
                  const ok = await runSwitchOrStartTask(mode, addTask, updateTask, a, undefined, { start: t('host:slotView.switchStepStart', { name: a.name }) })
                  if (!ok) showToast(t('host:toast.startFailed'), 'error')
                })
            }} className="text-green-500 hover:text-green-600 hover:bg-green-50 rounded p-1 transition-colors cursor-pointer" title={t('host:slotView.start')} aria-label={t('host:slotView.start')}>
              <Power size={14} />
            </button>
          )}
          <button onClick={(e) => { e.stopPropagation(); setMenuAndroid({ android: a, x: e.clientX, y: e.clientY }) }} className="text-gray-400 hover:text-gray-700 hover:bg-gray-200 rounded p-1 transition-colors cursor-pointer" title={t('host:screenshot.contextMenu.apiDetail')} aria-label={t('a11y.moreActions')}>
            <MoreHorizontal size={14} />
          </button>
        </div>
      </div>
    )
  }

  return (
    <>
      <div>
        {/* 列头（吸顶）：多设备列表模式由 MultiDeviceSlotView 在最上层统一渲染，此处跳过 */}
        {!hideHeader && <SlotTableHeader grid={rowGrid} pad={rowPad} flat={flat} />}

        {/* 行区与列头贴合（表格观感），坑位分组之间留呼吸间距。
            表格外框一律不画（2026-09-25 用户要求「内层表格边框隐藏」），行分隔线保留 */}
        <div className="space-y-2">
        {/* 摊平模式：全部行一张扁平列表（无坑位分组头、无折叠）；否则按坑位分组渲染 */}
        {flat ? (
          <div>
            {flatRows.map(({ a, group }, i) => renderRow(a, i === 0, group))}
          </div>
        ) : slotGroups.map(group => {
          // 坑位分组全选 = 该组全量容器（关机恢复可选，与行级复选框同口径）
          const groupIds = group.androids.map(a => a.id)
          const allSelected = groupIds.length > 0 && groupIds.every(id => selectedIds.has(id))
          const someSelected = groupIds.some(id => selectedIds.has(id)) && !allSelected
          const collapsed = collapsedSlots.has(group.index)
          const runningCount = group.androids.filter(a => a.status === 'running').length
          return (
          <div key={group.index}>
            <div className="flex items-center justify-between px-4 py-1.5 bg-gray-50/60 border-b border-gray-100">
              <div className="flex items-center gap-2">
                {group.androids.length > 0 && (
                  <input type="checkbox"
                    checked={allSelected} ref={el => { if (el) el.indeterminate = someSelected }}
                    aria-label={t('a11y.selectAllInSlot', { index: group.index })}
                    onChange={() => {
                      const next = new Set(selectedIds)
                      if (allSelected) groupIds.forEach(id => next.delete(id))
                      else groupIds.forEach(id => next.add(id))
                      onSetSelectedIds(next)
                    }}
                    className="w-3.5 h-3.5 border-gray-300 text-blue-500 focus:ring-blue-400 cursor-pointer" />
                )}
                <button onClick={() => toggleSlotCollapsed(group.index)}
                  className="flex items-center gap-1 text-xs font-medium text-gray-500 hover:text-gray-800 transition-colors cursor-pointer select-none"
                  title={collapsed ? t('host:slotView.expandSlot') : t('host:slotView.collapseSlot')}>
                  {collapsed ? <ChevronRight size={13} /> : <ChevronDown size={13} />}
                  <span>{t('host:slotView.slot', { index: group.index })}</span>
                </button>
                {group.expireSlot && group.expireSlot.state !== undefined ? (
                  <span className={`text-xs ${expireColor(group.expireSlot.state)}`}>{expireLabel(group.expireSlot.state)}</span>
                ) : (
                  <span className="text-xs text-gray-400">{t('host:slotView.unsubscribed')}</span>
                )}
                {collapsed && group.androids.length > 0 && (
                  <span className="text-[10px] px-1.5 py-px rounded bg-gray-100 text-gray-500">
                    {t('host:slotView.slotSummary', { total: group.androids.length, running: runningCount })}
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                {someSelected || allSelected ? (
                  <button onClick={() => {
                    const targets = group.androids.filter(a => selectedIds.has(a.id))
                    ask(t('host:confirm.batchDeleteSlots.title'), t('host:confirm.batchDeleteSlots.message', { slot: group.index, count: targets.length }), async () => {
                      for (const a of targets) {
                        try { await SdkApiService.DeleteAndroid(deviceId, a.name) } catch { /* ignore */ }
                      }
                      const next = new Set(selectedIds); groupIds.forEach(id => next.delete(id)); onSetSelectedIds(next)
                    })
                  }} className={btnDanger}>{t('host:slotView.batchDeleteCount', { count: groupIds.filter(id => selectedIds.has(id)).length })}</button>
                ) : null}
                <button onClick={() => setImportTarget(group.index)} className={btnPrimary}>{t('host:slotView.importAndroid')}</button>
              </div>
            </div>

            <div ref={el => {
              if (el) slotBodyRefs.current.set(group.index, el)
              else slotBodyRefs.current.delete(group.index)
            }} className={`slot-body ${collapsed ? 'h-0 invisible' : ''}`}>
            {group.androids.length === 0 && (
              <div className={`${SLOT_CHILD_PAD} py-3 flex items-center gap-2`}>
                <span className="text-xs text-gray-300">{t('host:slotView.idle')}</span>
                {onCreateInSlot && (
                  /* 空坑位创建入口：样式对齐截图模式占位卡的创建按钮（CardActionBar），
                     小一号更轻量；仅空坑位显示，已有实例的坑位不出现 */
                  <button
                    onClick={() => onCreateInSlot(group.index)}
                    className="whitespace-nowrap rounded bg-blue-500 px-2 py-0.5 text-xs text-white hover:brightness-110 transition-all cursor-pointer"
                    title={t('button.create')}
                  >
                    {t('button.create')}
                  </button>
                )}
              </div>
            )}

            {group.androids.map((a, ai) => renderRow(a, ai === 0, group))}
            </div>
          </div>
          )
        })}
        </div>
      </div>

      {confirmAction && (
        <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
          onConfirm={() => { confirmAction.onConfirm(); setConfirmAction(null) }}
          onCancel={() => setConfirmAction(null)} />
      )}
      {importTarget !== null && (
        <ImportModal deviceIP={deviceId} slotIndex={importTarget} onClose={() => setImportTarget(null)} />
      )}
      {noteTarget && (
        <AndroidNoteModal android={noteTarget} note={notes[noteTarget.id] || ''} onSave={persistNote}
          onClose={() => setNoteTarget(null)} />
      )}
      {tagEditor && (
        <SlotTagEditor
          x={tagEditor.x}
          y={tagEditor.y}
          /* 浮层持有的 android 是打开时的快照：保存成功后权威推送（androidlist:updated）
             会用新对象替换 androids 里的旧条目，快照的 tags 永远不变 → 新标签看起来
             「加了没反应」。渲染时按 id 找回最新条目取 tags，找不到（实例被删）再退快照。 */
          tags={androids.find(a => a.id === tagEditor.android.id)?.tags ?? tagEditor.android.tags ?? []}
          allTags={allTags}
          onSave={async (next) => {
            // 写设备端 /android/label（local 直连 / public 网关透传 / v2 走翻译层）；
            // Go 端 SetAndroidLabels 成功后统一乐观回填缓存并推 androidlist:updated
            // （androids props 换新 → 浮层即时收敛），同时 notifyAndroidsChanged
            // 触发容器列表查询，设备端真实 tags 秒级回灌
            try {
              // 标识必须与 RenameModal 同款走 getDeviceId：local 模式传 IP 直连，
              // 传 device_id（序列号）会 dial http://<序列号>:8000 → no such host
              await saveAndroidTags(deviceId, [tagEditor.android.name], next)
              showToast(t('host:toast.tagSaveSuccess'))
            } catch (e) {
              showToast(t('host:toast.tagSaveFailed', { error: String(e) }), 'error')
            }
          }}
          onClose={() => setTagEditor(null)}
        />
      )}
      {menuAndroid && (
        <SlotContextMenu
          target={menuAndroid} ask={ask} onClose={() => setMenuAndroid(null)}
          onCast={(ip, slot) => setCastTarget({ ip, slot })}
          onTerminal={setTerminalTarget} onFileManager={setFileManagerTarget}
          onSwitchModel={setSwitchModelTarget} onSwitchBackup={setSwitchBackupTarget} onUpgrade={setUpgradeTarget}
          onMove={setMoveTarget} onCopy={setCopyTarget} onExport={setExportTarget}
          onRename={setRenameTarget} onVpc={setVpcTarget} onStream={setStreamTarget}
          onGps={setGpsTarget} onGoogleCert={setGoogleCertTarget} onS5={setS5Target}
          onReset={setResetTarget} onApiDetail={setApiDetailTarget}
          onPasteClipboard={a => { pasteLocalClipboard(a) }}
          onDeviceProfile={setProfileTarget}
          onEditTags={a => setTagEditor({ android: a, x: menuAndroid.x, y: menuAndroid.y })}
          onSetNote={setNoteTarget}
        />
      )}
      <SlotDialogs
        device={device} androids={androids} devices={devices} slotExpiry={slotExpiry} mirrors={mirrors} notes={notes} ask={ask}
        castTarget={castTarget} moveTarget={moveTarget} exportTarget={exportTarget} copyTarget={copyTarget}
        upgradeTarget={upgradeTarget} terminalTarget={terminalTarget} switchModelTarget={switchModelTarget}
        vpcTarget={vpcTarget} fileManagerTarget={fileManagerTarget} apiDetailTarget={apiDetailTarget}
        streamTarget={streamTarget} renameTarget={renameTarget} switchBackupTarget={switchBackupTarget}
        resetTarget={resetTarget} s5Target={s5Target} gpsTarget={gpsTarget} googleCertTarget={googleCertTarget}
        profileTarget={profileTarget}
        onClose={{
          cast: () => setCastTarget(null),
          move: () => setMoveTarget(null),
          exportA: () => setExportTarget(null),
          copy: () => setCopyTarget(null),
          upgrade: () => setUpgradeTarget(null),
          terminal: () => setTerminalTarget(null),
          switchModel: () => setSwitchModelTarget(null),
          vpc: () => setVpcTarget(null),
          fileManager: () => setFileManagerTarget(null),
          apiDetail: () => setApiDetailTarget(null),
          stream: () => setStreamTarget(null),
          rename: () => setRenameTarget(null),
          switchBackup: () => setSwitchBackupTarget(null),
          reset: () => setResetTarget(null),
          s5: () => setS5Target(null),
          gps: () => setGpsTarget(null),
          googleCert: () => setGoogleCertTarget(null),
          profile: () => setProfileTarget(null),
        }}
      />
    </>
  )
}

// memo：多设备坑位视图下同时挂 N 个实例，心跳/截图事件频繁触发的父级重渲染
// 不应连带重建每个设备的整张坑位表（props 由宿主 useMemo/useCallback 稳定）
export default memo(SlotView)