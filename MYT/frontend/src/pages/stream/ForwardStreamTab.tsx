import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { showToast } from '../../components/Toast'
import ConfirmDialog from '../../components/ConfirmDialog'
import type { useStreamManager } from './useStreamManager'
import type { StreamInfo } from './streamTypes'

type Mgr = ReturnType<typeof useStreamManager>

interface Props {
  mgr: Mgr
  onDistribute: (streamName: string) => void
}

export default function ForwardStreamTab({ mgr, onDistribute }: Props) {
  const { t } = useTranslation()
  const [confirm, setConfirm] = useState<{ type: 'stop' | 'delete'; stream: StreamInfo } | null>(null)

  const copyText = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      showToast(t('stream:forward.copied'), 'success')
    }).catch(() => {})
  }

  const handleAdd = () => mgr.addStream()
  const handleDelete = (s: StreamInfo) => mgr.deleteStream(s.streamName)
  const handleStop = (s: StreamInfo) => mgr.stopPush(s.streamName)

  const handleConfirmed = async () => {
    if (!confirm) return
    const { type, stream } = confirm
    setConfirm(null)
    if (type === 'stop') {
      const res = await handleStop(stream) as any
      if (res?.success) {
        showToast(t('stream:forward.disconnected'), 'success')
        mgr.refreshStreams()
      } else {
        showToast(res?.message || t('stream:dist.stopFailed'), 'error')
      }
    } else {
      await handleDelete(stream)
      showToast(t('stream:forward.deleted'), 'success')
      mgr.refreshStreams()
    }
  }

  return (
    <div className="flex h-full flex-col gap-3">
      <div className="flex items-center gap-2">
        <input value={mgr.newStreamName} onChange={e => mgr.setNewStreamName(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleAdd()}
          placeholder={t('stream:forward.roomPlaceholder')} className="px-3 py-1.5 text-xs border border-gray-200 rounded-lg w-40" />
        <button onClick={handleAdd} className="px-3 py-1.5 text-xs bg-green-500 text-white rounded-lg hover:bg-green-600">{t('stream:forward.newStream')}</button>
        <button onClick={mgr.refreshStreams} className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600">{t('button.refresh')}</button>
      </div>
      <div className="px-3 py-2 bg-gray-50 rounded-lg text-xs text-gray-600 space-y-1">
        <div>{t('stream:forward.webrtcAddr')}: <span className="text-blue-500 cursor-pointer underline" onClick={() => copyText(`webrtc://${mgr.localIP}/live`)}>webrtc://{mgr.localIP}/live</span></div>
        <div>{t('stream:forward.rtmpAddr')}: <span className="text-blue-500 cursor-pointer underline" onClick={() => copyText(`rtmp://${mgr.localIP}:1935/live`)}>rtmp://{mgr.localIP}:1935/live</span></div>
      </div>
      <div className="flex-1 overflow-auto">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-gray-50">
            <tr>
              <th className="px-3 py-2 text-left">{t('stream:forward.col.room')}</th>
              <th className="px-3 py-2 text-left">{t('stream:forward.col.status')}</th>
              <th className="px-3 py-2 text-left">{t('stream:forward.col.sourceIP')}</th>
              <th className="px-3 py-2 text-left">{t('stream:forward.col.bitrate')}</th>
              <th className="px-3 py-2 text-left">{t('stream:forward.col.action')}</th>
            </tr>
          </thead>
          <tbody>
            {mgr.activeStreams.length === 0 && (
              <tr><td colSpan={5} className="text-center text-gray-400 py-5">{t('stream:forward.noStreams')}</td></tr>
            )}
            {mgr.activeStreams.map((s, i) => (
              <tr key={i} className="border-b border-gray-50 hover:bg-gray-50">
                <td className="px-3 py-2 font-medium">{s.streamName}</td>
                <td className="px-3 py-2">
                  <span className={`px-2 py-0.5 rounded text-[10px] ${s.publisherIP ? 'bg-green-50 text-green-600' : 'bg-gray-100 text-gray-400'}`}>
                    {s.publisherIP ? t('stream:forward.active') : t('stream:forward.idle')}
                  </span>
                </td>
                <td className="px-3 py-2">{s.publisherIP || '-'}</td>
                <td className="px-3 py-2">{s.publisherIP ? `${(s.videoBitrate / 1024).toFixed(1)} / ${(s.audioBitrate / 1024).toFixed(1)} kbps` : '-'}</td>
                <td className="px-3 py-2">
                  <div className="flex gap-1.5">
                    <button disabled={!s.publisherIP} onClick={() => onDistribute(s.streamName)}
                      className="px-2 py-1 text-[10px] bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-40">{t('stream:forward.distribute')}</button>
                    <button disabled={!s.publisherIP} onClick={() => setConfirm({ type: 'stop', stream: s })}
                      className="px-2 py-1 text-[10px] bg-amber-500 text-white rounded hover:bg-amber-600 disabled:opacity-40">{t('stream:forward.disconnect')}</button>
                    <button onClick={() => setConfirm({ type: 'delete', stream: s })}
                      className="px-2 py-1 text-[10px] bg-red-500 text-white rounded hover:bg-red-600">{t('button.delete')}</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {confirm && (
        <ConfirmDialog
          title={confirm.type === 'stop' ? t('stream:forward.stopPushTitle') : t('stream:forward.deleteStreamTitle')}
          message={confirm.type === 'stop'
            ? t('stream:forward.stopPushConfirm')
            : t('stream:forward.deleteStreamConfirm', { name: confirm.stream.streamName })}
          onConfirm={handleConfirmed}
          onCancel={() => setConfirm(null)} />
      )}
    </div>
  )
}
