import { useState, useEffect, useMemo, useCallback,  useRef} from 'react'
import { useTranslation } from 'react-i18next'
import { emitSlotStatus } from '../../../../../utils/wailsEvent'
import * as SdkApiService from '../../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as SlotService from '../../../../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import * as ScreenshotService from '../../../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import { DeviceState, AndroidInfo, SlotExpiry } from '../../../../../types/index'
import { showToast } from '../../../../../components/Toast'
import { fetchSingleDeviceSlots, RemoteDeviceInfo } from '../crossDeviceMove'
import { useLiveTimer, SlotButton, DeviceItem } from './MoveComponents'
import { useTaskQueue } from '../../../../../hooks/taskQueue/useTaskQueue'
import { useModalEnter } from '../../../../../hooks/useModalAnimation'

type MoveTarget = { type: 'same'; slot: number } | { type: 'cross'; deviceIP: string; deviceName: string; slot: number }
type MoveStage = 'idle' | 'exporting' | 'done' | 'error'

interface Props {
  deviceIP: string
  deviceID: string
  android: AndroidInfo
  slotExpiry: SlotExpiry[]
  androids: AndroidInfo[]
  devices: DeviceState[]
  displayName?: string
  onClose: () => void
}

function MoveModal({ deviceIP, deviceID, android, slotExpiry, androids, devices = [], displayName, onClose }: Props) {
  const { t } = useTranslation()
  const [slotCount, setSlotCount] = useState(12)
  const [target, setTarget] = useState<MoveTarget | null>(null)
  const [stage, setStage] = useState<MoveStage>('idle')
  const [error, setError] = useState('')
  const [selectedDeviceId, setSelectedDeviceId] = useState<string>(deviceID)
  const [remoteInfo, setRemoteInfo] = useState<RemoteDeviceInfo | null>(null)
  const [remoteSlotExpiry, setRemoteSlotExpiry] = useState<SlotExpiry[]>([])
  const [loadingSlots, setLoadingSlots] = useState(false)
  const exportTimer = useLiveTimer(stage === 'exporting')
  const { addTask, updateTask, openPanel } = useTaskQueue()

  useEffect(() => {
    SlotService.GetSlotCount(deviceID).then(setSlotCount).catch(() => {})
  }, [deviceID])

  useEffect(() => {
    if (selectedDeviceId === deviceID) {
      setRemoteInfo(null)
      setRemoteSlotExpiry([])
      return
    }
    const dev = devices.find(d => d.device_id === selectedDeviceId)
    if (!dev) return
    let cancelled = false
    setLoadingSlots(true)
    Promise.all([
      fetchSingleDeviceSlots(dev, SlotService.GetSlotCount),
      SlotService.QuerySlotInfo([dev.device_id]).then((results: { slots: { index: number; expire_time: string; state: number }[] }[]) => {
        if (results?.[0]?.slots) {
          return results[0].slots.map(s => ({ index: s.index, expire_time: s.expire_time, state: s.state }))
        }
        return []
      }).catch(() => []),
    ]).then(([info, expiry]) => {
      if (!cancelled) {
        setRemoteInfo(info)
        setRemoteSlotExpiry(expiry)
        setLoadingSlots(false)
      }
    }).catch(() => {
      if (!cancelled) { setRemoteInfo(null); setRemoteSlotExpiry([]); setLoadingSlots(false) }
    })
    return () => { cancelled = true }
  }, [selectedDeviceId, devices, deviceID])

  const otherDevices = useMemo(() => devices.filter(d => d.device_id !== deviceID && d.state === 0), [devices, deviceID])

  const occupiedSlots = useMemo(() => {
    // 只计开机中的容器（running/starting）——被 stopped 容器占的坑位允许移动覆盖（与创建实例同规则）
    const set = new Set<number>()
    for (const a of androids) if (a.id !== android.id && (a.status === 'running' || a.status === 'starting')) set.add(a.indexNum)
    return set
  }, [androids, android.id])

  const expiryMap = useMemo(() => {
    const map = new Map<number, SlotExpiry>()
    for (const s of slotExpiry) map.set(s.index, s)
    return map
  }, [slotExpiry])

  const remoteExpiryMap = useMemo(() => {
    const map = new Map<number, SlotExpiry>()
    for (const s of remoteSlotExpiry) map.set(s.index, s)
    return map
  }, [remoteSlotExpiry])

  const handleMove = useCallback(async () => {
    if (!target) return
    setError('')
    try {
      if (target.type === 'same') {
        setStage('exporting') // 即时反馈：请求期间显示"移动中"（结构变更无 WS 事件，靠返回后的查询刷新）
        await SdkApiService.MoveAndroid(deviceIP, android.name, target.slot)
        ScreenshotService.InvalidateSlot(deviceIP, android.indexNum).catch(() => {})
        ScreenshotService.InvalidateSlot(deviceIP, target.slot).catch(() => {})
        emitSlotStatus(`${deviceIP}_${android.indexNum}`, 'starting')
        emitSlotStatus(`${deviceIP}_${target.slot}`, 'starting')
        setStage('done')
        showToast(t('host:toast.moveSuccess'), 'success')
        return
      }
      setStage('exporting')
      const taskId = await addTask({
        type: 'moveAndroid',
        deviceIps: [deviceIP],
        total: 1,
        machineName: android.name,
        srcIP: deviceIP,
        dstIP: target.deviceIP,
        targetSlot: target.slot,
      })
      updateTask(taskId, { status: 'running', startTime: Date.now() })
      onClose()
      openPanel(taskId)
      setTimeout(async () => {
        try {
          await SdkApiService.CrossDeviceMove(deviceIP, target.deviceIP, android.name, android.image, target.slot, taskId)
          ScreenshotService.InvalidateSlot(deviceIP, android.indexNum).catch(() => {})
          emitSlotStatus(`${deviceIP}_${android.indexNum}`, 'starting')
          updateTask(taskId, { status: 'completed', progress: 100, completed: 1, moveStage: 'done' })
          showToast(t('host:toast.crossDeviceMoveSuccess'), 'success')
        } catch (err) {
          updateTask(taskId, {
            status: 'failed',
            failed: 1,
            failedTargets: [{ deviceIp: deviceIP, machineName: android.name, error: String(err) }],
          })
          showToast(t('host:toast.moveFailed', { error: String(err) }), 'error')
        }
      }, 0)
    } catch (err) {
      setStage('error')
      setError(`${err}`)
      showToast(t('host:toast.moveFailed', { error: String(err) }), 'error')
    }
  }, [target, deviceIP, android.name])

  const allSlots = Array.from({ length: slotCount }, (_, i) => i + 1)

  const isCurrentDevice = selectedDeviceId === deviceID
  const currentDevice = useMemo(() => {
    return devices.find(d => d.device_id === deviceID) || { device_id: deviceID, ip: deviceIP, state: 0, device_name: '', device_type: '', disk_used: 0, disk_total: 0 } as DeviceState
  }, [devices, deviceID, deviceIP])

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[600px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <div>
            <h3 className="text-sm font-medium text-gray-700">{t('host:move.title', { name: displayName || android.name })}</h3>
            <p className="text-xs text-gray-400 mt-0.5">{t('host:move.subtitle', { slot: android.indexNum })}</p>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        {stage === 'idle' ? (
          <div className="flex flex-1 overflow-hidden">
            <div className="w-48 border-r border-gray-100 flex flex-col shrink-0">
              <div className="px-3 py-2 border-b border-gray-50">
                <span className="text-xs font-medium text-gray-500">{t('host:move.selectDevice')}</span>
              </div>
              <div className="flex-1 overflow-y-auto">
                <DeviceItem device={currentDevice} isCurrent isSelected={isCurrentDevice} onClick={() => { setSelectedDeviceId(deviceID); setTarget(null) }} />
                {otherDevices.map(d => (
                  <DeviceItem key={d.device_id} device={d} isCurrent={false} isSelected={selectedDeviceId === d.device_id} onClick={() => { setSelectedDeviceId(d.device_id); setTarget(null) }} />
                ))}
              </div>
              <div className="px-3 py-1.5 border-t border-gray-100 text-[10px] text-gray-400">
                {t('host:move.onlineDevices', { count: otherDevices.length + 1 })}
              </div>
            </div>

            <div className="flex-1 overflow-y-auto p-5">
              <div className="text-xs text-gray-500 bg-blue-50/60 p-2 rounded-lg mb-3 space-y-0.5">
                <p>{t('host:move.hint')}, <span className="text-gray-400">{t('host:move.occupiedHint')}</span></p>
                {!isCurrentDevice && <p className="text-amber-600">{t('host:move.crossDeviceWarning')}</p>}
              </div>
              <div className="flex gap-1.5 mb-3 text-[10px] text-gray-400 flex-wrap">
                <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-blue-400 inline-block" />{t('host:move.selected')}</span>
                <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-gray-300 inline-block" />{t('host:move.idle')}</span>
                <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-green-500 inline-block" />{t('host:move.current')}</span>
                <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-green-400 inline-block" />{t('host:slotPicker.normal')}</span>
                <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-yellow-400 inline-block" />{t('host:slotPicker.expiring')}</span>
                <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-red-400 inline-block" />{t('host:slotPicker.expired')}</span>
              </div>

              {isCurrentDevice ? (
                <div>
                  <p className="text-xs font-medium text-gray-500 mb-1.5">{t('host:move.currentDevice')}</p>
                  <div className="grid grid-cols-4 gap-2">
                    {allSlots.map(idx => (
                      <SlotButton key={idx} idx={idx} isCurrent={idx === android.indexNum} isOccupied={occupiedSlots.has(idx)}
                        isSelected={target?.type === 'same' && target.slot === idx}
                        onClick={() => setTarget({ type: 'same', slot: idx })} expiryMap={expiryMap} />
                    ))}
                  </div>
                </div>
              ) : loadingSlots ? (
                <div className="flex items-center justify-center h-32 text-xs text-gray-400">
                  <svg className="w-4 h-4 animate-spin mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
                  {t('host:move.loadingSlots')}
                </div>
              ) : remoteInfo ? (
                <div>
                  <p className="text-xs font-medium text-gray-500 mb-1.5">{remoteInfo.name} <span className="text-gray-400">({remoteInfo.ip})</span></p>
                  <div className="grid grid-cols-4 gap-2">
                    {Array.from({ length: remoteInfo.slotCount }, (_, i) => i + 1).map(idx => {
                      const isOccupied = remoteInfo.occupiedSlots.has(idx)
                      const isSelected = target?.type === 'cross' && target.deviceIP === remoteInfo.ip && target.slot === idx
                      return (
                        <SlotButton key={idx} idx={idx} isCurrent={false} isOccupied={isOccupied}
                          isSelected={isSelected}
                          onClick={() => !isOccupied && setTarget({ type: 'cross', deviceIP: remoteInfo.ip, deviceName: remoteInfo.name, slot: idx })}
                          expiryMap={remoteExpiryMap} />
                      )
                    })}
                  </div>
                </div>
              ) : (
                <div className="flex items-center justify-center h-32 text-xs text-red-400">{t('host:move.slotsFailed')}</div>
              )}
            </div>
          </div>
        ) : (
          <div className="p-6 space-y-4">
            {stage === 'exporting' && (
              <div className="flex items-center gap-2 text-sm text-blue-600">
                <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
                {t('host:move.moving')} {exportTimer}
              </div>
            )}
            {stage === 'done' && (
              <div className="flex items-center gap-2 text-sm text-green-600">
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><polyline points="20 6 9 17 4 12" /></svg>
                {t('host:move.done')}
              </div>
            )}
            {stage === 'error' && (
              <div className="text-sm text-red-500 bg-red-50 p-2 rounded-lg">{error}</div>
            )}
          </div>
        )}

        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">
            {stage === 'done' ? t('button.close') : t('button.cancel')}
          </button>
          {stage === 'idle' && (
            <button onClick={handleMove} disabled={!target}
              className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50"
            >{t('host:actionBar.move')}</button>
          )}
        </div>
      </div>
    </div>
  )
}

export default MoveModal
