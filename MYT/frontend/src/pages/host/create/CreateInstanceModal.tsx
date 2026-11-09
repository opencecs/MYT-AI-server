import { useState, useEffect, useRef, useLayoutEffect } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import Select from '../../../components/Select'
import type { DeviceState, SlotExpiry } from '../../../types/index'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getDeviceId } from '../../../utils/deviceId'
import { useCreateInstanceForm } from './useCreateInstanceForm'
import { useDerivedState } from './lib/useCreateInstanceDerived'
import { useCreateInstanceData } from './useCreateInstanceData'
import { useCreateInstance } from './useCreateInstance'
import { useSlotMemory } from '../../../hooks/useSlotMemory'
import { useSuperSdkInstall } from '../../../hooks/useSuperSdkInstall'
import { DNS_OPTIONS, SANDBOX_OPTIONS, resolutionOptions, inputCls, labelCls, formatOsVer, toggleCls, toggleDotCls, type DisplayMirror } from './lib/CreateInstanceConstants'
import { CountryAndBridge } from './parts/CountryAndBridge'
import { NetworkSection, VpcSection, CustomResolutionFields, ContainerToggleOptions } from './parts/ContainerFormFields'
import { SlotPicker } from './parts/SlotPicker'
import { CreateProgress, SlotConfirmDialog } from './parts/CreateDialogParts'
import { useModalEnter, useDropdownEnter } from '../../../hooks/useModalAnimation'

interface Props {
  device: DeviceState
  slotExpiry: SlotExpiry[]
  defaultSlot?: number
  onClose: () => void
  onCreated: () => void
}

type F = ReturnType<typeof useCreateInstanceForm>
type D = ReturnType<typeof useDerivedState>

function CreateInstanceModal({ device, slotExpiry, defaultSlot, onClose, onCreated }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const isV2 = mode === 'v2'
  const deviceId = getDeviceId(device, mode)
  const form = useCreateInstanceForm(device.device_type)
  // v2 固定容器类型（81 create 只有容器体系；模拟器 A1 创建不适用）
  useEffect(() => { if (isV2 && form.createType !== 'container') form.setCreateType('container') }, [isV2])
  useCreateInstanceData(form, deviceId, device.device_id, device.ip)
  // 「坑位记忆」开关（设置页）：开启时表单默认回填上一次创建选择的坑位
  const [slotMemoryEnabled] = useSlotMemory()
  // v2 准入放宽：主机未装超级 SDK（81 缺失）时创建不可用——横幅引导一键安装，
  // 提交任务队列后关闭弹窗（onInstalled 回调），安装完成 devicelist:updated
  // 回流翻转 super_sdk_ready，用户重进即拿到全新数据（避免与弹窗内的多份 81
  // 数据加载竞态）
  const { install: installSuperSdk } = useSuperSdkInstall(onClose)
  const needSuperSdk = isV2 && device.super_sdk_ready === false
  const derived = useDerivedState(form, device.device_type, slotExpiry, defaultSlot, device.device_name, device.ip, slotMemoryEnabled)
  const { handleCreate, handleClose, canCreate } = useCreateInstance({ form, derived, device, deviceId, onCreated, onClose })

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[760px] max-h-[85vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <Header device={device} anySlotHasRunning={derived.anySlotHasRunning} onClose={onClose} />
        {needSuperSdk && (
          <div className="mx-6 mt-3 mb-1 flex items-center gap-2 px-3 py-2 rounded-lg bg-amber-50 text-xs text-amber-700">
            <span className="flex-1">{t('host:superSdk.createBlocked')}</span>
            <button onClick={() => void installSuperSdk(device)}
              className="shrink-0 px-2.5 py-1 rounded-md bg-blue-600 text-white text-xs font-medium
                hover:bg-blue-700 disabled:opacity-50 transition-colors">
              {t('host:superSdk.install')}
            </button>
          </div>
        )}
        {!isV2 && <CreateTypeTab form={form} />}
        {form.loadErrors.length > 0 && <LoadErrors errors={form.loadErrors} />}
        <div className="flex flex-1 overflow-hidden">
          <FormFields form={form} derived={derived} isV2={isV2} />
          <SlotPicker form={form} derived={derived} />
        </div>
        {form.error && <div className="mx-6 mb-2 text-xs text-red-500 bg-red-50 p-2 rounded-lg">{form.error}</div>}
        <CreateProgress form={form} />
        <Footer form={form} handleCreate={handleCreate} handleClose={handleClose} canCreate={canCreate && !needSuperSdk} />
      </div>
      {form.slotConfirm && <SlotConfirmDialog form={form} />}
    </div>
  )
}

