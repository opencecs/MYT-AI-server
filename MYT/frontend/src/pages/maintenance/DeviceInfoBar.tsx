import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { KeyRound, ArrowUpCircle, PackagePlus } from 'lucide-react'
import * as SdkApiService from '../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as V2Service from '../../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import { DeviceState } from '../../types/index'
import { showToast } from '../../components/Toast'
import { useDeployMode } from '../../hooks/useDeployMode'
import { getDeviceId } from '../../utils/deviceId'
import { useSdkUpgradeConfirm } from '../../hooks/useSdkUpgradeConfirm'
import { useSuperSdkInstall } from '../../hooks/useSuperSdkInstall'
import { useModalEnter } from '../../hooks/useModalAnimation'
import { MIN_SDK_VERSION } from '../../hooks/useSdkVersions'

interface Props {
  device: DeviceState
}

/**
 * 主机 tab 设备信息簇（内联）：API 版本 · 固件版本 · SDK 升级 · 设备密码操作。
 * 由原底部「设备密码」卡片去卡片化而来，现渲染在 ActionBar 第一行左侧（infoSlot）。
 * 查询/升级/密码逻辑保持不变。
 */
export function DeviceInfoBar({ device }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { requestUpgrade, upgradeDialog } = useSdkUpgradeConfirm()
  const [apiVersion, setApiVersion] = useState('-')
  const [firmwareVersion, setFirmwareVersion] = useState('-')
  const [hasPassword, setHasPassword] = useState(false)
  // SDK 升级状态三态：unknown=云端版本不可知（查询失败，不冒充"已最新"）
  // upgrade=可升级 latest=已最新
  const [sdkStatus, setSdkStatus] = useState<'unknown' | 'upgrade' | 'latest'>('unknown')
  const [upgrading] = useState(false)
  const [editTarget, setEditTarget] = useState<{ mode: 'set' | 'change' | 'clear' } | null>(null)
  const [newPwd, setNewPwd] = useState('')
  const [confirmPwd, setConfirmPwd] = useState('')
  const [currentPwd, setCurrentPwd] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const did = getDeviceId(device, mode)
  const online = device.state === 0
  const tp = (k: string) => t(`maintenance:devicePwd.${k}`)

  // v2 准入放宽：81 缺失（super_sdk_ready 缺省/false）时提示需安装超级 SDK
  const needSuperSdk = mode === 'v2' && online && device.super_sdk_ready === false
  const { install: installSuperSdk } = useSuperSdkInstall()
  const editRef = useRef<HTMLDivElement>(null)
  useModalEnter(editRef)

  useEffect(() => {
    setApiVersion('-'); setFirmwareVersion('-'); setHasPassword(false); setSdkStatus('unknown')
    if (!did) return
    // 版本/升级查询不再以列表在线态为前置：旧 SDK 设备可能被 WS 断连标成离线，
    // 但 HTTP API 仍可达——SDK 页面要能看到这些设备的版本才知道要升级。
    SdkApiService.GetAllDevicePasswords().then(pw => { if (pw?.[device.device_id]) setHasPassword(true) }).catch(() => {})
    SdkApiService.GetAPIVersionInfo(device.ip).then(info => {
      const i = info as { currentVersion?: number; stringCurrentVersion?: string } | null
      if (!i) return
      // v2 版本号是 string（stringCurrentVersion 非空，实为 8000 myt-sdk-go-v2
      // 包版本）：显示「SDK v{cur}」——裸 v 前缀的 v3 会与主机代际撞名；
      // V3/public/local 走原 int 版本（string 字段为空，行为不变）
      if (mode === 'v2') {
        const curStr = i.stringCurrentVersion ?? ''
        if (curStr !== '') { setApiVersion(curStr.startsWith('v') ? `SDK ${curStr}` : `SDK v${curStr}`); return }
        if (i.currentVersion) { setApiVersion(`SDK v${i.currentVersion}`); return }
        return
      }
      const curStr = i.stringCurrentVersion ?? ''
      if (curStr !== '') { setApiVersion(curStr.startsWith('v') ? curStr : `v${curStr}`); return }
      if (i.currentVersion) setApiVersion(String(i.currentVersion))
    }).catch(() => {})
    SdkApiService.GetDeviceInfo(did).then(info => {
      if (info?.version) { const m = info.version.match(/v\d+\.\d+\.\d+/); setFirmwareVersion(m ? m[0] : info.version) }
    }).catch(() => {})
    // 升级态查询（含 v2：SdkCheckUpgrade 后端按部署模式分流，v2 走
    // sdkCheckUpgradeV2 返回 stringCurrentVersion/stringLatestVersion）
    SdkApiService.SdkCheckUpgrade(device.ip).then(info => {
      const i = info as {
        currentVersion?: number; latestVersion?: number
        stringCurrentVersion?: string; stringLatestVersion?: string
      } | null
      if (!i) return // 查询失败：保持 unknown
      // v2 分支版本号是 string（int 字段为空）：转数字比较，转不动保持 unknown
      // （V3 分支 string 字段为空，走原 int 逻辑，行为不变）
      if (i.stringCurrentVersion || i.stringLatestVersion) {
        const cur = parseFloat(i.stringCurrentVersion ?? '')
        const latest = parseFloat(i.stringLatestVersion ?? '')
        if (!Number.isNaN(cur) && !Number.isNaN(latest)) {
          setSdkStatus(latest > cur ? 'upgrade' : 'latest')
        }
        return
      }
      const cur = i.currentVersion ?? 0
      const latest = i.latestVersion
      // 判定优先级：最低可用版本 > 云端最新版。低于 208（WS 功能受限，用户
      // 定义的阈值）必提示升级——不依赖公网版本查询（客户端可能不出网，
      // 设备自升级由设备自己从云端拉，192.0.2.202 曾因此显示"版本未知"）。
      if (cur > 0 && cur < MIN_SDK_VERSION) { setSdkStatus('upgrade'); return }
      if (typeof latest !== 'number' || latest < 0) return // 云端不可知且不低于 208：unknown
      setSdkStatus(latest > cur ? 'upgrade' : 'latest')
    }).catch(() => {})
  }, [device.device_id, did, device.ip, device.state, mode]) // state 变化重查：离线设备经探活翻在线后版本行不再停留 '-'

  // 老设备回填：入库早于「超级 SDK 就绪」功能的设备该字段为 undefined（心跳不
  // 探测 81，无人写 false/true）——v2 在线设备探测一次 CheckSuperSdkReady 回填
  // 落库并广播（devicelist:updated 回流，提示按实测值出现/消失）。sdkProbeRef
  // 防 in-flight 重入：同设备在途不重复发（81 探测常态 3s+，列表事件会频繁触发本组件重渲染）。
  const sdkProbeRef = useRef<Set<string>>(new Set())
  useEffect(() => {
    if (mode !== 'v2' || !did || !online) return
    if (device.super_sdk_ready !== undefined) return // 已有实测值（true/false），无需回填
    if (sdkProbeRef.current.has(did)) return
    sdkProbeRef.current.add(did)
    V2Service.CheckSuperSdkReady(did).catch(() => {}).finally(() => { sdkProbeRef.current.delete(did) })
  }, [mode, did, online, device.super_sdk_ready])

  const closeEdit = () => { setEditTarget(null); setNewPwd(''); setConfirmPwd(''); setCurrentPwd(''); setSubmitting(false) }

  const submitPassword = async () => {
    if (!editTarget || submitting) return
    if (editTarget.mode !== 'clear' && (!newPwd || !confirmPwd)) return
    if (editTarget.mode !== 'clear' && newPwd !== confirmPwd) { showToast(tp('passwordMismatch'), 'error'); return }
    if ((editTarget.mode === 'change' || editTarget.mode === 'clear') && !currentPwd) { showToast(tp('enterCurrentPassword'), 'error'); return }
    setSubmitting(true)
    try {
      if (editTarget.mode === 'set' || editTarget.mode === 'change') {
        await SdkApiService.SetDevicePassword(device.device_id, device.ip, currentPwd, newPwd)
        showToast(tp('setSuccess'))
      } else {
        await SdkApiService.CloseDevicePassword(device.device_id, device.ip, currentPwd)
        showToast(tp('cleared'))
      }
      setHasPassword(editTarget.mode === 'set' || editTarget.mode === 'change')
      closeEdit()
    } catch (err) { console.error('Password operation failed:', err); showToast(tp('operationFailed'), 'error') }
    finally { setSubmitting(false) }
  }

  const handleUpgrade = () => {
    if (upgrading) return
    // 公共 hook 统一处理：SdkCheckUpgrade → 确认弹窗（当前→最新版本 + 版本说明）
    // → 提交（v2 走 sdkInstall 指定版本任务；local/public 走设备自升级任务）。
    // 弹窗确认前的等待不再占「升级中」态——requestUpgrade 自己管理异步与错误提示
    void requestUpgrade(device.ip)
  }

  return (
    // 内联紧凑信息簇：渲染在宿主 ActionBar 第一行左侧（原独立横条已并入工具栏）
    // 「SDK 升级」簇全模式显示：v2 走 sdkInstall 安装任务（文案「更新」），
    // local/public 走设备自升级（文案「升级」）；API 版本/固件版本/密码操作各模式都显示
    <div className="flex items-center gap-x-4 min-w-0">
      {/* v2 准入放宽：主机未装超级 SDK 时提示 + 一键安装（提交任务队列，
          阶段进度在任务面板展示，完成后 devicelist:updated 回流翻转提示） */}
      {needSuperSdk && (
        <div className="flex items-center gap-1.5 text-xs shrink-0">
          <span className="text-[11px] text-amber-600 bg-amber-50 px-2 py-0.5 rounded">
            {t('host:superSdk.needInstall')}
          </span>
          <button onClick={() => void installSuperSdk(device)}
            className="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded-md bg-blue-50 text-blue-600 hover:bg-blue-100 disabled:opacity-50 cursor-pointer transition-colors">
            <PackagePlus className="w-3.5 h-3.5" />
            {t('host:superSdk.install')}
          </button>
        </div>
      )}
      <div className="flex items-center gap-1.5 text-xs shrink-0">
        <span className="text-[11px] text-gray-400">{tp('apiVersion')}</span>
        <span className="font-mono text-gray-600">{apiVersion}</span>
      </div>
      <div className="flex items-center gap-1.5 text-xs min-w-0">
        <span className="text-[11px] text-gray-400 shrink-0">{tp('firmwareVersion')}</span>
        <span className="font-mono text-gray-600 max-w-[160px] truncate" title={firmwareVersion}>{firmwareVersion}</span>
      </div>
      <div className="flex items-center gap-1.5 text-xs shrink-0">
        <span className="text-[11px] text-gray-400">{tp('sdkUpgrade')}</span>
        {online && sdkStatus === 'upgrade' ? (
          <button onClick={handleUpgrade} disabled={upgrading}
            className="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded-md bg-blue-50 text-blue-600 hover:bg-blue-100 disabled:opacity-50 cursor-pointer transition-colors">
            <ArrowUpCircle className="w-3.5 h-3.5" />{upgrading ? tp('upgrading') : mode === 'v2' ? tp('update') : tp('upgrade')}
          </button>
        ) : sdkStatus === 'latest' ? (
          <span className="text-[10px] px-2 py-0.5 rounded bg-green-50 text-green-600 font-medium">{tp('latest')}</span>
        ) : (
          <span className="text-[10px] px-2 py-0.5 rounded bg-gray-50 text-gray-400">{t('maintenance:devicePwd.unknown')}</span>
        )}
      </div>
      <div className="w-px h-4 bg-gray-200 shrink-0" />
      <div className="flex items-center gap-1.5 shrink-0">
        <span className="flex items-center gap-1 text-[11px] text-gray-400">
          <KeyRound className="w-3 h-3" />{tp('title')}
        </span>
        {online && !hasPassword && (
          <button onClick={() => setEditTarget({ mode: 'set' })}
            className="px-2.5 py-0.5 text-[11px] rounded-md bg-blue-50 text-blue-600 hover:bg-blue-100 cursor-pointer transition-colors">
            {tp('set')}
          </button>
        )}
        {online && hasPassword && (
          <>
            <button onClick={() => setEditTarget({ mode: 'change' })}
              className="px-2.5 py-0.5 text-[11px] rounded-md bg-blue-50 text-blue-600 hover:bg-blue-100 cursor-pointer transition-colors">
              {tp('modify')}
            </button>
            <button onClick={() => setEditTarget({ mode: 'clear' })}
              className="px-2.5 py-0.5 text-[11px] rounded-md bg-red-50 text-red-500 hover:bg-red-100 cursor-pointer transition-colors">
              {tp('clear')}
            </button>
          </>
        )}
      </div>

      {/* 密码编辑弹窗 */}
      {editTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 modal-overlay" ref={editRef} onClick={closeEdit}>
          <div className="bg-white rounded-xl shadow-2xl p-5 w-80 modal-content" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h4 className="text-sm font-semibold text-gray-800">
                {editTarget.mode === 'set' ? tp('setPassword') : editTarget.mode === 'change' ? tp('changePassword') : tp('clearPassword')}
              </h4>
              <button onClick={closeEdit} className="w-6 h-6 flex items-center justify-center text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-md text-lg leading-none">&times;</button>
            </div>
            {(editTarget.mode === 'change' || editTarget.mode === 'clear') && (
              <input type="password" placeholder={tp('currentPassword')} value={currentPwd} onChange={e => setCurrentPwd(e.target.value)}
                className="w-full text-sm px-3 py-2 border border-gray-200 rounded-lg mb-2.5 outline-none focus:border-blue-400 focus:ring-1 focus:ring-blue-100" />
            )}
            {editTarget.mode !== 'clear' && (
              <>
                <input type="password" placeholder={tp('newPassword')} value={newPwd} onChange={e => setNewPwd(e.target.value)}
                  className="w-full text-sm px-3 py-2 border border-gray-200 rounded-lg mb-2.5 outline-none focus:border-blue-400 focus:ring-1 focus:ring-blue-100" />
                <input type="password" placeholder={tp('confirmPassword')} value={confirmPwd} onChange={e => setConfirmPwd(e.target.value)}
                  className="w-full text-sm px-3 py-2 border border-gray-200 rounded-lg mb-2.5 outline-none focus:border-blue-400 focus:ring-1 focus:ring-blue-100" />
              </>
            )}
            <div className="flex justify-end gap-2 mt-4">
              <button onClick={closeEdit} className="px-3.5 py-1.5 text-xs text-gray-500 hover:text-gray-700 hover:bg-gray-50 rounded-lg cursor-pointer" disabled={submitting}>
                {t('button.cancel')}
              </button>
              <button onClick={submitPassword} disabled={submitting}
                className="px-3.5 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 cursor-pointer transition-colors">
                {submitting ? tp('processing') : editTarget.mode === 'clear' ? tp('confirmClear') : t('button.confirm')}
              </button>
            </div>
          </div>
        </div>
      )}
      {/* SDK 升级确认弹窗（useSdkUpgradeConfirm 管理） */}
      {upgradeDialog}
    </div>
  )
}
