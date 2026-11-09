import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { emitSlotStatus } from '../../../../utils/wailsEvent'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../utils/deviceId'
import { incrementIp } from '../../create/lib/deviceApi'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as MirrorService from '../../../../../bindings/github.com/moyunteng/myt/mirrorservicewrapper'
import * as ImageService from '../../../../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import * as ScreenshotService from '../../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import * as PhoneModelService from '../../../../../bindings/github.com/moyunteng/myt/phonemodelservice'
import { DeviceState, MirrorItem, AndroidInfo, SavedMirror, PhoneModel } from '../../../../types/index'
import type { PrivateNic } from '../../create/lib/networkTypes'
import { getErrorMessage } from '../../../../utils/error'
import { cast } from '../../../../utils/cast'
import { deviceFetch, pullImageToDevice, cidrToRange } from '../../create/lib/deviceApi'
import {
  mergeMirrors,
  filterMirrorsByDevice, filterContainerMirrorsByDevice, filterMirrorsByVersion, filterLocalMirrorsByType, filterModelsByVersion,
  getMirrorTypeByUrl, parseAndroidVersionFromImage,
  type DisplayMirror,
} from '../../create/lib/CreateInstanceConstants'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'

interface Params {
  device: DeviceState
  selectedAndroids: AndroidInfo[]
  onUpdated: () => void
  onClose: () => void
}

