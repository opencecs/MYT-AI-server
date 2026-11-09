import { useMemo, useEffect, useState } from 'react'
import type { AndroidInfo, SlotExpiry, PhoneModel, MirrorItem } from '../../../../types/index'
import { filterMirrorsByDevice, filterContainerMirrorsByDevice, filterMirrorsByVersion, filterLocalMirrorsByType, filterModelsByVersion, mergeMirrors, isSpecialMirror, ANDROID_VERSIONS, type DisplayMirror } from './CreateInstanceConstants'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getLastCreateSlots } from '../../../../hooks/useSlotMemory'
import * as V2Service from '../../../../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import type { FormState } from '../useCreateInstanceForm'

export interface DerivedState {
  allSlots: number[]
  androidsBySlot: Map<number, AndroidInfo[]>
  expiryMap: Map<number, SlotExpiry>
  anySlotHasRunning: boolean
  filteredMirrors: MirrorItem[]
  displayMirrors: DisplayMirror[]
  availableVersions: string[]  // 可选安卓版本（写死，与镜像源对齐）
  versionMirrorCount: Record<string, number>  // 各版本镜像数（左栏无镜像版本置灰）
  filteredModels: PhoneModel[]
  resolvedDns: string
  resW: string
  resH: string
  resDpi: string
  canCreate: boolean
  isLocalImage: (url: string) => boolean
  toggleSlot: (idx: number) => void
  slotBorderColor: (idx: number) => string
  slotDotColor: (idx: number) => string
}

