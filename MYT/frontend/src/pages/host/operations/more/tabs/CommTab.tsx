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

export default function CommTab({ androids, mirrors }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [smsAddress, setSmsAddress] = useState('10086')
  const [smsBody, setSmsBody] = useState('')
  const [callNumber, setCallNumber] = useState('')
  const [callType, setCallType] = useState(1)
  const [callDuration, setCallDuration] = useState(0)
  const [contactName, setContactName] = useState('')
  const [contactPhone, setContactPhone] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSendSMS = async () => {
    if (!smsAddress || !smsBody) { showToast(t('host:moreOpsTab.comm.fillNumberAndContent'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.ReceiveAndroidSMS(getDeviceId(a, mode), a.name, smsAddress, smsBody), t('host:moreOpsTab.comm.sendSms')) }
    finally { setLoading(false) }
  }

  const handleAddCallLog = async () => {
    if (!callNumber) { showToast(t('host:moreOpsTab.comm.fillNumber'), 'error'); return }
    setLoading(true)
    try { await runBatch(androids, mirrors, a => SdkApiService.AddAndroidCallLog(getDeviceId(a, mode), a.name, callNumber, callType, callDuration), t('host:moreOpsTab.comm.addCallLog')) }
    finally { setLoading(false) }
  }

  const handleAddContact = async () => {
    if (!contactName || !contactPhone) { showToast(t('host:moreOpsTab.comm.fillNameAndPhone'), 'error'); return }
    setLoading(true)
    try {
      const data = JSON.stringify([{ name: contactName, phone: contactPhone }])
      await runBatch(androids, mirrors, a => SdkApiService.AddAndroidContact(getDeviceId(a, mode), a.name, data), t('host:moreOpsTab.comm.addContact'))
      setContactName(''); setContactPhone('')
    } finally { setLoading(false) }
  }

  return (
    <div className="space-y-3">
      <OpBlock label={t('host:moreOpsTab.comm.simulateSms')} hint="/sms">
        <input type="text" value={smsAddress} onChange={e => setSmsAddress(e.target.value)} placeholder={t('host:moreOpsTab.comm.senderNumber')} className={inputCls} />
        <textarea value={smsBody} onChange={e => setSmsBody(e.target.value)} placeholder={t('host:moreOpsTab.comm.smsContent')} className={`${inputCls} h-16 resize-none`} />
        <div className="flex justify-end"><button onClick={handleSendSMS} disabled={loading} className={btnCls}>{t('host:moreOpsTab.comm.sendSms')}</button></div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.comm.addCallLog')} hint="/callog">
        <input type="text" value={callNumber} onChange={e => setCallNumber(e.target.value)} placeholder={t('host:moreOpsTab.comm.number')} className={inputCls} />
        <div className="flex gap-2">
          <Select value={String(callType)} onChange={v => setCallType(Number(v))} options={[
            { value: '1', label: t('host:moreOpsTab.comm.callIncoming') },
            { value: '2', label: t('host:moreOpsTab.comm.callOutgoing') },
            { value: '3', label: t('host:moreOpsTab.comm.callMissed') },
          ]} className="flex-1" />
          <input type="number" min={0} value={callDuration} onChange={e => setCallDuration(Number(e.target.value))} placeholder={t('host:moreOpsTab.comm.durationSec')} className={`${inputCls} flex-1`} />
        </div>
        <div className="flex justify-end"><button onClick={handleAddCallLog} disabled={loading} className={btnCls}>{t('host:moreOpsTab.comm.addCall')}</button></div>
      </OpBlock>

      <OpBlock label={t('host:moreOpsTab.comm.addContact')} hint="/addcontact">
        <div className="flex gap-2">
          <input type="text" value={contactName} onChange={e => setContactName(e.target.value)} placeholder={t('host:moreOpsTab.comm.name')} className={`${inputCls} flex-1`} />
          <input type="text" value={contactPhone} onChange={e => setContactPhone(e.target.value)} placeholder={t('host:moreOpsTab.comm.phone')} className={`${inputCls} flex-1`} />
        </div>
        <div className="flex justify-end"><button onClick={handleAddContact} disabled={loading || !contactName || !contactPhone} className={btnCls}>{t('host:moreOpsTab.comm.addContact')}</button></div>
      </OpBlock>
    </div>
  )
}
