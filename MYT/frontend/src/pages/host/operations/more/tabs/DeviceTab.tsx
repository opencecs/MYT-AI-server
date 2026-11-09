import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SdkApiService, OpBlock, inputCls, btnCls, runBatch, AndroidInfo, MirrorItem } from '../shared'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../../utils/deviceId'
import Select from '../../../../../components/Select'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
  allContainer: boolean
}

export default function DeviceTab({ androids, mirrors, allContainer }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [language, setLanguage] = useState('zh-CN')
  const [country, setCountry] = useState('CN')
  const [adid, setAdid] = useState('')
  const [adbRoot, setAdbRoot] = useState(1)
  const [bgPkg, setBgPkg] = useState('')
  const [bgCmd, setBgCmd] = useState(1)
  const [loading, setLoading] = useState(false)

  const handleSetLanguage = async () => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidLanguage(getDeviceId(a, mode), a.name, language, country), t('host:moreOpsTab.device.languageSetting')) }
    finally { setLoading(false) }
  }

  const handleSetADID = async (cmd: number) => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidADID(getDeviceId(a, mode), a.name, cmd, adid), cmd === 1 ? t('host:moreOpsTab.device.generateAdid') : t('host:moreOpsTab.device.setAdid')) }
    finally { setLoading(false) }
  }

  const handleAdbRoot = async () => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidADBRoot(getDeviceId(a, mode), a.name, adbRoot), adbRoot > 0 ? t('host:moreOpsTab.device.enableAdbRoot') : t('host:moreOpsTab.device.disableAdbRoot')) }
    finally { setLoading(false) }
  }

  const handleBackgroundApp = async () => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidBackgroundApp(getDeviceId(a, mode), a.name, bgCmd, bgPkg), bgCmd === 1 ? t('host:moreOpsTab.device.enableKeepAlive') : t('host:moreOpsTab.device.disableKeepAlive')) }
    finally { setLoading(false) }
  }

  const handleRefreshLocation = async () => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.RefreshAndroidLocation(getDeviceId(a, mode), a.name), t('host:moreOpsTab.device.refreshLocation')) }
    finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      <OpBlock label={t('host:moreOpsTab.device.languageCountry')} hint="cmd=13">
        <div className="flex gap-2">
          <Select value={language} onChange={setLanguage} options={[
            { value: 'zh-CN', label: t('host:moreOpsTab.device.langZhCN') },
            { value: 'en-US', label: 'English (en-US)' },
            { value: 'ja-JP', label: '日本語 (ja-JP)' },
            { value: 'ko-KR', label: '한국어 (ko-KR)' },
          ]} className="flex-1" />
          <Select value={country} onChange={setCountry} options={[
            { value: 'CN', label: 'CN' },
            { value: 'US', label: 'US' },
            { value: 'JP', label: 'JP' },
            { value: 'KR', label: 'KR' },
          ]} className="flex-1" />
          <button onClick={handleSetLanguage} disabled={loading} className={btnCls}>{t('host:moreOpsTab.device.apply')}</button>
        </div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.device.googleAdid')} hint="/adid">
        <div className="flex gap-2">
          <input type="text" value={adid} onChange={e => setAdid(e.target.value)} placeholder={t('host:moreOpsTab.device.adidPlaceholder')} className={`${inputCls} flex-1`} />
          <button onClick={() => handleSetADID(1)} disabled={loading} className="px-3 py-1.5 text-xs bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200 disabled:opacity-50">{t('host:moreOpsTab.device.generate')}</button>
          <button onClick={() => handleSetADID(2)} disabled={loading || !adid} className={btnCls}>{t('host:moreOpsTab.device.set')}</button>
        </div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.device.adbRoot')} hint="/adb">
        <div className="flex gap-2">
          <Select value={String(adbRoot)} onChange={v => setAdbRoot(Number(v))} options={[
            { value: '1', label: t('host:moreOpsTab.device.enable') },
            { value: '0', label: t('host:moreOpsTab.device.disable') },
          ]} className="flex-1" />
          <button onClick={handleAdbRoot} disabled={loading} className={btnCls}>{t('host:moreOpsTab.device.apply')}</button>
        </div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.device.backgroundKeepAlive')} hint="/background">
        <div className="flex gap-2">
          <Select value={String(bgCmd)} onChange={v => setBgCmd(Number(v))} options={[
            { value: '1', label: t('host:moreOpsTab.device.enable') },
            { value: '0', label: t('host:moreOpsTab.device.disable') },
          ]} className="flex-1" />
          <input type="text" value={bgPkg} onChange={e => setBgPkg(e.target.value)} placeholder={t('host:moreOpsTab.device.appPackage')} className={`${inputCls} flex-1`} />
          <button onClick={handleBackgroundApp} disabled={loading} className={btnCls}>{t('host:moreOpsTab.device.apply')}</button>
        </div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.device.refreshLocation')} hint="/task">
        <div className="flex justify-end">
          <button onClick={handleRefreshLocation} disabled={loading} className={btnCls}>{t('host:moreOpsTab.device.refreshLocation')}</button>
        </div>
      </OpBlock>
    </div>
  )
}
