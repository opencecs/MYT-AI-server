import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo } from '../../../../types/index'
import { getDeviceId } from '../../../../utils/deviceId'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { AskFn } from '../../../../hooks/useConfirm'
import { doSdk } from '../../../../utils/sdkHelper'
import { isRunnableStatus } from '../../../../utils/containerStatus'
import AndroidContextMenu from '../../../../components/AndroidContextMenu'
import type { CardMenuItem, CardMenuGroup } from '../screenshotDetail/cardMenuTypes'
import { buildQuickActions } from '../screenshotDetail/quickActions'
import { FolderOpen, Smartphone, RefreshCw, ArrowUpCircle, MoveRight, Copy, Upload, Pencil, Network, RadioTower, MapPin, Vibrate, KeyRound, Shield, Trash2, Info, AlertTriangle, ClipboardPaste, IdCard, Tags } from 'lucide-react'

/** 列表模式（SlotView）「...」按钮菜单项/组：与卡片右键（ScreenshotCard）完全同源，
 *  在 MenuItemConfig/MenuGroup 上扩展 v4Hide 标记——V4（tebox）部署模式无对应
 *  SDK 端点的项在 V4 下隐藏，v2/local/public 不受影响。
 *  （2026-10-28）补齐 V4 专属两项（粘贴本机剪贴板/设备档案）并去掉「切换云机」，
 *  （2026-10-30）终端去 v4Hide（V4 走实例 ADB 直连）+ 补「设置标签」「设置备注」，
 *  V4 下可见项与 ScreenshotCard 右键完全一致。切换云机弹窗（SwitchAndroidModal）
 *  组件保留，其余入口（SlotPlaceholderCard 点击占位区等）不受影响。
 *  （v4Hide 类型定义抽到 screenshotDetail/cardMenuTypes.ts，快捷五按钮抽 quickActions.ts。 */
type SlotMenuItem = CardMenuItem
type SlotMenuGroup = CardMenuGroup

/** 坑位模式操作菜单：setter 由父组件透传，统一模式 setX(a)+close。 */
export interface SlotContextMenuProps {
  target: { android: AndroidInfo; x: number; y: number }
  ask: AskFn
  onClose: () => void
  onCast: (ip: string, slot: number) => void
  onTerminal: (a: AndroidInfo) => void
  onFileManager: (a: AndroidInfo) => void
  onSwitchModel: (a: AndroidInfo) => void
  onSwitchBackup: (a: AndroidInfo) => void
  onUpgrade: (a: AndroidInfo) => void
  onMove: (a: AndroidInfo) => void
  onCopy: (a: AndroidInfo) => void
  onExport: (a: AndroidInfo) => void
  onRename: (a: AndroidInfo) => void
  onVpc: (a: AndroidInfo) => void
  onStream: (a: AndroidInfo) => void
  onGps: (a: AndroidInfo) => void
  onGoogleCert: (a: AndroidInfo) => void
  onS5: (a: AndroidInfo) => void
  onReset: (a: AndroidInfo) => void
  onApiDetail: (a: AndroidInfo) => void
  /** 粘贴本机剪贴板（V4 专属，POST /android/clipboard paste=true）；父组件实现 */
  onPasteClipboard: (a: AndroidInfo) => void
  /** 设备档案（V4 专属，POST /android/configure）；父组件打开 DeviceProfileModal */
  onDeviceProfile: (a: AndroidInfo) => void
  /** 设置标签（POST /android/label，不要求容器运行）；父组件打开 SlotTagEditor 浮层 */
  onEditTags: (a: AndroidInfo) => void
  /** 设置备注（SetAndroidNote）；父组件打开 AndroidNoteModal */
  onSetNote: (a: AndroidInfo) => void
}