function Header({ device, anySlotHasRunning, onClose }: { device: DeviceState; anySlotHasRunning: boolean; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
      <div>
        <h2 className="text-base font-semibold text-gray-800">{t('host:create.title')}</h2>
        <p className="text-xs text-gray-400">{device.ip} - {device.device_type.toUpperCase()}</p>
      </div>
      <div className="flex items-center gap-3">
        {anySlotHasRunning && <span className="text-xs text-amber-600 bg-amber-50 px-2 py-0.5 rounded">{t('host:create.slotRunningHint')}</span>}
        <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
      </div>
    </div>
  )
}

function LoadErrors({ errors }: { errors: string[] }) {
  const { t } = useTranslation()
  return <div className="mx-6 mt-2 text-xs text-amber-700 bg-amber-50 p-2 rounded-lg">{t('host:create.loadErrors', { errors: errors.join(', ') })}</div>
}

const tabCls = 'flex-1 text-center text-xs py-1.5 cursor-pointer transition-colors'
const tabActiveCls = 'text-blue-600 border-b-2 border-blue-500 font-medium'
const tabInactiveCls = 'text-gray-400 hover:text-gray-600 border-b-2 border-transparent'

function CreateTypeTab({ form }: { form: F }) {
  const { t } = useTranslation()
  return (
    <div className="mx-6 mt-2 flex gap-4 border-b border-gray-100">
      <div className={tabCls + (form.createType === 'simulator' ? ` ${tabActiveCls}` : ` ${tabInactiveCls}`)} onClick={() => form.setCreateType('simulator')}>{t('host:create.simulator')}</div>
      <div className={tabCls + (form.createType === 'container' ? ` ${tabActiveCls}` : ` ${tabInactiveCls}`)} onClick={() => form.setCreateType('container')}>{t('host:create.container')}</div>
    </div>
  )
}

function FormFields({ form, derived, isV2 }: { form: F; derived: D; isV2: boolean }) {
  const { t } = useTranslation()
  const isSimulator = form.createType === 'simulator'
  return (
    <div className="flex-1 p-5 space-y-3 overflow-y-auto border-r border-gray-100">
      <div className="flex gap-3">
        <div className="flex-1">
          <label className={labelCls}>{t('host:create.name')}</label>
          <input type="text" value={form.name} onChange={e => form.setName(e.target.value)} placeholder={t('host:create.namePlaceholder')} className={inputCls} />
        </div>
        <div className="flex-1">
          <label className={labelCls}>{t('host:create.remark')} <span className="text-gray-300 font-normal">{t('host:create.remarkHint')}</span></label>
          <input type="text" value={form.remark} onChange={e => form.setRemark(e.target.value)} placeholder={t('host:create.remarkPlaceholder')} className={inputCls} />
        </div>
      </div>
      <div className="flex gap-3 flex-wrap">
        <div className="flex-1">
          <label className={labelCls}>{t('host:create.dnsAddress')}</label>
          <Select value={form.dnsType} onChange={form.setDnsType} options={DNS_OPTIONS.map(o => ({ value: o.value, label: t(`host:create.${o.labelKey}`) }))} />
        </div>
        {form.dnsType === 'custom' && (
          <div className="flex-1">
            <label className={labelCls}>{t('host:create.customDns')}</label>
            <input type="text" value={form.customDns} onChange={e => form.setCustomDns(e.target.value)} placeholder={t('host:create.customDnsPlaceholder')} className={inputCls} />
          </div>
        )}
        {!isSimulator && (
          <div className="flex-1">
            <label className={labelCls}>{t('host:create.sandboxMode')}</label>
            <Select
              value={form.sandboxMode ? form.sandboxSize : ''}
              onChange={v => {
                if (v === '') { form.setSandboxMode(false); form.setSandboxSize('16GB') }
                else { form.setSandboxMode(true); form.setSandboxSize(v) }
              }}
              options={[
                { value: '', label: t('host:create.sandboxOff') },
                ...SANDBOX_OPTIONS.map(s => ({ value: s, label: s })),
              ]}
            />
          </div>
        )}
        {isSimulator && (
          <div className="flex-1">
            <label className={labelCls}>{t('host:create.sandboxSize')}</label>
            <Select value={form.sandboxSize} onChange={form.setSandboxSize} options={SANDBOX_OPTIONS.map(s => ({ value: s, label: s }))} />
          </div>
        )}
      </div>
      <ResolutionAndVersion form={form} isSimulator={isSimulator} />
      {form.resolution === 'custom' && <CustomResolutionFields form={form} />}
      <MirrorSelect form={form} derived={derived} />
      {isV2 && (
        // v2 表单对齐 V2 Web：安全模式（enforce）开关——81 create 的 enforce 参数
        <label className="flex items-center gap-2 cursor-pointer w-fit">
          <input type="checkbox" checked={form.enforce} onChange={e => form.setEnforce(e.target.checked)} className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500 focus:ring-blue-400" />
          <span className="text-xs text-gray-600">{t('host:create.safeMode')} <span className="text-gray-300">{t('host:create.safeModeHint')}</span></span>
        </label>
      )}
      {isSimulator && <ModelSelect form={form} derived={derived} />}
      {isSimulator && <CountryAndBridge form={form} />}
      {isSimulator && <ToggleOptions form={form} />}
      {!isSimulator && !isV2 && <NetworkSection form={form} />}
      {!isSimulator && !isV2 && <VpcSection form={form} />}
      {!isSimulator && !isV2 && <ContainerToggleOptions form={form} />}
    </div>
  )
}

