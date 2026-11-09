import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { emitSlotStatus } from '../../../../utils/wailsEvent'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../utils/deviceId'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as MirrorService from '../../../../../bindings/github.com/moyunteng/myt/mirrorservicewrapper'
import * as ImageService from '../../../../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import * as ScreenshotService from '../../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import * as PhoneModelService from '../../../../../bindings/github.com/moyunteng/myt/phonemodelservice'
import { DeviceState, MirrorItem, AndroidInfo, SavedMirror, PhoneModel } from '../../../../types/index'
import type { PrivateNic } from '../lib/networkTypes'
import { getErrorMessage } from '../../../../utils/error'
import { cast } from '../../../../utils/cast'
import { deviceFetch, pullImageToDevice, cidrToRange } from '../lib/deviceApi'
import {
  mergeMirrors,
  filterMirrorsByDevice, filterContainerMirrorsByDevice, filterMirrorsByVersion, filterLocalMirrorsByType, filterModelsByVersion,
  getMirrorTypeByUrl, parseAndroidVersionFromImage,
  isSpecialMirror,
  type DisplayMirror,
} from '../lib/CreateInstanceConstants'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'

function ipToNum(ip: string) {
  const p = ip.split('.')
  return ((parseInt(p[0]) << 24) | (parseInt(p[1]) << 16) | (parseInt(p[2]) << 8) | parseInt(p[3])) >>> 0
}

export interface UseUpdateImageStateParams {
  device: DeviceState
  android: AndroidInfo
  onUpdated: () => void
  onClose: () => void
}

