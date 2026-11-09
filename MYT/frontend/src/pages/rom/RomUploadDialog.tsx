import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialogs } from '@wailsio/runtime'
import { FileUp, Loader2, X } from 'lucide-react'
import { useModalEnter } from '../../hooks/useModalAnimation'
import * as V4Service from '../../../bindings/github.com/moyunteng/myt/v4servicewrapper'
import { onEvent } from '../../utils/wailsEvent'
import { showToast } from '../../components/Toast'
import { getErrorMessage } from '../../utils/error'
import Select from '../../components/Select'
import { IMAGE_FILE_NAMES, formatBytes, isValidVariantName } from './romShared'

/** rom:upload-progress 事件 payload（Go 侧 V4RomUploadProgress） */
interface RomUploadProgressEvent {
  host: string
  variant: string
  sent: number
  total: number
  percent: number
}

interface Props {
  host: string
  /** 预填变体名（列表行点「上传」时带入）；空 = 新建变体 */
  initialVariant?: string
  onClose: () => void
  /** 上传成功后回调，页面据此刷新列表 */
  onDone: () => void
}

/**
 * ROM 上传弹窗（POST /image/upload/:variant，multipart 字段名 file）。
 *
 * 服务端支持两种方式：
 *   ① zip 包（推荐）：包内平铺三件套，一次上传即完成整个变体；
 *   ② 单个 img：需显式指定文件名（system.img / vendor.img / initramfs.img），
 *      分三次上传。
 * 这里按文件后缀自动判断：.zip 走方式①（隐藏文件名下拉），其余要求选文件名。
 *
 * 文件路径由系统文件对话框选取（Wails Dialogs.OpenFile 返回本地绝对路径），
 * Go 侧流式读文件上传，不经前端内存——镜像动辄几百 MB，走 base64 会卡死 WebView。
 */