function ResolutionAndVersion({ form, isSimulator }: { form: F; isSimulator: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="flex gap-3">
      <div className="flex-1">
        <label className={labelCls}>{t('host:create.resolution')}</label>
        <Select value={form.resolution} onChange={form.setResolution} options={resolutionOptions(isSimulator).map(o => ({ value: o.value, label: t(`host:create.${o.labelKey}`) }))} />
      </div>
      <div className="w-20">
        <label className={labelCls}>{t('host:create.count')}</label>
        <input type="number" min={1} max={form.slotCountFallback} value={form.count} onChange={e => form.setCount(Math.max(1, Number(e.target.value)))} className={inputCls} />
      </div>
    </div>
  )
}

function MirrorSelect({ form, derived }: { form: F; derived: D }) {
  const { t } = useTranslation()
  return (
    <MirrorDropdown
      value={form.selectedImage}
      onChange={v => form.setSelectedImage(v)}
      mirrors={derived.displayMirrors}
      hasLoadError={form.loadErrors.includes(t('host:create.errLocalImage'))}
      customUrl={form.customImageUrl}
      onCustomUrlChange={form.setCustomImageUrl}
      androidVersion={form.androidVersion}
      onAndroidVersionChange={form.setAndroidVersion}
      availableVersions={derived.availableVersions}
      versionMirrorCount={derived.versionMirrorCount}
    />
  )
}

