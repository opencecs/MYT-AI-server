import { useState, useRef, useEffect, memo } from 'react'
import { useTranslation } from 'react-i18next'
import { FolderOpen, Smartphone, RefreshCw, ArrowUpCircle, MoveRight, Copy, Upload, Pencil, Network, RadioTower, MapPin, Vibrate, KeyRound, Shield, Trash2, Info, AlertTriangle, ClipboardPaste, IdCard } from 'lucide-react'
import AndroidContextMenu from '../../../components/AndroidContextMenu'
import { showToast } from '../../../components/Toast'
import * as DiscoverService from '../../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as ScreencastService from '../../../../bindings/github.com/moyunteng/myt/screencastservicewrapper'
import { AndroidInfo, SlotExpiry, DeviceState, MirrorItem } from '../../../types/index'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { containerDisplayName } from '../../../utils/containerName'
import ScreenCastModal from '../screencast/ScreenCastModal'
import SetStreamModal from '../screencast/SetStreamModal'
import MoveModal from '../operations/data/move/MoveModal'
import ExportModal from '../operations/data/ExportModal'
import UpdateImageModal from '../create/updateImage/UpdateImageModal'
import TerminalModal from '../terminal/TerminalModal'
import VpcModal from '../vpc/VpcModal'
import FileManagerModal from '../terminal/FileManagerModal'
import BatchSwitchModelModal from '../operations/model/BatchSwitchModelModal'
import RenameModal from '../operations/system/RenameModal'
import SwitchAndroidModal from '../operations/system/SwitchAndroidModal'
import SwitchBackupModal from '../operations/model/SwitchBackupModal'
import ResetModal from '../operations/system/ResetModal'
import S5ProxyModal from '../operations/network/S5ProxyModal'
import GpsModal from '../operations/network/GpsModal'
import DeviceProfileModal from '../operations/profile/DeviceProfileModal'
import GoogleCertModal from '../operations/system/GoogleCertModal'
import ApiDetailModal from '../diagnose/ApiDetailModal'
import ScreenshotInfoTip, { useInfoTipHover } from './screenshotDetail/ScreenshotInfoTip'
import CardActionBar from './screenshotDetail/CardActionBar'
import CardHeader from './screenshotDetail/CardHeader'
import { useStableImageSrc } from './screenshotDetail/useStableScreenshot'
import AndroidNoteModal from './screenshotDetail/AndroidNoteModal'
import { castClickBlocked } from '../../../utils/castGuard'
import { useConfirm } from '../../../hooks/useConfirm'
import CopyModal from '../operations/data/CopyModal'

import { doSdk } from '../../../utils/sdkHelper'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getDeviceId } from '../../../utils/deviceId'
import { type SlotTagMap } from '../../../hooks/useSlotTags'
import { type CardStatus, statusLabel, isRunnableStatus } from '../../../utils/containerStatus'
import { Tags } from 'lucide-react'
import { buildQuickActions } from './screenshotDetail/quickActions'
import type { CardMenuItem, CardMenuGroup } from './screenshotDetail/cardMenuTypes'

/**
 * 卡片右键菜单项/组：在 AndroidContextMenu 的 MenuItemConfig/MenuGroup 上扩展
 * v4Hide 标记——V4 部署模式无对应 SDK 端点的项（V4 下隐藏，v2/local/public 不受影响）。
 * 隐藏 16 项：终端/文件管理/设置标签/新机/切换备份/更新/移动/复制/导出/重命名/VPC/
 * 设流/GPS 定位/摇一摇/Google 证书/S5 代理；保留 10 项：投屏/设置备注/
 * 粘贴剪贴板/设备档案/重启/停止/重置/删除/API 详情。
 * （2026-10-28 用户要求：去掉「切换云机」——底部按钮条「切换」与占位卡入口保留。）
 * （v4Hide 类型定义已抽 screenshotDetail/cardMenuTypes.ts，快捷五按钮抽 quickActions.ts。）
 */

