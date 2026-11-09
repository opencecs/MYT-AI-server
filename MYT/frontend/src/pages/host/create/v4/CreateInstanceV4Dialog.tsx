import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2, X } from 'lucide-react'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import * as V4Service from '../../../../../bindings/github.com/moyunteng/myt/v4servicewrapper'
import Select from '../../../../components/Select'
import { showToast } from '../../../../components/Toast'
import type { V4BackendOption, V4CreateInstanceParams, V4ImageOption } from '../../../../../bindings/github.com/moyunteng/myt/models'

interface Props {
  /** 目标主机 IP（多主机时由调用方指定；单主机可传空由 Go 侧兜底） */
  host: string
  onClose: () => void
  /** 创建成功（含已发起开机）后回调：页面据此刷新列表 */
  onCreated: () => void
}

/** 默认资源配置（与 tobox_player 服务端默认值一致：4 核 / 4096MB / 8192MB） */
const DEFAULT_CPU = 4
const DEFAULT_MEMORY = 4096
const DEFAULT_DISK = 8192

/**
 * 创建云机弹窗（V4 模式）。
 *
 * 字段设计对齐 POST /android/create 的可选参数：name / cpu_cores / memory_mb /
 * disk_mb / backend / variant。三件套绝对路径（system_image 等）不做 UI——正常
 * 用户没有宿主机文件路径的概念，镜像统一走「镜像变体」下拉（GET /image/list）。
 *
 * 创建后实例是 stopped，必须再 /android/start：默认勾选「创建后开机」，与 V3 创建
 * 弹窗的默认行为一致（V3 也是创建后显式开机）。开机失败不回滚创建，只 toast 提示。
 */