function MirrorDropdown({ value, onChange, mirrors, hasLoadError, customUrl, onCustomUrlChange, androidVersion, onAndroidVersionChange, availableVersions, versionMirrorCount }: {
  value: string
  onChange: (url: string) => void
  mirrors: DisplayMirror[]
  hasLoadError: boolean
  customUrl: string
  onCustomUrlChange: (v: string) => void
  androidVersion: string
  onAndroidVersionChange: (v: string) => void
  availableVersions: string[]
  versionMirrorCount: Record<string, number>
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ top: 0, left: 0, width: 0 })
  const { t } = useTranslation()
  useDropdownEnter(panelRef, open)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node) && panelRef.current && !panelRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  useLayoutEffect(() => {
    if (!open || !ref.current) return
    const panelW = 480, panelH = 224
    const update = () => {
      const rect = ref.current!.getBoundingClientRect()
      const spaceBelow = window.innerHeight - rect.bottom
      const top = spaceBelow < panelH + 8 ? rect.top - panelH - 4 : rect.bottom + 4
      // 左右两栏需宽面板；超出右边时右对齐避免溢出屏幕
      let left = rect.left
      if (left + panelW > window.innerWidth - 8) left = window.innerWidth - panelW - 8
      if (left < 8) left = 8
      setPos({ top, left, width: panelW })
    }
    update()
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    return () => { window.removeEventListener('scroll', update, true); window.removeEventListener('resize', update) }
  }, [open])

  const selected = mirrors.find(m => m.url === value)

  return (
    <div ref={ref} className="relative">
      <label className={labelCls}>{t('host:create.image')}</label>
      <button type="button" onClick={() => setOpen(!open)}
        className={`w-full flex items-center justify-between bg-gray-50 text-sm px-3 py-1.5 rounded-lg border text-left ${open ? 'border-blue-400' : 'border-gray-200 hover:border-gray-300'}`}>
        <span className={selected ? 'text-gray-700' : 'text-gray-400'}>
          {selected ? <>{selected.name} <span className="text-gray-400">({formatOsVer(selected.os_ver)})</span></> : t('host:create.selectImage')}
        </span>
        <svg className={`w-3 h-3 text-gray-400 transition-transform ${open ? 'rotate-180' : ''}`} fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" /></svg>
      </button>
      {open && createPortal(
        <div ref={panelRef} style={{ position: 'fixed', top: pos.top, left: pos.left, width: pos.width, zIndex: 9999 }}
          className="bg-white border border-gray-200 rounded-lg shadow-xl flex h-56">
          {/* 左栏：安卓版本 */}
          <div className="w-20 border-r border-gray-100 overflow-y-auto shrink-0">
            {availableVersions.map(v => {
              const count = versionMirrorCount[v] ?? 0
              const disabled = count === 0
              return (
                <button key={v} type="button"
                  disabled={disabled}
                  onClick={() => !disabled && onAndroidVersionChange(v)}
                  className={`w-full px-2 py-1.5 text-left text-sm whitespace-nowrap ${disabled ? 'text-gray-300 cursor-not-allowed' : v === androidVersion ? 'bg-blue-50 text-blue-600 font-medium' : 'text-gray-600 hover:bg-blue-50'}`}>
                  安卓 {v}
                </button>
              )
            })}
          </div>
          {/* 右栏：镜像列表（当前版本过滤后） */}
          <div className="flex-1 overflow-y-auto">
            {mirrors.map(m => (
              <button key={m.id} type="button"
                onClick={() => { onChange(m.url); setOpen(false) }}
                className={`w-full px-3 py-1.5 text-left text-sm hover:bg-blue-50 flex items-center gap-2 ${m.url === value ? 'bg-blue-50' : ''}`}>
                <span className="truncate flex-1">{m.name}</span>
                <span className={`shrink-0 px-1.5 py-px rounded text-[10px] font-medium ${m.source === 'local' ? 'bg-green-50 text-green-600' : m.source === 'special' ? 'bg-purple-50 text-purple-600' : 'bg-gray-100 text-gray-500'}`}>
                  {m.source === 'local' ? t('host:create.imageLocal') : m.source === 'special' ? t('host:create.imageSpecial') : t('host:create.imageOnline')}
                </span>
              </button>
            ))}
            {mirrors.length === 0 && hasLoadError && <div className="px-3 py-2 text-xs text-gray-400">{t('host:create.imageLoadFailed')}</div>}
            {mirrors.length === 0 && !hasLoadError && <div className="px-3 py-2 text-xs text-gray-400">{t('host:create.noCompatibleImage')}</div>}
            <button type="button"
              onClick={() => { onChange('__custom__'); setOpen(false) }}
              className={`w-full px-3 py-1.5 text-left text-sm border-t border-gray-100 hover:bg-blue-50 ${value === '__custom__' ? 'bg-blue-50 text-blue-600' : 'text-gray-500'}`}>
              {t('host:create.customImage')}
            </button>
          </div>
        </div>,
      document.body)}
      {value === '__custom__' && (
        <input type="text" value={customUrl} onChange={e => onCustomUrlChange(e.target.value)}
          placeholder={t('host:create.customImagePlaceholder')}
          className="mt-1 w-full text-sm px-3 py-1.5 rounded-lg border border-gray-200 outline-none focus:border-blue-400 bg-white" />
      )}
    </div>
  )
}

