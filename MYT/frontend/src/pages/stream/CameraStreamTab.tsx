import { useState, useEffect, useRef, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useDeployMode } from '../../hooks/useDeployMode'
import { useAndroidNotes } from '../../hooks/useNoteSync'
import { useOnlineDevices } from '../../hooks/useOnlineDevices'
import { getDeviceId } from '../../utils/deviceId'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import type { useStreamManager } from './useStreamManager'
import type { CameraStream } from './streamTypes'
import type { AndroidInfo, DeviceState } from '../../types/index'

type Mgr = ReturnType<typeof useStreamManager>

export default function CameraStreamTab({ mgr }: { mgr: Mgr }) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { devices } = useOnlineDevices()
  const [camList, setCamList] = useState<string[]>([])
  const [previewURL, setPreviewURL] = useState('')
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewId, setPreviewId] = useState('')
  const [addOpen, setAddOpen] = useState(false)
  const previewTimer = useRef<number | null>(null)

  useEffect(() => {
    mgr.listCameras().then((res: any) => {
      if (res?.cameras) setCamList(res.cameras)
    }).catch(() => {})
    return () => { if (previewTimer.current) clearInterval(previewTimer.current) }
  }, [])

  const fetchPreview = async (item: CameraStream, keepOld = false) => {
    if (!keepOld) setPreviewURL('')
    setPreviewLoading(true)
    try {
      const res = await mgr.capturePreview(item.camName || '', 640, 360)
      if (res?.success && res.dataURL) setPreviewURL(res.dataURL)
    } catch {} finally { setPreviewLoading(false) }
  }

  const startPreviewTimer = (item: CameraStream) => {
    if (previewTimer.current) clearInterval(previewTimer.current)
    previewTimer.current = window.setInterval(() => {
      const cur = mgr.cameraStreams.find(s => s.id === item.id)
      if (cur && cur.status === 'running') fetchPreview(cur, true)
      else { if (previewTimer.current) clearInterval(previewTimer.current) }
    }, 1000)
  }

  const handleStart = async (item: CameraStream) => {
    mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, loading: true } : s))
    try {
      const args = ['--camera', item.controlHost || item.deviceIp, String(item.devicePort || 30105),
        '--width', String(item.width), '--height', String(item.height),
        '--fps', String(item.fps), '--bitrate', String(item.bitrate),
        ...(item.camName ? ['--cam-name', item.camName] : [])]
      const res = await mgr.startCamera(args)
      if (res?.success) {
        mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, status: 'running', pid: res.pid, loading: false } : s))
        setPreviewId(item.id)
        fetchPreview(item)
        startPreviewTimer(item)
      }
    } catch {} finally {
      mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, loading: false } : s))
    }
  }

  const handleStop = async (item: CameraStream) => {
    mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, loading: true } : s))
    try {
      await mgr.stopCamera(item.pid || 0)
      mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, status: 'stopped', pid: null, loading: false } : s))
      if (previewId === item.id) { if (previewTimer.current) clearInterval(previewTimer.current); setPreviewURL(''); setPreviewId('') }
    } catch {
      mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, status: 'stopped', pid: null, loading: false } : s))
    }
  }

  const handleRemove = async (item: CameraStream) => {
    if (item.status === 'running') await handleStop(item)
    mgr.setCameraStreams(prev => prev.filter(s => s.id !== item.id))
  }

  return (
    <div className="flex h-full gap-3">
      <div className="flex-1 flex flex-col gap-3 min-w-0">
        <div className="flex gap-2 items-center justify-between">
          <span className="text-xs text-gray-400">{t('stream:camera.available', { list: camList.join(', ') || t('stream:camera.noCamera') })}</span>
          <button onClick={() => setAddOpen(true)}
            className="px-3 py-1.5 text-xs bg-green-500 text-white rounded-lg hover:bg-green-600">
            + {t('stream:camera.add')}
          </button>
        </div>
        <div className="flex-1 overflow-auto">
          <table className="w-full text-xs">
            <thead className="sticky top-0 bg-gray-50">
              <tr>
                <th className="px-3 py-2 text-left">{t('stream:camera.col.deviceIP')}</th>
                <th className="px-3 py-2 text-left">{t('stream:camera.col.camera')}</th>
                <th className="px-3 py-2 text-left">{t('stream:camera.col.resolution')}</th>
                <th className="px-3 py-2 text-left">{t('stream:camera.col.status')}</th>
                <th className="px-3 py-2 text-left">{t('stream:camera.col.action')}</th>
              </tr>
            </thead>
            <tbody>
              {mgr.cameraStreams.length === 0 && (
                <tr><td colSpan={5} className="text-center text-gray-400 py-5">{t('stream:camera.noStreams')}</td></tr>
              )}
              {mgr.cameraStreams.map(item => (
                <tr key={item.id} className={`border-b border-gray-50 hover:bg-gray-50 ${previewId === item.id ? 'bg-blue-50' : ''}`}>
                  <td className="px-3 py-2">{item.cloudMachineName || item.deviceIp}</td>
                  <td className="px-3 py-2 text-gray-500">{item.camName || t('stream:camera.default')}</td>
                  <td className="px-3 py-2">{item.width}x{item.height}@{item.fps}fps / {item.bitrate}kbps</td>
                  <td className="px-3 py-2">
                    <span className={`px-2 py-0.5 rounded text-[10px] ${item.status === 'running' ? 'bg-green-50 text-green-600' : 'bg-gray-100 text-gray-400'}`}>
                      {item.status === 'running' ? t('stream:camera.streaming') : t('stream:camera.stopped')}
                    </span>
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex gap-1.5">
                      {item.status !== 'running' ? (
                        <button onClick={() => handleStart(item)} disabled={item.loading}
                          className="px-2 py-1 text-[10px] bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-40">{t('stream:camera.start')}</button>
                      ) : (
                        <button onClick={() => handleStop(item)} disabled={item.loading}
                          className="px-2 py-1 text-[10px] bg-amber-500 text-white rounded hover:bg-amber-600 disabled:opacity-40">{t('stream:camera.stop')}</button>
                      )}
                      <button onClick={() => handleRemove(item)}
                        className="px-2 py-1 text-[10px] bg-red-500 text-white rounded hover:bg-red-600">{t('button.delete')}</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <div className="w-80 shrink-0 flex flex-col p-3">
        <div className="text-xs text-gray-400 mb-2">{t('stream:camera.preview')}</div>
        {previewLoading && !previewURL ? (
          <div className="flex-1 flex items-center justify-center bg-gray-50 rounded-lg text-gray-400 text-xs">{t('stream:camera.capturing')}</div>
        ) : previewURL ? (
          <div className="flex-1 flex items-center justify-center bg-black rounded-lg relative overflow-hidden">
            <img src={previewURL} className="max-w-full max-h-full object-contain rounded-lg" alt="preview" />
            {previewLoading && <div className="absolute inset-0 flex items-center justify-center bg-black/30 text-white text-xs">{t('stream:camera.refreshing')}</div>}
          </div>
        ) : (
          <div className="flex-1 flex flex-col items-center justify-center bg-gray-50 rounded-lg text-gray-300 text-xs gap-2">
            <span className="text-4xl">📷</span>
            <span>{t('stream:camera.autoPreview')}</span>
          </div>
        )}
      </div>
      {addOpen && (
        <AddCameraDialog
          camList={camList}
          devices={devices}
          mode={mode}
          onAdd={(stream) => { mgr.setCameraStreams(prev => [stream, ...prev]); setAddOpen(false) }}
          onClose={() => setAddOpen(false)}
        />
      )}
    </div>
  )
}

function AddCameraDialog({ camList, devices, mode, onAdd, onClose }: {
  camList: string[]
  devices: DeviceState[]
  mode: 'local' | 'public'
  onAdd: (stream: CameraStream) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const [selectedDevice, setSelectedDevice] = useState('')
  const [androids, setAndroids] = useState<AndroidInfo[]>([])
  const [selectedAndroid, setSelectedAndroid] = useState('')
  const [camName, setCamName] = useState('')
  const [width, setWidth] = useState('1280')
  const [height, setHeight] = useState('720')
  const [fps, setFps] = useState('30')
  const [bitrate, setBitrate] = useState('2000')

  const onlineDevices = useMemo(() => devices.filter(d => d.state === 0), [devices])

  // 加载选中设备的云机列表
  useEffect(() => {
    if (!selectedDevice) { setAndroids([]); return }
    const load = async () => {
      try {
        const all = await DiscoverService.GetAndroidList() as AndroidInfo[]
        const did = mode === 'public' ? selectedDevice : undefined
        setAndroids(all.filter(a => mode === 'public' ? a.deviceId === did : a.ip === selectedDevice).filter(a => a.status === 'running'))
      } catch { setAndroids([]) }
    }
    load()
  }, [selectedDevice, mode])

  const handleConfirm = () => {
    const device = onlineDevices.find(d => getDeviceId(d, mode) === selectedDevice)
    const android = androids.find(a => a.name === selectedAndroid)
    if (!device || !android) return
    const stream: CameraStream = {
      id: `cam-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      deviceIp: getDeviceId(device, mode),
      controlHost: getDeviceId(device, mode),
      devicePort: 30105,
      cloudMachineId: android.id,
      cloudMachineName: android.name,
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30" onClick={onClose}>
      <div className="bg-white rounded-xl shadow-lg p-5 w-[420px]" onClick={e => e.stopPropagation()}>
        <h3 className="text-sm font-semibold mb-4">{t('stream:camera.addTitle')}</h3>
        <div className="space-y-3">
          {/* 设备选择 */}
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
          {/* 云机选择 */}
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:camera.selectMachine')}</label>
            <select value={selectedAndroid} onChange={e => setSelectedAndroid(e.target.value)} disabled={!selectedDevice}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50 disabled:opacity-50">
              <option value="">{t('stream:camera.selectMachine')}</option>
              {androids.map(a => (
                <option key={a.id} value={a.name}>{notes[a.id] || a.name} (坑位 {a.indexNum})</option>
              ))}
            </select>
            {selectedDevice && androids.length === 0 && <p className="text-[10px] text-gray-400 mt-1">{t('stream:camera.noMachines')}</p>}
          </div>
          {/* 摄像头选择 */}
          <div>
            <label className="text-xs text-gray-500 block mb-1">{t('stream:camera.cameraLabel')}</label>
            <select value={camName} onChange={e => setCamName(e.target.value)} disabled={camList.length === 0}
              className="w-full text-xs border border-gray-200 rounded-lg px-3 py-1.5 bg-gray-50 disabled:opacity-50">
              {camList.length === 0
                ? <option value="">{t('stream:camera.noCamera')}</option>
                : <>
                    <option value="">{t('stream:camera.default')}</option>
                    {camList.map(c => <option key={c} value={c}>{c}</option>)}
                  </>
              }
            </select>
          </div>
          {/* 分辨率/帧率/码率 */}
          <div className="grid grid-cols-4 gap-2">
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
          <button onClick={onClose} className="px-4 py-1.5 text-xs border border-gray-200 rounded-lg">{t('button.cancel')}</button>
          <button onClick={handleConfirm} disabled={!selectedDevice || !selectedAndroid}
            className="px-4 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-40">{t('button.confirm')}</button>
        </div>
      </div>
    </div>
  )
}