export interface ScreenshotCardProps {
  android: AndroidInfo
  androids: AndroidInfo[]
  /** 完整云机列表（含非 running，切换云机弹窗用）。默认空数组（群控等场景不切换）。 */
  allAndroids?: AndroidInfo[]
  deviceId: string
  deviceInfo: DeviceState | undefined
  devices: DeviceState[]
  slotExpiry: SlotExpiry[]
  mirrors: MirrorItem[]
  /** 截图区容器宽高比（宽/高），由宿主按「本页主流截图比例」统一下发，保证整页卡片等高 */
  aspectRatio: number
  isPortrait: boolean
  note: string
  onNoteChange: (id: string, value: string) => void
  dataUrl: string | null
  status: CardStatus
  selected: boolean
  onToggleSelect: (id: string) => void
  /** 单击截图直接投屏（云机页面用），与选中功能互斥 */
  clickToCast?: boolean
  /** 坑位标签映射（key = 设备ID#坑位号）。不传则不显示标签条（群控页等场景）。 */
  slotTags?: SlotTagMap
  /** 打开标签编辑浮层（写入设备端，不依赖容器运行）；不传则标签条只读 */
  onEditTags?: (x: number, y: number, android: AndroidInfo) => void
  /** 打开「创建云机」弹窗（宿主层持有弹窗状态，复用 CreateInstanceModal）；不传则不显示创建按钮 */
  onCreateInstance?: (device: DeviceState, slot: number) => void
  /** 云机 Tab 紧凑场景：整条不渲染底部按钮条（2026-10-16 用户要求：卡片只剩 header +
      截图区，ADB 也砍）；主机 Tab（device 布局）不传，保持三按钮 */
  compact?: boolean
  /** 缩放系数（滑条值/100）：卡片内字号/间距按源码 calc(px*scale) 缩放；不传 = 1 */
  scale?: number
  /**
   * 截图 `<img>` 加载完成回调（naturalWidth/naturalHeight）：宿主据此统计本页主流比例，
   * 决定容器 aspectRatio（见 screenshotDetail/useDominantAspect.ts）。
   */
  onImageLoad?: (key: string, w: number, h: number) => void
  /**
   * 截图二进制 URL（http://wails.localhost/screenshots/{key}?v={version}，非空 = 后端已有图）。
   * 与 dataUrl 同源同真（版本号驱动换 URL → 浏览器按 URL 缓存新图）；二者都为 null 时无图。
   * 传入后 img src 优先用本字段（去 base64 IPC 通道），dataUrl 作为回落。
   */
  imageUrl?: string | null
}

