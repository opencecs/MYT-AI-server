import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Package, RefreshCw, Download, AlertCircle, Inbox } from 'lucide-react'
import * as SdkApiService from '../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { useDeployMode } from '../../hooks/useDeployMode'
import { useModalEnter } from '../../hooks/useModalAnimation'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import ConfirmDialog from '../../components/ConfirmDialog'
import { showToast } from '../../components/Toast'

interface VersionFile {
  filename: string
  size: number
  description: string
  upload_time: string
  checksum: string
}

interface SdkVersion {
  sdk_type: string
  version: string
  status: string
  files: VersionFile[]
}

interface Props {
  /** 目标设备标识（local 为 IP，public 为 device_id）；为空时隐藏安装按钮 */
  deviceIdent: string
  onClose: () => void
}

const DEFAULT_SDK_TYPE = 'box_sdk'
/** v2 模式 SDK 类型：v2 主机用独立的 v2 SDK 包（newapi 按此类型下发） */
const V2_SDK_TYPE = 'v2_box_sdk'

/** 按部署模式取 SDK 类型：v2 → v2_box_sdk，local/public → box_sdk */
function sdkTypeForMode(mode: string): string {
  return mode === 'v2' ? V2_SDK_TYPE : DEFAULT_SDK_TYPE
}

/** 人类可读文件大小 */
function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

/**
 * SDK 版本管理弹窗：展示云端 newapi 的 SDK 版本列表。
 * sdk_type 随部署模式区分：v2 → v2_box_sdk，local/public → box_sdk；
 * 打开时按当前模式请求一次，模式变化需重开弹窗生效。
 * 接口已按版本号降序返回，直接展示；test 状态版本用琥珀色徽标标注，不静默过滤。
 * 每行带「安装」按钮（v2/local/public 均开放）：提交 sdkInstall 任务队列任务
 * （客户端下载指定版本 → 上传设备 → 轮询确认版本变化，v2 走 InstallV2SdkVersion
 * string 版本链路），版本号统一按 string 传给后端。
 */
export function SdkVersionsModal({ deviceIdent, onClose }: Props) {  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { openPanel } = useTaskQueue()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [versions, setVersions] = useState<SdkVersion[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [installTarget, setInstallTarget] = useState<SdkVersion | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const tp = (k: string, opts?: Record<string, unknown>) => t(`maintenance:sdkVersions.${k}`, opts)

  // 安装按钮所有模式开放（v2 走 InstallV2SdkVersion string 版本链路）；
  // 无设备标识（如从全局入口打开）时隐藏
  const canInstall = !!deviceIdent

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      const list = await SdkApiService.ListSdkVersions(sdkTypeForMode(mode))
      setVersions((list ?? []) as SdkVersion[])
    } catch (err) {
      console.error('ListSdkVersions failed:', err)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void load() }, [])

  const submitInstall = async () => {
    if (!installTarget || submitting) return
    const version = installTarget.version.trim()
    if (!version) {
      showToast(tp('invalidVersion'), 'error')
      return
    }
    setSubmitting(true)
    try {
      await SdkApiService.SubmitSdkInstallTask([deviceIdent], version)
      showToast(tp('installQueued'), 'success')
      openPanel() // 进度到任务队列面板看
      setInstallTarget(null)
      onClose()
    } catch (err) {
      console.error('SubmitSdkInstallTask failed:', err)
      showToast(tp('installFailed'), 'error')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-xl shadow-2xl w-[520px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
          <h4 className="flex items-center gap-2 text-sm font-semibold text-gray-800">
            <Package className="w-4 h-4 text-blue-500" />{tp('title')}
            <span className="text-[11px] font-normal text-gray-400 font-mono">{sdkTypeForMode(mode)}</span>
          </h4>
          <button onClick={onClose} className="w-6 h-6 flex items-center justify-center text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-md text-lg leading-none">&times;</button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4">
          {loading ? (
            <div className="flex flex-col items-center justify-center py-12 text-gray-400">
              <RefreshCw className="w-6 h-6 animate-spin mb-2" />
              <span className="text-xs">{tp('loading')}</span>
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center py-12 text-gray-500">
              <AlertCircle className="w-6 h-6 mb-2 text-red-400" />
              <span className="text-xs mb-3">{tp('loadFailed')}</span>
              <span className="text-[11px] text-gray-400 max-w-[400px] break-all mb-3">{error}</span>
              <button onClick={() => void load()}
                className="px-3 py-1.5 text-xs rounded-lg bg-blue-50 text-blue-600 hover:bg-blue-100 cursor-pointer transition-colors">
                {tp('retry')}
              </button>
            </div>
          ) : versions.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-gray-400">
              <Inbox className="w-6 h-6 mb-2" />
              <span className="text-xs">{tp('empty')}</span>
            </div>
          ) : (
            <div className="space-y-3">
              {versions.map(v => (
                <div key={v.version} className="border border-gray-100 rounded-lg p-3 hover:border-gray-200 transition-colors">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-mono text-sm font-semibold text-gray-800">v{v.version}</span>
                    {v.status === 'test' && (
                      <span className="text-[10px] px-2 py-0.5 rounded bg-amber-50 text-amber-600 font-medium">{tp('test')}</span>
                    )}
                    {canInstall && (
                      <button onClick={() => setInstallTarget(v)}
                        className="ml-auto flex items-center gap-1 px-2.5 py-1 text-[11px] rounded-md bg-blue-50 text-blue-600 hover:bg-blue-100 cursor-pointer transition-colors">
                        <Download className="w-3 h-3" />{tp('install')}
                      </button>
                    )}
                  </div>
                  {v.files?.[0] && (
                    <div className="flex items-center gap-3 text-[11px] text-gray-400 mt-0.5">
                      <span>{formatSize(v.files[0].size)}</span>
                      <span>{v.files[0].upload_time}</span>
                    </div>
                  )}
                  {v.files?.[0]?.description?.trim() && (
                    <p className="text-[11px] text-gray-500 whitespace-pre-line mt-1 leading-relaxed break-words">
                      {v.files[0].description}
                    </p>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="flex justify-end px-5 py-3 border-t border-gray-100">
          <button onClick={onClose}
            className="px-3.5 py-1.5 text-xs text-gray-500 hover:text-gray-700 hover:bg-gray-50 rounded-lg cursor-pointer transition-colors">
            {tp('close')}
          </button>
        </div>

        {installTarget && (
          <ConfirmDialog
            title={tp('installConfirmTitle', { version: installTarget.version })}
            message={tp('installConfirmMsg', { version: installTarget.version, device: deviceIdent })}
            confirmLabel={tp('install')}
            onConfirm={submitInstall}
            onCancel={() => !submitting && setInstallTarget(null)}
          />
        )}
      </div>
    </div>
  )
}