export default function CreateInstanceV4Dialog({ host, onClose, onCreated }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  const [name, setName] = useState('')
  const [cpu, setCpu] = useState(String(DEFAULT_CPU))
  const [memory, setMemory] = useState(String(DEFAULT_MEMORY))
  const [disk, setDisk] = useState(String(DEFAULT_DISK))
  const [backend, setBackend] = useState('')
  const [variant, setVariant] = useState('')
  const [startNow, setStartNow] = useState(true)

  const [backends, setBackends] = useState<V4BackendOption[]>([])
  const [images, setImages] = useState<V4ImageOption[]>([])
  const [loadingOptions, setLoadingOptions] = useState(true)
  const [optionsError, setOptionsError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // 后端 + 镜像：打开即拉（镜像按所选后端各自一套？——镜像目录由 SDK 统一管理，
  // 与后端无关，因此只在打开时拉一次；后端切换不需要重拉镜像）
  useEffect(() => {
    let cancelled = false
    setLoadingOptions(true)
    Promise.all([
      V4Service.ListBackends(host).catch(() => [] as V4BackendOption[]),
      V4Service.ListImages(host).catch(() => null as V4ImageOption[] | null),
    ]).then(([beList, imgList]) => {
      if (cancelled) return
      const bes = (beList ?? []) as V4BackendOption[]
      setBackends(bes)
      // 默认后端优先（与服务端「不传 backend = 默认后端」一致，但显式选中更清楚）
      const def = bes.find(b => b.default)
      setBackend(def?.name ?? bes[0]?.name ?? '')
      if (imgList === null) {
        setOptionsError(t('instances.create.imagesLoadFailed'))
      } else {
        const imgs = (imgList ?? []) as V4ImageOption[]
        setImages(imgs)
        // 默认选第一个可用变体；没有可用变体则留空（= 用服务端默认系统镜像）
        const ready = imgs.find(i => i.ready)
        setVariant(ready?.variant ?? '')
      }
      setLoadingOptions(false)
    })
    return () => { cancelled = true }
  }, [host, t])

  const backendOptions = useMemo(
    () => backends.map(b => ({
      value: b.name,
      label: b.default ? `${b.name} ${t('instances.create.backendDefault')}` : b.name,
      disabled: b.status !== 'connected',
    })),
    [backends, t],
  )

  const imageOptions = useMemo(
    () => [
      { value: '', label: t('instances.create.imageServerDefault') },
      ...images.map(i => ({
        value: i.variant,
        label: i.ready ? i.variant : `${i.variant} ${t('instances.create.imageNotReady')}`,
        disabled: !i.ready,
      })),
    ],
    [images, t],
  )

  // 数字校验：留空走服务端默认（传 0 = omitempty 不传），填了必须是正整数
  const numOrZero = useCallback((raw: string): number => {
    const v = Number(raw.trim())
    return Number.isFinite(v) && v > 0 ? Math.floor(v) : 0
  }, [])

  const submit = useCallback(async () => {
    if (submitting) return
    // 资源配置填了但不是正整数 → 明确报错，不静默改成默认值
    for (const [raw, label] of [[cpu, t('instances.create.cpu')], [memory, t('instances.create.memory')], [disk, t('instances.create.disk')]] as const) {
      const trimmed = raw.trim()
      if (trimmed !== '' && numOrZero(trimmed) === 0) {
        showToast(t('instances.create.invalidNumber', { field: label }), 'error')
        return
      }
    }
    setSubmitting(true)
    try {
      const params: V4CreateInstanceParams = {
        host,
        name: name.trim(),
        cpuCores: numOrZero(cpu),
        memoryMB: numOrZero(memory),
        diskMB: numOrZero(disk),
        backend,
        variant,
        start: startNow,
      }
      const res = await V4Service.CreateInstance(params)
      if (!res) {
        showToast(t('instances.create.failed', { message: t('instances.create.noResult') }), 'error')
        return
      }
      if (res.startError) {
        // 实例已创建成功，仅开机失败：不阻断，明确告知用户可手动启动
        showToast(t('instances.create.createdStartFailed', { name: res.name || res.id, message: res.startError }), 'warning')
      } else {
        showToast(t('instances.create.created', { name: res.name || res.id }))
      }
      onCreated()
      onClose()
    } catch (e) {
      showToast(t('instances.create.failed', { message: String(e) }), 'error')
    } finally {
      setSubmitting(false)
    }
  }, [submitting, cpu, memory, disk, host, name, backend, variant, startNow, numOrZero, t, onCreated, onClose])

  const fieldCls = 'w-full rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 outline-none placeholder:text-gray-400 focus:border-blue-400 disabled:bg-gray-50 disabled:text-gray-400'

  return (
    <div
      className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay"
      ref={rootRef}
      role="dialog"
      aria-modal="true"
      onKeyDown={e => { if (e.key === 'Escape' && !submitting) onClose() }}
    >
      <div className="bg-white rounded-lg shadow-xl w-[420px] max-w-[calc(100vw-32px)] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        {/* 头部 */}
        <div className="flex items-center px-5 pt-4 pb-3 border-b border-gray-100">
          <h3 className="text-sm font-semibold text-gray-800">{t('instances.create.title')}</h3>
          <button
            onClick={onClose}
            disabled={submitting}
            className="ml-auto text-gray-400 hover:text-gray-600 disabled:opacity-40 cursor-pointer"
            aria-label={t('button.cancel')}
          >
            <X size={16} />
          </button>
        </div>

        {/* 表单 */}
        <div className="px-5 py-4 flex flex-col gap-3">
          <label className="flex flex-col gap-1">
            <span className="text-xs text-gray-500">{t('instances.create.name')}</span>
            <input
              className={fieldCls}
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder={t('instances.create.namePlaceholder')}
              disabled={submitting}
              autoFocus
            />
          </label>

          <div className="flex gap-3">
            <label className="flex-1 flex flex-col gap-1">
              <span className="text-xs text-gray-500">{t('instances.create.cpu')}</span>
              <input className={fieldCls} value={cpu} onChange={e => setCpu(e.target.value)} inputMode="numeric" disabled={submitting} />
            </label>
            <label className="flex-1 flex flex-col gap-1">
              <span className="text-xs text-gray-500">{t('instances.create.memory')}</span>
              <input className={fieldCls} value={memory} onChange={e => setMemory(e.target.value)} inputMode="numeric" disabled={submitting} />
            </label>
            <label className="flex-1 flex flex-col gap-1">
              <span className="text-xs text-gray-500">{t('instances.create.disk')}</span>
              <input className={fieldCls} value={disk} onChange={e => setDisk(e.target.value)} inputMode="numeric" disabled={submitting} />
            </label>
          </div>

          {/* 后端：仅多套 tebox 栈时有意义（单后端只显示一项，仍展示便于确认打到哪套） */}
          {backends.length > 0 && (
            <label className="flex flex-col gap-1">
              <span className="text-xs text-gray-500">{t('instances.create.backend')}</span>
              <Select
                value={backend}
                onChange={setBackend}
                options={backendOptions}
                disabled={submitting || backends.length <= 1}
                buttonClassName="h-8"
              />
            </label>
          )}

          <label className="flex flex-col gap-1">
            <span className="text-xs text-gray-500">{t('instances.create.image')}</span>
            <Select
              value={variant}
              onChange={setVariant}
              options={imageOptions}
              disabled={submitting || loadingOptions}
              buttonClassName="h-8"
              searchable
            />
            {loadingOptions && (
              <span className="flex items-center gap-1 text-[11px] text-gray-400">
                <Loader2 size={11} className="animate-spin" />
                {t('instances.create.loadingImages')}
              </span>
            )}
            {!loadingOptions && optionsError && (
              <span className="text-[11px] text-amber-600">{optionsError}</span>
            )}
            {!loadingOptions && !optionsError && images.length === 0 && (
              <span className="text-[11px] text-gray-400">{t('instances.create.noImages')}</span>
            )}
          </label>

          <label className="flex items-center gap-2 text-xs text-gray-600 mt-0.5">
            <input type="checkbox" checked={startNow} onChange={e => setStartNow(e.target.checked)} disabled={submitting} />
            {t('instances.create.startNow')}
          </label>
        </div>

        {/* 底部 */}
        <div className="flex justify-end gap-2 px-5 pb-4">
          <button
            onClick={onClose}
            disabled={submitting}
            className="px-3 py-1.5 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700 disabled:opacity-50 cursor-pointer"
          >
            {t('button.cancel')}
          </button>
          <button
            onClick={submit}
            disabled={submitting}
            className="flex items-center gap-1 px-3 py-1.5 text-xs bg-blue-500 rounded hover:bg-blue-600 text-white disabled:opacity-60 cursor-pointer"
          >
            {submitting && <Loader2 size={12} className="animate-spin" />}
            {submitting ? t('instances.create.submitting') : t('instances.create.submit')}
          </button>
        </div>
      </div>
    </div>
  )
}
