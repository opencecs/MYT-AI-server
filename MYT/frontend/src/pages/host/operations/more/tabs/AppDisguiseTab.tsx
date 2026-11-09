import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SdkApiService, OpBlock, inputCls, btnCls, showToast, runBatch, AndroidInfo, MirrorItem } from '../shared'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../../utils/deviceId'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
}

export default function AppDisguiseTab({ androids, mirrors }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [hidePkgs, setHidePkgs] = useState('')
  const [disguiseJSON, setDisguiseJSON] = useState('[{"pkg":"com.example.app","label":"TestApp","versionCode":1,"versionName":"1.0"}]')
  const [loading, setLoading] = useState(false)

  const handleHide = async () => {
    const pkgs = hidePkgs.split(/[,\s]+/).filter(Boolean)
    if (pkgs.length === 0) { showToast(t('host:moreOpsTab.disguise.fillPackage'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.HideAndroidApp(getDeviceId(a, mode), a.name, pkgs), t('host:moreOpsTab.disguise.hideAppLabel')) }
    finally { setLoading(false) }
  }

  const handleDisguise = async () => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.DisguiseAndroidApp(getDeviceId(a, mode), a.name, disguiseJSON), t('host:moreOpsTab.disguise.disguiseAppLabel')) }
    finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      <OpBlock label={t('host:moreOpsTab.disguise.hideApp')} hint="cmd=3">
        <input type="text" value={hidePkgs} onChange={e => setHidePkgs(e.target.value)} placeholder={t('host:moreOpsTab.disguise.pkgPlaceholder')} className={inputCls} />
        <div className="flex justify-end"><button onClick={handleHide} disabled={loading || !hidePkgs} className={btnCls}>{t('host:moreOpsTab.disguise.hide')}</button></div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.disguise.disguiseApp')} hint="cmd=24">
        <textarea value={disguiseJSON} onChange={e => setDisguiseJSON(e.target.value)} className={`${inputCls} h-20 resize-none font-mono`} />
        <div className="flex justify-end"><button onClick={handleDisguise} disabled={loading} className={btnCls}>{t('host:moreOpsTab.disguise.execute')}</button></div>
      </OpBlock>
    </div>
  )
}
