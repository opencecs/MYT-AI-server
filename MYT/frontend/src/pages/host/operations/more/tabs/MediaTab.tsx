import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SdkApiService, OpBlock, inputCls, btnCls, showToast, runBatch, AndroidInfo, MirrorItem } from '../shared'
import { useDeployMode } from '../../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../../utils/deviceId'
import Select from '../../../../../components/Select'

interface Props {
  androids: AndroidInfo[]
  mirrors: MirrorItem[]
}

export default function MediaTab({ androids, mirrors }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [rotation, setRotation] = useState(1)
  const [cameraFace, setCameraFace] = useState(false)
  const [audioType, setAudioType] = useState('media')
  const [audioSource, setAudioSource] = useState('/sdcard/music.mp3')
  const [audioState, setAudioState] = useState('play')
  const [domainHost, setDomainHost] = useState('')
  const [loading, setLoading] = useState(false)

  const handleRotate = async () => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.RotateAndroidCamera(getDeviceId(a, mode), a.name, rotation, cameraFace), t('host:moreOpsTab.media.rotateCameraLabel')) }
    finally { setLoading(false) }
  }

  const handleAudio = async () => {
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidAudio(getDeviceId(a, mode), a.name, audioType, audioSource, audioState), t('host:moreOpsTab.media.setAudio')) }
    finally { setLoading(false) }
  }

  const handleDomain = async () => {
    if (!domainHost) { showToast(t('host:moreOpsTab.media.fillDomain'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.SetAndroidGlobalDomain(getDeviceId(a, mode), a.name, domainHost), t('host:moreOpsTab.media.setDomain')) }
    finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      <OpBlock label={t('host:moreOpsTab.media.rotateCamera')} hint="cmd=22">
        <div className="flex gap-2">
          <Select value={String(rotation)} onChange={v => setRotation(Number(v))} options={[
            { value: '0', label: '0°' },
            { value: '1', label: '90°' },
            { value: '2', label: '180°' },
            { value: '3', label: '270°' },
          ]} className="flex-1" />
          <Select value={String(cameraFace ? 1 : 0)} onChange={v => setCameraFace(v === '1')} options={[
            { value: '0', label: t('host:moreOpsTab.media.rear') },
            { value: '1', label: t('host:moreOpsTab.media.front') },
          ]} className="flex-1" />
          <button onClick={handleRotate} disabled={loading} className={btnCls}>{t('host:moreOpsTab.media.rotate')}</button>
        </div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.media.audioResource')} hint="cmd=14">
        <div className="flex gap-2">
          <Select value={audioType} onChange={setAudioType} options={[
            { value: 'media', label: 'media' },
            { value: 'webrtc', label: 'webrtc' },
            { value: 'rtmp', label: 'rtmp' },
            { value: 'camera', label: 'camera' },
          ]} className="flex-1" />
          <Select value={audioState} onChange={setAudioState} options={[
            { value: 'play', label: t('host:moreOpsTab.media.play') },
            { value: 'stop', label: t('host:moreOpsTab.media.stop') },
          ]} className="flex-1" />
        </div>
        <div className="flex gap-2">
          <input type="text" value={audioSource} onChange={e => setAudioSource(e.target.value)} placeholder={t('host:moreOpsTab.media.audioPath')} className={`${inputCls} flex-1`} />
          <button onClick={handleAudio} disabled={loading} className={btnCls}>{t('host:moreOpsTab.media.set')}</button>
        </div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.media.globalDomain')} hint="sethost">
        <div className="flex gap-2">
          <input type="text" value={domainHost} onChange={e => setDomainHost(e.target.value)} placeholder={t('host:moreOpsTab.media.domainPlaceholder')} className={`${inputCls} flex-1`} />
          <button onClick={handleDomain} disabled={loading || !domainHost} className={btnCls}>{t('host:moreOpsTab.media.set')}</button>
        </div>
      </OpBlock>
    </div>
  )
}
