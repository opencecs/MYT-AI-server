import { useState, useEffect, useMemo,  useRef} from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as SlotService from '../../../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import { AndroidInfo, SlotExpiry } from '../../../../types/index'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import { useModalEnter } from '../../../../hooks/useModalAnimation'

interface Props {
  deviceIP: string
  deviceID: string
  android: AndroidInfo
  slotExpiry: SlotExpiry[]
  androids: AndroidInfo[]
  onClose: () => void
}

function CopyModal({ deviceIP, deviceID, android, slotExpiry, androids, onClose }: Props) {
  const { t } = useTranslation()
  const [slotCount, setSlotCount] = useState(12)
  const [targetSlot, setTargetSlot] = useState<number | null>(null)
  const [count, setCount] = useState(1)
  const { addTask, updateTask } = useTaskQueue()

  useEffect(() => {
    SlotService.GetSlotCount(deviceID).then(setSlotCount).catch(() => {})
  }, [deviceID])

  const occupiedSlots = useMemo(() => {
    const set = new Set<number>()
    for (const a of androids) set.add(a.indexNum)
    return set
  }, [androids])

  const expiryMap = useMemo(() => {
    const map = new Map<number, SlotExpiry>()
    for (const s of slotExpiry) map.set(s.index, s)
    return map
  }, [slotExpiry])

  const slotColor = (idx: number, exp?: SlotExpiry) => {
    if (!exp || exp.state === undefined) return { border: 'border-gray-200', dot: 'bg-gray-300' }
    if (exp.state === 0) return { border: 'border-green-300', dot: 'bg-green-400' }
    if (exp.state === 1) return { border: 'border-yellow-300', dot: 'bg-yellow-400' }
    return { border: 'border-red-300', dot: 'bg-red-400' }
  }

  const allSlots = Array.from({ length: slotCount }, (_, i) => i + 1)

  const handleCopy = async () => {
    if (!targetSlot) return
    const taskId = await addTask({ type: 'copy', targets: [{ deviceIp: deviceIP }], deviceIps: [deviceIP], total: count, sourceName: android.name })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    onClose()
    setTimeout(async () => {
      try {
        await SdkApiService.CopyAndroid(deviceIP, android.name, targetSlot, count, taskId)
        updateTask(taskId, { status: 'completed', progress: 100, endTime: Date.now() })
      } catch (err) {
        updateTask(taskId, { status: 'failed', error: String(err), endTime: Date.now() })
      }
    }, 0)
  }

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[500px] max-h-[80vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <div>
            <h3 className="text-sm font-medium text-gray-700">{t('host:copy.title', { name: android.name })}</h3>
            <p className="text-xs text-gray-400 mt-0.5">{t('host:copy.subtitle', { slot: android.indexNum })}</p>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        <div className="flex-1 overflow-y-auto p-5">
          <div className="text-xs text-gray-500 bg-blue-50/60 p-2 rounded-lg mb-3 space-y-0.5">
            <p>{t('host:copy.hint')}</p>
            <p className="text-gray-400">{t('host:copy.occupiedHint')}</p>
          </div>
          <div className="flex gap-1.5 mb-3 text-[10px] text-gray-400 flex-wrap">
            <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-blue-400 inline-block" />{t('host:copy.selected')}</span>
            <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-gray-300 inline-block" />{t('host:copy.idle')}</span>
            <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-green-400 inline-block" />{t('host:slotPicker.normal')}</span>
            <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-yellow-400 inline-block" />{t('host:slotPicker.expiring')}</span>
            <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-red-400 inline-block" />{t('host:slotPicker.expired')}</span>
          </div>

          <div className="grid grid-cols-4 gap-2 mb-4">
            {allSlots.map(idx => {
              const sc = slotColor(idx, expiryMap.get(idx))
              const isOccupied = occupiedSlots.has(idx)
              const isSelected = targetSlot === idx
              return (
                <button key={idx} onClick={() => setTargetSlot(idx)}
                  className={`aspect-square rounded-lg border-2 transition-all flex flex-col items-center justify-center gap-0.5
                    ${isSelected ? 'bg-blue-500 text-white border-blue-500 shadow-sm' : isOccupied ? 'bg-orange-50 text-orange-500 border-orange-200' : 'bg-white hover:bg-gray-50 ' + sc.border}`}
                >
                  <span className={`w-1.5 h-1.5 rounded-full ${isSelected ? 'bg-white' : sc.dot}`} />
                  <span className="text-xs font-semibold leading-none">{idx}</span>
                  {isOccupied && <span className="text-[9px] leading-none">{t('host:copy.occupied')}</span>}
                </button>
              )
            })}
          </div>

          <div className="space-y-2">
            <label className="text-sm text-gray-600">{t('host:copy.copyCount')}</label>
            <input
              type="number" min={1} max={20} value={count}
              onChange={e => setCount(Math.max(1, Math.min(20, Number(e.target.value))))}
              className="w-full border border-gray-200 rounded-lg px-3 py-1.5 text-sm outline-none focus:border-blue-400"
            />
          </div>
        </div>

        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={handleCopy} disabled={!targetSlot}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">
            {t('host:copy.confirmCopy')}
          </button>
        </div>
      </div>
    </div>
  )
}

export default CopyModal
