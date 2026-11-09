// 点对点 Tab：添加弹窗（设备/云机/端口/备注/分辨率）+ 列表（含云机列）+ 行内启停完整链路。
import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { showToast } from '../../components/Toast'
import { useAndroidNotes } from '../../hooks/useNoteSync'
import { distributionDisplayName } from './androidName'
import type { DeviceState } from '../../types/index'
import type { useStreamManager } from './useStreamManager'
import type { Distribution } from './streamTypes'
import P2PAddDialog from './distribution/P2PAddDialog'
import {
  startP2PDistribution, stopP2PDistribution, findMachine, resolveCameraPort, distErrorText,
} from './distribution/distributionActions'

type Mgr = ReturnType<typeof useStreamManager>

interface Props {
  mgr: Mgr
  devices: DeviceState[]
}

export default function P2PStreamTab({ mgr, devices }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [busyId, setBusyId] = useState('')

  const p2pStreams = useMemo(
    () => mgr.distributions.filter(d => d.protocol === 'p2p'),
    [mgr.distributions],
  )

  const handleStart = async (item: Distribution) => {
    if (!item.p2pListenPort) { showToast(t('stream:p2p.missingListenPort'), 'error'); return }
    setBusyId(item.id)
    try {
      const machine = findMachine(mgr.androids, item.deviceIp, item.cloudMachineId)
      if (!machine) throw new Error('machineNotFound')
      const listenUrl = await startP2PDistribution({
        localIP: await mgr.localIpForDevice(item.deviceIp), deviceIp: item.deviceIp, machine,
        streamName: item.streamName, listenPort: item.p2pListenPort, resolution: item.resolution || '1',
      })
      mgr.setDistributions(prev => prev.map(d => d.id === item.id ? {
        ...d, status: 'active', statusReason: `srtListen:${listenUrl}`,
        mappedPort: resolveCameraPort(machine) ?? undefined,
      } : d))
    } catch (e) {
      showToast(distErrorText(e, t), 'error')
    } finally { setBusyId('') }
  }

  const handleStop = async (item: Distribution) => {
    setBusyId(item.id)
    try {
      const machine = findMachine(mgr.androids, item.deviceIp, item.cloudMachineId)
      await stopP2PDistribution({
        deviceIp: item.deviceIp, machine, streamName: item.streamName, mappedPort: item.mappedPort,
      })
      mgr.setDistributions(prev => prev.map(d => d.id === item.id
        ? { ...d, status: 'inactive', statusReason: 'stoppedReason' } : d))
    } catch (e) {
      showToast(distErrorText(e, t), 'error')
    } finally { setBusyId('') }
  }

  return (
    <div className="flex h-full flex-col gap-3">
      <div>
        <button onClick={() => setDialogOpen(true)}
          className="px-3 py-1.5 text-xs bg-amber-500 text-white rounded-lg hover:bg-amber-600">
          {t('stream:p2p.add')}
        </button>
      </div>
      <div className="flex-1 overflow-auto">
        <table className="w-full text-xs">
          <thead className="sticky top-0 bg-gray-50">
            <tr>
              <th className="px-3 py-2 text-left">{t('stream:p2p.col.streamAddr')}</th>
              <th className="px-3 py-2 text-left">{t('stream:p2p.col.deviceIP')}</th>
              <th className="px-3 py-2 text-left">{t('stream:p2p.col.cloudMachine')}</th>
              <th className="px-3 py-2 text-left">{t('stream:p2p.col.listenAddr')}</th>
              <th className="px-3 py-2 text-left">{t('stream:p2p.col.status')}</th>
            </tr>
          </thead>
          <tbody>
            {p2pStreams.length === 0 && (
              <tr><td colSpan={5} className="text-center text-gray-400 py-5">{t('stream:p2p.noStreams')}</td></tr>
            )}
            {p2pStreams.map(item => (
              <tr key={item.id} className="border-b border-gray-50 hover:bg-gray-50">
                <td className="px-3 py-2 font-medium">{item.streamName}</td>
                <td className="px-3 py-2">{item.deviceIp || '-'}</td>
                <td className="px-3 py-2 truncate max-w-32" title={item.cloudMachineName || item.cloudMachineId}>
                  {distributionDisplayName(item, mgr.androids, notes) || '-'}
                </td>
                <td className="px-3 py-2">
                  <span
                    className={`text-blue-500 ${item.p2pListenPort && mgr.localIP ? 'cursor-pointer underline' : ''}`}
                    onClick={() => {
                      if (item.p2pListenPort && mgr.localIP) {
                        navigator.clipboard.writeText(`srt://${mgr.localIP}:${item.p2pListenPort}`).catch(() => {})
                      }
                    }}>
                    {item.p2pListenPort && mgr.localIP ? `srt://${mgr.localIP}:${item.p2pListenPort}` : '-'}
                  </span>
                </td>
                <td className="px-3 py-2">
                  {item.status !== 'active' ? (
                    <button onClick={() => handleStart(item)} disabled={busyId === item.id}
                      className="px-2 py-1 text-[10px] bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-40">
                      {t('stream:dist.startP2P')}
                    </button>
                  ) : (
                    <button onClick={() => handleStop(item)} disabled={busyId === item.id}
                      className="px-2 py-1 text-[10px] bg-amber-500 text-white rounded hover:bg-amber-600 disabled:opacity-40">
                      {t('stream:dist.stopP2P')}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {dialogOpen && <P2PAddDialog mgr={mgr} devices={devices} onClose={() => setDialogOpen(false)} />}
    </div>
  )
}
