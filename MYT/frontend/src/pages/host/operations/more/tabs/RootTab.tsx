import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SdkApiService, OpBlock, inputCls, btnCls, showToast, getErrorMessage, isContainerImage, runBatch, AndroidInfo, MirrorItem } from '../shared'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../../utils/deviceId'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
}

export default function RootTab({ androids, mirrors }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const primary = androids.find(a => isContainerImage(a.image, mirrors)) ?? androids[0]
  const isBatch = androids.length > 1
  const [apps, setApps] = useState<string[]>([])
  const [pkg, setPkg] = useState('')
  const [loading, setLoading] = useState(false)

  const fetchList = async () => {
    setLoading(true)
    try {
      const data = await SdkApiService.GetAndroidRootList(getDeviceId(primary, mode), primary.name)
      const appList = (data?.data as { apps?: string[] })?.apps ?? []
      setApps(appList)
      if (isBatch) showToast(t('host:moreOpsTab.root.gotRootList', { name: primary.name }), 'success')
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally { setLoading(false) }
  }

  const handleGrant = async () => {
    if (!pkg) { showToast(t('host:moreOpsTab.root.fillPackage'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.GrantAndroidRoot(getDeviceId(a, mode), a.name, pkg), t('host:moreOpsTab.root.grantForPkg', { pkg })) }
    finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      {isBatch && (
        <div className="text-xs text-gray-500 bg-gray-50 px-3 py-2 rounded">
          {t('host:moreOpsTab.batchListOnlyRoot', { name: primary.name })}
        </div>
      )}
      <OpBlock label={t('host:moreOpsTab.root.rootList')} hint="cmd=10">
        <div className="flex gap-2">
          <button onClick={fetchList} disabled={loading} className={btnCls}>{t('host:moreOpsTab.root.getList')}</button>
        </div>
        {apps.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {apps.map(a => (
              <span key={a} className="text-xs bg-gray-50 text-gray-700 px-2.5 py-1 rounded-md border border-gray-200">{a}</span>
            ))}
          </div>
        )}
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.root.grantRoot')} hint="cmd=10">
        <div className="flex gap-2">
          <input type="text" value={pkg} onChange={e => setPkg(e.target.value)} placeholder="com.example.app" className={`${inputCls} flex-1`} />
          <button onClick={handleGrant} disabled={loading || !pkg} className={btnCls}>{t('host:moreOpsTab.root.grant')}</button>
        </div>
      </OpBlock>
    </div>
  )
}
