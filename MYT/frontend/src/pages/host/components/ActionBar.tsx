import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { RefreshCw, Settings } from 'lucide-react'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getDeviceId } from '../../../utils/deviceId'
import * as AndroidListService from '../../../../bindings/github.com/moyunteng/myt/androidlistservicewrapper'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as ScreencastService from '../../../../bindings/github.com/moyunteng/myt/screencastservicewrapper'
import { AndroidInfo, AdbResultItem, Orientation } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import { clearStatusIntents } from '../../../utils/statusIntent'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { cast } from '../../../utils/cast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import Select from '../../../components/Select'
import MenuDropdown from '../../../components/MenuDropdown'
import BatchTagModal from '../operations/tags/BatchTagModal'
import BatchUploadModal from '../operations/data/BatchUploadModal'
import BatchSwitchModelModal from '../operations/model/BatchSwitchModelModal'
import BatchUpdateImageModal from '../operations/model/BatchUpdateImageModal'
import MoreOpsModal from '../operations/more/MoreOpsModal'
import BatchAdbPanel from '../../groupcontrol/BatchAdbPanel'
import ScreencastSettingsModal from './ScreencastSettingsModal'
import { useBatchProjection } from './useBatchProjection'
import { useBatchActions } from './useBatchActions'
import { useBatchConfirms } from './useBatchConfirms'
import { isEmulatorMirrorType } from '../../mirror/constants'
import { countAdbSkipped, filterAdbTargets } from './screenshotDetail/batchSelectGate'
import { parseAndroidVersionFromImage } from '../create/lib/CreateInstanceConstants'
import type { ActionBarProps as Props, ConfirmInfo } from './actionBarTypes'

