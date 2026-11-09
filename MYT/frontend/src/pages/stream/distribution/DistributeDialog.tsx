// 分发流到云机弹窗：流 + 设备 + 云机 + 协议 + 分辨率。
// 新建分发与「修改流」共用（initial.record 预填），确认走完整 modifydev 链。
import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { showToast } from '../../../components/Toast'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { androidDisplayName } from '../androidName'
import type { DeviceState } from '../../../types/index'
import type { useStreamManager } from '../useStreamManager'
import type { Distribution } from '../streamTypes'
import {
  applyDistribution, stopP2PDistribution, resolveMytosPort, distErrorText,
} from './distributionActions'

type Mgr = ReturnType<typeof useStreamManager>

interface Props {
  mgr: Mgr
  devices: DeviceState[]
  initial: { streamName?: string; record?: Distribution }
  onClose: () => void
}

export default function DistributeDialog({ mgr, devices, initial, onClose }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const editing = !!initial.record
  const [streamName, setStreamName] = useState(initial.record?.streamName ?? initial.streamName ?? '')
  const [deviceIp, setDeviceIp] = useState(initial.record?.deviceIp ?? '')
  const [cloudMachineId, setCloudMachineId] = useState(initial.record?.cloudMachineId ?? '')
  const [protocol, setProtocol] = useState(initial.record?.protocol || 'httpflv')
  const [resolution, setResolution] = useState(initial.record?.resolution || '1')
  const [loading, setLoading] = useState(false)

  useEffect(() => { mgr.refreshAndroids() }, [mgr])

  const availableMachines = useMemo(
    () => mgr.androids.filter(a => a.ip === deviceIp && a.status === 'running'),
    [mgr.androids, deviceIp],
  )

  const streamOptions = useMemo(
    () => mgr.activeStreams.map(s => s.streamName).filter(Boolean),
    [mgr.activeStreams],
  )

  const handleConfirm = async () => {
    if (!streamName.trim()) { showToast(t('stream:dist.streamRequired'), 'error'); return }
    if (!deviceIp) { showToast(t('stream:dist.deviceRequired'), 'error'); return }
    if (!cloudMachineId) { showToast(t('stream:dist.machineRequired'), 'error'); return }
    const machine = availableMachines.find(m => m.id === cloudMachineId || m.name === cloudMachineId)
    if (!machine) { showToast(t('stream:dist.machineNotFound'), 'error'); return }
    const mytosPort = resolveMytosPort(machine)
    if (!mytosPort) { showToast(t('stream:dist.portNotFound9082'), 'error'); return }

    setLoading(true)
    try {
      // 同设备+云机已有 P2P 分发 → 先停（pc-android confirmDistribute 同构）
      const existingP2P = mgr.distributions.find(d =>
        d.protocol === 'p2p' && d.deviceIp === deviceIp && d.cloudMachineId === cloudMachineId)
      if (existingP2P) {
        try {
          await stopP2PDistribution({
            deviceIp, machine, streamName: existingP2P.streamName, mappedPort: existingP2P.mappedPort,
          })
        } catch { /* 旧会话停止失败不阻断新分发 */ }
      }
      // 面向目标设备的本机源 IP（TUN 代理机器上通用 localIP 不可达 → 设备拉不到流黑屏）
      const localIP = await mgr.localIpForDevice(deviceIp)
      await applyDistribution({ localIP, deviceIp, machine, streamName: streamName.trim(), protocol, resolution })
      const record: Distribution = {
        id: initial.record?.id ?? `dist-${Date.now()}-${Math.random().toString(16).slice(2)}`,
        streamName: streamName.trim(),
        deviceIp,
        cloudMachineId,
        cloudMachineName: machine.name || machine.id,
        protocol,
        mappedPort: mytosPort,
        resolution,
        status: 'pending',
        statusReason: 'pendingCheck',
      }
      mgr.setDistributions(prev => editing
        ? prev.map(d => d.id === record.id ? record : d)
        : [record, ...prev])
      showToast(t(editing ? 'stream:dist.updated' : 'stream:dist.distributed'), 'success')
      onClose()
    } catch (e) {
      showToast(distErrorText(e, t), 'error')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
      <div className="bg-white rounded-xl shadow-lg p-5 w-96">
        <h3 className="text-sm font-semibold mb-3">{t('stream:forward.distributeTitle')}</h3>
        <div className="space-y-3">
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:forward.stream')}</label>
            <input value={streamName} onChange={e => setStreamName(e.target.value)}
              placeholder={t('stream:forward.streamPlaceholder')}
              list="dist-stream-options"
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50" />
            <datalist id="dist-stream-options">
              {streamOptions.map(s => <option key={s} value={s} />)}
            </datalist>
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:p2p.selectDevice')}</label>
            <select value={deviceIp} onChange={e => { setDeviceIp(e.target.value); setCloudMachineId('') }}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50">
              <option value="">{t('stream:p2p.selectDevice')}</option>
              {devices.map(d => (
                <option key={d.device_id} value={d.ip}>
                  {d.ip}{d.device_name ? ` (${d.device_name})` : ''}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:p2p.selectMachine')}</label>
            <select value={cloudMachineId} onChange={e => setCloudMachineId(e.target.value)} disabled={!deviceIp}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50 disabled:opacity-50">
              <option value="">{t('stream:p2p.selectMachine')}</option>
              {availableMachines.map(m => (
                <option key={m.id} value={m.id} title={m.name}>{androidDisplayName(m, notes)} (坑位 {m.indexNum})</option>
              ))}
            </select>
            {deviceIp && availableMachines.length === 0 &&
              <p className="text-[10px] text-gray-400 mt-1">{t('stream:camera.noMachines')}</p>}
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:forward.protocol')}</label>
            <select value={protocol} onChange={e => setProtocol(e.target.value)}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5">
              <option value="httpflv">HTTP-FLV</option>
              <option value="rtmp">RTMP</option>
            </select>
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:forward.resolution')}</label>
            <select value={resolution} onChange={e => setResolution(e.target.value)}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5">
              <option value="1">{t('stream:forward.auto')}</option>
              <option value="2">1920x1080@30</option>
              <option value="3">1280x720@30</option>
            </select>
          </div>
        </div>
        <div className="flex justify-end gap-2 mt-4">
          <button onClick={onClose} disabled={loading}
            className="px-3 py-1.5 text-xs border border-gray-200 rounded-lg">{t('button.cancel')}</button>
          <button onClick={handleConfirm} disabled={loading}
            className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-40">
            {loading ? t('stream:dist.querying') : t('stream:forward.confirmDistribute')}
          </button>
        </div>
      </div>
    </div>
  )
}
