import { useState, useEffect, useCallback, memo, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import * as SlotService from '../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import * as AndroidListService from '../../../bindings/github.com/moyunteng/myt/androidlistservicewrapper'
import ConfirmDialog from '../../components/ConfirmDialog'
import { useNavigation } from '../../hooks/useNavigation'
import type { DeviceState } from '../../types'
import DeviceList from './components/DeviceList'
import ActionBar from './components/ActionBar'
import ScreenshotView from './components/ScreenshotView'
import MultiDeviceSlotView from './components/MultiDeviceSlotView'
import CreateInstanceModal from './create/CreateInstanceModal'
import CreateInstanceV4Dialog from './create/v4/CreateInstanceV4Dialog'
import { useHostManagement } from './useHostManagement'
import { useSdkVersions } from '../../hooks/useSdkVersions'
import { useDeployMode } from '../../hooks/useDeployMode'
import { useCardScale } from '../../hooks/useCardScale'
import { useSingleCastScale } from '../../hooks/useSingleCastScale'

function HostManagement() {
  const { t } = useTranslation()
  const navigate = useNavigation()
  const { mode: deployMode } = useDeployMode()
  // 创建云机：卡片「创建」按钮回调带坑位号 → createTarget 记设备 + 预选坑位
  const [createSlot, setCreateSlot] = useState<number | undefined>(undefined)
  // 购买引导（非阻断）：目标设备未购买授权实例（bind_status 0=未绑定 / 2=被其他账号
  // 绑定）时，打开创建弹窗前先弹 ConfirmDialog 建议去授权页；undefined=未知（如缓存
  // omitempty 缺字段）不提示，避免误弹。确认跳 payment Tab；取消/Esc 关提示后**照常
  // 打开创建弹窗**（记录 pending slot，取消路径回放）。只提示不拦截。
  const [bindPrompt, setBindPrompt] = useState<{ device: DeviceState; slot?: number } | null>(null)
  const {
    devices, scanning, selectedDevice, selectedDeviceIds,
    viewMode, handleViewModeChange, orientation, setOrientation,
    createTarget, setCreateTarget, androidList, slotExpiry,
    selectedAndroidIds, mirrors, readyKeys, setReadyKeys,
    discoverDevices, handleSelectDevice, handleToggleSelect,
    handleExcludeAndroidFromDevice,
    handleSelectAll, handleDeselectAll, handleDeleteDevice,
    handleToggleAndroidSelect, handleSelectAllAndroids,
    handleDeselectAllAndroids, handleToggleGroupSelect, handleToggleGroupDevices, handleSetSelectedAndroidIds,
    pendingDevices, handleAddPendingDevices, handleAddAllPendingDevices,
    handleClearPendingDevices, handleDeleteDevices,
    // 勾选设备中「正在跑」的云机（running/starting）
    // 有效容器集合（侧栏过滤器收窄后）：右侧两视图的展示集合
    effectiveAndroids,
    selectedAndroids,
    // 侧栏过滤器（决定右侧展示范围）+ 其切换回调：左侧勾选走它，与右侧批量勾选分离
    sidebarAndroidIds, handleToggleSidebarAndroid,
    // 设备 ID → 该设备下的云机列表（多设备坑位视图按设备分区、侧栏设备行展开子列表用）
    androidGroups,
    // 坑位标签控制包：下发给两视图，与 effectiveAndroids 的标签收窄同源
    tagControl,
    // 侧栏设备行展开状态（展示运行中容器子列表）+ 容器级勾选（联动设备勾选）
    expandedDeviceIds, handleToggleDeviceExpand,
    // 「云机展示=全部」设置：视图层据此放宽 onlyRunning / 渲染关机占位卡
    showOffline,
  } = useHostManagement()

  // 创建云机统一入口：bind_status 0=未购买、2=被其他账号绑定 → 先弹购买引导（非阻断，
  // 取消后仍打开创建弹窗，见 bindPrompt）；1=已绑定或缺失/未知 → 直接放行。所有创建
  // 按钮（侧栏 DeviceList、截图/坑位卡片、切换弹窗）都汇聚到此。
  const openCreateInstance = useCallback((device: DeviceState, slot?: number) => {
    const bs = device.bind_status
    if (bs === 0 || bs === 2) { setBindPrompt({ device, slot }); return }
    setCreateSlot(slot)
    setCreateTarget(device)
  }, [setCreateTarget])

  // 设备 SDK 版本：低版本（<208）设备在列表标记提示（部分 WS 功能不可用）
  const { lowSdkIds } = useSdkVersions(devices)

  const [cardScale, setCardScale] = useCardScale() // 缩放滑条持久化（localStorage，android-client 同款）
  const [singleCastScale, setSingleCastScale] = useSingleCastScale() // 单台投屏窗口大小（localStorage + Go config 落档）

  // 截图模式/坑位模式的列表过滤必须 useMemo：截图服务每秒推一轮变化，
  // ScreenshotView 内部会因此重渲染；若这里内联 filter，每次都是新数组引用，
  // 下游 groupedAndroids → slotCountMap effect → filteredGroups → 每张卡片
  // 全链路 memo 一起失效（115 台设备时表现为整页每秒重渲染 = 卡顿）。
  const screenshotModeAndroids = useMemo(
    // 截图模式只跟随有效容器集合：容器级勾选收窄（左侧勾了某设备的部分容器 → 只展示
    // 勾选的那些），单击查看（selectedDevice）不并入显示范围——否则取消最后一个勾选时
    // 截图卡片永远残留一台（无途径清空 selectedDevice）
    () => androidList.filter(a => effectiveAndroids.includes(a)),
    [androidList, effectiveAndroids],
  )
  // 坑位模式（多设备）：分区来源 = 勾选设备 ∪ 有效集合中的未勾选设备（仅勾容器未勾
  // 设备时，该设备的分区也要出现，否则其容器在列表模式无处渲染）。
  const slotModeDevices = useMemo(() => {
    const inEffective = new Set(effectiveAndroids.map(a => a.deviceId))
    return devices.filter(d => selectedDeviceIds.has(d.device_id) || inEffective.has(d.device_id))
  }, [devices, selectedDeviceIds, effectiveAndroids])

  // 坑位模式的 androidGroups 按有效容器集合收窄：容器级勾选后，分区头统计/全选框、
  // SlotView 的坑位分组都只认被勾选的容器（无勾选的设备保持全量）。
  // ⚠️ 弹窗（移动/复制/VPC 等用全量坑位判定冲突）会受影响——收窄后弹窗只见到
  // 被展示的容器，但那些操作都是从可见行发起的（可见=可操作），语义自洽。
  const slotModeAndroidGroups = useMemo(() => {
    if (effectiveAndroids.length === androidList.length) return androidGroups
    const ids = new Set(effectiveAndroids.map(a => a.id))
    const out: Record<string, typeof androidList> = {}
    for (const [deviceId, list] of Object.entries(androidGroups)) {
      const kept = list.filter(a => ids.has(a.id))
      if (kept.length > 0) out[deviceId] = kept
    }
    return out
  }, [androidGroups, effectiveAndroids, androidList])

  // 坑位数：多设备坑位视图每个设备一份，页面级并行获取后分发（各 SlotView 不再自行 IPC）。
  // 依赖用「设备 ID 串」签名而非数组引用——否则每次 render 都会重发一轮跨进程请求
  // （ScreenshotView 同款写法，115 台设备时这是「设备一多就卡」的主因之一）。
  const slotCountDeviceIds = useMemo(
    () => slotModeDevices.map(d => d.device_id).sort(),
    [slotModeDevices],
  )
  const slotCountSignature = slotCountDeviceIds.join(',')
  const [slotCountMap, setSlotCountMap] = useState<Record<string, number>>({})
  useEffect(() => {
    if (!slotCountSignature) { setSlotCountMap({}); return }
    let cancelled = false
    const ids = slotCountSignature.split(',')
    Promise.all(ids.map(async id => {
      try {
        return [id, await SlotService.GetSlotCount(id) as number] as const
      } catch {
        return null
      }
    })).then(pairs => {
      if (cancelled) return
      const next: Record<string, number> = {}
      for (const p of pairs) if (p) next[p[0]] = p[1]
      setSlotCountMap(next)
    })
    return () => { cancelled = true }
  }, [slotCountSignature])

  return (
    <div className="flex h-full">
      <DeviceList
        devices={devices}
        selectedDevice={selectedDevice}
        selectedDeviceIds={selectedDeviceIds}
        viewMode={viewMode}
        scanning={scanning}
        pendingDevices={pendingDevices}
        lowSdkIds={lowSdkIds}
        onSelect={handleSelectDevice}
        onToggleSelect={handleToggleSelect}
        onSelectAll={handleSelectAll}
        onDeselectAll={handleDeselectAll}
        onDelete={handleDeleteDevice}
        onDeleteDevices={handleDeleteDevices}
        onAddPendingDevices={handleAddPendingDevices}
        onAddAllPendingDevices={handleAddAllPendingDevices}
        onClearPendingDevices={handleClearPendingDevices}
        // v2 模式创建已接通（sdkapi v2 翻译层 → 81 dc_api），入口与本地一致
        onCreateInstance={openCreateInstance}
        onRefresh={discoverDevices}
        androidGroups={androidGroups}
        expandedDeviceIds={expandedDeviceIds}
        onToggleDeviceExpand={handleToggleDeviceExpand}
        onToggleAndroidSelect={handleToggleSidebarAndroid}
        onExcludeAndroidFromDevice={handleExcludeAndroidFromDevice}
        selectedAndroidIds={sidebarAndroidIds}
        onSelectGroupDevices={handleToggleGroupDevices}
      />
      <div className="flex-1 flex flex-col">
        <ActionBar
          viewMode={viewMode}
          onViewModeChange={handleViewModeChange}
          orientation={orientation}
          onOrientationChange={setOrientation}
          selectedAndroids={selectedAndroids}
          // N/M 分母 = 当前展示集合（effectiveAndroids，含关机云机）：全选范围同源，
          // 点全选必然 N === M
          allAndroids={effectiveAndroids}
          slotAndroids={effectiveAndroids}
          mirrors={mirrors}
          devices={devices}
          readyKeys={readyKeys}
          slotExpiry={slotExpiry}
          onSelectAllAndroids={handleSelectAllAndroids}
          onDeselectAllAndroids={handleDeselectAllAndroids}
          hasSelectedDevices={selectedDeviceIds.size > 0}
        />
        <div className="flex-1 overflow-auto px-4 pb-4">
          {viewMode === 'screenshot' ? (
            <ScreenshotView
              androids={screenshotModeAndroids} allAndroids={androidList} devices={devices}
              slotExpiry={slotExpiry} mirrors={mirrors}
              orientation={orientation} onOrientationChange={setOrientation}
              hasSelectedDevices={selectedDeviceIds.size > 0}
              selectedAndroidIds={selectedAndroidIds}
              onToggleAndroidSelect={handleToggleAndroidSelect}
              onToggleGroupSelect={handleToggleGroupSelect}
              onReadyKeysChange={setReadyKeys}
              cardScale={cardScale}
              onCardScaleChange={setCardScale}
              singleCastScale={singleCastScale}
              onSingleCastScaleChange={setSingleCastScale}
              layout="flat"
              tagControl={tagControl}
              showOffline={showOffline}
              onCreateInstance={(d, slot) => openCreateInstance(d, slot)}
            />
          ) : viewMode === 'slot' ? (
            <MultiDeviceSlotView
              devices={slotModeDevices}
              allDevices={devices}
              androidGroups={slotModeAndroidGroups}
              slotExpiry={slotExpiry}
              slotCountMap={slotCountMap}
              mirrors={mirrors}
              selectedIds={selectedAndroidIds}
              onToggleSelect={handleToggleAndroidSelect}
              onSetSelectedIds={handleSetSelectedAndroidIds}
              tagControl={tagControl}
              showOffline={showOffline}
            />
          ) : (
            <div className="flex items-center justify-center h-full text-gray-400">
              {t('host:empty.selectDevice')}
            </div>
          )}
        </div>
      </div>

      {/* 创建云机：V4 的创建流程（tobox /android/create：资源配置 + 后端 + 镜像变体）
          与 V3（坑位/机型/镜像 URL）完全不同，只有弹窗这一处分叉；入口、布局、
          其余交互与 V3 一致。创建成功后 Go 侧已 REST 补拉并推 androidlist:updated，
          这里再拉一次兜底（与 V4 的刷新语义一致）。 */}
      {createTarget && (deployMode === 'v4' ? (
        <CreateInstanceV4Dialog
          host={createTarget.ip || ''}
          onClose={() => { setCreateTarget(null); setCreateSlot(undefined) }}
          onCreated={() => {
            const ips = devices.map(d => d.ip).filter(Boolean)
            if (ips.length > 0) AndroidListService.NotifyAndroidsChanged(ips)
            setCreateTarget(null); setCreateSlot(undefined)
          }}
        />
      ) : (
        <CreateInstanceModal
          device={createTarget}
          slotExpiry={slotExpiry[createTarget.device_id] ?? []}
          defaultSlot={createSlot}
          onClose={() => { setCreateTarget(null); setCreateSlot(undefined) }}
          onCreated={() => { setCreateTarget(null); setCreateSlot(undefined) }}
        />
      ))}

      {/* 购买引导（非阻断）：未购买（bind_status 0/2）点创建时先提示，「去授权页」跳转；
          取消/Esc 关提示后照常打开创建弹窗（回放 pending slot） */}
      {bindPrompt && (
        <ConfirmDialog
          title={t('host:confirm.bindPrompt.title')}
          message={t('host:confirm.bindPrompt.message')}
          confirmLabel={t('host:confirm.bindPrompt.goPayment')}
          onConfirm={() => { setBindPrompt(null); navigate('payment') }}
          onCancel={() => { setCreateSlot(bindPrompt.slot); setCreateTarget(bindPrompt.device); setBindPrompt(null) }}
        />
      )}
    </div>
  )
}

export default memo(HostManagement)