export function SlotContextMenu({ target, ask, onClose, onCast, onTerminal, onFileManager, onSwitchModel, onSwitchBackup, onUpgrade, onMove, onCopy, onExport, onRename, onVpc, onStream, onGps, onGoogleCert, onS5, onReset, onApiDetail, onPasteClipboard, onDeviceProfile, onEditTags, onSetNote }: SlotContextMenuProps) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const isV4 = mode === 'v4'
  const a = target.android
  const id = getDeviceId(a, mode)
  const close = () => { onClose() }
  const act = (fn: () => void) => () => { fn(); close() }
  const status: 'screenshot' = 'screenshot'
  // V2 无 8000 端口，壳子（V2 Web）也没有对应方法的 7 项直接砍掉
  // （切换备份/更新/移动/复制/导出/重命名/VPC——审计见 discuss/V2右键菜单功能清单.md）
  const cut = mode === 'v2'
  const groups: SlotMenuGroup[] = [
    // 以下按功能域分组（文件/设备配置/镜像与备份/传输/网络/V4专属/危险/信息），
    // 与 ScreenshotCard 的 menuGroups 同源；各项的 onClick/disabled/v4Hide/v2 条件表达式均未改动，只挪组
    {
      // 组1 文件
      items: [
        { icon: FolderOpen, label: t('host:screenshot.contextMenu.fileManager'), disabled: status !== 'screenshot', onClick: act(() => onFileManager(a)), v4Hide: true },
      ],
    },
    {
      // 组2 设备配置
      items: [
        // 设置标签/设置备注：与卡片右键同源（标签 v4Hide——V4 无 tags 写入端点）
        { icon: Tags, label: t('host:tags.setTags'), onClick: act(() => onEditTags(a)), v4Hide: true },
        { icon: Pencil, label: t('host:deviceList.setNote'), onClick: act(() => onSetNote(a)) },
        { icon: RadioTower, label: t('host:screenshot.contextMenu.setStream'), onClick: act(() => onStream(a)), v4Hide: true },
        { icon: MapPin, label: t('host:screenshot.contextMenu.gpsLocation'), onClick: act(() => onGps(a)), v4Hide: true },
        { icon: Vibrate, label: t('host:screenshot.contextMenu.shake'), onClick: act(() => doSdk(t('host:actionBar.shake'), () => SdkApiService.Shake(id, a.name), id, a.indexNum)()), v4Hide: true },
      ],
    },
    {
      // 组3 镜像与备份（V2 无 8000 端口且壳子（V2 Web）无对应方法，砍掉——审计见 discuss/V2右键菜单功能清单.md）
      items: [
        // 新机（切换机型）：壳子标记 v3-only（V2 Web 无此项）→ v2 砍；V4 无端点 → v4Hide
        ...(!cut ? [{ icon: Smartphone, label: t('host:screenshot.contextMenu.newMachine'), onClick: act(() => onSwitchModel(a)), v4Hide: true }] : []),
        ...(!cut ? [
          { icon: RefreshCw, label: t('host:screenshot.contextMenu.switchBackup'), onClick: act(() => onSwitchBackup(a)), v4Hide: true },
          { icon: ArrowUpCircle, label: t('host:screenshot.contextMenu.update'), onClick: act(() => onUpgrade(a)), v4Hide: true },
        ] : []),
      ],
    },
    // 组4 传输：v2 整组砍（组级条件原样保留）
    ...(!cut ? [{ items: [
      { icon: MoveRight, label: t('host:screenshot.contextMenu.move'), onClick: act(() => onMove(a)), v4Hide: true },
      { icon: Copy, label: t('host:screenshot.contextMenu.copy'), onClick: act(() => onCopy(a)), v4Hide: true },
      { icon: Upload, label: t('host:screenshot.contextMenu.export'), onClick: act(() => onExport(a)), v4Hide: true },
    ]}] : []),
    {
      // 组5 网络
      items: [
        { icon: Network, label: t('host:screenshot.contextMenu.vpc'), onClick: act(() => onVpc(a)), v4Hide: true },
        // S5 代理：开/关合并为一个入口（2026-09-27）——弹窗内查询状态并提供「关闭代理」
        { icon: Shield, label: t('host:screenshot.contextMenu.s5Proxy'), onClick: act(() => onS5(a)), v4Hide: true },
        { icon: KeyRound, label: t('host:screenshot.contextMenu.googleCert'), onClick: act(() => onGoogleCert(a)), v4Hide: true },
      ],
    },
    {
      // 组6 V4 专属：粘贴本机剪贴板（POST /android/clipboard，paste=true 直贴云机焦点输入框）
      // 与设备档案（POST /android/configure）；V3 无对应端点，v2/local 不显示（与卡片右键同源）
      items: [
        ...(isV4 ? [{ icon: ClipboardPaste, label: t('host:screenshot.contextMenu.pasteClipboard'), onClick: act(() => onPasteClipboard(a)) }] : []),
        ...(isV4 ? [{ icon: IdCard, label: t('host:screenshot.contextMenu.deviceProfile'), onClick: act(() => onDeviceProfile(a)) }] : []),
      ],
    },
    { danger: true,
      // 组7 危险
      items: [
        { icon: AlertTriangle, label: t('host:screenshot.contextMenu.reset'), onClick: act(() => onReset(a)) },
        { icon: Trash2, label: t('host:screenshot.contextMenu.delete'), onClick: () => { ask(t('host:confirm.deleteInstance.title'), t('host:confirm.deleteInstance.message'), doSdk(t('host:actionBar.delete'), () => SdkApiService.DeleteAndroid(id, a.name), id, a.indexNum)); close() } },
      ]},
    {
      // 组8 信息
      items: [
        { icon: Info, label: t('host:screenshot.contextMenu.apiDetail'), onClick: act(() => onApiDetail(a)) },
      ]},
  ]

  // 顶部横排快捷按钮区（Win11 风格五操作：投屏/终端/重命名/重启/关机）。
  // 这五项已从下方 groups 移除（避免与快捷区重复）；v2 砍掉重命名，过滤后为空不渲染。
  // 五项结构抽共享（screenshotDetail/quickActions.ts，与卡片右键 ScreenshotCard 同源），
  // disabled 口径与 handler 按坑位表上下文注入（status 恒 'screenshot'，cast 由父级
  // onCast 回调发起）。
  const quickActions: SlotMenuItem[] = buildQuickActions({
    t,
    castDisabled: isV4 ? !isRunnableStatus(a.status) : false,
    terminalDisabled: !(mode === 'v2' || isV4) && status !== 'screenshot',
    hideRename: cut,
    onCast: act(() => onCast(id, a.indexNum)),
    onTerminal: act(() => onTerminal(a)),
    onRename: act(() => onRename(a)),
    onRestart: () => { ask(t('host:confirm.restart.title'), t('host:confirm.restart.message', { name: a.name }), doSdk(t('host:actionBar.restart'), () => SdkApiService.RestartAndroid(id, a.name), id, a.indexNum)); close() },
    onStop: () => { ask(t('host:confirm.stop.title'), t('host:confirm.stop.message', { name: a.name }), doSdk(t('host:actionBar.stop'), () => SdkApiService.StopAndroid(id, a.name), id, a.indexNum)); close() },
  })

  return (
    <AndroidContextMenu x={target.x} y={target.y} onClose={onClose}
      /* V4（tebox）裁剪：过滤掉无对应 SDK 端点的 v4Hide 项；其余模式原样全量渲染。
         过滤后组可能变空，AndroidContextMenu 内部已做空组过滤，不会残留分隔线。 */
      groups={groups
        .map(g => ({ ...g, items: isV4 ? g.items.filter(it => !it.v4Hide) : g.items }))
        .filter(g => g.items.length > 0)}
      quickActions={quickActions.filter(it => !(isV4 && it.v4Hide))}
      width={280}
    />
  )
}
