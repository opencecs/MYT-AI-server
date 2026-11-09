// 添加摄像头推流弹窗：设备 + 云机 + 设备端口（10006 映射自动探测）+ 摄像头（可刷新）+ 分辨率/帧率/码率。
import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { androidDisplayName } from '../androidName'
import type { DeviceState } from '../../../types/index'
import type { useStreamManager } from '../useStreamManager'
import type { CameraStream } from '../streamTypes'
import { getControlHost, resolveCameraPort } from '../distribution/distributionActions'
import { getDeviceId } from '../../../utils/deviceId'

type Mgr = ReturnType<typeof useStreamManager>

interface Props {
  mgr: Mgr
  devices: DeviceState[]
  mode: 'local' | 'public'
  onAdd: (stream: CameraStream) => void
  onClose: () => void
}

export default function CameraAddDialog({ mgr, devices, mode, onAdd, onClose }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const [selectedDevice, setSelectedDevice] = useState('')
  const [selectedAndroid, setSelectedAndroid] = useState('')
  const [camName, setCamName] = useState('')
  const [width, setWidth] = useState('1280')
  const [height, setHeight] = useState('720')
  const [fps, setFps] = useState('30')
  const [bitrate, setBitrate] = useState('2000')
  const [camList, setCamList] = useState<string[]>([])
  const [camListLoading, setCamListLoading] = useState(false)

  // 打开弹窗时刷新云机缓存与摄像头列表
  useEffect(() => {
    mgr.refreshAndroids()
    fetchCameraList()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const availableMachines = useMemo(
    () => mgr.androids.filter(a => (mode === 'public' ? a.deviceId === selectedDevice : a.ip === selectedDevice) && a.status === 'running'),
    [mgr.androids, mode, selectedDevice],
  )

  const selectedMachine = useMemo(
    () => availableMachines.find(m => m.id === selectedAndroid || m.name === selectedAndroid) || null,
    [availableMachines, selectedAndroid],
  )
  const detectedPort = useMemo(() => (selectedMachine ? resolveCameraPort(selectedMachine) : null), [selectedMachine])

  const fetchCameraList = async () => {
    setCamListLoading(true)
    try {
      const res = await mgr.listCameras() as any
      if (res?.success && Array.isArray(res.cameras)) {
        setCamList(res.cameras)
        if (camName && !res.cameras.includes(camName)) setCamName('')
      }
    } catch { /* keep current list */ } finally { setCamListLoading(false) }
  }

  const onlineDevices = useMemo(() => devices.filter(d => d.state === 0), [devices])

  const handleConfirm = () => {
    const device = onlineDevices.find(d => getDeviceId(d, mode) === selectedDevice)
    const machine = selectedMachine
    if (!device || !machine) return
    const stream: CameraStream = {
      id: `cam-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      deviceIp: getDeviceId(device, mode),
      controlHost: getControlHost(getDeviceId(device, mode), machine),
      devicePort: detectedPort || 30105,
      cloudMachineId: machine.id,
      cloudMachineName: machine.name,
      camName: camName || (camList[0] || ''),
      width: Number(width) || 1280,
      height: Number(height) || 720,
      fps: Number(fps) || 30,
      bitrate: Number(bitrate) || 2000,
      status: 'stopped',
    }
    onAdd(stream)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
      <div className="bg-white rounded-xl shadow-lg p-5 w-[420px]">
        <h3 className="text-sm font-semibold mb-4">{t('stream:camera.addTitle')}</h3>
        <div className="space-y-3">
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:camera.selectDevice')}</label>
            <select value={selectedDevice} onChange={e => { setSelectedDevice(e.target.value); setSelectedAndroid('') }}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50">
              <option value="">{t('stream:camera.selectDevice')}</option>
              {onlineDevices.map(d => (
                <option key={d.device_id} value={getDeviceId(d, mode)}>
                  {mode === 'public' ? d.device_id : d.ip} - {d.device_type?.toUpperCase()}
                </option>
              ))}
            </select>
            {onlineDevices.length === 0 && <p className="text-[10px] text-gray-400 mt-1">{t('stream:camera.noDevices')}</p>}
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:camera.selectMachine')}</label>
            <select value={selectedAndroid} onChange={e => setSelectedAndroid(e.target.value)} disabled={!selectedDevice}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50 disabled:opacity-50">
              <option value="">{t('stream:camera.selectMachine')}</option>
              {availableMachines.map(m => (
                <option key={m.id} value={m.id} title={m.name}>{androidDisplayName(m, notes)} (坑位 {m.indexNum})</option>
              ))}
            </select>
            {selectedDevice && availableMachines.length === 0 &&
              <p className="text-[10px] text-gray-400 mt-1">{t('stream:camera.noMachines')}</p>}
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:camera.devicePortLabel')}</label>
            <div className="text-xs text-gray-700 px-3 py-1.5 bg-gray-50 rounded-lg">
              {detectedPort ?? '—'}
              {!detectedPort && <span className="text-amber-500 text-[10px] ml-2">{t('stream:camera.portHint')}</span>}
            </div>
          </div>
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:camera.cameraLabel')}</label>
            <div className="flex gap-2">
              <select value={camName} onChange={e => setCamName(e.target.value)} disabled={camListLoading}
                className="flex-1 text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50 disabled:opacity-50">
                {camList.length === 0
                  ? <option value="">{t('stream:camera.noCamera')}</option>
                  : <>
                      <option value="">{t('stream:camera.default')}</option>
                      {camList.map(c => <option key={c} value={c}>{c}</option>)}
                    </>
                }
              </select>
              <button onClick={fetchCameraList} disabled={camListLoading}
                className="px-3 py-1.5 text-xs border border-gray-200 rounded-lg hover:bg-gray-50 disabled:opacity-40">
                {camListLoading ? t('stream:camera.refreshing') : t('stream:camera.refresh')}
              </button>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-400 block mb-0.5">{t('stream:camera.width')}</label>
              <input type="number" value={width} onChange={e => setWidth(e.target.value)}
                className="w-full text-xs border border-gray-200 rounded-lg px-2 py-1 bg-gray-50" />
            </div>
            <div>
              <label className="text-[10px] text-gray-400 block mb-0.5">{t('stream:camera.height')}</label>
              <input type="number" value={height} onChange={e => setHeight(e.target.value)}
                className="w-full text-xs border border-gray-200 rounded-lg px-2 py-1 bg-gray-50" />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-gray-400 block mb-0.5">{t('stream:camera.fps')}</label>
              <input type="number" value={fps} onChange={e => setFps(e.target.value)}
                className="w-full text-xs border border-gray-200 rounded-lg px-2 py-1 bg-gray-50" />
            </div>
            <div>
              <label className="text-[10px] text-gray-400 block mb-0.5">{t('stream:camera.bitrate')}</label>
              <input type="number" value={bitrate} onChange={e => setBitrate(e.target.value)}
                className="w-full text-xs border border-gray-200 rounded-lg px-2 py-1 bg-gray-50" />
            </div>
          </div>
        </div>
        <div className="flex justify-end gap-2 mt-4">
          <button onClick={onClose} className="px-3 py-1.5 text-xs border border-gray-200 rounded-lg">{t('button.cancel')}</button>
          <button onClick={handleConfirm} disabled={!selectedDevice || !selectedAndroid}
            className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-40">{t('stream:camera.confirmAdd')}</button>
        </div>
      </div>
    </div>
  )
}
