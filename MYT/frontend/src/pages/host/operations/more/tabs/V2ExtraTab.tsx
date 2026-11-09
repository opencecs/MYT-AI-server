import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SdkApiService, OpBlock, btnCls, runBatch, AndroidInfo, MirrorItem } from '../shared'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../../utils/deviceId'
import Select from '../../../../../components/Select'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
}

export default function V2ExtraTab({ androids, mirrors }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [disableKey, setDisableKey] = useState(0)
  const [loading, setLoading] = useState(false)

  const handleDisableKey = async () => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidDisableKey(getDeviceId(a, mode), a.name, disableKey), t('host:moreOpsTab.v2extra.blockKeysLabel')) }
    finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      <OpBlock label={t('host:moreOpsTab.v2extra.blockKeys')} hint="/disablekey">
        <div className="flex gap-2">
          <Select value={String(disableKey)} onChange={v => setDisableKey(Number(v))} options={[
            { value: '0', label: t('host:moreOpsTab.v2extra.disableKeyOff') },
            { value: '1', label: t('host:moreOpsTab.v2extra.blockBack') },
            { value: '2', label: t('host:moreOpsTab.v2extra.blockHome') },
            { value: '3', label: t('host:moreOpsTab.v2extra.blockBackHome') },
          ]} className="flex-1" />
          <button onClick={handleDisableKey} disabled={loading} className={btnCls}>{t('host:moreOpsTab.v2extra.apply')}</button>
        </div>
      </OpBlock>
    </div>
  )
}
