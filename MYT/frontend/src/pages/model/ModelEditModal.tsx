import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as ModelApi from '../../api/phoneModelApi'
import { useModalEnter } from '../../hooks/useModalAnimation'
import { showToast } from '../../components/Toast'
import { SIM_FIELDS, NET_FIELDS, KERNEL_FIELDS, DRM_FIELDS, GPU_FIELDS, SYSTEM_FIELDS, BUILD_FIELDS, PM_FIELDS, SIM_PRESET_FIELDS, LOCALE_FIELDS } from './modelFields'
import { parseDeviceInfoFills } from './modelDeviceInfoFill'
import { OverlayTab, DgConfigTab } from './EditSubTabs'

interface Props {
  modelName: string
  onClose: () => void
  onDone: () => void
}

type SubTab = 'basic' | 'screen' | 'overlay' | 'sim' | 'net' | 'kernel' | 'drm' | 'gpu' | 'system' | 'pm' | 'locale' | 'prop'
const ANDROID_VERSIONS = ['10', '11', '12', '13', '14', '15', '16']
const inputCls = 'w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none focus:border-blue-400 bg-white'
const labelCls = 'text-[10px] text-gray-400 mb-0.5 block'

function ModelEditModal({ modelName, onClose, onDone }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [model, setModel] = useState(modelName)
  const [brand, setBrand] = useState('')
  const [manufacturer, setManufacturer] = useState('')
  const [androidVersion, setAndroidVersion] = useState('')
  const [securityPatch, setSecurityPatch] = useState('')
  const [width, setWidth] = useState('')
  const [height, setHeight] = useState('')
  const [dpi, setDpi] = useState('')
  const [refreshRate, setRefreshRate] = useState('')
  const [randomizeIDs, setRandomizeIDs] = useState(true)
  const [generating, setGenerating] = useState(false)
  const [subTab, setSubTab] = useState<SubTab>('basic')
  const [showBuildAdvanced, setShowBuildAdvanced] = useState(false)

  const [overlayKeys, setOverlayKeys] = useState<string[]>([])
  const [enabledOverlayKeys, setEnabledOverlayKeys] = useState<Set<string>>(new Set())
  const [overlayValues, setOverlayValues] = useState<Record<string, string>>({})
  const [prop, setProp] = useState<Record<string, string>>({})
  const [supportedVersion, setSupportedVersion] = useState('')

  const [simConfig, setSimConfig] = useState<Record<string, string>>({})
  const [netConfig, setNetConfig] = useState<Record<string, string>>({})
  const [kernelConfig, setKernelConfig] = useState<Record<string, string>>({})
  const [drmConfig, setDrmConfig] = useState<Record<string, string>>({})
  const [gpuConfig, setGpuConfig] = useState<Record<string, string>>({})
  const [systemConfig, setSystemConfig] = useState<Record<string, string>>({})
  const [buildFields, setBuildFields] = useState<Record<string, string>>({})
  const [pmConfig, setPmConfig] = useState<Record<string, string>>({})
  const [simPresetConfig, setSimPresetConfig] = useState<Record<string, string>>({})
  const [localeConfig, setLocaleConfig] = useState<Record<string, string>>({})

  useEffect(() => {
    ModelApi.GetModelCfg(modelName).then(json => {
      try {
        const cfg = JSON.parse(json)
        if (cfg.overlay) {
          const keys = Object.keys(cfg.overlay)
          setOverlayKeys(keys)
          setEnabledOverlayKeys(new Set(keys))
          const vals: Record<string, string> = {}
          for (const [k, v] of Object.entries(cfg.overlay)) vals[k] = String(v)
          setOverlayValues(vals)
        }
        if (cfg.prop) {
          const p: Record<string, string> = {}
          for (const [k, v] of Object.entries(cfg.prop)) p[k] = String(v)
          setProp(p)
        }
        if (cfg.supported_version) setSupportedVersion(cfg.supported_version)
      } catch {}
    }).catch(() => {})

    // 并行加载 device_info.json 回填（SIM/网络/内核/DRM 等分区 cfg.json 里没有）
    ModelApi.GetModelDeviceInfo(modelName).then(json => {
      const fills = parseDeviceInfoFills(json)
      if (!fills) return
      if (Object.keys(fills.simConfig).length) setSimConfig(fills.simConfig)
      if (Object.keys(fills.netConfig).length) setNetConfig(fills.netConfig)
      if (Object.keys(fills.kernelConfig).length) setKernelConfig(fills.kernelConfig)
      if (Object.keys(fills.drmConfig).length) setDrmConfig(fills.drmConfig)
      if (Object.keys(fills.gpuConfig).length) setGpuConfig(fills.gpuConfig)
      if (Object.keys(fills.systemConfig).length) setSystemConfig(fills.systemConfig)
      if (Object.keys(fills.pmConfig).length) setPmConfig(fills.pmConfig)
      if (Object.keys(fills.simPresetConfig).length) setSimPresetConfig(fills.simPresetConfig)
      if (Object.keys(fills.localeConfig).length) setLocaleConfig(fills.localeConfig)
    }).catch(() => {})
  }, [modelName])

  const collectParams = (): Record<string, unknown> => {
    const params: Record<string, unknown> = { randomizeIDs }
    if (model.trim()) params.model = model.trim()
    if (brand.trim()) params.brand = brand.trim()
    if (manufacturer.trim()) params.manufacturer = manufacturer.trim()
    if (androidVersion) params.androidVersion = androidVersion
    if (securityPatch.trim()) params.securityPatch = securityPatch.trim()
    if (width) params.width = Number(width)
    if (height) params.height = Number(height)
    if (dpi) params.dpi = Number(dpi)
    if (refreshRate) params.refreshRate = Number(refreshRate)
    if (randomizeIDs) params.overlayKeys = Array.from(enabledOverlayKeys)
    if (Object.keys(overlayValues).length > 0) params.overlay = overlayValues
    if (Object.keys(prop).length > 0) params.prop = prop
    if (supportedVersion.trim()) params.supportedVersion = supportedVersion.trim()
    if (Object.keys(simConfig).length > 0) params.simConfig = simConfig
    if (Object.keys(netConfig).length > 0) params.netConfig = netConfig
    if (Object.keys(kernelConfig).length > 0) params.kernelConfig = kernelConfig
    if (Object.keys(drmConfig).length > 0) params.drmConfig = drmConfig
    if (Object.keys(gpuConfig).length > 0) params.gpuConfig = gpuConfig
    if (Object.keys(systemConfig).length > 0) params.systemConfig = systemConfig
    if (Object.keys(pmConfig).length > 0) params.pmConfig = pmConfig
    if (Object.keys(simPresetConfig).length > 0) params.simPresetConfig = simPresetConfig
    if (Object.keys(localeConfig).length > 0) params.localeConfig = localeConfig
    for (const [k, v] of Object.entries(buildFields)) {
      if (v.trim()) (params as Record<string, unknown>)[k] = v.trim()
    }
    return params
  }

  const handleGenerate = async () => {
    setGenerating(true)
    try {
      await ModelApi.EditPhoneModel(modelName, JSON.stringify(collectParams()))
      showToast(t('model:edit.generateSuccess'))
      onDone()
    } catch (e) {
      showToast(t('model:edit.generateFailed', { error: e instanceof Error ? e.message : t('taskQueue.failure.unknownError') }), 'error')
    } finally {
      setGenerating(false)
    }
  }

  const toggleOverlayKey = (key: string) => {
    setEnabledOverlayKeys(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const subTabs: { id: SubTab; label: string }[] = [
    { id: 'basic', label: 'subTabs.basic' },
    { id: 'screen', label: 'subTabs.screen' },
    { id: 'overlay', label: 'subTabs.overlay' },
    { id: 'sim', label: 'subTabs.sim' },
    { id: 'net', label: 'subTabs.net' },
    { id: 'kernel', label: 'subTabs.kernel' },
    { id: 'drm', label: 'subTabs.drm' },
    { id: 'gpu', label: 'subTabs.gpu' },
    { id: 'system', label: 'subTabs.system' },
    { id: 'pm', label: 'subTabs.pm' },
    { id: 'locale', label: 'subTabs.locale' },
    { id: 'prop', label: 'subTabs.prop' },
  ]

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[560px] max-h-[85vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="shrink-0 bg-white px-4 pt-4 pb-2 border-b border-gray-100 flex items-start justify-between">
          <div>
            <h4 className="text-sm font-medium text-gray-700">{t('model:edit.title', { modelName })}</h4>
            <p className="text-[10px] text-gray-400 mt-0.5">{t('model:edit.subtitle')}</p>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        <div className="shrink-0 flex flex-wrap border-b border-gray-100 px-4">
          {subTabs.map(st => (
            <button key={st.id} onClick={() => setSubTab(st.id)}
              className={`px-2.5 py-1.5 text-[10px] ${subTab === st.id ? 'text-blue-500 border-b-2 border-blue-500 font-medium' : 'text-gray-400'}`}>
              {t('model:' + st.label)}
            </button>
          ))}
        </div>

        <div className="flex-1 overflow-y-auto p-4 space-y-3">
          {subTab === 'basic' && <BasicTab model={model} setModel={setModel} brand={brand} setBrand={setBrand} manufacturer={manufacturer} setManufacturer={setManufacturer} androidVersion={androidVersion} setAndroidVersion={setAndroidVersion} securityPatch={securityPatch} setSecurityPatch={setSecurityPatch} showBuildAdvanced={showBuildAdvanced} setShowBuildAdvanced={setShowBuildAdvanced} buildFields={buildFields} setBuildFields={setBuildFields} />}
          {subTab === 'screen' && <ScreenTab width={width} setWidth={setWidth} height={height} setHeight={setHeight} dpi={dpi} setDpi={setDpi} refreshRate={refreshRate} setRefreshRate={setRefreshRate} />}
          {subTab === 'overlay' && <OverlayTab keys={overlayKeys} enabledKeys={enabledOverlayKeys} values={overlayValues} onToggleKey={toggleOverlayKey} onChangeValue={(k, v) => setOverlayValues({ ...overlayValues, [k]: v })} randomizeIDs={randomizeIDs} onRandomizeChange={setRandomizeIDs} />}
          {subTab === 'sim' && <>
            <DgConfigTab fields={SIM_FIELDS} values={simConfig} onChange={setSimConfig} />
            <div className="border-t border-gray-100 pt-2 mt-2">
              <p className="text-[10px] font-medium text-gray-500 mb-1">{t('model:edit.countryPreset')}</p>
              <DgConfigTab fields={SIM_PRESET_FIELDS} values={simPresetConfig} onChange={setSimPresetConfig} hint={t('model:edit.countryPresetHint')} />
            </div>
          </>}
          {subTab === 'net' && <DgConfigTab fields={NET_FIELDS} values={netConfig} onChange={setNetConfig} />}
          {subTab === 'kernel' && <DgConfigTab fields={KERNEL_FIELDS} values={kernelConfig} onChange={setKernelConfig} />}
          {subTab === 'drm' && <DgConfigTab fields={DRM_FIELDS} values={drmConfig} onChange={setDrmConfig} hint={t('model:dgConfig.drmHint')} />}
          {subTab === 'gpu' && <DgConfigTab fields={GPU_FIELDS} values={gpuConfig} onChange={setGpuConfig} hint={t('model:dgConfig.gpuHint')} />}
          {subTab === 'system' && <DgConfigTab fields={SYSTEM_FIELDS} values={systemConfig} onChange={setSystemConfig} hint={t('model:dgConfig.systemHint')} />}
          {subTab === 'pm' && <DgConfigTab fields={PM_FIELDS} values={pmConfig} onChange={setPmConfig} hint={t('model:dgConfig.pmHint')} />}
          {subTab === 'locale' && <DgConfigTab fields={LOCALE_FIELDS} values={localeConfig} onChange={setLocaleConfig} hint={t('model:dgConfig.localeHint')} />}
          {subTab === 'prop' && <PropTab prop={prop} setProp={setProp} supportedVersion={supportedVersion} setSupportedVersion={setSupportedVersion} />}
        </div>

        <div className="shrink-0 bg-white px-4 py-3 border-t border-gray-100 flex justify-end gap-2">
          <button onClick={onClose} disabled={generating} className="px-4 py-1.5 text-xs text-gray-500 hover:text-gray-700">{t('button.cancel')}</button>
          <button onClick={handleGenerate} disabled={generating}
            className="px-4 py-1.5 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">
            {generating ? t('model:edit.generating') : t('model:edit.generate')}
          </button>
        </div>
      </div>
    </div>
  )
}

function BasicTab({ model, setModel, brand, setBrand, manufacturer, setManufacturer, androidVersion, setAndroidVersion, securityPatch, setSecurityPatch, showBuildAdvanced, setShowBuildAdvanced, buildFields, setBuildFields }: {
  model: string; setModel: (v: string) => void; brand: string; setBrand: (v: string) => void
  manufacturer: string; setManufacturer: (v: string) => void; androidVersion: string; setAndroidVersion: (v: string) => void
  securityPatch: string; setSecurityPatch: (v: string) => void
  showBuildAdvanced: boolean; setShowBuildAdvanced: (v: boolean) => void
  buildFields: Record<string, string>; setBuildFields: (v: Record<string, string>) => void
}) {
  const { t } = useTranslation()
  const updateBuild = (key: string, val: string) => setBuildFields({ ...buildFields, [key]: val })
  return (
    <div className="space-y-2">
      <div><label className={labelCls}>{t('model:edit.modelName')}</label><input value={model} onChange={e => setModel(e.target.value)} className={inputCls} /></div>
      <div className="grid grid-cols-2 gap-2">
        <div><label className={labelCls}>{t('model:edit.brand')}</label><input value={brand} onChange={e => setBrand(e.target.value)} placeholder={t('model:edit.brandPlaceholder')} className={inputCls} /></div>
        <div><label className={labelCls}>{t('model:edit.manufacturer')}</label><input value={manufacturer} onChange={e => setManufacturer(e.target.value)} placeholder={t('model:edit.manufacturerPlaceholder')} className={inputCls} /></div>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <div><label className={labelCls}>{t('model:edit.androidVersion')}</label><select value={androidVersion} onChange={e => setAndroidVersion(e.target.value)} className={inputCls}><option value="">{t('model:edit.noChange')}</option>{ANDROID_VERSIONS.map(v => <option key={v} value={v}>Android {v}</option>)}</select></div>
        <div><label className={labelCls}>{t('model:edit.securityPatch')}</label><input type="date" value={securityPatch} onChange={e => setSecurityPatch(e.target.value)} className={inputCls} /></div>
      </div>
      {(model.trim() || brand.trim()) && !showBuildAdvanced && (
        <div className="bg-yellow-50 rounded p-2 text-[10px] text-yellow-700">
          {t('model:edit.buildAdvancedHint')}
          <button onClick={() => setShowBuildAdvanced(true)} className="ml-1 text-blue-500 underline">{t('model:edit.expandConfig')}</button>
        </div>
      )}
      <button onClick={() => setShowBuildAdvanced(!showBuildAdvanced)}
        className="text-[10px] text-blue-500 hover:text-blue-700">
        {showBuildAdvanced ? t('model:edit.hideBuildAdvanced') : t('model:edit.showBuildAdvanced')}
      </button>
      {showBuildAdvanced && (
        <div className="space-y-1.5 pl-2 border-l-2 border-blue-100">
          {BUILD_FIELDS.map(f => (
            <div key={f.key} className="flex items-center gap-2">
              <span className="text-[10px] text-gray-500 w-24 shrink-0">{t('model:' + f.label, { defaultValue: f.label })}</span>
              <input value={buildFields[f.key] || ''} onChange={e => updateBuild(f.key, e.target.value)}
                placeholder={f.placeholder ? t('model:' + f.placeholder, { defaultValue: f.key }) : f.key} className="flex-1 text-[10px] px-2 py-1 border border-gray-200 rounded outline-none focus:border-blue-400 bg-white font-mono" />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function ScreenTab({ width, setWidth, height, setHeight, dpi, setDpi, refreshRate, setRefreshRate }: {
  width: string; setWidth: (v: string) => void; height: string; setHeight: (v: string) => void
  dpi: string; setDpi: (v: string) => void; refreshRate: string; setRefreshRate: (v: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-2">
      <div className="grid grid-cols-[1fr_auto_1fr] gap-1 items-end">
        <div><label className={labelCls}>{t('model:edit.screenWidth')}</label><input type="number" value={width} onChange={e => setWidth(e.target.value)} placeholder="1080" className={inputCls} /></div>
        <span className="text-gray-300 pb-1.5">×</span>
        <div><label className={labelCls}>{t('model:edit.screenHeight')}</label><input type="number" value={height} onChange={e => setHeight(e.target.value)} placeholder="2412" className={inputCls} /></div>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <div><label className={labelCls}>{t('model:edit.dpi')}</label><input type="number" value={dpi} onChange={e => setDpi(e.target.value)} placeholder="480" className={inputCls} /></div>
        <div><label className={labelCls}>{t('model:edit.refreshRate')}</label><input type="number" value={refreshRate} onChange={e => setRefreshRate(e.target.value)} placeholder="120" className={inputCls} /></div>
      </div>
    </div>
  )
}

function PropTab({ prop, setProp, supportedVersion, setSupportedVersion }: {
  prop: Record<string, string>; setProp: (v: Record<string, string>) => void
  supportedVersion: string; setSupportedVersion: (v: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-2">
      <div><label className={labelCls}>{t('model:edit.supportedVersion')}</label><input value={supportedVersion} onChange={e => setSupportedVersion(e.target.value)} placeholder="1-990" className={inputCls} /></div>
      <div className="text-[11px] font-medium text-gray-600 mt-2">{t('model:edit.propTitle')}</div>
      {Object.entries(prop).sort(([a], [b]) => a.localeCompare(b)).map(([key, val]) => (
        <div key={key} className="flex items-center gap-2">
          <span className="text-[10px] text-gray-500 w-56 shrink-0 truncate font-mono" title={key}>{key}</span>
          <input value={val} onChange={e => { const n = { ...prop }; n[key] = e.target.value; setProp(n) }}
            className="flex-1 text-[10px] px-2 py-1 border border-gray-200 rounded outline-none focus:border-blue-400 bg-white font-mono" />
          <button onClick={() => { const n = { ...prop }; delete n[key]; setProp(n) }}
            className="text-[10px] text-red-400 hover:text-red-600 shrink-0">{t('model:edit.deleteProp')}</button>
        </div>
      ))}
    </div>
  )
}

export default ModelEditModal