function ModelSelect({ form, derived }: { form: F; derived: D }) {
  const { t } = useTranslation()
  const loadErrModel = derived.filteredModels.length === 0 && form.loadErrors.includes(t('host:create.errModelList'))
  const loadErrLocal = form.localModelNames.length === 0 && form.loadErrors.includes(t('host:create.errLocalModel'))
  return (
    <div className="flex gap-3">
      <div className="flex-1">
        <label className={labelCls}>{t('host:create.modelSource')}</label>
        <Select value={form.modelKind} onChange={v => form.setModelKind(v as 'online' | 'local')} options={[
          { value: 'online', label: t('host:create.onlineModel') },
          { value: 'local', label: t('host:create.localModel') },
        ]} />
      </div>
      <div className="flex-1">
        {form.modelKind === 'online' ? (
          <>
            <label className={labelCls}>{t('host:create.onlineModel')}</label>
            <Select value={form.selectedModelId} onChange={v => { form.setSelectedModelId(v); if (v !== 'random') form.setSelectedLocalModel('') }} options={[
              { value: 'random', label: t('host:create.random') },
              ...derived.filteredModels.map(m => ({ value: m.id, label: `${m.name} (安卓 ${m.android_version})` })),
              ...(loadErrModel ? [{ value: '', label: t('host:create.loadFailed'), disabled: true }] : []),
            ]} />
          </>
        ) : (
          <>
            <label className={labelCls}>{t('host:create.localModel')}</label>
            <Select value={form.selectedLocalModel} onChange={v => { form.setSelectedLocalModel(v); if (v) form.setSelectedModelId('random') }} options={[
              { value: '', label: t('host:create.notUsed') },
              { value: '__random__', label: t('host:create.random') },
              ...form.localModelNames.map(n => ({ value: n, label: n })),
              ...(loadErrLocal ? [{ value: '', label: t('host:create.loadFailed'), disabled: true }] : []),
            ]} />
          </>
        )}
      </div>
    </div>
  )
}

function ToggleOptions({ form }: { form: F }) {
  const { t } = useTranslation()
  return (
    <div className="flex gap-6 items-center pt-1">
      <label className="flex items-center gap-2 cursor-pointer">
        <div className={toggleCls + (form.enableMagisk ? ' bg-blue-500' : ' bg-gray-200')} onClick={() => form.setEnableMagisk(!form.enableMagisk)}>
          <div className={toggleDotCls + (form.enableMagisk ? ' translate-x-4' : '')} />
        </div>
        <div className="flex flex-col leading-tight"><span className="text-xs text-gray-600">{t('host:create.magisk')}</span><span className="text-[10px] text-gray-300">{t('host:create.magiskHint')}</span></div>
      </label>
      <label className="flex items-center gap-2 cursor-pointer">
        <div className={toggleCls + (form.enableGMS ? ' bg-blue-500' : ' bg-gray-200')} onClick={() => form.setEnableGMS(!form.enableGMS)}>
          <div className={toggleDotCls + (form.enableGMS ? ' translate-x-4' : '')} />
        </div>
        <div className="flex flex-col leading-tight"><span className="text-xs text-gray-600">{t('host:create.gms')}</span><span className="text-[10px] text-gray-300">{t('host:create.gmsHint')}</span></div>
      </label>
      <label className="flex items-center gap-2 cursor-pointer">
        <input type="checkbox" checked={form.enforce} onChange={e => form.setEnforce(e.target.checked)} className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500 focus:ring-blue-400" />
        <span className="text-xs text-gray-600">{t('host:create.safeMode')} <span className="text-gray-300">{t('host:create.safeModeHint')}</span></span>
      </label>
      <label className="flex items-center gap-2 cursor-pointer">
        <input type="checkbox" checked={form.randomFile} onChange={e => form.setRandomFile(e.target.checked)} className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500 focus:ring-blue-400" />
        <span className="text-xs text-gray-600">{t('host:create.randomFile')}</span>
      </label>
      <label className="flex items-center gap-2 cursor-pointer">
        <div className={toggleCls + (form.compatMode ? ' bg-blue-500' : ' bg-gray-200')} onClick={() => form.setCompatMode(!form.compatMode)}>
          <div className={toggleDotCls + (form.compatMode ? ' translate-x-4' : '')} />
        </div>
        <div className="flex flex-col leading-tight"><span className="text-xs text-gray-600">{t('host:create.compatMode')}</span><span className="text-[10px] text-gray-300">{t('host:create.compatModeHint')}</span></div>
      </label>
    </div>
  )
}

function Footer({ form, handleCreate, handleClose, canCreate }: { form: F; handleCreate: () => void; handleClose: () => void; canCreate: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
      <button onClick={handleClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{form.creating ? t('host:create.cancelCreate') : t('button.cancel')}</button>
      <button onClick={handleCreate} disabled={form.creating || !canCreate} className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">
        {form.creating ? (form.createStep ? `${form.createStep}${form.createProgress >= 0 ? ` ${form.createProgress}%` : ''}` : t('host:create.creating')) : t('host:create.create')}
      </button>
    </div>
  )
}

export default CreateInstanceModal
