// 添加 P2P 流弹窗：设备 + 云机 + 监听端口（±步进）+ 备注 + 分辨率 + 监听地址展示。
// 确认即完整链路：modifydev cmd=4 type=camera → StartP2P → 写入分发记录（pending，稍后自动校验）。
import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { showToast } from '../../../components/Toast'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { androidDisplayName } from '../androidName'
import type { DeviceState } from '../../../types/index'
import type { useStreamManager } from '../useStreamManager'
import type { Distribution } from '../streamTypes'
import { addAndStartP2P, stopP2PDistribution, resolveCameraPort, distErrorText } from './distributionActions'

type Mgr = ReturnType<typeof useStreamManager>

interface Props {
  mgr: Mgr
  devices: DeviceState[]
  onClose: () => void
}

export default function P2PAddDialog({ mgr, devices, onClose }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const [deviceIp, setDeviceIp] = useState('')
  const [cloudMachineId, setCloudMachineId] = useState('')
  const [listenPort, setListenPort] = useState('9000')
  const [streamName, setStreamName] = useState('')
  const [resolution, setResolution] = useState('1')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    mgr.refreshAndroids()
    // 打开时自动选下一个可用端口（pc-android getNextAvailableP2PPort(9000) 同构）
    setListenPort(String(nextAvailablePort(9000)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const p2pStreams = useMemo(
    () => mgr.distributions.filter(d => d.protocol === 'p2p'),
    [mgr.distributions],
  )
  const usedPorts = useMemo(() => {
    const ports = new Set<number>()
    p2pStreams.forEach(d => { if (d.p2pListenPort && d.status !== 'inactive') ports.add(d.p2pListenPort) })
    return ports
  }, [p2pStreams])

  const availableMachines = useMemo(
    () => mgr.androids.filter(a => a.ip === deviceIp && a.status === 'running'),
    [mgr.androids, deviceIp],
  )

  const portNum = Number(listenPort)
  const conflict = !!portNum && usedPorts.has(portNum)
  const listenUrl = mgr.localIP && portNum ? `srt://${mgr.localIP}:${portNum}` : ''

  const nextAvailablePort = (start: number) => {
    let p = start || 9000
    while (usedPorts.has(p)) p += 1
    return p
  }
  const increasePort = () => setListenPort(String(nextAvailablePort((Number(listenPort) || 9000) + 1)))
  const decreasePort = () => {
    let p = (Number(listenPort) || 9000) - 1
    if (p < 1) p = 1
    while (p > 0 && usedPorts.has(p)) p -= 1
    if (p <= 0) p = nextAvailablePort(9000)
    setListenPort(String(p))
  }

  const handleConfirm = async () => {
    if (!deviceIp) { showToast(t('stream:dist.deviceRequired'), 'error'); return }
    if (!cloudMachineId) { showToast(t('stream:dist.machineRequired'), 'error'); return }
    if (!portNum || portNum <= 0) { showToast(t('stream:p2p.invalidPort'), 'error'); return }
    if (conflict) { showToast(t('stream:p2p.portConflict'), 'error'); return }
    const machine = availableMachines.find(m => m.id === cloudMachineId || m.name === cloudMachineId)
    if (!machine) { showToast(t('stream:dist.machineNotFound'), 'error'); return }

    const name = streamName.trim() || `p2p-${portNum}`
    setLoading(true)
    try {
      // 同设备+云机已有 P2P 记录 → 先停（pc-android confirmP2PDistribute 同构）
      const existing = p2pStreams.find(d => d.deviceIp === deviceIp && d.cloudMachineId === cloudMachineId)
      if (existing) {
        try {
          await stopP2PDistribution({
            deviceIp, machine, streamName: existing.streamName, mappedPort: existing.mappedPort,
          })
        } catch { /* 忽略旧会话停止失败 */ }
      }
      const effectiveListenUrl = await addAndStartP2P({
        localIP: await mgr.localIpForDevice(deviceIp), deviceIp, machine, streamName: name, listenPort: portNum, resolution,
      })
      const record: Distribution = {
        id: `dist-${Date.now()}-${Math.random().toString(16).slice(2)}`,
        streamName: name,
        deviceIp,
        cloudMachineId,
        cloudMachineName: machine.name || machine.id,
        protocol: 'p2p',
        mappedPort: resolveCameraPort(machine) ?? undefined,
        p2pListenPort: portNum,
        resolution,
        status: 'pending',
        statusReason: `srtListen:${effectiveListenUrl}`,
      }
      // 复用已有记录 ID（同设备+云机覆盖更新）
      const reuseId = existing?.id
      if (reuseId) record.id = reuseId
      mgr.setDistributions(prev => reuseId
        ? prev.map(d => d.id === reuseId ? record : d)
        : [record, ...prev])
      showToast(t('stream:p2p.addSuccess'), 'success')
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
        <h3 className="text-sm font-semibold mb-3">{t('stream:p2p.addTitle')}</h3>
        <div className="space-y-3">
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
            <label className="text-xs text-gray-500 block mb-1">{t('stream:p2p.portLabel')}</label>
            <div className="flex gap-1.5 items-center">
              <input type="number" value={listenPort} onChange={e => setListenPort(e.target.value)}
                className="flex-1 text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50" />
              <button onClick={decreasePort} className="px-2.5 py-1.5 text-xs border border-gray-200 rounded-lg hover:bg-gray-50">-</button>
              <button onClick={increasePort} className="px-2.5 py-1.5 text-xs border border-gray-200 rounded-lg hover:bg-gray-50">+</button>
            </div>
            {conflict && <p className="text-[10px] text-amber-500 mt-1">{t('stream:p2p.portInUse')}</p>}
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:p2p.noteLabel')}</label>
            <input value={streamName} onChange={e => setStreamName(e.target.value)}
              placeholder={t('stream:p2p.notePlaceholder')}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5" />
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:p2p.resolution')}</label>
            <select value={resolution} onChange={e => setResolution(e.target.value)}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5">
              <option value="1">{t('stream:forward.auto')}</option>
              <option value="2">1920x1080@30</option>
              <option value="3">1280x720@30</option>
            </select>
          </div>
          <div className="text-xs text-gray-500">
            {listenUrl
              ? t('stream:p2p.listenAddrLabel', { url: listenUrl })
              : t('stream:p2p.listenAddrEmpty')}
          </div>
        </div>
        <div className="flex justify-end gap-2 mt-4">
          <button onClick={onClose} disabled={loading}
            className="px-3 py-1.5 text-xs border border-gray-200 rounded-lg">{t('button.cancel')}</button>
          <button onClick={handleConfirm} disabled={loading}
            className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-40">
            {loading ? t('stream:dist.querying') : t('stream:p2p.startBtn')}
          </button>
        </div>
      </div>
    </div>
  )
}
