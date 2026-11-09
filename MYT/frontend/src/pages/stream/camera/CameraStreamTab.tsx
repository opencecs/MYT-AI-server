// PC 摄像头 Tab：推流列表 + 启停/删除 + 实时预览（1s 抓帧）。
import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { showToast } from '../../../components/Toast'
import ConfirmDialog from '../../../components/ConfirmDialog'
import { useDeployMode } from '../../../hooks/useDeployMode'
import type { DeviceState } from '../../../types/index'
import type { useStreamManager } from '../useStreamManager'
import type { CameraStream } from '../streamTypes'
import CameraAddDialog from './CameraAddDialog'
import { androidDisplayName } from '../androidName'
import { useAndroidNotes } from '../../../hooks/useNoteSync'

type Mgr = ReturnType<typeof useStreamManager>

interface Props {
  mgr: Mgr
  devices: DeviceState[]
}

export default function CameraStreamTab({ mgr, devices }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  // 云机列显示名：备注优先 + 短名（与主机/云机 Tab 同规则）；title 保留全名
  const nameOf = (id?: string, fallbackName?: string) => {
    const a = id ? mgr.androids.find(m => m.id === id) : undefined
    return a ? androidDisplayName(a, notes) : (fallbackName || '')
  }
  const { mode } = useDeployMode()
  const [camList, setCamList] = useState<string[]>([])
  const [previewURL, setPreviewURL] = useState('')
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewId, setPreviewId] = useState('')
  const [addOpen, setAddOpen] = useState(false)
  const [pendingRemove, setPendingRemove] = useState<CameraStream | null>(null)
  const previewTimer = useRef<number | null>(null)

  useEffect(() => {
    mgr.listCameras().then((res: any) => {
      if (res?.cameras) setCamList(res.cameras)
    }).catch(() => {})
    return () => { if (previewTimer.current) clearInterval(previewTimer.current) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
        showToast(t('stream:camera.started'), 'success')
      } else {
        showToast(`${t('stream:dist.startFailed')}: ${res?.message || ''}`, 'error')
      }
    } catch (e) {
      showToast(`${t('stream:dist.startFailed')}: ${e instanceof Error ? e.message : String(e)}`, 'error')
    } finally {
      mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, loading: false } : s))
    }
  }

  const handleStop = async (item: CameraStream) => {
    mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, loading: true } : s))
    try {
      await mgr.stopCamera(item.pid || 0)
      mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, status: 'stopped', pid: null, loading: false } : s))
      if (previewId === item.id) { if (previewTimer.current) clearInterval(previewTimer.current); setPreviewURL(''); setPreviewId('') }
      showToast(t('stream:camera.stopped'), 'success')
    } catch {
      mgr.setCameraStreams(prev => prev.map(s => s.id === item.id ? { ...s, status: 'stopped', pid: null, loading: false } : s))
    }
  }

  const handleRemove = async (item: CameraStream) => {
    if (item.status === 'running') await handleStop(item)
    mgr.setCameraStreams(prev => prev.filter(s => s.id !== item.id))
    showToast(t('stream:camera.deleted'), 'success')
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
                  <td className="px-3 py-2 truncate max-w-32" title={item.cloudMachineName || item.deviceIp}>
                    {nameOf(item.cloudMachineId, item.cloudMachineName) || item.deviceIp}
                  </td>
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
                      <button onClick={() => setPendingRemove(item)}
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
        <div className="text-xs text-gray-400 mb-2 flex items-center justify-between">
          <span>{t('stream:camera.preview')}</span>
          <span className="text-[10px]">
            {previewId ? t('stream:camera.previewHint') : t('stream:camera.autoPreview')}
          </span>
        </div>
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
        <CameraAddDialog mgr={mgr} devices={devices} mode={mode}
          onAdd={(stream) => {
            mgr.setCameraStreams(prev => [stream, ...prev])
            setAddOpen(false)
            showToast(t('stream:camera.addedHint'), 'success')
          }}
          onClose={() => setAddOpen(false)} />
      )}
      {pendingRemove && (
        <ConfirmDialog
          title={t('stream:camera.deleteTitle')}
          message={t('stream:camera.deleteConfirm')}
          onConfirm={() => { const item = pendingRemove; setPendingRemove(null); handleRemove(item) }}
          onCancel={() => setPendingRemove(null)} />
      )}
    </div>
  )
}
