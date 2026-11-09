import { useState, useMemo, useRef, useEffect } from 'react'
import type { AndroidInfo, SlotInfo, PhoneModel, CountryCode, SavedMirror, MirrorItem } from '../../../types/index'
import { cidrToRange } from './lib/deviceApi'
import { DEFAULT_RESOLUTION, resolutionOptions } from './lib/CreateInstanceConstants'
import type { PrivateNic, VpcGroup } from './lib/networkTypes'

export interface FormState {
  // 加载的数据
  mirrors: MirrorItem[]
  savedMirrors: SavedMirror[]
  androids: AndroidInfo[]
  slotInfos: SlotInfo[]
  slotCountFallback: number
  phoneModels: PhoneModel[]
  countryCodes: CountryCode[]
  loadErrors: string[]
  setMirrors: (v: MirrorItem[]) => void
  setSavedMirrors: (v: SavedMirror[]) => void
  setAndroids: (v: AndroidInfo[]) => void
  setSlotInfos: (v: SlotInfo[]) => void
  setSlotCountFallback: (v: number) => void
  setPhoneModels: (v: PhoneModel[]) => void
  setCountryCodes: (v: CountryCode[]) => void
  setLoadErrors: (v: string[]) => void
  // 创建类型
  createType: 'simulator' | 'container'; setCreateType: (v: 'simulator' | 'container') => void
  // 表单字段
  name: string; setName: (v: string) => void
  remark: string; setRemark: (v: string) => void
  dnsType: string; setDnsType: (v: string) => void
  customDns: string; setCustomDns: (v: string) => void
  sandboxSize: string; setSandboxSize: (v: string) => void
  // 容器沙盒模式开关（关闭时 sandboxSize 传空字符串）
  sandboxMode: boolean; setSandboxMode: (v: boolean) => void
  selectedSlots: number[]; setSelectedSlots: (v: number[] | ((prev: number[]) => number[])) => void
  count: number; setCount: (v: number) => void
  resolution: string; setResolution: (v: string) => void
  androidVersion: string; setAndroidVersion: (v: string) => void
  selectedImage: string; setSelectedImage: (v: string) => void
  customImageUrl: string; setCustomImageUrl: (v: string) => void
  // 机型来源类型（在线/本地）：决定机型选择区右框展示哪类列表
  modelKind: 'online' | 'local'; setModelKind: (v: 'online' | 'local') => void
  selectedModelId: string; setSelectedModelId: (v: string) => void
  localModelNames: string[]; setLocalModelNames: (v: string[]) => void
  selectedLocalModel: string; setSelectedLocalModel: (v: string) => void
  selectedCountry: string; setSelectedCountry: (v: string) => void
  enableMagisk: boolean; setEnableMagisk: (v: boolean) => void
  enableGMS: boolean; setEnableGMS: (v: boolean) => void
  enforce: boolean; setEnforce: (v: boolean) => void
  randomFile: boolean; setRandomFile: (v: boolean) => void
  compatMode: boolean; setCompatMode: (v: boolean) => void
  enableBridge: boolean; setEnableBridge: (v: boolean) => void
  macVlanIp: string; setMacVlanIp: (v: string) => void
  macVlanGw: string; setMacVlanGw: (v: string) => void
  macVlanSubnet: string; setMacVlanSubnet: (v: string) => void
  // 容器网络体系：网卡类型（私有/公有）
  networkCardType: 'private' | 'public'; setNetworkCardType: (v: 'private' | 'public') => void
  mytBridgeName: string; setMytBridgeName: (v: string) => void
  privateNics: PrivateNic[]; setPrivateNics: (v: PrivateNic[]) => void
  // 容器 VPC 网络
  vpcGroups: VpcGroup[]; setVpcGroups: (v: VpcGroup[]) => void
  vpcGroupId: string; setVpcGroupId: (v: string) => void
  vpcNodeId: string; setVpcNodeId: (v: string) => void
  vpcSelectMode: 'specified' | 'random'; setVpcSelectMode: (v: 'specified' | 'random') => void
  // 容器自定义分辨率
  customResolution: { width: string; height: string; dpi: string }; setCustomResolution: (v: { width: string; height: string; dpi: string }) => void
  // 创建过程
  creating: boolean; setCreating: (v: boolean) => void
  createStep: string; setCreateStep: (v: string) => void
  createProgress: number; setCreateProgress: (v: number) => void
  createProgressText: string; setCreateProgressText: (v: string) => void
  abortRef: React.MutableRefObject<AbortController | null>
  error: string; setError: (v: string) => void
  slotConfirm: { message: string; onConfirm: () => void } | null
  setSlotConfirm: (v: { message: string; onConfirm: () => void } | null) => void
  // 桥接
  bridgeRange: { start: string; end: string } | null
  // 国家搜索
  countrySearch: string; setCountrySearch: (v: string) => void
  countryOpen: boolean; setCountryOpen: (v: boolean) => void
  countryRef: React.RefObject<HTMLDivElement | null>
  countryPanelRef: React.RefObject<HTMLDivElement | null>
  filteredCountryCodes: CountryCode[]
  selectedCountryName: string
}

