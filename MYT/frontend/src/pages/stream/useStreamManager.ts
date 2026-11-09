import { useState, useEffect, useRef, useCallback } from 'react'
import { Events } from '@wailsio/runtime'
import * as StreamService from '../../../bindings/github.com/moyunteng/myt/streamservicewrapper'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { showToast } from '../../components/Toast'
import { useTranslation } from 'react-i18next'
import type { AndroidInfo } from '../../types/index'
import type { StreamInfo, Distribution, CameraStream } from './streamTypes'

export function useStreamManager() {
  const { t } = useTranslation()
  const [localIP, setLocalIP] = useState('')
  const [activeStreams, setActiveStreams] = useState<StreamInfo[]>([])
  const [newStreamName, setNewStreamName] = useState('')
  const [distributions, setDistributions] = useState<Distribution[]>([])
  const [cameraStreams, setCameraStreams] = useState<CameraStream[]>([])
  const [androids, setAndroids] = useState<AndroidInfo[]>([])
  const [applyLoading, setApplyLoading] = useState(false)
  const timerRef = useRef<number | null>(null)

  useEffect(() => {
    // 用生成 bindings（Call.ByID）——此前 Call.ByName 用了错误的 'main.' 包前缀
    // （真实 FQN 是 github.com/moyunteng/myt.StreamServiceWrapper.*），查不到方法 → 静默 reject
    StreamService.GetLocalIp().then((ip) => {
      if (typeof ip === 'string') setLocalIP(ip)
    }).catch(() => {})
    timerRef.current = window.setInterval(refreshStreams, 5000)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [])

  useEffect(() => {
    try {
      const saved = localStorage.getItem('streamDistributions')
      if (saved) setDistributions(JSON.parse(saved))
    } catch {}
    try {
      const saved = localStorage.getItem('cameraStreams')
      if (saved) setCameraStreams(JSON.parse(saved))
    } catch {}
  }, [])

  useEffect(() => {
    localStorage.setItem('streamDistributions', JSON.stringify(distributions))
  }, [distributions])

  useEffect(() => {
    localStorage.setItem('cameraStreams', JSON.stringify(cameraStreams))
  }, [cameraStreams])

  const localIpForDevice = useCallback(async (deviceIP: string): Promise<string> => {
    try {
      const ip = await StreamService.GetLocalIpForDevice(deviceIP) as unknown as string
      if (typeof ip === 'string' && ip) return ip
    } catch { /* fallback */ }
    return localIP
  }, [localIP])

  const refreshAndroids = useCallback(async () => {
    try {
      const all = await DiscoverService.GetAndroidList() as unknown as AndroidInfo[]
      setAndroids(Array.isArray(all) ? all : [])
    } catch {
      setAndroids([])
    }
  }, [])

  useEffect(() => {
    // 首次加载 + 容器列表事件驱动更新（与云机页同源），保证分发面板/弹窗数据新鲜
    refreshAndroids()
    const off = Events.On('androidlist:updated', () => { refreshAndroids() })
    return () => { off() }
  }, [refreshAndroids])

  const refreshStreams = useCallback(async () => {
    try {
      const streams = await StreamService.GetActiveStreams() as unknown as StreamInfo[]
      setActiveStreams(Array.isArray(streams) ? streams : [])
    } catch {
      setActiveStreams([])
    }
  }, [])

  const addStream = async () => {
    const name = newStreamName.trim()
    if (!name) {
      showToast(t('stream:forward.emptyRoomName'), 'error')
      return
    }
    try {
      await StreamService.AddStreamName(name)
      showToast(t('stream:forward.addSuccess'), 'success')
    } catch (err) {
      showToast(`${t('stream:forward.newStream')}: ${err}`, 'error')
      return
    }
    setNewStreamName('')
    refreshStreams()
  }

  const deleteStream = async (name: string) => {
    try {
      await StreamService.DeleteStreamName(name)
    } catch (err) {
      showToast(`${t('button.delete')}: ${err}`, 'error')
      return
    }
    refreshStreams()
  }

  const stopPush = async (name: string) => {
    const res = await StreamService.StopPushSession(name) as any
    if (res?.success) refreshStreams()
    return res
  }

  const proxyHttpGet = async (url: string): Promise<any> => {
    try {
      const result = await StreamService.ProxyHttpGet(url) as string
      return JSON.parse(result)
    } catch {
      return { code: 500, reason: 'Request Failed' }
    }
  }

  const startP2P = async (deviceIP: string, port: number, streamName: string, listenPort: number) => {
    return await StreamService.StartP2P(deviceIP, port, streamName, listenPort) as any
  }

  const stopP2P = async (deviceIP: string, port: number, streamName: string) => {
    return await StreamService.StopP2P(deviceIP, port, streamName) as any
  }

  const getP2PStatus = async (deviceIP: string, port: number, streamName: string) => {
    return await StreamService.GetP2PStatus(deviceIP, port, streamName) as any
  }

  const startCamera = async (args: string[]) => {
    return await StreamService.StartWindowsPusher(args) as any
  }

  const stopCamera = async (id: number) => {
    return await StreamService.StopWindowsPusher(id) as any
  }

  const capturePreview = async (camName: string, w: number, h: number) => {
    return await StreamService.CapturePreviewFrame(camName, w, h) as any
  }

  const listCameras = async () => {
    return await StreamService.ListCameraDevices() as any
  }

  return {
    localIP, activeStreams, newStreamName, setNewStreamName,
    distributions, setDistributions, cameraStreams, setCameraStreams,
    applyLoading, setApplyLoading,
    androids, refreshAndroids,
    localIpForDevice,
    refreshStreams, addStream, deleteStream, stopPush, proxyHttpGet,
    startP2P, stopP2P, getP2PStatus,
    startCamera, stopCamera, capturePreview, listCameras,
  }
}
