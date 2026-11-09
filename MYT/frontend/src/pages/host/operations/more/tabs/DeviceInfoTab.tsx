import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SdkApiService, OpBlock, inputCls, btnCls, preCls, showToast, getErrorMessage, isContainerImage, runBatch, AndroidInfo, MirrorItem } from '../shared'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../../utils/deviceId'
import Select from '../../../../../components/Select'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
}

export default function DeviceInfoTab({ androids, mirrors }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const primary = androids.find(a => isContainerImage(a.image, mirrors)) ?? androids[0]
  const isBatch = androids.length > 1
  const [backupData, setBackupData] = useState('')
  const [restoreData, setRestoreData] = useState('')
  const [orientation, setOrientation] = useState(0)
  const [tilt, setTilt] = useState(0)
  const [shake, setShake] = useState(1)
  const [shakeDelay, setShakeDelay] = useState(0)
  const [scale, setScale] = useState('1.0')
  const [loading, setLoading] = useState(false)

  const handleBackup = async () => {
    setLoading(true)
    try {
      const data = await SdkApiService.BackupAndroidDeviceInfo(getDeviceId(primary, mode), primary.name)
      setBackupData(typeof data === 'string' ? data : JSON.stringify(data, null, 2))
      if (isBatch) showToast(t('host:moreOpsTab.devinfo.backedUpDeviceInfo', { name: primary.name }), 'success')
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally { setLoading(false) }
  }

  const handleRestore = async () => {
    if (!restoreData) { showToast(t('host:moreOpsTab.devinfo.fillDeviceInfo'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.RestoreAndroidDeviceInfo(getDeviceId(a, mode), a.name, restoreData), t('host:moreOpsTab.devinfo.restoreDeviceInfoLabel')) }
    finally { setLoading(false) }
  }

  const handleSensor = async () => {
    const scaleVal = parseFloat(scale)
    if (isNaN(scaleVal)) { showToast(t('host:moreOpsTab.devinfo.scaleInvalid'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidSensor(getDeviceId(a, mode), a.name, orientation, tilt, shake, shakeDelay, scaleVal), t('host:moreOpsTab.devinfo.setSensor')) }
    finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      {isBatch && (
        <div className="text-xs text-gray-500 bg-gray-50 px-3 py-2 rounded">
          {t('host:moreOpsTab.batchBackupOnly', { name: primary.name })}
        </div>
      )}
      <OpBlock label={t('host:moreOpsTab.devinfo.backupDeviceInfo')} hint="cmd=8">
        <div className="flex justify-end"><button onClick={handleBackup} disabled={loading} className={btnCls}>{t('host:moreOpsTab.devinfo.backup')}</button></div>
        {backupData && <pre className={`${preCls} h-24`}>{backupData}</pre>}
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.devinfo.restoreDeviceInfo')} hint="cmd=9">
        <textarea value={restoreData} onChange={e => setRestoreData(e.target.value)} placeholder={t('host:moreOpsTab.devinfo.restorePlaceholder')} className={`${inputCls} h-20 resize-none`} />
        <div className="flex justify-end"><button onClick={handleRestore} disabled={loading || !restoreData} className={btnCls}>{t('host:moreOpsTab.devinfo.restore')}</button></div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.devinfo.sensorSensitivity')} hint="cmd=17">
        <div className="flex gap-2">
          <Select value={String(orientation)} onChange={v => setOrientation(Number(v))} options={[
            { value: '0', label: t('host:moreOpsTab.devinfo.direction0') },
            { value: '90', label: t('host:moreOpsTab.devinfo.direction90') },
            { value: '180', label: t('host:moreOpsTab.devinfo.direction180') },
            { value: '270', label: t('host:moreOpsTab.devinfo.direction270') },
            { value: '-1', label: t('host:moreOpsTab.devinfo.notSet') },
          ]} className="flex-1" />
          <input type="number" min={0} max={90} value={tilt} onChange={e => setTilt(Number(e.target.value))} placeholder={t('host:moreOpsTab.devinfo.tiltPlaceholder')} className={`${inputCls} flex-1`} />
        </div>
        <div className="flex gap-2">
          <Select value={String(shake)} onChange={v => setShake(Number(v))} options={[
            { value: '0', label: t('host:moreOpsTab.devinfo.shakeOff') },
            { value: '1', label: t('host:moreOpsTab.devinfo.shakeOn') },
            { value: '2', label: t('host:moreOpsTab.devinfo.sensitivity2') },
            { value: '3', label: t('host:moreOpsTab.devinfo.sensitivity3') },
          ]} className="flex-1" />
          <input type="number" min={0} value={shakeDelay} onChange={e => setShakeDelay(Number(e.target.value))} placeholder={t('host:moreOpsTab.devinfo.delayMs')} className={`${inputCls} flex-1`} />
          <input type="text" value={scale} onChange={e => setScale(e.target.value)} placeholder={t('host:moreOpsTab.devinfo.scale')} className={`${inputCls} flex-1`} />
        </div>
        <div className="flex justify-end"><button onClick={handleSensor} disabled={loading} className={btnCls}>{t('host:moreOpsTab.devinfo.set')}</button></div>
      </OpBlock>
    </div>
  )
}