export function useDerivedState(form: FormState, deviceType: string, slotExpiry: SlotExpiry[], defaultSlot: number | undefined, deviceName: string, deviceIP: string, slotMemoryEnabled = false): DerivedState {
  const maxSlot = form.slotCountFallback
  const allSlots = Array.from({ length: maxSlot }, (_, i) => i + 1)

  const androidsBySlot = useMemo(() => {
    const map = new Map<number, AndroidInfo[]>()
    for (const a of form.androids) {
      const list = map.get(a.indexNum) || []
      list.push(a)
      map.set(a.indexNum, list)
    }
    return map
  }, [form.androids])

  const expiryMap = useMemo(() => {
    const map = new Map<number, SlotExpiry>()
    for (const s of slotExpiry) map.set(s.index, s)
    return map
  }, [slotExpiry])

  const anySlotHasRunning = useMemo(() =>
    form.selectedSlots.some(s => (androidsBySlot.get(s) || []).some(a => a.status === 'running')),
    [androidsBySlot, form.selectedSlots]
  )

  const deviceTypeLower = deviceType.toLowerCase()
  const isSimulator = form.createType === 'simulator'
  const { mode } = useDeployMode()
  const isV2 = mode === 'v2'
  // v2：壳子 dType 是推送串第 4 段完整型号（如 r1p_v2）。device_name 保留完整
  // 型号，device_type 是发现层截断值（r1p_v2→R1P）——优先 device_name，空则退
  // device_type，统一小写比较。
  const v2Model = (deviceName || deviceType || '').toLowerCase()

  // v2：主机安卓版本探测（81 get_key_value ANDROID_VERSION）。打开弹窗/设备
  // 切换时异步探测；undefined=探测中（用默认 'and12' 过滤，避免先全量后收窄的
  // 闪烁）、''=探测失败（跳过 os_ver 过滤，展示各版本——旧 mytsdk 无此接口时
  // 不至于全空）、有值=严格按值过滤；cancelled 标记保证设备切换时旧响应不覆盖。
  const [hostAndroidVer, setHostAndroidVer] = useState<string | undefined>(undefined)
  useEffect(() => {
    if (!isV2 || !deviceIP) return
    let cancelled = false
    V2Service.GetHostAndroidVersion(deviceIP)
      .then((v: unknown) => { if (!cancelled) setHostAndroidVer(typeof v === 'string' ? v : '') })
      .catch(() => { if (!cancelled) setHostAndroidVer('') })
    return () => { cancelled = true }
  }, [isV2, deviceIP])

  // 壳子同款特例：p1 固定 and14；p1_10 探测到 and12（含探测失败取默认）强制 and14。
  // null=探测失败，不过滤版本。
  const v2AndroidVersion = useMemo(() => {
    if (v2Model === 'p1') return 'and14'
    if (hostAndroidVer === undefined) return 'and12'
    if (hostAndroidVer === '') return null
    if (v2Model === 'p1_10' && hostAndroidVer === 'and12') return 'and14'
    return hostAndroidVer
  }, [v2Model, hostAndroidVer])

  // v2：壳子同款分类（ttype==dType / ttype2 含 dType / q1 兼容 C1），sort 降序。
  // 型号未知（旧数据 device_type=v2host 或空）时不过滤机型——宁多勿缺。
  // os_ver 过滤：镜像 os_ver 必须等于主机安卓版本（=== 语义，os_ver 为空的
  // 镜像被过滤，与壳子一致）。
  const filteredMirrors = useMemo(() => {
    if (isV2) {
      const model = v2Model
      const byModel = model && model !== 'v2host'
        ? form.mirrors.filter(m => {
            const tt = (m.ttype || '').toLowerCase()
            const t2 = (m.ttype2 || []).map(x => (x || '').toLowerCase())
            if ((model === '' || model === 'q1') && tt === 'c1') return true
            return tt === model || t2.includes(model)
          })
        : form.mirrors
      // 探测失败（null）时跳过 os_ver 过滤；否则严格 === 主机版本
      const byVer = v2AndroidVersion == null
        ? byModel
        : byModel.filter(m => (m.os_ver || '') === v2AndroidVersion)
      return [...byVer].sort((a, b) => (b.sort ?? 0) - (a.sort ?? 0))
    }
    return isSimulator ? filterMirrorsByDevice(form.mirrors, deviceType) : filterContainerMirrorsByDevice(form.mirrors, deviceType)
  }, [form.mirrors, deviceTypeLower, isSimulator, isV2, v2Model, v2AndroidVersion])

  const displayMirrors = useMemo(() => {
    if (isV2) {
      // v2：原样展示 + type=1 付费镜像加「专业版」后缀（壳子同款）；自定义地址入口由 MirrorDropdown 自带
      return filteredMirrors.map(m => ({
        id: m.id,
        name: m.type === 1 ? `${m.name}（专业版）` : m.name,
        url: m.url,
        os_ver: m.os_ver,
        source: 'online' as const,
      }))
    }
    const osVer = `and${form.androidVersion}`
    if (!isSimulator) {
      // 容器模式：按版本过滤，合并在线+本地
      const online = filterMirrorsByVersion(filteredMirrors, osVer)
      const localMirrors = filterLocalMirrorsByType(form.savedMirrors, deviceType, osVer, 'container')
      return mergeMirrors(online, localMirrors)
    }
    const online = filterMirrorsByVersion(filteredMirrors, osVer)
    const local = filterLocalMirrorsByType(form.savedMirrors, deviceType, osVer, 'emulator')
    return mergeMirrors(online, local)
  }, [filteredMirrors, form.savedMirrors, form.androidVersion, deviceTypeLower, isSimulator, isV2])

  // 可选安卓版本：写死（与镜像源对齐，不从过滤镜像提取以免丢版本）
  const availableVersions = ANDROID_VERSIONS

  // 各版本镜像数（左栏无镜像版本置灰用）
  const versionMirrorCount = useMemo(() => {
    const m: Record<string, number> = {}
    for (const v of ANDROID_VERSIONS) {
      m[v] = filteredMirrors.filter(mir => mir.os_ver === `and${v}`).length
    }
    return m
  }, [filteredMirrors])

  const filteredModels = useMemo(() => {
    const byVer = filterModelsByVersion(form.phoneModels, form.androidVersion)
    // 特质版镜像：只允许选 Samsung_S24 机型
    const selectedMirror = filteredMirrors.find(m => m.url === form.selectedImage)
    if (selectedMirror && isSpecialMirror(selectedMirror)) {
      return byVer.filter(m => m.name === 'Samsung_S24')
    }
    return byVer
  }, [form.phoneModels, form.androidVersion, filteredMirrors, form.selectedImage])

  // 默认选择
  useEffect(() => { form.setSelectedImage('') }, [form.androidVersion])
  useEffect(() => {
    if (filteredModels.length === 0) return
    // 特质版镜像：强制选中 Samsung_S24
    const selectedMirror = filteredMirrors.find(m => m.url === form.selectedImage)
    if (selectedMirror && isSpecialMirror(selectedMirror)) {
      const s24 = filteredModels.find(m => m.name === 'Samsung_S24')
      if (s24 && form.selectedModelId !== s24.id) {
        form.setSelectedModelId(s24.id)
      }
      return
    }
    if (form.selectedModelId !== 'random' && !filteredModels.find(m => m.id === form.selectedModelId)) {
      form.setSelectedModelId('random')
    }
  }, [filteredModels, filteredMirrors, form.selectedImage])
  useEffect(() => {
    if (form.selectedImage === '__custom__') return
    if (displayMirrors.length > 0 && !displayMirrors.find(m => m.url === form.selectedImage)) {
      form.setSelectedImage(displayMirrors[0].url)
    }
  }, [displayMirrors])
  useEffect(() => {
    if (form.selectedSlots.length === 0) {
      // 优先级：来源坑位（截图模式空坑位卡「创建」/坑位表「创建」传入）> 坑位记忆
      // > 什么都不选。defaultSlot 有效（在可用坑位范围内）时直接选中。
      if (defaultSlot && defaultSlot > 0 && allSlots.includes(defaultSlot)) {
        form.setSelectedSlots([defaultSlot])
        return
      }
      // 坑位记忆开启：回填上一次创建选择的坑位（按当前设备可用坑位过滤有效性，
      // 并过滤已占用坑位——androidsBySlot 中有条目的坑位不可再创建；
      // 全部无效则保持不选）
      if (slotMemoryEnabled) {
        const remembered = getLastCreateSlots()
          .filter(s => allSlots.includes(s) && !androidsBySlot.has(s))
        if (remembered.length > 0) {
          form.setSelectedSlots(remembered)
          return
        }
      }
      // 记忆关闭 / 无有效记忆：默认不选任何坑位（用户手动选择后才可创建）
    }
  }, [maxSlot, androidsBySlot.size, defaultSlot, slotMemoryEnabled])

  const resolvedDns = form.dnsType === 'custom' ? form.customDns : form.dnsType
  const [resW, resH, resDpi] = useMemo(() => {
    if (form.resolution === 'model') return ['', '', '']
    if (form.resolution === 'custom') {
      return [form.customResolution.width, form.customResolution.height, form.customResolution.dpi]
    }
    return form.resolution.split('x')
  }, [form.resolution, form.customResolution])
  const customResValid = !!form.customResolution.width && !!form.customResolution.height && !!form.customResolution.dpi
  // 'model'（跟随机型分辨率）放行：此时请求体不传宽高/DPI，由设备端按机型默认走
  const canCreate = !!((form.selectedImage === '__custom__' ? form.customImageUrl.trim() : form.selectedImage) && form.selectedSlots.length > 0 && resolvedDns
    && (form.resolution !== 'custom' || customResValid))

  const isLocalImage = (url: string) => form.savedMirrors.some(m => m.url === url)

  const toggleSlot = (idx: number) => {
    form.setSelectedSlots(prev => {
      if (prev.includes(idx)) return prev.filter(s => s !== idx)
      return [...prev, idx].sort((a, b) => a - b)
    })
  }

  const slotBorderColor = (idx: number) => {
    const exp = expiryMap.get(idx)
    if (exp?.state === 0) return 'border-blue-400'
    if (exp?.state === 1) return 'border-yellow-400'
    if (exp?.state === 2) return 'border-red-400'
    return 'border-gray-300'
  }
  const slotDotColor = (idx: number) => {
    const exp = expiryMap.get(idx)
    if (exp?.state === 0) return 'bg-blue-400'
    if (exp?.state === 1) return 'bg-yellow-400'
    if (exp?.state === 2) return 'bg-red-400'
    return 'bg-gray-300'
  }

  return {
    allSlots, androidsBySlot, expiryMap, anySlotHasRunning,
    filteredMirrors, displayMirrors, availableVersions, versionMirrorCount, filteredModels,
    resolvedDns, resW, resH, resDpi, canCreate, isLocalImage,
    toggleSlot, slotBorderColor, slotDotColor,
  }
}