function ActionBar({ viewMode, onViewModeChange, infoSlot, orientation, onOrientationChange, selectedAndroids, allAndroids, slotAndroids, mirrors, devices, readyKeys, slotExpiry, onSelectAllAndroids, onDeselectAllAndroids, hasSelectedDevices }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [batching, setBatching] = useState('')
  const [showUpload, setShowUpload] = useState(false)
  const [showSwitchModel, setShowSwitchModel] = useState(false)
  const [showUpdateImage, setShowUpdateImage] = useState(false)
  const [confirm, setConfirm] = useState<ConfirmInfo | null>(null)
  const { notes } = useAndroidNotes()
  const [moreOpsTargets, setMoreOpsTargets] = useState<AndroidInfo[]>([])
  const [showSettings, setShowSettings] = useState(false)
  const [gpuAccel, setGpuAccel] = useState(false)

  // 批量操作目标：必须明确选择云机，无选中时为空（按钮禁用）
  const effectiveAll = viewMode === 'slot' ? slotAndroids : allAndroids
  const batchTargets = selectedAndroids
  // 全选态：与分母同源，点完必然 N === M
  const allSelected = effectiveAll.length > 0 && selectedAndroids.length === effectiveAll.length
  const batchUpdateDevice = batchTargets.length > 0
    ? devices.find(d => getDeviceId(d, mode) === getDeviceId(batchTargets[0], mode))
    : undefined

  // 选中云机中是否同时包含容器和模拟器（特质版算模拟器）
  const hasMixedBatch = batchTargets.some(a => {
    const m = mirrors.find(it => it.url === a.image)
    return m ? isEmulatorMirrorType(m) : false
  }) && batchTargets.some(a => {
    const m = mirrors.find(it => it.url === a.image)
    return m ? !isEmulatorMirrorType(m) : true
  })

  // 批量更新附加拦截：选中云机不得跨安卓版本（镜像列表已锁定为云机当前版本，混版本无统一镜像可选；
  // 解析不出版本的自定义镜像不参与判定）
  const hasMixedVersionBatch = (() => {
    const vs = new Set<string>()
    for (const a of batchTargets) {
      const v = parseAndroidVersionFromImage(a.image || '')
      if (v) vs.add(v)
    }
    return vs.size > 1
  })()

  // 加载投屏 GPU 加速开关状态
  useEffect(() => {
    ScreencastService.GetGpuAccel().then((v: boolean) => setGpuAccel(v)).catch(() => {})
  }, [])

  const toggleGpuAccel = async (on: boolean) => {
    setGpuAccel(on)
    try {
      await ScreencastService.SetGpuAccel(on)
      showToast(on ? t('host:actionBar.gpuAccelEnabledToast') : t('host:actionBar.gpuAccelDisabledToast'), 'success')
    } catch {
      setGpuAccel(!on)
      showToast(t('host:actionBar.gpuAccelSetFailed'), 'error')
    }
  }

  const actions = useBatchActions({ batchTargets, setBatching })

  // 投屏相关批量逻辑（不进任务队列）
  const { runBatchScreenCast, runBatchStopCast, runResetCasting, matrixScale, sidebar, setSidebar } = useBatchProjection({
    selectedAndroids: batchTargets, readyKeys, setBatching, orientation,
  })

  const confirms = useBatchConfirms(setConfirm, batchTargets.length, {
    ...actions, runBatchScreenCast, runResetCasting,
  })

  // ====== 群控矩阵投屏（主控窗口 + CtrlList 广播控制，从群控 tab 移植）======
  const [matrixCasting, setMatrixCasting] = useState(false)
  const [matrixPrimaryIP, setMatrixPrimaryIP] = useState('')
  const [matrixPrimaryIdx, setMatrixPrimaryIdx] = useState(-1)

  const runMatrixControl = async () => {
    if (batchTargets.length === 0 || matrixCasting) return
    // V4（tobox_player）：群控矩阵依赖 V3 的 RTSP 投屏窗口 + CtrlList 广播控制协议，
    // V4 的投屏是 tebox 协议（无被控窗口概念）。按钮保留（与其他模式视觉一致），
    // 点击给出明确提示而不是静默无反应。
    if (mode === 'v4') {
      showToast(t('host:toast.v4MatrixUnsupported'), 'error')
      return
    }
    setMatrixCasting(true)
    try {
      const items = batchTargets.map(a => ({ deviceIP: getDeviceId(a, mode), indexNum: a.indexNum, orientation }))
      await ScreencastService.StartMatrixProjection(items, matrixScale)
    } catch (err) {
      showToast(t('groupcontrol:batchControlFailed', { error: String(err) }), 'error')
    }
    setMatrixCasting(false)
  }

  const switchMatrixPrimary = async (ip: string, index: number) => {
    if (batchTargets.length < 2 || matrixCasting) return
    setMatrixCasting(true)
    setMatrixPrimaryIP(ip); setMatrixPrimaryIdx(index)
    try {
      const items = batchTargets.map(a => ({ deviceIP: getDeviceId(a, mode), indexNum: a.indexNum, orientation }))
      await ScreencastService.SwitchMatrixPrimary(items, ip, index, matrixScale)
    } catch (err) {
      showToast(t('groupcontrol:switchPrimaryFailed', { error: String(err) }), 'error')
    }
    setMatrixCasting(false)
  }

  // ====== 批量 ADB（从群控 tab 移植）======
  const [showAdbPanel, setShowAdbPanel] = useState(false)
  const [showTagModal, setShowTagModal] = useState(false)
  const [adbCommand, setAdbCommand] = useState('')
  const [adbExecution, setAdbExecution] = useState({ running: false, command: '', results: [] as AdbResultItem[] })

  const executeAdb = async (command: string) => {
    if (!command.trim() || batchTargets.length === 0 || adbExecution.running) return
    // ADB 批量是唯一无 Go 逐台兜底的路径（BatchDockerExec 无逐台 failed 明细）：
    // 关机容器混入会整组报错回退——执行前过滤跳过并提示（其余批量操作不过滤，
    // 上传等有 Go 侧护栏逐台返回 failed 明细，见 batchSelectGate 注释）。
    const skipped = countAdbSkipped(batchTargets)
    const targets = skipped > 0 ? filterAdbTargets(batchTargets) : batchTargets
    if (skipped > 0 && targets.length === 0) {
      showToast(t('host:batchSelect.batchAdbSkipped', { count: skipped }), 'warning')
      return
    }
    setAdbExecution({ running: true, command, results: [] })
    if (skipped > 0) showToast(t('host:batchSelect.batchAdbSkipped', { count: skipped }), 'warning')
    try {
      const ips = targets.map(a => getDeviceId(a, mode))
      const names = targets.map(a => a.name)
      const results = cast<AdbResultItem[]>(await SdkApiService.BatchDockerExec(ips, names, command))
      setAdbExecution({ running: false, command, results: results ?? [] })
    } catch (err) {
      setAdbExecution({ running: false, command, results: [] })
      showToast(t('groupcontrol:batchExecFailed', { error: String(err) }), 'error')
    }
  }

  // 轻边框工具栏：统一 border-gray-200 白底 + 圆角，hover 浅底，主操作蓝（危险项在「批量操作」菜单内红字）
  const btn = 'h-8 px-3 text-xs rounded-md border border-gray-200 bg-white text-gray-600 hover:bg-gray-50 hover:border-gray-300 transition-colors disabled:opacity-40'
  const btnPrimary = 'h-8 px-3 text-xs rounded-md border border-blue-400 bg-blue-500 text-white hover:bg-blue-600 hover:border-blue-500 transition-colors disabled:opacity-40'

  // 视图模式三选一：竖屏/横屏都是截图模式（仅卡片方向不同），列表 = 坑位模式
  const isPortrait = viewMode === 'screenshot' && orientation === 'portrait'
  const isLandscape = viewMode === 'screenshot' && orientation === 'landscape'

  // 竖屏/横屏互切只改方向：仅当当前不在截图模式时才走 onViewModeChange
  // （它会清空云机级勾选——两模式云机集合语义不同，但同模式内切方向不该清掉已选）
  const selectScreenshotMode = (o: Orientation) => {
    if (viewMode !== 'screenshot') onViewModeChange?.('screenshot')
    onOrientationChange(o)
  }

  return (
    <div className="flex flex-col gap-2 px-5 py-2.5 bg-white border-b border-gray-100">
      {/* 第一行：左侧视图模式三选一（宿主页无双模式语义时不渲染）+ 设备信息（infoSlot），
          右侧刷新/投屏设置（全选与「已选/总数」已移到第二行选择区）。flex-wrap：窄窗时信息簇换行降级 */}
      <div className="flex items-center gap-2 flex-wrap gap-y-1">
        {/* 视图模式三选一：竖屏/横屏 = 截图模式的两种卡片方向，列表 = 坑位模式 */}
        {onViewModeChange && (
          <div className="flex bg-gray-100 rounded-md overflow-hidden">
            <button
              onClick={() => selectScreenshotMode('portrait')}
              className={`px-3 h-8 text-xs transition-colors ${isPortrait ? 'bg-blue-500 text-white' : 'text-gray-600 hover:bg-gray-200'}`}
            >
              {t('host:actionBar.portrait')}
            </button>
            <button
              onClick={() => selectScreenshotMode('landscape')}
              className={`px-3 h-8 text-xs transition-colors ${isLandscape ? 'bg-blue-500 text-white' : 'text-gray-600 hover:bg-gray-200'}`}
            >
              {t('host:actionBar.landscape')}
            </button>
            <button
              onClick={() => onViewModeChange('slot')}
              className={`px-3 h-8 text-xs transition-colors ${viewMode === 'slot' ? 'bg-blue-500 text-white' : 'text-gray-600 hover:bg-gray-200'}`}
            >
              {t('host:actionBar.listMode')}
            </button>
          </div>
        )}
        {infoSlot}

        <div className="flex-1" />

        <button
          onClick={() => {
            // 手动刷新是优先级最高且强制的：后端已跳过离线退避/在线过滤并等待抢占 CAS。
            // 返回失败设备 IP 列表（空=全部成功）；同时清除操作意图——
            // 刷新后列表已反映真实状态，残留意图会遮蔽真实状态（显示旧状态）。
            AndroidListService.RefreshSelected().then((failedIPs) => {
              clearStatusIntents()
              const failed = Array.isArray(failedIPs) ? failedIPs : []
              if (failed.length > 0) {
                // 列出前 3 个失败 IP，用户能定位哪些设备仍是旧缓存
                const shown = failed.slice(0, 3).join(', ')
                const more = failed.length > 3 ? t('host:actionBar.refreshFailedMore', { count: failed.length - 3 }) : ''
                showToast(t('host:actionBar.refreshFailedDevices', { ips: shown }) + more, 'error')
              } else {
                showToast(t('host:actionBar.refreshed'), 'success')
              }
            }).catch(() => {})
          }}
          disabled={!hasSelectedDevices}
          className={btn}
          title={t('host:actionBar.refreshList')}
        >
          <RefreshCw size={12} className="inline mr-1" />
          {t('host:actionBar.refresh')}
        </button>
        <button
          onClick={() => setShowSettings(true)}
          className={btn}
          title={t('host:actionBar.screencastSettings')}
        >
          <Settings size={12} className="inline mr-1" />
          {t('host:actionBar.screencastSettings')}
        </button>
      </div>

      {/* 第二行：选择（全选 + 已选/总数）+ 批量操作 + 设备控制 + 数据 + 投屏/控制；主控选择器独占行最右（2026-09-27） */}
      <div className="flex items-center gap-2 flex-wrap">
        {/* 选择区：全选/取消全选 + 「已选 / 总数」（2026-09-27 从第一行移到此处，取代原「批量」标签） */}
        {effectiveAll.length > 0 && (
          <button
            onClick={allSelected ? onDeselectAllAndroids : onSelectAllAndroids}
            className={btn}
          >
            {allSelected ? t('button.deselectAll') : t('button.selectAll')}
          </button>
        )}
        {effectiveAll.length > 0 && (
          <span className="text-xs text-blue-600 font-medium">
            {selectedAndroids.length} / {effectiveAll.length}
          </span>
        )}
        {/* 设备控制组 */}
        {viewMode === 'slot' && (
          <button onClick={confirms.confirmBatchStart} disabled={!!batching || batchTargets.length === 0} className={btn}>
            {batching === t('host:actionBar.start') ? t('host:actionBar.starting') : t('host:actionBar.start')}
          </button>
        )}
        <button onClick={confirms.confirmBatchRestart} disabled={!!batching || batchTargets.length === 0} className={btn}>
          {batching === t('host:actionBar.restart') ? t('host:actionBar.restarting') : t('host:actionBar.restart')}
        </button>
        <button onClick={confirms.confirmBatchShutdown} disabled={!!batching || batchTargets.length === 0} className={btn}>
          {batching === t('host:actionBar.stop') ? t('host:actionBar.stopping') : t('host:actionBar.stop')}
        </button>

        <div className="w-px h-5 bg-gray-200 mx-1.5" />

        {/* 数据与其他组 */}
        <button onClick={() => setShowUpload(true)} disabled={!!batching || batchTargets.length === 0} className={btn}>{t('button.upload')}</button>
        {/* 「批量操作」下拉：低频批量项收进菜单（2026-09-25 用户要求），逐项保留原有禁用条件与提示 */}
        <MenuDropdown
          label={t('host:actionBar.batchOps')}
          className={btn}
          disabled={!!batching || batchTargets.length === 0}
          items={[
            // 切换机型：壳子标记 v3-only（V2 Web 无此项）→ v2 砍
            ...(!(mode === 'v2') ? [{ key: 'switchModel', label: t('host:actionBar.switchModel'), onClick: () => setShowSwitchModel(true) }] : []),
            // V2 无 8000 端口且壳子（V2 Web）无对应方法 → 砍「更新」（switchImage）；其余项走 MYTOS/已翻译通道，均可用
            ...(!(mode === 'v2') ? [{
              key: 'update', label: t('host:actionBar.update'), onClick: () => setShowUpdateImage(true),
              disabled: !batchUpdateDevice || hasMixedBatch || hasMixedVersionBatch,
              title: hasMixedVersionBatch ? t('host:actionBar.mixedVersionTooltip') : hasMixedBatch ? t('host:actionBar.mixedBatchTooltip') : '',
            }] : []),
            { key: 'closeProxy', label: t('host:actionBar.closeS5Proxy'), onClick: confirms.confirmBatchCloseProxy },
            { key: 'shake', label: t('host:actionBar.shake'), onClick: confirms.confirmBatchShake },
            // 批量 ADB：从群控 tab 移植，对选中安卓批量执行 ADB 命令
            { key: 'adb', label: t('groupcontrol:batchAdb'), onClick: () => setShowAdbPanel(true) },
            // 批量设置标签：标签绑坑位，故对选中云机所在的「设备 + 坑位」统一加/减标签
            { key: 'tags', label: t('host:tags.setTags'), onClick: () => setShowTagModal(true) },
            {
              key: 'moreOps', label: t('host:actionBar.moreOps'), onClick: () => setMoreOpsTargets(batchTargets),
              disabled: hasMixedBatch,
              title: hasMixedBatch ? t('host:actionBar.mixedBatchTooltip') : '',
            },
            // 危险操作：红字警示（原第二行危险组散按钮收进菜单，2026-09-25 用户要求）
            { key: 'reset', label: t('host:actionBar.reset'), onClick: confirms.confirmBatchReset, danger: true },
            { key: 'delete', label: t('host:actionBar.delete'), onClick: confirms.confirmBatchDelete, danger: true },
          ]}
        />

        {/* 投屏 / 关闭投屏 / 控制（2026-09-27 用户要求从行最右挪到左侧批量区，与「批量操作」同组）：
            整组前一条分隔线把它与「上传/批量操作」分开，组内三按钮连排不再插线 */}
        <div className="w-px h-5 bg-gray-200 mx-1.5" />
        <button onClick={confirms.confirmBatchCast} disabled={!!batching || batchTargets.length === 0} className={btnPrimary}>
          {batching === t('host:actionBar.batchCast') ? t('host:actionBar.casting') : t('host:actionBar.batchCast')}
        </button>
        <button onClick={runBatchStopCast} disabled={!!batching || batchTargets.length === 0} className={btn}>
          {batching === t('host:actionBar.closeCast') ? t('host:actionBar.closing') : t('host:actionBar.closeCast')}
        </button>
        {/* 重置投屏（选中实例批量，投屏卡死/一直加载中的自救）：kill hwcodec + 重启投屏进程 */}
        <button onClick={confirms.confirmResetCasting} disabled={!!batching || batchTargets.length === 0} className={btn}>
          {batching === t('host:actionBar.resetCast') ? t('host:actionBar.resetting') : t('host:actionBar.resetCast')}
        </button>
        {/* 批量控制（群控矩阵）：主控窗口 + CtrlList 广播控制，从群控 tab 移植 */}
        <button onClick={runMatrixControl} disabled={matrixCasting || batchTargets.length === 0} className={btnPrimary}>
          {matrixCasting ? t('groupcontrol:controlling') : t('host:actionBar.control')}
        </button>

        <div className="flex-1" />

        {/* 主控选择器：独占行最右（2026-09-27 用户要求；与上面的「控制」按钮分列两端） */}
        {batchTargets.length >= 2 && (
          <div className="flex items-center gap-1.5">
            <span className="text-[11px] text-gray-500 whitespace-nowrap">{t('groupcontrol:primaryControl')}</span>
            <Select
              value={`${matrixPrimaryIP}_${matrixPrimaryIdx}`}
              onChange={v => {
                const [ip, idx] = v.split('_')
                switchMatrixPrimary(ip, Number(idx))
              }}
              options={batchTargets.map((a, i) => {
                const ip = getDeviceId(a, mode)
                return { value: `${ip}_${a.indexNum}`, label: `${i === 0 ? '★ ' : ''}${a.name || `${ip}#${a.indexNum}`}` }
              })}
              disabled={matrixCasting}
              className="w-[150px]"
              buttonClassName="h-8"
            />
          </div>
        )}

        {/* 侧边栏开关 + 投屏大小滑块已移到“投屏设置”弹窗（ScreencastSettingsModal）*/}
      </div>

      {showUpload && <BatchUploadModal selectedAndroids={batchTargets} notes={notes} onClose={() => setShowUpload(false)} />}
      {showSwitchModel && <BatchSwitchModelModal selectedAndroids={batchTargets} mirrors={mirrors} onClose={() => setShowSwitchModel(false)} />}
      {showUpdateImage && batchUpdateDevice && (
        <BatchUpdateImageModal
          device={batchUpdateDevice}
          selectedAndroids={batchTargets}
          onClose={() => setShowUpdateImage(false)}
          onUpdated={() => {}}
        />
      )}
      {moreOpsTargets.length > 0 && <MoreOpsModal androids={moreOpsTargets} mirrors={mirrors} onClose={() => setMoreOpsTargets([])} />}
      {showTagModal && <BatchTagModal androids={batchTargets} onClose={() => setShowTagModal(false)} />}
      <BatchAdbPanel
        visible={showAdbPanel}
        targetCount={batchTargets.length}
        adbCommand={adbCommand} onAdbCommandChange={setAdbCommand}
        adbExecution={adbExecution}
        onExecute={executeAdb}
        onClose={() => setShowAdbPanel(false)}
      />
      {confirm && (
        <ConfirmDialog
          title={confirm.title}
          message={confirm.message}
          confirmLabel={confirm.confirmLabel}
          onCancel={() => setConfirm(null)}
          onConfirm={confirm.onConfirm}
        />
      )}
      {showSettings && (
        <ScreencastSettingsModal
          gpuAccel={gpuAccel}
          onToggleGpuAccel={toggleGpuAccel}
          sidebar={sidebar}
          onToggleSidebar={setSidebar}
          onClose={() => setShowSettings(false)}
        />
      )}
    </div>
  )
}

export default ActionBar