export function useUpdateImageState({ device, android, onUpdated, onClose }: UseUpdateImageStateParams) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const deviceId = getDeviceId(device, mode)
  const { addTask, updateTask } = useTaskQueue()
  const [mirrors, setMirrors] = useState<MirrorItem[]>([])
  const [savedMirrors, setSavedMirrors] = useState<SavedMirror[]>([])
  const [phoneModels, setPhoneModels] = useState<PhoneModel[]>([])
  const [localModelNames, setLocalModelNames] = useState<string[]>([])
  const [selectedImage, setSelectedImage] = useState('')
  // 自定义镜像路径（selectedImage === '__custom__' 时生效，与创建流程同哨兵语义）
  const [customImageUrl, setCustomImageUrl] = useState('')
  // 机型来源类型（在线/本地）：与创建表单同款双框交互
  const [modelKind, setModelKind] = useState<'online' | 'local'>('online')
  const [selectedModelId, setSelectedModelId] = useState('random')
  const [selectedLocalModel, setSelectedLocalModel] = useState('')
  const [dnsType, setDnsType] = useState('223.5.5.5')
  const [customDns, setCustomDns] = useState('')
  const [sandboxSize, setSandboxSize] = useState('16GB')
  const [resolution, setResolution] = useState('720x1280x320')
  const [androidVersion, setAndroidVersion] = useState('14')
  const [enforce, setEnforce] = useState(true)
  const [enableMagisk, setEnableMagisk] = useState(false)
  const [enableGMS, setEnableGMS] = useState(false)
  const [enableBridge, setEnableBridge] = useState(false)
  const [macVlanIp, setMacVlanIp] = useState('')
  const [macVlanGw, setMacVlanGw] = useState('')
  const [macVlanSubnet, setMacVlanSubnet] = useState('')
  // 网卡类型（设备端 V1 支持 mytBridgeName，模拟器/容器都可切私有网卡）
  const [networkCardType, setNetworkCardType] = useState<'private' | 'public'>('public')
  const [mytBridgeName, setMytBridgeName] = useState('')
  const [privateNics, setPrivateNics] = useState<PrivateNic[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    const loadData = async () => {
      try { const m = await MirrorService.FetchMirrors(); setMirrors(m as MirrorItem[]) } catch {}
      try { const s = await ImageService.GetSavedMirrors(); setSavedMirrors(cast<SavedMirror[]>(s)) } catch {}
      try {
        const m = await deviceFetch(deviceId, '/android/phoneModel?page=0')
        const list = Array.isArray(m) ? m : (m?.list ?? [])
        setPhoneModels(cast<PhoneModel[]>(list))
      } catch {}
      try {
        const names = await PhoneModelService.GetLocalModelNames() as string[]
        if (Array.isArray(names)) setLocalModelNames(names)
      } catch {}
      try {
        const nic = await deviceFetch(deviceId, '/macvlan')
        const parseNic = (raw: Record<string, unknown>) => {
          if (!raw || typeof raw !== 'object') return { gw: '', subnet: '' }
          const config = (raw.IPAM as Record<string, unknown>)?.Config as Record<string, string>[] | undefined
          if (config?.[0]) return { gw: config[0].Gateway || '', subnet: config[0].Subnet || '' }
          return { gw: (raw.gw || raw.gateway) as string || '', subnet: raw.subnet as string || '' }
        }
        const info = nic as Record<string, unknown>
        if (info.macVlan) {
          const p = parseNic(info.macVlan as Record<string, unknown>)
          if (p.gw) setMacVlanGw(p.gw)
          if (p.subnet) setMacVlanSubnet(p.subnet)
        } else if (info.netWork_eth0) {
          const p = parseNic(info.netWork_eth0 as Record<string, unknown>)
          if (p.gw) setMacVlanGw(p.gw)
          if (p.subnet) setMacVlanSubnet(p.subnet)
        }
      } catch {}
      // 私有网卡列表（网卡类型切换用）
      try {
        const raw = await deviceFetch(deviceId, '/mytBridge')
        const list = Array.isArray(raw) ? raw : (raw?.list ?? [])
        setPrivateNics(cast<PrivateNic[]>(list))
      } catch {}
    }
    loadData()
  }, [deviceId])

  useEffect(() => {
    if (!android.image) return
    // 从 tag 提取安卓版本（P16_16_Xiaomi → 16；tag 无 andN 字样，旧正则恒落空默认 14）
    const v = parseAndroidVersionFromImage(android.image)
    if (v) setAndroidVersion(v)
    // 初始化选中当前云机的镜像（若在列表中）
    setSelectedImage(android.image)
  }, [android.image])

  const deviceTypeLower = device.device_type.toLowerCase()
  const bridgeRange = useMemo(() => macVlanSubnet ? cidrToRange(macVlanSubnet) : null, [macVlanSubnet])
  // 根据当前云机的 image 判定模拟器/容器（坑位模式同款判定：在线列表+本地已下载反查）
  const mirrorType = useMemo(
    () => getMirrorTypeByUrl(android.image, mirrors, savedMirrors),
    [android.image, mirrors, savedMirrors]
  )
  // unknown（镜像源下架/断网/自定义镜像）不再兜底猜模拟器——坑位视图对未知镜像也
  // 显示"未知"标签。此时不按类型过滤（只按设备机型+版本），弹窗顶部警示由调用方展示。
  const filteredMirrors = useMemo(
    () => mirrorType === 'container'
      ? filterContainerMirrorsByDevice(mirrors, device.device_type)
      : mirrorType === 'emulator'
        ? filterMirrorsByDevice(mirrors, device.device_type)
        : mirrors.filter(m => m.ttype2.some(t => t.trim().toLowerCase().startsWith(deviceTypeLower + '_'))),
    [mirrors, deviceTypeLower, mirrorType]
  )

  const displayMirrors: DisplayMirror[] = useMemo(() => {
    const osVer = `and${androidVersion}`
    const online = filterMirrorsByVersion(filteredMirrors, osVer)
    const local = filterLocalMirrorsByType(savedMirrors, device.device_type, osVer, mirrorType === 'container' ? 'container' : 'emulator')
    return mergeMirrors(online, local)
  }, [filteredMirrors, savedMirrors, androidVersion, deviceTypeLower, mirrorType])

  // 版本下拉已移除：更新只能选当前云机同版本的镜像（androidVersion 由 android.image 解析后锁定）。
  // 版本解析失败（自定义/未知镜像）时保持默认，用户可用「自定义镜像」直接填地址。

  const filteredModels = useMemo(() => {
    const byVer = filterModelsByVersion(phoneModels, androidVersion)
    // 特质版镜像：只允许选 Samsung_S24 机型
    const selectedMirror = filteredMirrors.find(m => m.url === selectedImage)
    if (selectedMirror && isSpecialMirror(selectedMirror)) {
      return byVer.filter(m => m.name === 'Samsung_S24')
    }
    return byVer
  }, [phoneModels, androidVersion, filteredMirrors, selectedImage])

  // 版本切换：若当前云机 image 在新版本列表里则保留，否则清空（由 displayMirrors effect 兜底选第一个）
  useEffect(() => {
    if (!displayMirrors.find(m => m.url === selectedImage)) setSelectedImage('')
  }, [androidVersion])
  // 当前选中镜像不在列表时，默认选第一个（自定义哨兵 '__custom__' 豁免，否则选了自定义会被顶掉）
  useEffect(() => {
    if (selectedImage === '__custom__') return
    if (displayMirrors.length > 0 && !displayMirrors.find(m => m.url === selectedImage)) {
      setSelectedImage(displayMirrors[0].url)
    }
  }, [displayMirrors])
  useEffect(() => {
    if (filteredModels.length === 0) return
    // 特质版镜像：强制选中 Samsung_S24
    const selectedMirror = filteredMirrors.find(m => m.url === selectedImage)
    if (selectedMirror && isSpecialMirror(selectedMirror)) {
      const s24 = filteredModels.find(m => m.name === 'Samsung_S24')
      if (s24 && selectedModelId !== s24.id) {
        setSelectedModelId(s24.id)
      }
      return
    }
    if (selectedModelId !== 'random' && !filteredModels.find(m => m.id === selectedModelId)) {
      setSelectedModelId('random')
    }
  }, [filteredModels, filteredMirrors, selectedImage])

  const resolvedDns = dnsType === 'custom' ? customDns : dnsType
  // 'model'：宽高/DPI 传空串，Go 侧 SwitchImageRequest 的 omitempty 会把它们从请求体省略，
  // 设备端按机型默认分辨率走（不传 ≠ 传 0，后者会被当成非法值）
  const [resW, resH, resDpi] = resolution === 'model' ? ['', '', ''] : resolution.split('x')

  const isValidBridgeIp = useMemo(() => {
    if (!enableBridge || !macVlanIp || !bridgeRange) return true
    try { const n = ipToNum(macVlanIp); return n >= ipToNum(bridgeRange.start) && n <= ipToNum(bridgeRange.end) } catch { return false }
  }, [enableBridge, macVlanIp, bridgeRange])

  // 实际生效的镜像地址：自定义哨兵时取输入框（与创建流程 useCreateInstance 同语义）
  const resolvedImage = selectedImage === '__custom__' ? customImageUrl.trim() : selectedImage

  // 'model'（跟随机型分辨率）放行：宽高/DPI 传空，Go 侧 omitempty 省略，设备端按机型默认走
  const canUpdate = !!resolvedImage && !!resolvedDns && isValidBridgeIp
    && (networkCardType === 'public' || !!mytBridgeName)

  const handleUpdate = async () => {
    if (!resolvedImage || !resolvedDns) return
    if (networkCardType === 'private' && !mytBridgeName) return
    if (enableBridge && macVlanIp && !isValidBridgeIp) { setError(t('host:update.bridgeIpRange', { start: bridgeRange!.start, end: bridgeRange!.end })); return }
    const taskId = await addTask({ type: 'updateImage', targets: [{ deviceIp: deviceId, name: android.name }], deviceIps: [deviceId], total: 1 })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    // 提交即关闭，进度在任务队列查看
    onClose()

    setTimeout(async () => {
    try {
      const images = await deviceFetch(deviceId, '/android/image')
      const list: { imageUrl: string }[] = Array.isArray(images) ? images : ((images as { list?: { imageUrl: string }[] })?.list ?? [])
      const imageExists = list.some(img => img.imageUrl === resolvedImage)
      if (!imageExists) {
        const local = savedMirrors.find(m => m.url === resolvedImage)
        if (local) {
          await ImageService.PushToDevices(local.id, [deviceId])
        } else {
          await pullImageToDevice(deviceId, resolvedImage, () => {})
        }
      }
      let resolvedModelId = selectedModelId
      if (selectedModelId === 'random') {
        if (filteredModels.length > 0) {
          const picked = filteredModels[Math.floor(Math.random() * filteredModels.length)]
          resolvedModelId = picked.id
        } else {
          try {
            const m = await deviceFetch(deviceId, '/android/phoneModel?page=0')
            const mList: PhoneModel[] = cast<PhoneModel[]>(Array.isArray(m) ? m : (m?.list ?? []))
            if (mList.length > 0) resolvedModelId = mList[0].id
          } catch {}
        }
      }
      // 本地机型：'__random__' 从列表随机挑一个（列表空等价不使用），推送后用设备注册名
      let resolvedLocalModel = selectedLocalModel
      if (selectedLocalModel === '__random__') {
        resolvedLocalModel = localModelNames.length > 0 ? localModelNames[Math.floor(Math.random() * localModelNames.length)] : ''
      }
      if (resolvedLocalModel) {
        const deviceName = await PhoneModelService.PushPhoneModel(deviceId, resolvedLocalModel) as string
        if (deviceName) resolvedLocalModel = deviceName
      }
      await SdkApiService.SwitchImage(
        deviceId, android.name, resolvedImage,
        resolvedModelId,
        filteredModels.find(m => m.id === resolvedModelId)?.name || '',
        resolvedLocalModel,
        resolvedDns, sandboxSize, resW, resH, resDpi,
        enforce,
        enableMagisk ? '1' : '0',
        enableGMS ? '1' : '0',
        '', networkCardType === 'private' ? mytBridgeName : '', networkCardType === 'public' && enableBridge ? macVlanIp : '',
      )
      ScreenshotService.InvalidateSlot(deviceId, android.indexNum).catch(() => {})
      emitSlotStatus(`${deviceId}_${android.indexNum}`, 'starting')
      onUpdated()
      updateTask(taskId, { status: 'completed', progress: 100, endTime: Date.now() })
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
    }
    }, 0)
  }

  return {
    mirrorType,
    networkCardType, setNetworkCardType, mytBridgeName, setMytBridgeName, privateNics,
    selectedImage, setSelectedImage,
    customImageUrl, setCustomImageUrl,
    modelKind, setModelKind,
    selectedModelId, setSelectedModelId,
    selectedLocalModel, setSelectedLocalModel,
    dnsType, setDnsType, customDns, setCustomDns,
    sandboxSize, setSandboxSize, resolution, setResolution,
    androidVersion,
    enforce, setEnforce,
    enableMagisk, setEnableMagisk,
    enableGMS, setEnableGMS,
    enableBridge, setEnableBridge,
    macVlanIp, setMacVlanIp,
    macVlanGw, bridgeRange,
    displayMirrors, filteredModels, localModelNames,
    error, canUpdate,
    handleUpdate,
  }
}
