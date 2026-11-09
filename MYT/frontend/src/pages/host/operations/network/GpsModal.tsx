import { useState, useEffect, useRef, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo, CountryCode } from '../../../../types/index'
import { getErrorMessage } from '../../../../utils/error'
import { showToast } from '../../../../components/Toast'
import { cast } from '../../../../utils/cast'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../utils/deviceId'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import { useDropdownEnter } from '../../../../hooks/useModalAnimation'
import Select from '../../../../components/Select'

interface Props {
  android: AndroidInfo
  onClose: () => void
}

const inputCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200 focus:border-blue-400 focus:outline-none'
const selectCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200'
const labelCls = 'text-xs text-gray-500 mb-1 block'

// 设备 REST：加载国家代码列表（经 Go 代理，自动适配 local/public 模式）
async function fetchCountryCodes(deviceId: string): Promise<CountryCode[]> {
  const resp = await SdkApiService.DeviceFetch(deviceId, 'GET', '/android/countryCode?page=0', '')
  const json = JSON.parse(resp || '{}')
  const data = json.data ?? json
  const list = Array.isArray(data) ? data : (data?.list ?? [])
  return cast<CountryCode[]>(list)
}

// 带搜索的国家下拉
function CountrySelect({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: CountryCode[] }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const ref = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  useDropdownEnter(panelRef, open)

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [])

  const selected = options.find(c => c.countryCode === value)
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return options
    return options.filter(c =>
      c.countryName.toLowerCase().includes(q) || c.countryCode.toLowerCase().includes(q)
    )
  }, [options, query])

  return (
    <div className="relative" ref={ref}>
      <button type="button" onClick={() => setOpen(o => !o)} className={selectCls + ' text-left flex items-center justify-between'}>
        <span className="truncate">{selected ? `${selected.countryName} (${selected.countryCode})` : value || t('host:gps.selectPlaceholder')}</span>
        <svg className={`w-3.5 h-3.5 text-gray-400 transition-transform ${open ? 'rotate-180' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="6 9 12 15 18 9" /></svg>
      </button>
      {open && (
        <div ref={panelRef} className="absolute z-20 mt-1 w-full bg-white border border-gray-200 rounded-lg shadow-lg flex flex-col max-h-60">
          <div className="p-2 border-b border-gray-100">
            <input
              autoFocus
              type="text"
              value={query}
              onChange={e => setQuery(e.target.value)}
              placeholder={t('host:gps.searchPlaceholder')}
              className="w-full text-sm px-2 py-1 bg-gray-50 rounded border border-gray-200 focus:border-blue-400 focus:outline-none"
            />
          </div>
          <div className="overflow-auto">
            {filtered.length === 0 ? (
              <div className="px-3 py-2 text-xs text-gray-400">{t('host:gps.noMatch')}</div>
            ) : filtered.map(c => (
              <button
                key={c.countryCode}
                type="button"
                onClick={() => { onChange(c.countryCode); setOpen(false); setQuery('') }}
                className={`w-full text-left px-3 py-1.5 text-sm hover:bg-blue-50 ${c.countryCode === value ? 'bg-blue-50 text-blue-600' : 'text-gray-700'}`}
              >
                {c.countryName} ({c.countryCode})
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

// GPS 定位设置（定位 IP + 国家/地区）
function GpsModal({ android, onClose }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const deviceId = getDeviceId(android, mode)
  const [locateIp, setLocateIp] = useState('8.8.8.8')
  const [country, setCountry] = useState('CN')
  const [language, setLanguage] = useState('zh-CN')
  const [countries, setCountries] = useState<CountryCode[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    // v2 模式设备无 8000 端口（壳子 V2 Web 的国家列表同样调不通），
    // 跳过拉取——国家选择仅本地展示，不参与 SetIPLocate 提交（只传 locateIp + language）
    if (mode === 'v2') return
    fetchCountryCodes(deviceId).then(setCountries).catch(() => {})
  }, [deviceId, mode])

  const handleApply = async () => {
    if (!locateIp) { showToast(t('host:toast.gpsFillIp'), 'error'); return }
    setLoading(true)
    try {
      await SdkApiService.SetIPLocate(deviceId, android.name, locateIp, language)
      showToast(t('host:toast.gpsSuccess'), 'success')
      onClose()
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[420px] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-base font-semibold text-gray-800">{t('host:gps.title')}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="px-6 py-4 space-y-3">
          <div>
            <label className={labelCls}>{t('host:gps.locateIp')}</label>
            <input type="text" value={locateIp} onChange={e => setLocateIp(e.target.value)} placeholder={t('host:gps.locateIpPlaceholder')} className={inputCls} />
          </div>
          <div className="flex gap-3">
            <div className="flex-1">
              <label className={labelCls}>{t('host:gps.countryRegion')}</label>
              <CountrySelect value={country} onChange={setCountry} options={countries} />
            </div>
            <div className="flex-1">
              <label className={labelCls}>{t('host:gps.language')}</label>
              <Select value={language} onChange={setLanguage} options={[{ value: 'zh-CN', label: t('host:gps.langZh') }, { value: 'en-US', label: 'English' }]} />
            </div>
          </div>
        </div>
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={handleApply} disabled={loading || !locateIp}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">{loading ? t('host:gps.setting') : t('host:gps.apply')}</button>
        </div>
      </div>
    </div>
  )
}

export default GpsModal
