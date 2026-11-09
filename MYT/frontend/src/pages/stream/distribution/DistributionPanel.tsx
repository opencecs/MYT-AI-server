// 右侧分发管理面板：已配置分发 / 失效分发两张表 + 刷新状态（批量检查 + 一次性参数同步）+
// 行内操作（修改流 / 启动P2P / 停止P2P / 投屏 / 删除）。
// 行为对齐 pc-android-client StreamManagement.vue 右侧面板。
import { useState, useRef, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { showToast } from '../../../components/Toast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { distributionDisplayName } from '../androidName'
import type { DeviceState } from '../../../types/index'
import type { useStreamManager } from '../useStreamManager'
import type { Distribution } from '../streamTypes'
import {
  checkDistributionStatus, projectDistribution, startP2PDistribution, stopP2PDistribution,
  applyDistribution, applyCameraMode, findMachine, resolveCameraPort, distErrorText,
} from './distributionActions'

type Mgr = ReturnType<typeof useStreamManager>

interface Props {
  mgr: Mgr
  devices: DeviceState[]
  onDistribute: () => void
  onEdit: (record: Distribution) => void
}

export default function DistributionPanel({ mgr, devices, onDistribute, onEdit }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const [refreshing, setRefreshing] = useState(false)
  const [busyId, setBusyId] = useState('')
  const [pendingDelete, setPendingDelete] = useState<Distribution | null>(null)
  const refreshingRef = useRef(false)
  const autoSyncDoneRef = useRef(false)
  const checkStatusRef = useRef<() => void>(() => {})

  const activeDistributions = mgr.distributions.filter(d => d.status !== 'inactive')
  const inactiveDistributions = mgr.distributions.filter(d => d.status === 'inactive')

  const patchRecord = (id: string, patch: Partial<Distribution>) => {
    mgr.setDistributions(prev => prev.map(d => d.id === id ? { ...d, ...patch } : d))
  }

  // 批量状态检查（10 并发）+ 一次性 active 分发参数重推（4 并发，pc-android autoSync 同构）
  const checkStatus = useCallback(async () => {
    if (refreshingRef.current) return
    refreshingRef.current = true
    setRefreshing(true)
    const snapshot = [...mgr.distributions]
    try {
      for (let i = 0; i < snapshot.length; i += 10) {
        await Promise.all(snapshot.slice(i, i + 10).map(async item => {
          try {
            const patch = await checkDistributionStatus(item, { devices, androids: mgr.androids })
            patchRecord(item.id, patch)
          } catch {
            patchRecord(item.id, { status: 'unknown', statusReason: 'requestError' })
          }
        }))
      }
      await autoSyncActive(snapshot)
    } finally {
      refreshingRef.current = false
      setRefreshing(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mgr.distributions, devices, mgr.androids, mgr.localIP])

  // active 且对应流在推流的分发 → 重推一次参数（会话内仅一次）
  const autoSyncActive = async (items: Distribution[]) => {
    if (autoSyncDoneRef.current) return
    const streamActive = (name: string) =>
      mgr.activeStreams.some(s => s.streamName === name && !!s.publisherIP)
    const targets = items.filter(d => d.status === 'active' && streamActive(d.streamName))
    if (targets.length === 0) return
    for (let i = 0; i < targets.length; i += 4) {
      await Promise.allSettled(targets.slice(i, i + 4).map(async item => {
        if (!item.deviceIp || !item.cloudMachineId) return
        const machine = findMachine(mgr.androids, item.deviceIp, item.cloudMachineId)
        if (!machine) return
        try {
          if (item.protocol === 'p2p') {
            await applyCameraMode({ deviceIp: item.deviceIp, machine, resolution: item.resolution || '1' })
          } else {
            // 面向目标设备的源 IP（TUN 代理机器通用 localIP 不可达）
            const localIP = await mgr.localIpForDevice(item.deviceIp)
            await applyDistribution({
              localIP, deviceIp: item.deviceIp, machine,
              streamName: item.streamName, protocol: item.protocol || 'httpflv',
              resolution: item.resolution || '1',
            })
          }
        } catch { /* 单条同步失败忽略 */ }
      }))
    }
    autoSyncDoneRef.current = true
  }

  useEffect(() => { checkStatusRef.current = checkStatus })

  // 设备就绪且有分发记录 → 延时 2s 自动检查一次
  useEffect(() => {
    if (devices.length > 0 && mgr.distributions.length > 0 && !refreshingRef.current) {
      const timer = setTimeout(() => checkStatusRef.current(), 2000)
      return () => clearTimeout(timer)
    }
  }, [devices.length, mgr.distributions.length])

  // 分发记录增删 → 300ms 后自动校验新记录（pc-android setTimeout(checkDistributionsStatus, 300) 同构）
  const lenRef = useRef(mgr.distributions.length)
  useEffect(() => {
    if (mgr.distributions.length !== lenRef.current) {
      lenRef.current = mgr.distributions.length
      if (mgr.distributions.length > 0) {
        const timer = setTimeout(() => checkStatusRef.current(), 300)
        return () => clearTimeout(timer)
      }
    }
  }, [mgr.distributions.length])

  // 流列表变化（5s 轮询）→ 400ms 防抖检查，直至完成首次参数同步（pc-android watch activeStreams 同构）
  useEffect(() => {
    if (autoSyncDoneRef.current || refreshingRef.current) return
    if (mgr.distributions.length === 0) return
    const streamActive = (name: string) =>
      mgr.activeStreams.some(s => s.streamName === name && !!s.publisherIP)
    if (!mgr.distributions.some(d => d.status !== 'inactive' && streamActive(d.streamName))) return
    const timer = setTimeout(() => checkStatusRef.current(), 400)
    return () => clearTimeout(timer)
  }, [mgr.activeStreams])

  const handleStartP2P = async (item: Distribution) => {
    if (!item.p2pListenPort) { showToast(t('stream:p2p.missingListenPort'), 'error'); return }
    setBusyId(item.id)
    try {
      const machine = findMachine(mgr.androids, item.deviceIp, item.cloudMachineId)
      if (!machine) throw new Error('machineNotFound')
      const localIP = await mgr.localIpForDevice(item.deviceIp)
      const listenUrl = await startP2PDistribution({
        localIP, deviceIp: item.deviceIp, machine,
        streamName: item.streamName, listenPort: item.p2pListenPort, resolution: item.resolution || '1',
      })
      patchRecord(item.id, {
        status: 'active',
        statusReason: `srtListen:${listenUrl}`,
        mappedPort: resolveCameraPort(machine) ?? undefined,
      })
    } catch (e) {
      showToast(distErrorText(e, t), 'error')
    } finally { setBusyId('') }
  }

  const handleStopP2P = async (item: Distribution) => {
    setBusyId(item.id)
    try {
      const machine = findMachine(mgr.androids, item.deviceIp, item.cloudMachineId)
      await stopP2PDistribution({
        deviceIp: item.deviceIp, machine, streamName: item.streamName, mappedPort: item.mappedPort,
      })
      patchRecord(item.id, { status: 'inactive', statusReason: 'stoppedReason' })
    } catch (e) {
      showToast(distErrorText(e, t), 'error')
    } finally { setBusyId('') }
  }

  const handleProjection = async (item: Distribution) => {
    try {
      await projectDistribution(item, mgr.androids)
      showToast(t('stream:dist.projectionStarted'), 'success')
    } catch (e) {
      showToast(`${t('stream:dist.projectionFailed')}: ${distErrorText(e, t)}`, 'error')
    }
  }

  const handleDelete = async () => {
    if (!pendingDelete) return
    mgr.setDistributions(prev => prev.filter(d => d.id !== pendingDelete.id))
    setPendingDelete(null)
    showToast(t('stream:dist.deleted'), 'success')
  }

  const renderRows = (items: Distribution[], inactive: boolean) => items.map(item => (
    <tr key={item.id} className={`border-b border-gray-50 hover:bg-gray-50 ${inactive ? 'text-gray-400' : ''}`}>
      <td className="px-2 py-2 font-medium truncate max-w-24" title={item.streamName}>{item.streamName}</td>
      <td className="px-2 py-2 truncate max-w-24">{item.deviceIp}</td>
      <td className="px-2 py-2 truncate max-w-28" title={item.cloudMachineName || item.cloudMachineId}>
        {distributionDisplayName(item, mgr.androids, notes)}
      </td>
      <td className="px-2 py-2">{item.protocol === 'p2p' ? 'P2P' : (item.protocol || 'httpflv').toUpperCase()}</td>
      {inactive && (
        <td className="px-2 py-2">
          <span className="px-1.5 py-0.5 rounded text-[10px] bg-gray-100 text-gray-500">
            {renderReason(item.statusReason)}
          </span>
        </td>
      )}
      <td className="px-2 py-2">
        <div className="flex gap-1 flex-wrap">
          {item.protocol !== 'p2p' && (
            <button onClick={() => onEdit(item)}
              className="px-2 py-1 text-[10px] bg-blue-50 text-blue-600 border border-blue-200 rounded hover:bg-blue-100">
              {t('stream:dist.editStream')}
            </button>
          )}
          {item.protocol === 'p2p' && item.status !== 'active' && (
            <button onClick={() => handleStartP2P(item)} disabled={busyId === item.id}
              className="px-2 py-1 text-[10px] bg-green-500 text-white rounded hover:bg-green-600 disabled:opacity-40">
              {t('stream:dist.startP2P')}
            </button>
          )}
          {item.protocol === 'p2p' && item.status === 'active' && (
            <button onClick={() => handleStopP2P(item)} disabled={busyId === item.id}
              className="px-2 py-1 text-[10px] bg-amber-500 text-white rounded hover:bg-amber-600 disabled:opacity-40">
              {t('stream:dist.stopP2P')}
            </button>
          )}
          <button onClick={() => handleProjection(item)}
            className="px-2 py-1 text-[10px] bg-emerald-500 text-white rounded hover:bg-emerald-600">
            {t('stream:dist.projection')}
          </button>
          <button onClick={() => setPendingDelete(item)}
            className="px-2 py-1 text-[10px] bg-red-500 text-white rounded hover:bg-red-600">
            {t('button.delete')}
          </button>
        </div>
      </td>
    </tr>
  ))

  // 状态原因 → 展示文案（存储键 + 冒号附加的动态信息）
  function renderReason(reason?: string) {
    if (!reason) return t('stream:dist.unverified')
    if (reason === 'running') return t('stream:dist.running')
    if (reason === 'normal') return t('stream:dist.normal')
    if (reason === 'stoppedReason' || reason === 'stopped') return t('stream:dist.stoppedReason')
    if (reason === 'notConfigured') return t('stream:dist.notConfigured')
    if (reason === 'deviceOffline') return t('stream:dist.deviceOffline')
    if (reason === 'machineNotFound') return t('stream:dist.machineInfoNotFound')
    if (reason === 'portNotMapped') return t('stream:dist.portNotMapped')
    if (reason === 'requestError') return t('stream:dist.requestError')
    if (reason === 'pendingCheck') return t('stream:dist.pendingCheck')
    if (reason.startsWith('pathMismatch:')) return t('stream:dist.pathMismatch', { path: reason.slice(13) })
    if (reason.startsWith('queryFailed:')) return t('stream:dist.queryFailed', { reason: reason.slice(12) || '-' })
    if (reason.startsWith('srtListen:')) return t('stream:dist.srtListen', { url: reason.slice(10) })
    return reason
  }

  const tableHead = (inactive: boolean) => (
    <thead className="sticky top-0 bg-gray-50">
      <tr>
        <th className="px-2 py-2 text-left">{t('stream:dist.col.stream')}</th>
        <th className="px-2 py-2 text-left">{t('stream:dist.col.deviceIP')}</th>
        <th className="px-2 py-2 text-left">{t('stream:dist.col.cloudMachine')}</th>
        <th className="px-2 py-2 text-left">{t('stream:dist.col.protocol')}</th>
        {inactive && <th className="px-2 py-2 text-left">{t('stream:dist.col.statusInfo')}</th>}
        <th className="px-2 py-2 text-left">{t('stream:camera.col.action')}</th>
      </tr>
    </thead>
  )

  return (
    <div className="w-[26rem] shrink-0 flex flex-col gap-2 min-h-0">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-gray-600">{t('stream:dist.title')}</span>
        <div className="flex gap-1.5">
          <button onClick={onDistribute}
            className="px-2.5 py-1 text-[10px] bg-blue-500 text-white rounded hover:bg-blue-600">
            {t('stream:dist.distributeTo')}
          </button>
          <button onClick={() => { mgr.refreshAndroids(); checkStatusRef.current() }}
            disabled={refreshing}
            className="px-2.5 py-1 text-[10px] border border-gray-200 rounded hover:bg-gray-50 disabled:opacity-40">
            {refreshing ? t('stream:dist.querying') : t('stream:dist.refreshStatus')}
          </button>
        </div>
      </div>
      <div className="flex-1 min-h-0 overflow-auto flex flex-col gap-2">
        <div className="rounded-lg border border-gray-100">
          <div className="px-2 py-1 text-[10px] font-medium text-green-600 bg-green-50 rounded-t-lg">
            {t('stream:dist.activeTitle')}
          </div>
          <table className="w-full text-xs">
            {tableHead(false)}
            <tbody>
              {activeDistributions.length === 0 && (
                <tr><td colSpan={5} className="text-center text-gray-400 py-4">
                  {refreshing ? t('stream:dist.querying') : t('stream:dist.noActive')}
                </td></tr>
              )}
              {renderRows(activeDistributions, false)}
            </tbody>
          </table>
        </div>
        <div className="rounded-lg border border-gray-100">
          <div className="px-2 py-1 text-[10px] font-medium text-gray-400 bg-gray-50 rounded-t-lg">
            {t('stream:dist.inactiveTitle')}
          </div>
          <table className="w-full text-xs">
            {tableHead(true)}
            <tbody>
              {inactiveDistributions.length === 0 && (
                <tr><td colSpan={6} className="text-center text-gray-400 py-4">{t('stream:dist.noInactive')}</td></tr>
              )}
              {renderRows(inactiveDistributions, true)}
            </tbody>
          </table>
        </div>
      </div>
      {pendingDelete && (
        <ConfirmDialog
          title={t('stream:dist.deleteTitle')}
          message={t('stream:dist.deleteConfirm')}
          confirmLabel={t('button.confirm')}
          onConfirm={handleDelete}
          onCancel={() => setPendingDelete(null)} />
      )}
    </div>
  )
}
