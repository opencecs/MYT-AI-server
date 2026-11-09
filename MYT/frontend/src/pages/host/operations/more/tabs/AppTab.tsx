import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SdkApiService, OpBlock, inputCls, btnCls, btnSecondaryCls, preCls, showToast, getErrorMessage, isContainerImage, runBatch, AndroidInfo, MirrorItem } from '../shared'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../../utils/deviceId'
import Select from '../../../../../components/Select'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
  allContainer: boolean
}

export default function AppTab({ androids, mirrors, allContainer }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const primary = androids.find(a => isContainerImage(a.image, mirrors)) ?? androids[0]
  const isBatch = androids.length > 1
  const [pkg, setPkg] = useState('')
  const [saveTo, setSaveTo] = useState('/sdcard/backup')
  const [backupPath, setBackupPath] = useState('')
  const [bootList, setBootList] = useState<Record<string, unknown> | null>(null)
  const [bootPkgs, setBootPkgs] = useState('')
  const [moduleCmd, setModuleCmd] = useState('list')
  const [moduleName, setModuleName] = useState('magisk')
  const [moduleResult, setModuleResult] = useState<Record<string, unknown> | null>(null)
  const [loading, setLoading] = useState(false)

  const handleBackup = async () => {
    if (!pkg) { showToast(t('host:moreOpsTab.app.fillPackage'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.BackupAndroidApp(getDeviceId(a, mode), a.name, pkg, saveTo), t('host:moreOpsTab.app.appBackup')) }
    finally { setLoading(false) }
  }

  const handleRestore = async () => {
    if (!backupPath) { showToast(t('host:moreOpsTab.app.fillBackupPath'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.RestoreAndroidApp(getDeviceId(a, mode), a.name, backupPath), t('host:moreOpsTab.app.appRestore')) }
    finally { setLoading(false) }
  }

  const fetchBootList = async () => {
    setLoading(true)
    try {
      const data = await SdkApiService.GetAndroidBootStartList(getDeviceId(primary, mode), primary.name)
      setBootList(data)
      if (isBatch) showToast(t('host:moreOpsTab.app.gotBootList', { name: primary.name }), 'success')
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally { setLoading(false) }
  }

  const handleSetBootStart = async () => {
    const pkgs = bootPkgs.split(/[,\s]+/).filter(Boolean)
    if (pkgs.length === 0) { showToast(t('host:moreOpsTab.app.fillPackage'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidBootStart(getDeviceId(a, mode), a.name, pkgs), t('host:moreOpsTab.app.setBootStart')) }
    finally { setLoading(false) }
  }

  const handleModule = async () => {
    setLoading(true)
    try {
      if (moduleCmd === 'list') {
        const data = await SdkApiService.AndroidModuleMgr(getDeviceId(primary, mode), primary.name, moduleCmd, moduleName)
        setModuleResult(data)
        showToast(t('host:moreOpsTab.app.gotModuleList', { name: primary.name }), 'success')
      } else {
        await runBatch(androids, mirrors, a => SdkApiService.AndroidModuleMgr(getDeviceId(a, mode), a.name, moduleCmd, moduleName), `${moduleCmd} ${moduleName}`)
        const data = await SdkApiService.AndroidModuleMgr(getDeviceId(primary, mode), primary.name, 'list', moduleName)
        setModuleResult(data)
      }
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      {isBatch && (
        <div className="text-xs text-gray-500 bg-gray-50 px-3 py-2 rounded">
          {t('host:moreOpsTab.batchListOnly', { name: primary.name })}
        </div>
      )}
      <OpBlock label={t('host:moreOpsTab.app.appBackup')} hint="/backrestore?cmd=backup">
        <input type="text" value={pkg} onChange={e => setPkg(e.target.value)} placeholder={t('host:moreOpsTab.app.appPackage')} className={inputCls} />
        <input type="text" value={saveTo} onChange={e => setSaveTo(e.target.value)} placeholder={t('host:moreOpsTab.app.savePath')} className={inputCls} />
        <div className="flex justify-end"><button onClick={handleBackup} disabled={loading || !pkg} className={btnCls}>{t('host:moreOpsTab.app.backup')}</button></div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.app.appRestore')} hint="/backrestore?cmd=recovery">
        <input type="text" value={backupPath} onChange={e => setBackupPath(e.target.value)} placeholder={t('host:moreOpsTab.app.backupFilePath')} className={inputCls} />
        <div className="flex justify-end"><button onClick={handleRestore} disabled={loading || !backupPath} className={btnCls}>{t('host:moreOpsTab.app.restore')}</button></div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.app.bootStart')} hint="/appbootstart">
        <div className="flex gap-2">
          <button onClick={fetchBootList} disabled={loading} className={btnSecondaryCls}>{t('host:moreOpsTab.app.getList')}</button>
        </div>
        {bootList && <pre className={`${preCls} h-24`}>{JSON.stringify(bootList, null, 2)}</pre>}
        <div className="flex gap-2">
          <input type="text" value={bootPkgs} onChange={e => setBootPkgs(e.target.value)} placeholder={t('host:moreOpsTab.app.pkgListPlaceholder')} className={`${inputCls} flex-1`} />
          <button onClick={handleSetBootStart} disabled={loading || !bootPkgs} className={btnCls}>{t('host:moreOpsTab.app.set')}</button>
        </div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.app.moduleMgr')} hint="/modulemgr">
        <div className="flex gap-2">
          <Select value={moduleCmd} onChange={setModuleCmd} options={[
            { value: 'list', label: t('host:moreOpsTab.app.list') },
            { value: 'install', label: t('host:moreOpsTab.app.install') },
            { value: 'uninstall', label: t('host:moreOpsTab.app.uninstall') },
          ]} className="flex-1" />
          <Select value={moduleName} onChange={setModuleName} options={[
            { value: 'magisk', label: 'magisk' },
            { value: 'gms', label: 'gms' },
          ]} className="flex-1" />
          <button onClick={handleModule} disabled={loading} className={btnCls}>{t('host:moreOpsTab.app.execute')}</button>
        </div>
        {moduleResult && <pre className={`${preCls} h-24`}>{JSON.stringify(moduleResult, null, 2)}</pre>}
      </OpBlock>
    </div>
  )
}