export default function RomUploadDialog({ host, initialVariant, onClose, onDone }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  const [variant, setVariant] = useState(initialVariant ?? '')
  const [filePath, setFilePath] = useState('')
  const [filename, setFilename] = useState<string>(IMAGE_FILE_NAMES[0])
  const [uploading, setUploading] = useState(false)
  // 上传进度（rom:upload-progress 事件，Go 侧约每 5MB 推一次 + 收尾 100%）
  const [progress, setProgress] = useState<RomUploadProgressEvent | null>(null)
  // 已取消标记：上传被用户取消后短暂展示「已取消」再关闭弹窗
  const [cancelled, setCancelled] = useState(false)

  const isZip = filePath.toLowerCase().endsWith('.zip')

  // 上传中监听进度事件：只认 host+variant 匹配的帧（防止其他变体/主机上传串台）
  useEffect(() => {
    if (!uploading) return
    const off = onEvent('rom:upload-progress', data => {
      const ev = data as RomUploadProgressEvent | null
      if (!ev || ev.host !== host || ev.variant !== variant.trim()) return
      setProgress(ev)
    })
    return () => off()
  }, [uploading, host, variant])

  // 取消上传：调 Go 侧 CancelRomUpload（进度回调触发 UploadRomImage 返回取消错误）
  const cancelUpload = useCallback(() => {
    V4Service.CancelRomUpload(host, variant.trim()).catch(() => {})
    setCancelled(true)
  }, [host, variant])

  const pickFile = useCallback(async () => {
    try {
      const picked = await Dialogs.OpenFile({
        Title: t('rom:upload.pickTitle'),
        CanChooseFiles: true,
        CanChooseDirectories: false,
        Filters: [
          { DisplayName: t('rom:upload.filterImage'), Pattern: '*.zip;*.img' },
          { DisplayName: 'ZIP', Pattern: '*.zip' },
          { DisplayName: 'IMG', Pattern: '*.img' },
        ],
      })
      const path = Array.isArray(picked) ? picked[0] : picked
      if (path) {
        setFilePath(path)
        // 单个 img 时按文件名自动选中对应的三件套项（减少误选）
        const base = path.split(/[\\/]/).pop() ?? ''
        if ((IMAGE_FILE_NAMES as readonly string[]).includes(base)) setFilename(base)
      }
    } catch {
      /* 用户取消：忽略 */
    }
  }, [t])

  const submit = useCallback(async () => {
    if (uploading) return
    const name = variant.trim()
    if (!isValidVariantName(name)) {
      showToast(t('rom:upload.invalidVariant'), 'error')
      return
    }
    if (!filePath) {
      showToast(t('rom:upload.noFile'), 'error')
      return
    }
    setUploading(true)
    setProgress(null)
    setCancelled(false)
    try {
      const res = await V4Service.UploadRomImage(host, name, filePath, isZip ? '' : filename)
      if (res?.ready) {
        showToast(t('rom:upload.doneReady', { variant: name }))
      } else {
        // 单文件方式上传成功但三件套未齐（正常，还需上传另外两个文件）
        showToast(t('rom:upload.donePartial', { variant: name, filename: res?.filename ?? filename }), 'warning')
      }
      onDone()
      onClose()
    } catch (e) {
      // 用户取消：UploadRomImage 返回「上传已取消」，不按失败报错，弹窗留着可重试
      const msg = String(e)
      if (msg.includes('已取消') || msg.includes('cancelled') || msg.includes('canceled')) {
        showToast(t('rom:upload.cancelledToast'), 'warning')
      } else {
        // getErrorMessage 从 {"message":..,"cause":{}} 结构里提取内层文案
        showToast(t('rom:upload.failed', { message: getErrorMessage(e) }), 'error')
      }
    } finally {
      setUploading(false)
      setProgress(null)
    }
  }, [uploading, variant, filePath, isZip, filename, host, t, onDone, onClose])

  const fieldCls = 'w-full rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 outline-none placeholder:text-gray-400 focus:border-blue-400 disabled:bg-gray-50 disabled:text-gray-400'

  return (
    <div
      className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay"
      ref={rootRef}
      role="dialog"
      aria-modal="true"
      onKeyDown={e => { if (e.key === 'Escape' && !uploading) onClose() }}
    >
      <div className="bg-white rounded-lg shadow-xl w-[480px] max-w-[calc(100vw-32px)] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center px-5 pt-4 pb-3 border-b border-gray-100">
          <h3 className="text-sm font-semibold text-gray-800">{t('rom:upload.title')}</h3>
          <button
            onClick={onClose}
            disabled={uploading}
            className="ml-auto text-gray-400 hover:text-gray-600 disabled:opacity-40 cursor-pointer"
            aria-label={t('button.cancel')}
          >
            <X size={16} />
          </button>
        </div>

        <div className="px-5 py-4 flex flex-col gap-3">
          <div className="text-[11px] text-gray-400">{t('rom:upload.targetHost', { host })}</div>

          <label className="flex flex-col gap-1">
            <span className="text-xs text-gray-500">{t('rom:upload.variant')}</span>
            <input
              className={fieldCls}
              value={variant}
              onChange={e => setVariant(e.target.value)}
              placeholder={t('rom:upload.variantPlaceholder')}
              disabled={uploading}
              autoFocus={!initialVariant}
            />
            <span className="text-[10px] text-gray-400">{t('rom:upload.variantHint')}</span>
          </label>

          <label className="flex flex-col gap-1">
            <span className="text-xs text-gray-500">{t('rom:upload.file')}</span>
            <div className="flex items-center gap-2">
              <input
                className={`${fieldCls} flex-1 font-mono text-[11px]`}
                value={filePath}
                readOnly
                placeholder={t('rom:upload.filePlaceholder')}
              />
              <button
                onClick={pickFile}
                disabled={uploading}
                className="flex items-center gap-1 px-2.5 py-1.5 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700 disabled:opacity-50 cursor-pointer shrink-0"
              >
                <FileUp size={12} />
                {t('rom:upload.browse')}
              </button>
            </div>
            <span className="text-[10px] text-gray-400">{t('rom:upload.fileHint')}</span>
          </label>

          {/* zip 包一次上传三件套；单个 img 需指定是哪一件 */}
          {filePath && !isZip && (
            <label className="flex flex-col gap-1">
              <span className="text-xs text-gray-500">{t('rom:upload.filename')}</span>
              <Select
                value={filename}
                onChange={setFilename}
                options={IMAGE_FILE_NAMES.map(n => ({ value: n, label: n }))}
                disabled={uploading}
                buttonClassName="h-8"
              />
            </label>
          )}
        </div>

        <div className="flex justify-end gap-2 px-5 pb-4">
          {/* 上传中：进度条（约每 5MB 推一次事件，开传后首个事件到达前无进度展示） */}
          {uploading && (
            <div className="flex-1 flex flex-col gap-1 min-w-0 mr-auto justify-center">
              <div className="h-1.5 rounded-full bg-gray-100 overflow-hidden">
                <div
                  className="h-full bg-blue-500 rounded-full transition-all duration-300"
                  style={{ width: `${progress ? Math.min(100, Math.max(2, progress.percent)) : 2}%` }}
                />
              </div>
              <span className="text-[10px] text-gray-400 whitespace-nowrap">
                {progress
                  ? t('rom:upload.progressText', {
                      percent: progress.percent.toFixed(1),
                      sent: formatBytes(progress.sent),
                      total: formatBytes(progress.total),
                    })
                  : t('rom:upload.uploading')}
              </span>
            </div>
          )}
          {cancelled && !uploading && (
            <span className="mr-auto self-center text-[11px] text-amber-600">{t('rom:upload.cancelled')}</span>
          )}
          {uploading ? (
            <button
              onClick={cancelUpload}
              className="flex items-center gap-1 px-3 py-1.5 text-xs bg-red-50 rounded hover:bg-red-100 text-red-600 cursor-pointer"
            >
              <Loader2 size={12} className="animate-spin" />
              {t('rom:upload.cancel')}
            </button>
          ) : (
            <>
              <button
                onClick={onClose}
                className="px-3 py-1.5 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700 cursor-pointer"
              >
                {t('button.cancel')}
              </button>
              <button
                onClick={submit}
                className="flex items-center gap-1 px-3 py-1.5 text-xs bg-blue-500 rounded hover:bg-blue-600 text-white cursor-pointer"
              >
                {t('rom:upload.submit')}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