export function useBatchUpdateImageState({ device, selectedAndroids, onUpdated, onClose }: Params) {
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

  // 从选中实例镜像推导安卓版本（取第一个有版本的；tag 无 andN 字样，用 P/Q 前缀规律提取）
  useEffect(() => {
    for (const a of selectedAndroids) {
      const v = a.image ? parseAndroidVersionFromImage(a.image) : ''
      if (v) { setAndroidVersion(v); return }
    }
  }, [selectedAndroids])

  // 初始化选中第一个云机的镜像（若在列表中）
  useEffect(() => {
    if (selectedAndroids.length > 0 && selectedAndroids[0].image) {
      setSelectedImage(selectedAndroids[0].image)
    }
  }, [selectedAndroids])

  const deviceTypeLower = device.device_type.toLowerCase()
  const bridgeRange = useMemo(() => macVlanSubnet ? cidrToRange(macVlanSubnet) : null, [macVlanSubnet])
  // 批量场景取第一个云机的 image 判定类型（混选已被 ActionBar hasMixedBatch 拦截，
  // 能进到这里的选中云机镜像类型一致）。unknown 不兜底猜模拟器（坑位视图同款语义）：
  // 显示全部类型镜像 + 弹窗警示，由用户自行选择。
  const mirrorType = useMemo(
    () => selectedAndroids.length > 0
      ? getMirrorTypeByUrl(selectedAndroids[0].image, mirrors, savedMirrors)
      : 'unknown',
    [selectedAndroids, mirrors, savedMirrors]
  )
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

  // 版本下拉已移除：更新只能选同版本镜像（androidVersion 由选中云机 image 推导后锁定，
  // 混选不同版本已被 ActionBar 拦截）。版本解析失败时保持默认，可用「自定义镜像」填地址。

  const filteredModels = useMemo(() => filterModelsByVersion(phoneModels, androidVersion), [phoneModels, androidVersion])

  // 版本切换：当前选中镜像不在新版本列表里则清空（由 displayMirrors effect 兜底选第一个）
  useEffect(() => {
    if (!displayMirrors.find(m => m.url === selectedImage)) setSelectedImage('')
  }, [androidVersion])
  // 当前选中镜像不在列表时默认选第一个（自定义哨兵 '__custom__' 豁免，否则选了自定义会被顶掉）
  useEffect(() => {
    if (selectedImage === '__custom__') return
    if (displayMirrors.length > 0 && !displayMirrors.find(m => m.url === selectedImage)) {
      setSelectedImage(displayMirrors[0].url)
    }
  }, [displayMirrors])
  useEffect(() => {
    if (filteredModels.length > 0 && selectedModelId !== 'random' && !filteredModels.find(m => m.id === selectedModelId)) {
      setSelectedModelId('random')
    }
  }, [filteredModels])

  const resolvedDns = dnsType === 'custom' ? customDns : dnsType
  // 'model'：宽高/DPI 传空串，Go 侧 SwitchImageRequest 的 omitempty 会把它们从请求体省略，
  // 设备端按机型默认分辨率走（不传 ≠ 传 0，后者会被当成非法值）
  const [resW, resH, resDpi] = resolution === 'model' ? ['', '', ''] : resolution.split('x')
  // 实际生效的镜像地址：自定义哨兵时取输入框（与创建流程 useCreateInstance 同语义）
  const resolvedImage = selectedImage === '__custom__' ? customImageUrl.trim() : selectedImage
  // 'model'（跟随机型分辨率）放行：宽高/DPI 传空，Go 侧 omitempty 省略，设备端按机型默认走
  const canUpdate = !!resolvedImage && !!resolvedDns
    && (networkCardType === 'public' || !!mytBridgeName)

  const handleBatchUpdate = async () => {
    if (!resolvedImage || !resolvedDns) return
    if (selectedAndroids.length === 0) return
    const targets = selectedAndroids.map(a => ({ deviceIp: getDeviceId(a, mode), name: a.name }))
    const taskId = await addTask({ type: 'updateImage', targets, deviceIps: [deviceId], total: selectedAndroids.length })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    onClose()

    setTimeout(async () => {
    try {
      // 镜像是否已在设备上
      const images = await deviceFetch(deviceId, '/android/image')
      const list: { imageUrl: string }[] = Array.isArray(images) ? images : ((images as { list?: { imageUrl: string }[] })?.list ?? [])
      if (!list.some(img => img.imageUrl === resolvedImage)) {
        const local = savedMirrors.find(m => m.url === resolvedImage)
        if (local) await ImageService.PushToDevices(local.id, [deviceId])
        else await pullImageToDevice(deviceId, resolvedImage, () => {})
      }
      // 机型逐台解析：随机 = 每台云机独立挑选（与创建流程同语义），不再全员统一。
      // 本地机型随机懒推送：按机型去重缓存注册名，同一 zip 不重复上传（文件导入 5 分钟级）。
      const localModelRandom = selectedLocalModel === '__random__'
      const fixedLocalModel = selectedLocalModel && !localModelRandom ? selectedLocalModel : ''
      let resolvedLocalModel = fixedLocalModel
      if (fixedLocalModel) {
        const deviceName = await PhoneModelService.PushPhoneModel(deviceId, fixedLocalModel) as string
        if (deviceName) resolvedLocalModel = deviceName
      }
      const localRegistered = new Map<string, string>()
      const resolveRandomLocalModel = async (modelName: string): Promise<string> => {
        if (!localRegistered.has(modelName)) {
          const deviceName = await PhoneModelService.PushPhoneModel(deviceId, modelName) as string
          localRegistered.set(modelName, deviceName || modelName)
        }
        return localRegistered.get(modelName)!
      }
      // 随机本地机型列表为空时等价不使用（走线上机型）
      const resolveLocalModel = localModelRandom
        ? async () => {
          const picked = localModelNames.length > 0 ? localModelNames[Math.floor(Math.random() * localModelNames.length)] : ''
          return picked ? await resolveRandomLocalModel(picked) : ''
        }
        : async () => resolvedLocalModel
      // 逐个更新（启用桥接时 macVlanIp 逐台递增，避免多台 IP 冲突）
      let completed = 0, failed = 0
      for (const [idx, a] of selectedAndroids.entries()) {
        try {
          let resolvedModelId = selectedModelId
          if (selectedModelId === 'random') {
            if (filteredModels.length > 0) {
              resolvedModelId = filteredModels[Math.floor(Math.random() * filteredModels.length)].id
            } else {
              try {
                const m = await deviceFetch(deviceId, '/android/phoneModel?page=0')
                const mList: PhoneModel[] = cast<PhoneModel[]>(Array.isArray(m) ? m : (m?.list ?? []))
                if (mList.length > 0) resolvedModelId = mList[0].id
              } catch {}
            }
          }
          const slotLocalModel = await resolveLocalModel()
          const modelName = slotLocalModel || filteredModels.find(m => m.id === resolvedModelId)?.name || ''
          await SdkApiService.SwitchImage(
            deviceId, a.name, resolvedImage,
            resolvedModelId, modelName, slotLocalModel,
            resolvedDns, sandboxSize, resW, resH, resDpi,
            enforce,
            enableMagisk ? '1' : '0',
            enableGMS ? '1' : '0',
            '', networkCardType === 'private' ? mytBridgeName : '', networkCardType === 'public' && enableBridge ? incrementIp(macVlanIp, idx) : '',
          )
          ScreenshotService.InvalidateSlot(deviceId, a.indexNum).catch(() => {})
          emitSlotStatus(`${deviceId}_${a.indexNum}`, 'starting')
          completed++
        } catch {
          failed++
        }
        const pct = Math.round((completed + failed) / selectedAndroids.length * 100)
        updateTask(taskId, { progress: pct, completed, failed, status: 'running' })
      }
      onUpdated()
      updateTask(taskId, { status: failed === 0 ? 'completed' : 'failed', progress: 100, completed, failed, endTime: Date.now() })
      if (failed > 0) setError(t('host:toast.batchUpdateFailed', { count: failed }))
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
    handleBatchUpdate,
  }
}