export function useCreateInstanceForm(deviceType: string): FormState {
  const [mirrors, setMirrors] = useState<MirrorItem[]>([])
  const [savedMirrors, setSavedMirrors] = useState<SavedMirror[]>([])
  const [androids, setAndroids] = useState<AndroidInfo[]>([])
  const [slotInfos, setSlotInfos] = useState<SlotInfo[]>([])
  const [slotCountFallback, setSlotCountFallback] = useState(12)
  const [phoneModels, setPhoneModels] = useState<PhoneModel[]>([])
  const [countryCodes, setCountryCodes] = useState<CountryCode[]>([])
  const [loadErrors, setLoadErrors] = useState<string[]>([])

  const [createType, setCreateTypeState] = useState<'simulator' | 'container'>('simulator')
  const [name, setName] = useState('')
  const [remark, setRemark] = useState('')
  const [dnsType, setDnsType] = useState('223.5.5.5')
  const [customDns, setCustomDns] = useState('')
  const [sandboxSize, setSandboxSize] = useState('16GB')
  const [sandboxMode, setSandboxMode] = useState(true)
  const [selectedSlots, setSelectedSlots] = useState<number[]>([])
  const [count, setCount] = useState(1)
  const [resolution, setResolution] = useState(DEFAULT_RESOLUTION)
  // 切换 Tab 时校正分辨率：按目标 Tab 的可选项校验当前值，不合法则重置为默认值。
  // 覆盖两个方向：模拟器下选了 'model' 再切到容器 Tab（容器不展示 'model'），
  // 以及容器下选了 'custom' 再切回模拟器且自定义分辨率未填等。
  const setCreateType = (v: 'simulator' | 'container') => {
    setCreateTypeState(v)
    setResolution(prev => resolutionOptions(v === 'simulator').some(o => o.value === prev) ? prev : DEFAULT_RESOLUTION)
  }
  const [androidVersion, setAndroidVersion] = useState('14')
  const [selectedImage, setSelectedImage] = useState('')
  const [customImageUrl, setCustomImageUrl] = useState('')
  const [modelKind, setModelKind] = useState<'online' | 'local'>('online')
  const [selectedModelId, setSelectedModelId] = useState('random')
  const [localModelNames, setLocalModelNames] = useState<string[]>([])
  const [selectedLocalModel, setSelectedLocalModel] = useState('')
  const [selectedCountry, setSelectedCountry] = useState('CN')
  const [enableMagisk, setEnableMagisk] = useState(false)
  const [enableGMS, setEnableGMS] = useState(false)
  const [enforce, setEnforce] = useState(true)
  const [randomFile, setRandomFile] = useState(false)
  const [compatMode, setCompatMode] = useState(false)
  const [enableBridge, setEnableBridge] = useState(false)
  const [macVlanIp, setMacVlanIp] = useState('')
  const [macVlanGw, setMacVlanGw] = useState('')
  const [macVlanSubnet, setMacVlanSubnet] = useState('')
  const [networkCardType, setNetworkCardType] = useState<'private' | 'public'>('public')
  const [mytBridgeName, setMytBridgeName] = useState('')
  const [privateNics, setPrivateNics] = useState<PrivateNic[]>([])
  const [vpcGroups, setVpcGroups] = useState<VpcGroup[]>([])
  const [vpcGroupId, setVpcGroupId] = useState('')
  const [vpcNodeId, setVpcNodeId] = useState('')
  const [vpcSelectMode, setVpcSelectMode] = useState<'specified' | 'random'>('specified')
  const [customResolution, setCustomResolution] = useState({ width: '', height: '', dpi: '' })

  const [creating, setCreating] = useState(false)
  const [createStep, setCreateStep] = useState('')
  const [createProgress, setCreateProgress] = useState(-1)
  const [createProgressText, setCreateProgressText] = useState('')
  const abortRef = useRef<AbortController | null>(null)
  const [error, setError] = useState('')
  const [slotConfirm, setSlotConfirm] = useState<{ message: string; onConfirm: () => void } | null>(null)

  const [countrySearch, setCountrySearch] = useState('')
  const [countryOpen, setCountryOpen] = useState(false)
  const countryRef = useRef<HTMLDivElement>(null)
  const countryPanelRef = useRef<HTMLDivElement>(null)

  const filteredCountryCodes = useMemo(() => {
    if (!countrySearch) return countryCodes
    const q = countrySearch.toLowerCase()
    return countryCodes.filter(c =>
      c.countryName.toLowerCase().includes(q) || c.countryCode.toLowerCase().includes(q)
    )
  }, [countryCodes, countrySearch])

  const selectedCountryName = useMemo(() => {
    const found = countryCodes.find(c => c.countryCode === selectedCountry)
    return found ? `${found.countryName} (${found.countryCode})` : selectedCountry
  }, [countryCodes, selectedCountry])

  const bridgeRange = useMemo(() => macVlanSubnet ? cidrToRange(macVlanSubnet) : null, [macVlanSubnet])

  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      const t = e.target as Node
      // 下拉面板用 createPortal 渲染到 document.body，脱离 countryRef 子树，
      // 必须同时检查面板 ref，否则点选项的瞬间会被外部点击逻辑误关闭，导致无法选择。
      const inTrigger = countryRef.current?.contains(t)
      const inPanel = countryPanelRef.current?.contains(t)
      if (!inTrigger && !inPanel) setCountryOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  return {
    mirrors, savedMirrors, androids, slotInfos, slotCountFallback, phoneModels, countryCodes, loadErrors,
    setMirrors, setSavedMirrors, setAndroids, setSlotInfos, setSlotCountFallback, setPhoneModels, setCountryCodes, setLoadErrors,
    createType, setCreateType,
    name, setName, remark, setRemark, dnsType, setDnsType, customDns, setCustomDns, sandboxSize, setSandboxSize, sandboxMode, setSandboxMode,
    selectedSlots, setSelectedSlots, count, setCount, resolution, setResolution, androidVersion, setAndroidVersion,
    selectedImage, setSelectedImage, customImageUrl, setCustomImageUrl, modelKind, setModelKind, selectedModelId, setSelectedModelId, localModelNames, setLocalModelNames,
    selectedLocalModel, setSelectedLocalModel, selectedCountry, setSelectedCountry,
    enableMagisk, setEnableMagisk, enableGMS, setEnableGMS, enforce, setEnforce, randomFile, setRandomFile, compatMode, setCompatMode,
    enableBridge, setEnableBridge, macVlanIp, setMacVlanIp, macVlanGw, setMacVlanGw, macVlanSubnet, setMacVlanSubnet,
    networkCardType, setNetworkCardType, mytBridgeName, setMytBridgeName, privateNics, setPrivateNics,
    vpcGroups, setVpcGroups, vpcGroupId, setVpcGroupId, vpcNodeId, setVpcNodeId, vpcSelectMode, setVpcSelectMode,
    customResolution, setCustomResolution,
    creating, setCreating, createStep, setCreateStep, createProgress, setCreateProgress, createProgressText, setCreateProgressText,
    abortRef, error, setError, slotConfirm, setSlotConfirm,
    bridgeRange, countrySearch, setCountrySearch, countryOpen, setCountryOpen, countryRef, countryPanelRef,
    filteredCountryCodes, selectedCountryName,
  }
}