export const ScreenshotCard = memo(function ScreenshotCard({ android, androids, allAndroids = [], deviceId, deviceInfo, devices, slotExpiry, mirrors, aspectRatio, isPortrait, note, onNoteChange, dataUrl, imageUrl, status, selected, onToggleSelect, clickToCast, slotTags, onEditTags, onCreateInstance, onImageLoad, compact, scale = 1 }: ScreenshotCardProps) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const id = getDeviceId(android, mode)
  // V4 投屏身份 = (host, backend, instanceID)：android.id 是 "backend/id" 联合主键，
  // 坑位号（indexNum）在 V4 是客户端派生序号，服务端不认识。拆分逻辑与 useV4InstanceOps 同源。
  const isV4 = mode === 'v4'
  const v4Slash = isV4 ? android.id.indexOf('/') : -1
  const v4Backend = v4Slash > 0 ? android.id.slice(0, v4Slash) : ''
  const v4InstanceID = isV4 ? (v4Slash > 0 ? android.id.slice(v4Slash + 1) : android.id) : ''
  const [castTarget, setCastTarget] = useState<{ ip: string; slot: number } | null>(null)
  const [moveTarget, setMoveTarget] = useState(false)
  const [exportTarget, setExportTarget] = useState(false)
  const [upgradeTarget, setUpgradeTarget] = useState<AndroidInfo | null>(null)
  const [terminalTarget, setTerminalTarget] = useState(false)
  const [switchModelTarget, setSwitchModelTarget] = useState(false)
  const [vpcTarget, setVpcTarget] = useState(false)
  const [fileManagerTarget, setFileManagerTarget] = useState(false)
  const [apiDetailTarget, setApiDetailTarget] = useState(false)
  const [copyTarget, setCopyTarget] = useState(false)
  const [streamTarget, setStreamTarget] = useState(false)
  const [renameTarget, setRenameTarget] = useState(false)
  const [switchAndroidTarget, setSwitchAndroidTarget] = useState(false)
  const [switchBackupTarget, setSwitchBackupTarget] = useState(false)
  const [resetTarget, setResetTarget] = useState(false)
  const [s5Target, setS5Target] = useState(false)
  const [gpsTarget, setGpsTarget] = useState(false)
  const [profileTarget, setProfileTarget] = useState(false)
  const [googleCertTarget, setGoogleCertTarget] = useState(false)
  const [noteTarget, setNoteTarget] = useState(false)
  const { confirmAction, setConfirmAction, ask } = useConfirm()
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(null)
  // 截图显示：换图防闪（预加载+decode 成功后才替换 src）。
  // 详见 screenshotDetail/useStableScreenshot.ts。
  // 容器比例 = 宿主下发的「本页主流截图比例」（整页统一 → 卡片等高）；图片 object-cover
  // 填满容器：比例一致时零黑边零裁切，个别不同分辨率的 ROM 极轻微裁切（远优于左右黑边）。
  // 历史：曾用固定 9:16 + object-contain，V4 的 9:20 图在 9:16 容器里左右留黑边（用户否决）；
  // 也曾按单图真实比例改容器高度，但各 ROM 分辨率不同 → 卡片墙高度参差不齐（用户否决）。
  // 截图显示源：imageUrl（asset server 二进制通道，版本号在 URL 里）优先，
  // dataUrl（base64 dataURL 过 IPC）回落——imageUrl 是新主通道，dataUrl 保留兜底
  //（HTTP 通道异常时按需补拉仍走 IPC 拿 dataURL，卡片不因通道切换而无图）。
  const stableDataUrl = useStableImageSrc(imageUrl ?? dataUrl)
  // 双保险：缓存侧已用「渲染保护集」保证正在显示的图不被清理（screenshotLru.ts），
  // 这里再兜一层——dataUrl 短暂变 null（poll 失败写 null 等场景）时，显示层保留上一张
  // 成功的图，等按需补拉回来再换，避免整块闪「加载中」。注意只对「有截图状态但缺图」
  // 生效：切模式/关机/空坑位走 status 变化路径，不受此 ref 影响（stableDataUrl 为 null
  // 时渲染分支会正确切换到占位文案）。
  const lastScreenshotRef = useRef<string | null>(null)
  if (imageUrl ?? dataUrl) lastScreenshotRef.current = imageUrl ?? dataUrl as string
  const displayDataUrl = stableDataUrl ?? (status === 'screenshot' ? lastScreenshotRef.current : null)
  // 切换新机中的本地标记：确认切换后设为true，显示黑屏；截图源内容变化时解除
  const [switchingModel, setSwitchingModel] = useState(false)
  const prevDataUrlRef = useRef<string | null>(null)
  useEffect(() => {
    const src = imageUrl ?? dataUrl
    if (switchingModel && src && src !== prevDataUrlRef.current) {
      setSwitchingModel(false)
    }
    if (!switchingModel && src) {
      prevDataUrlRef.current = src
    }
  }, [imageUrl, dataUrl, switchingModel])
  // 备注：入口=右键菜单「设置备注」（AndroidNoteModal；备注行已删——2026-10-10 用户要求）
  const saveNote = async (id: string, value: string) => {
    await DiscoverService.SetAndroidNote(id, value)
    onNoteChange(id, value)
    ScreencastService.UpdateProjectionTitle(id, android.indexNum, value || android.name || '').catch(() => {})
  }
  // 标签绑容器（设备端 /android/label）：写入不依赖容器运行/截图就绪，
  // 截图未就绪（开机中/关机）的云机同样可打标——编辑入口已上移 CardHeader。
  const canEditTags = true
  // 悬停信息卡：停 300ms 弹出基本信息
  const infoTip = useInfoTipHover()
  // V4 剪贴板：读本机剪贴板 → POST /android/clipboard（paste=true 直贴云机焦点输入框）。
  // readText 需要 WebView2 的 clipboard-read 权限，被拒/失败时 toast 明确提示。
  const pasteLocalClipboard = async () => {
    try {
      const text = await navigator.clipboard.readText()
      if (!text) { showToast(t('host:screenshot.contextMenu.clipboardEmpty'), 'error'); return }
      await SdkApiService.SetAndroidClipboard(id, android.name, text, true)
      showToast(t('host:toast.opSuccess', { op: t('host:screenshot.contextMenu.pasteClipboard') }), 'success')
    } catch (err) {
      showToast(String(t('host:screenshot.contextMenu.clipboardReadFailed', { error: String(err) })), 'error')
    }
  }
  // 有实例即显示「切换」（该坑位至少存在当前实例）
  const hasInstance = allAndroids.some(a => a.indexNum === android.indexNum && a.deviceId === android.deviceId)
  // 空坑位：坑位无任何实例数据（android.status 为空 = 占位数据），只显示「创建」，
  // 截图区显示源码 .screenshot-empty 占位
  const isEmptySlot = !android.status

  // 右键菜单数据：从 JSX 提取为 menuGroups，v4Hide 标记 V4 无对应端点的项。
  // 渲染处按 isV4 过滤后交给 AndroidContextMenu（组件自身已过滤空组）。
  // 注意：v2 条件 spread 保留原判断（!(mode === 'v2')），v4Hide 不改变 v2 行为——
  // v4Hide 只在 isV4 时生效，v2/local/public 模式下菜单与改造前完全一致。
  const menuGroups: CardMenuGroup[] = [
    // 以下按功能域分组（文件/设备配置/镜像与备份/传输/网络/V4专属/危险/信息），
    // 各项的 onClick/disabled/v4Hide/v2 条件表达式均未改动，只挪组
    {
      // 组1 文件
      items: [
        { icon: FolderOpen, label: t('host:screenshot.contextMenu.fileManager'), disabled: status !== 'screenshot', onClick: () => { setFileManagerTarget(true); setContextMenu(null) }, v4Hide: true },
      ],
    },
    {
      // 组2 设备配置
      items: [
        // v4Hide：V4 无 tags 写入端点
        { icon: Tags, label: t('host:tags.setTags'), disabled: !canEditTags || !onEditTags, onClick: () => { const cx = contextMenu?.x ?? 0; const cy = contextMenu?.y ?? 0; setContextMenu(null); onEditTags?.(cx, cy, android) }, v4Hide: true },
        { icon: Pencil, label: t('host:deviceList.setNote'), onClick: () => { setNoteTarget(true); setContextMenu(null) } },
        { icon: RadioTower, label: t('host:screenshot.contextMenu.setStream'), onClick: () => { setStreamTarget(true); setContextMenu(null) }, v4Hide: true },
        { icon: MapPin, label: t('host:screenshot.contextMenu.gpsLocation'), onClick: () => { setGpsTarget(true); setContextMenu(null) }, v4Hide: true },
        { icon: Vibrate, label: t('host:screenshot.contextMenu.shake'), onClick: () => { doSdk(t('host:actionBar.shake'), () => SdkApiService.Shake(id, android.name), id, android.indexNum)(); setContextMenu(null) }, v4Hide: true },
      ],
    },
    {
      // 组3 镜像与备份（V2 无 8000 端口且壳子（V2 Web）无对应方法，砍掉——审计见 discuss/V2右键菜单功能清单.md）
      items: [
        // 新机（切换机型）：壳子标记 v3-only（V2 Web 无此项）→ v2 砍
        ...(!(mode === 'v2') ? [{ icon: Smartphone, label: t('host:screenshot.contextMenu.newMachine'), onClick: () => { setSwitchModelTarget(true); setContextMenu(null) }, v4Hide: true }] : []),
        ...(!(mode === 'v2') ? [
          { icon: RefreshCw, label: t('host:screenshot.contextMenu.switchBackup'), onClick: () => { setSwitchBackupTarget(true); setContextMenu(null) }, v4Hide: true },
          { icon: ArrowUpCircle, label: t('host:screenshot.contextMenu.update'), onClick: () => { setUpgradeTarget(android); setContextMenu(null) }, v4Hide: true },
        ] : []),
      ],
    },
    // 组4 传输：v2 整组砍（组级条件原样保留）
    ...(!(mode === 'v2') ? [{
      items: [
        { icon: MoveRight, label: t('host:screenshot.contextMenu.move'), onClick: () => { setMoveTarget(true); setContextMenu(null) }, v4Hide: true },
        { icon: Copy, label: t('host:screenshot.contextMenu.copy'), onClick: () => { setCopyTarget(true); setContextMenu(null) }, v4Hide: true },
        { icon: Upload, label: t('host:screenshot.contextMenu.export'), onClick: () => { setExportTarget(true); setContextMenu(null) }, v4Hide: true },
      ],
    }] : []),
    {
      // 组5 网络
      items: [
        { icon: Network, label: t('host:screenshot.contextMenu.vpc'), onClick: () => { setVpcTarget(true); setContextMenu(null) }, v4Hide: true },
        // S5 代理：开/关合并为一个入口（2026-09-27）——弹窗内查询状态并提供「关闭代理」
        { icon: Shield, label: t('host:screenshot.contextMenu.s5Proxy'), onClick: () => { setS5Target(true); setContextMenu(null) }, v4Hide: true },
        { icon: KeyRound, label: t('host:screenshot.contextMenu.googleCert'), onClick: () => { setGoogleCertTarget(true); setContextMenu(null) }, v4Hide: true },
      ],
    },
    {
      // 组6 V4 专属：粘贴本机剪贴板（POST /android/clipboard，paste=true 直贴焦点输入框）
      // 与设备档案（POST /android/configure）；V3 无对应端点，v2/local 不显示
      items: [
        ...(isV4 ? [{ icon: ClipboardPaste, label: t('host:screenshot.contextMenu.pasteClipboard'), onClick: () => { pasteLocalClipboard(); setContextMenu(null) } }] : []),
        ...(isV4 ? [{ icon: IdCard, label: t('host:screenshot.contextMenu.deviceProfile'), onClick: () => { setProfileTarget(true); setContextMenu(null) } }] : []),
      ],
    },
    {
      danger: true,
      // 组7 危险
      items: [
        { icon: AlertTriangle, label: t('host:screenshot.contextMenu.reset'), onClick: () => { setResetTarget(true); setContextMenu(null) } },
        { icon: Trash2, label: t('host:screenshot.contextMenu.delete'), onClick: () => { ask(t('host:confirm.deleteInstance.title'), t('host:confirm.deleteInstance.message'), doSdk(t('host:actionBar.delete'), () => SdkApiService.DeleteAndroid(id, android.name), id, android.indexNum)); setContextMenu(null) } },
      ],
    },
    {
      // 组8 信息
      items: [
        { icon: Info, label: t('host:screenshot.contextMenu.apiDetail'), onClick: () => { setApiDetailTarget(true); setContextMenu(null) } },
      ],
    },
  ]

  // 顶部横排快捷按钮区（Win11 风格五操作：投屏/终端/重命名/重启/关机）。
  // 这五项已从下方 menuGroups 移除（避免与快捷区重复），v2 砍掉重命名，过滤后为空不渲染。
  // 五项结构抽共享（screenshotDetail/quickActions.ts，与坑位表 SlotContextMenu 同源），
  // disabled 口径与 handler 按卡片上下文注入。
  const quickActions: CardMenuItem[] = buildQuickActions({
    t,
    castDisabled: isV4 ? !isRunnableStatus(android.status) : status !== 'screenshot',
    terminalDisabled: !(mode === 'v2' || isV4) && status !== 'screenshot',
    hideRename: mode === 'v2',
    onCast: () => { setCastTarget({ ip: id, slot: android.indexNum }); setContextMenu(null) },
    onTerminal: () => { setTerminalTarget(true); setContextMenu(null) },
    onRename: () => { setRenameTarget(true); setContextMenu(null) },
    onRestart: () => { ask(t('host:confirm.restart.title'), t('host:confirm.restart.message', { name: android.name }), doSdk(t('host:actionBar.restart'), () => SdkApiService.RestartAndroid(id, android.name), id, android.indexNum)); setContextMenu(null) },
    onStop: () => { ask(t('host:confirm.stop.title'), t('host:confirm.stop.message', { name: android.name }), doSdk(t('host:actionBar.stop'), () => SdkApiService.StopAndroid(id, android.name), id, android.indexNum)); setContextMenu(null) },
  })

  return (
    <>
      <div className="group" onMouseEnter={infoTip.handlers.onMouseEnter} onMouseMove={infoTip.handlers.onMouseMove} onMouseLeave={infoTip.handlers.onMouseLeave} onContextMenu={e => { e.preventDefault(); setContextMenu({ x: e.clientX, y: e.clientY }) }}>
        {/* 外框：hover 上浮 + 阴影过渡（源码 .cloud-machine-card hover: translateY(-2px) + box-shadow）；
            圆角随缩放（源码 border-radius: calc(8px*scale)） */}
        <div
          className="rounded-lg border border-gray-200 bg-white shadow-sm overflow-hidden transition-all duration-200 hover:shadow-lg hover:-translate-y-0.5"
          style={{ ['--card-scale' as string]: scale, borderRadius: `calc(8px * var(--card-scale))` }}
        >
        {/* ① header 信息条：勾选圈+坑位号 / 实例名 / 标签chips+备份tag（勾选点击区整体可点；
            名称+标签回到 header（源码 .cloud-machine-card-header-light 形态，2026-10-10 用户定稿） */}
        <CardHeader
          android={android}
          selected={selected}
          onEditTags={onEditTags}
          status={android.status}
          scale={scale}
          note={note}
          onToggleSelect={onToggleSelect}
        />
        {/* ② 截图区：内嵌（源码 el-card body padding 0，截图区贴边但上下被 header/按钮条包住）。
            容器比例由 style.aspectRatio 给（宿主下发的本页主流截图比例，整页统一 → 卡片等高） */}
        <div className={`relative bg-gray-900 overflow-hidden transition-colors ${clickToCast ? 'cursor-pointer' : selected ? 'cursor-pointer outline outline-2 outline-blue-400 outline-offset-1' : 'cursor-pointer'}`}
          style={{ aspectRatio: String(aspectRatio) }}
          onClick={clickToCast
            ? (isV4
              /* V4 没有截图，「截图就绪」这个门槛永远不成立 → 用「可运行」代替
                 （starting 阶段已能握手出帧，与 V3 的 castable 口径一致） */
              ? (isRunnableStatus(android.status) && !switchingModel
                ? () => { if (!castClickBlocked()) setCastTarget({ ip: id, slot: android.indexNum }) }
                : undefined)
              : (status === 'screenshot' && dataUrl && !switchingModel
                ? () => { if (!castClickBlocked()) setCastTarget({ ip: id, slot: android.indexNum }) }
                : undefined))
            : undefined
          }>
          {!switchingModel && status === 'screenshot' && displayDataUrl ? (
            <img src={displayDataUrl} alt={android.name}
              /* 截图卡片数量随勾选设备数线性增长（实测 115 台 = 数百张）。
                 decoding=async 把 JPEG 解码挪出主线程，避免一轮推送几十张图时
                 同步解码把 UI 卡住；loading=lazy 让滚出视口的卡片不参与渲染。
                 注意 src 用的是 useStableImageSrc 的「已解码」地址：轮询新图先预加载
                 decode，成功后才切换——直接换 src 会让浏览器先丢旧帧再解码，
                 期间该区域露出容器底色（bg-gray-900），即用户说的「闪一下」。
                 displayDataUrl 在缺图瞬间会回落到 lastScreenshotRef 的上一张成功图
                 （上方注释），同样是为了不闪。
                 object-cover：填满容器、不留黑边。容器比例取自本页主流截图比例
                 （宿主统计后下发），比例一致时零裁切；个别分辨率不同的 ROM 极轻微裁切。 */
              loading="lazy" decoding="async"
              /* 横屏：容器宽 W、高 H=W/aspectRatio（aspectRatio 为横屏值 = 1/竖屏比例）。
                 旋转后视觉宽 = 元素高、视觉高 = 元素宽，要铺满容器需 元素高=W、元素宽=H：
                 width = (1/aspectRatio)*100%（= H/W）、height = aspectRatio*100%（= W/H）。
                 元素自身比例 = (W/aspectRatio):W = 竖屏比例，object-cover 下零裁切。
                 与原先 9:16 硬编码的 w-[56.25%] h-[177.78%] 等价，只是跟随 aspectRatio 参数化。 */
              className={isPortrait ? 'w-full h-full object-cover' : 'absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 object-cover -rotate-90'}
              style={isPortrait ? undefined : { width: `${(1 / aspectRatio) * 100}%`, height: `${aspectRatio * 100}%` }}
              onLoad={onImageLoad
                ? e => onImageLoad(`${android.ip}_${android.indexNum}`, e.currentTarget.naturalWidth, e.currentTarget.naturalHeight)
                : undefined}
              onDoubleClick={clickToCast ? undefined : () => setCastTarget({ ip: id, slot: android.indexNum })} />
          ) : isEmptySlot ? (
            /* 空坑位占位（源码 .screenshot-empty：#f0f2f5 底、#909399 字、max(8px,14px*scale)），
               无图标无交互——点击不投屏、按钮区只有「创建」（与源码一致） */
            <div className="absolute inset-0 flex items-center justify-center text-gray-400"
              style={{ background: '#f0f2f5', color: '#909399', fontSize: Math.max(8, Math.round(14 * scale)) }}>
              {t('host:slotView.emptySlot')}
            </div>
          ) : (
            <div className="absolute inset-0 flex flex-col items-center justify-center text-gray-400 gap-1">
              {/* 截图区状态文案：按容器真实状态显示（starting「开机中」/ created「已创建」/
                  restarting「重启中」/ resetting「重置中」/ removing「删除中」），过渡中状态
                  与 created 用 spinner（截图链路仍在进行），running 无截图保持「加载中」。
                  文案复用 containerStatus.statusLabel（与 header 状态 tag / 占位卡同源） */}
              {status === 'stopped' ? (<><svg className="w-5 h-5 text-gray-300" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="12" cy="12" r="10"/><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"/></svg><span className="text-xs">{t('host:screenshot.stopped')}</span></>)
              : (status === 'starting' || switchingModel) ? (<><svg className="w-5 h-5 text-blue-400 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg><span className="text-xs text-blue-400">{t('host:screenshot.starting')}</span></>)
              : (status === 'created' || status === 'restarting' || status === 'resetting' || status === 'removing') ? (<><svg className="w-5 h-5 text-blue-400 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg><span className="text-xs text-blue-400">{statusLabel(status, t)}</span></>)
              : <span className="text-xs">{t('host:screenshot.loading')}</span>}
            </div>
          )}
        </div>
        {/* 备注/标签行已删（2026-10-10 用户要求）：备注入口=右键菜单「设置备注」，
            名称在 header，标签在 header（icon+数量 chip）。空坑位与正常卡均无此行，
            卡片段落结构（header/截图区/按钮条）天然等高 */}
        {/* ③ 底部按钮条：compact（云机 Tab）整条不渲染（2026-10-16 用户要求：卡片只剩
            header + 截图区，ADB 也砍——终端入口保留右键菜单）；主机 Tab（不传 compact）
            保持 创建/ADB/切换 三按钮 */}
        {!compact && (
          <CardActionBar
            status={status}
            androidStatus={isEmptySlot ? '' : android.status}
            running={false}
            hasInstance={hasInstance && !isEmptySlot}
            v2Mode={mode === 'v2'}
            scale={scale}
            onCreate={deviceInfo && onCreateInstance ? () => onCreateInstance(deviceInfo, android.indexNum) : undefined}
            onAdb={() => setTerminalTarget(true)}
            // 卡片底部「切换」= 实例切换（SwitchAndroidModal，与 android-client 真实行为对齐）；
            // 备份恢复入口只剩右键菜单「恢复备份」（switchBackupTarget）
            onSwitch={() => setSwitchAndroidTarget(true)}
          />
        )}
        </div>
      </div>

      {infoTip.tipPos && (
        <ScreenshotInfoTip android={android} note={note} mirrors={mirrors} x={infoTip.tipPos.x} y={infoTip.tipPos.y} />
      )}

      {contextMenu && (
        <AndroidContextMenu x={contextMenu.x} y={contextMenu.y} onClose={() => setContextMenu(null)}
          /* V4 部署模式裁剪：过滤掉无对应 SDK 端点的 v4Hide 项；其余模式原样全量渲染。
             过滤后组可能变空（如 V4 下第三组全隐藏），AndroidContextMenu 内部已做
             空组过滤，不会残留分隔线。 */
          groups={menuGroups
            .map(g => ({ ...g, items: isV4 ? g.items.filter(it => !it.v4Hide) : g.items }))
            .filter(g => g.items.length > 0)}
          quickActions={quickActions
            .filter(it => !(isV4 && it.v4Hide))}
          width={280}
        />
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')} onConfirm={() => { confirmAction.onConfirm(); setConfirmAction(null) }} onCancel={() => setConfirmAction(null)} />}
      {castTarget && (
        <ScreenCastModal
          deviceIP={castTarget.ip} slot={castTarget.slot}
          orientation={isPortrait ? 'portrait' : 'landscape'}
          v4Backend={v4Backend} v4InstanceID={v4InstanceID} v4Title={note || containerDisplayName(android.name, android.deviceId, android.indexNum)}
          onClose={() => setCastTarget(null)}
        />
      )}
      {moveTarget && <MoveModal deviceIP={id} deviceID={deviceId} android={android} slotExpiry={slotExpiry} androids={androids} devices={devices} displayName={note || android.name} onClose={() => setMoveTarget(false)} />}
      {exportTarget && <ExportModal deviceIP={id} androidName={android.name} onClose={() => setExportTarget(false)} />}
      {copyTarget && <CopyModal deviceIP={id} deviceID={deviceId} android={android} slotExpiry={slotExpiry} androids={androids} onClose={() => setCopyTarget(false)} />}
      {upgradeTarget && deviceInfo && <UpdateImageModal device={deviceInfo} android={upgradeTarget} onClose={() => setUpgradeTarget(null)} onUpdated={() => setUpgradeTarget(null)} />}
      {terminalTarget && (
        <TerminalModal deviceIP={id} containerName={android.name} displayName={note || android.name}
          isV2={mode === 'v2'} isV4={isV4} onClose={() => setTerminalTarget(false)} />
      )}
      {switchModelTarget && <BatchSwitchModelModal selectedAndroids={[android]} mirrors={mirrors} onClose={() => setSwitchModelTarget(false)} onSwitched={() => setSwitchingModel(true)} />}
      {vpcTarget && <VpcModal deviceIP={id} androidName={android.name} displayName={note || android.name} androids={androids} onClose={() => setVpcTarget(false)} />}
      {fileManagerTarget && <FileManagerModal deviceIP={id} androidName={android.name} displayName={note || android.name} onClose={() => setFileManagerTarget(false)} />}
      {apiDetailTarget && <ApiDetailModal android={android} deviceIP={id} onClose={() => setApiDetailTarget(false)} />}
      {streamTarget && <SetStreamModal deviceIP={id} android={android} onClose={() => setStreamTarget(false)} />}
      {renameTarget && <RenameModal android={android} onClose={() => setRenameTarget(false)} />}
      {switchAndroidTarget && (
        <SwitchAndroidModal
          androids={allAndroids.filter(a => a.indexNum === android.indexNum && a.deviceId === android.deviceId)}
          slot={android.indexNum}
          onClose={() => setSwitchAndroidTarget(false)}
        />
      )}
      {switchBackupTarget && <SwitchBackupModal android={android} onClose={() => setSwitchBackupTarget(false)} />}
      {resetTarget && <ResetModal android={android} ask={ask} onClose={() => setResetTarget(false)} />}
      {s5Target && <S5ProxyModal android={android} onClose={() => setS5Target(false)} />}
      {gpsTarget && <GpsModal android={android} onClose={() => setGpsTarget(false)} />}
      {profileTarget && <DeviceProfileModal android={android} onClose={() => setProfileTarget(false)} />}
      {googleCertTarget && <GoogleCertModal android={android} onClose={() => setGoogleCertTarget(false)} />}
      {noteTarget && <AndroidNoteModal android={android} note={note} onSave={saveNote} onClose={() => setNoteTarget(false)} />}
    </>
  )
}, (prev, next) => {
  // 只在影响渲染的关键 props 变化时重渲染
  // androids/devices/slotExpiry/mirrors 等大数组引用变化但内容不变时跳过。
  // android 内容比对（浅层逐字段）：tags/CPU/Mem 等字段随设备端推送每轮换引用，
  // 引用比对会让 memo 全失效（每秒一轮 stats = 每卡全重渲染）；浅比内容即可。
  if (prev.android === next.android) return false // 引用相同：后面内容比对全过
  if (prev.android.id !== next.android.id || prev.android.status !== next.android.status ||
      prev.android.name !== next.android.name ||
      prev.android.indexNum !== next.android.indexNum || prev.android.ip !== next.android.ip) return false
  // cpuUsage/memUsage 不比对：卡片 UI 不展示这两个字段（每秒一轮的 stats 推送
  // 只换引用不变值，比对只会把 memo 击穿），字段改动前先在此登记
  // tags 浅比（长度 + 逐项）：标签低频变更，内容同则跳过
  const prevTags = prev.android.tags ?? []
  const nextTags = next.android.tags ?? []
  if (prevTags.length !== nextTags.length || prevTags.some((t, i) => t !== nextTags[i])) return false
  if (prev.dataUrl !== next.dataUrl) return false
  if (prev.imageUrl !== next.imageUrl) return false
  if (prev.status !== next.status) return false
  if (prev.selected !== next.selected) return false
  if (prev.note !== next.note) return false
  // 标签内容已由上面 android.tags 浅比覆盖（chip 显示源已切设备端，slotTags 不再参与渲染）
  if (prev.onEditTags !== next.onEditTags) return false
  if (prev.aspectRatio !== next.aspectRatio) return false
  if (prev.isPortrait !== next.isPortrait) return false
  if (prev.onImageLoad !== next.onImageLoad) return false
  if (prev.onNoteChange !== next.onNoteChange) return false
  if (prev.onToggleSelect !== next.onToggleSelect) return false
  if (prev.onCreateInstance !== next.onCreateInstance) return false
  if (prev.compact !== next.compact) return false
  if (prev.scale !== next.scale) return false
  // allAndroids 是备份数/切换按钮的数据源：引用变化时浅比对内容
  // （长度 + 每项 id/status/indexNum/deviceId），避免每轮列表推送全量重渲染
  const prevAll = prev.allAndroids ?? []
  const nextAll = next.allAndroids ?? []
  if (prevAll.length !== nextAll.length) return false
  for (let i = 0; i < prevAll.length; i++) {
    if (prevAll[i].id !== nextAll[i].id || prevAll[i].status !== nextAll[i].status || prevAll[i].indexNum !== nextAll[i].indexNum || prevAll[i].deviceId !== nextAll[i].deviceId) return false
  }
  // slotExpiry 浅比较内容（数组长度 + 每项关键字段）
  const prevSlots = prev.slotExpiry
  const nextSlots = next.slotExpiry
  if (prevSlots.length !== nextSlots.length) return false
  for (let i = 0; i < prevSlots.length; i++) {
    if (prevSlots[i].index !== nextSlots[i].index || prevSlots[i].state !== nextSlots[i].state || prevSlots[i].expire_time !== nextSlots[i].expire_time) return false
  }
  return true
})