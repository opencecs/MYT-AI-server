import { useState, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { AndroidInfo, MirrorItem } from '../../../../../types/index'
import { showToast } from '../../../../../components/Toast'
import { SdkApiService, OpBlock, inputCls, btnCls, runBatch } from '../shared'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../../utils/deviceId'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
}

const PICKER_URL = 'https://lbs.baidu.com/maptool/getpoint'
const ORIGIN = 'https://lbs.baidu.com'

export default function LocationTab({ androids, mirrors }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const [coord, setCoord] = useState('116.4074,39.9042')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.origin !== ORIGIN) return
      const data = e.data
      if (typeof data === 'object' && data !== null) {
        const lng = typeof data.lng === 'number' ? data.lng.toFixed(6) : ''
        const lat = typeof data.lat === 'number' ? data.lat.toFixed(6) : ''
        if (lng && lat) setCoord(`${lng},${lat}`)
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [])

  const parseCoord = (text: string): { lat: number; lng: number } | null => {
    const m = text.trim().match(/(-?\d+\.?\d*)[,\s]+(-?\d+\.?\d*)/)
    if (!m) return null
    const lngVal = parseFloat(m[1])
    const latVal = parseFloat(m[2])
    if (isNaN(latVal) || isNaN(lngVal)) return null
    return { lat: latVal, lng: lngVal }
  }

  const handlePaste = () => {
    navigator.clipboard.readText().then(text => {
      const parsed = parseCoord(text)
      if (parsed) {
        setCoord(`${parsed.lng.toFixed(6)},${parsed.lat.toFixed(6)}`)
      } else {
        showToast(t('host:moreOpsTab.location.clipboardUnrecognized'), 'error')
      }
    }).catch(() => showToast(t('host:moreOpsTab.location.clipboardReadFailed'), 'error'))
  }

  const handleApply = async () => {
    const parsed = parseCoord(coord)
    if (!parsed) { showToast(t('host:moreOpsTab.location.coordInvalid'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidLocation(getDeviceId(a, mode), a.name, parsed.lat, parsed.lng), t('host:moreOpsTab.location.setLocation')) }
    finally { setLoading(false) }
  }

  return (
    <div className="flex flex-col h-full gap-3">
      <div className="text-xs text-gray-500 bg-blue-50 border border-blue-100 rounded-lg px-3 py-2 shrink-0">
        {t('host:moreOpsTab.location.tip')}
      </div>

      <div className="flex-1 min-h-0 rounded-lg border border-gray-100 overflow-hidden">
        <iframe
          ref={iframeRef}
          src={PICKER_URL}
          title={t('host:moreOpsTab.location.mapTitle')}
          className="w-full h-full border-0"
          loading="lazy"
        />
      </div>

      <p className="text-[10px] text-gray-400 text-center shrink-0">{t('host:moreOpsTab.location.disclaimer')}</p>

      <OpBlock label={t('host:moreOpsTab.location.coord')} hint="cmd=12">
        <div className="flex gap-2">
          <input
            type="text"
            value={coord}
            onChange={e => setCoord(e.target.value)}
            placeholder={t('host:moreOpsTab.location.coordPlaceholder')}
            className={`${inputCls} flex-1`}
          />
          <button onClick={handlePaste} className="px-3 py-1.5 text-xs bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200 whitespace-nowrap">{t('host:moreOpsTab.location.pasteCoord')}</button>
          <button onClick={handleApply} disabled={loading} className={`${btnCls} whitespace-nowrap`}>{t('host:moreOpsTab.location.applyToMachine')}</button>
        </div>
      </OpBlock>
    </div>
  )
}
