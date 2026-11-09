import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import * as MirrorService from '../../../../bindings/github.com/moyunteng/myt/mirrorservicewrapper'
import * as ImageService from '../../../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import * as SlotService from '../../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import * as PhoneModelService from '../../../../bindings/github.com/moyunteng/myt/phonemodelservice'
import * as V2Service from '../../../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import { deviceFetch } from './lib/deviceApi'
import { cast } from '../../../utils/cast'
import { useDeployMode } from '../../../hooks/useDeployMode'
import type { FormState } from './useCreateInstanceForm'
import type { AndroidInfo, SlotInfo, PhoneModel, CountryCode, SavedMirror, MirrorItem } from '../../../types/index'
import type { PrivateNic, VpcGroup } from './lib/networkTypes'

export function useCreateInstanceData(form: FormState, deviceIP: string, deviceID: string, realIP?: string) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const isV2 = mode === 'v2'
  // 根据创建类型加载对应镜像（模拟器/容器严格区分）
  useEffect(() => {
    if (isV2) {
      // v2 回显：容器列表（已创建云机占用坑位）走 81 通道；坑位总数与壳子同款
      // 固定规则（P1=24 / 其它=12，GetSlotCount 纯本地前缀判断），不做推算——
      // 早期按容器推导（max(8, maxIdx+1)）会让已有容器的设备看起来只有 8 个坑位。
      // 镜像列表与 v3 同源（云端 get_mirror_list → GetCachedContainerMirrors）——
      // V2 Web 创建表单用的就是这一份（initMirrorList），81 get_img_list 只是主机 cfg 基础配置
      let cancelled = false
      ;(async () => {
        try {
          const list = await V2Service.ListAndroidsForDevice(deviceID, realIP || deviceIP)
          const androids = (list ?? []) as unknown as AndroidInfo[]
          if (cancelled) return
          form.setAndroids(androids)
        } catch { if (cancelled) return; form.setAndroids([]) }
        try {
          const c = await SlotService.GetSlotCount(deviceID) as number
          if (!cancelled) form.setSlotCountFallback(c || 12)
        } catch { if (!cancelled) form.setSlotCountFallback(12) }
      })()
      ;(async () => {
        try {
          const m = await MirrorService.GetCachedContainerMirrors()
          if (!cancelled) form.setMirrors(m as MirrorItem[])
        } catch { if (!cancelled) form.setMirrors([]) }
      })()
      return () => { cancelled = true }
    }
    const loadMirrors = async () => {
      try {
        if (form.createType === 'container') {
          const m = await MirrorService.GetCachedContainerMirrors()
          form.setMirrors(m as MirrorItem[])
        } else {
          const m = await MirrorService.GetCachedEmulatorMirrors()
          form.setMirrors(m as MirrorItem[])
        }
      } catch { form.setMirrors([]) }
    }
    loadMirrors()
  }, [form.createType, isV2, deviceIP])

  useEffect(() => {
    // v2：坑位数据走 SlotService（本地缓存），8000 专属数据（容器列表/机型/
    // 国家/网卡/VPC）全部不适用——deviceFetch 经翻译层会报不支持，直接跳过
    if (isV2) return
    const loadData = async () => {
      const errs: string[] = []
      try {
        const c = await SlotService.GetSlotCount(deviceID) as number
        if (c) form.setSlotCountFallback(c)
      } catch { errs.push(t('host:create.errSlotInfo')) }

      try { const s = await ImageService.GetSavedMirrors(); form.setSavedMirrors(cast<SavedMirror[]>(s)) } catch { errs.push(t('host:create.errLocalImage')) }

      try {
        const a = await deviceFetch(deviceIP, '/android')
        form.setAndroids(cast<AndroidInfo[]>(Array.isArray(a) ? a : (a?.list ?? [])))
      } catch { errs.push(t('host:create.errAndroidList')) }

      try {
        const result = await SlotService.QuerySlotInfo([deviceID]) as { slots: SlotInfo[] }[]
        if (result?.[0]?.slots) form.setSlotInfos(result[0].slots || [])
      } catch { /* slotExpiry 非关键 */ }

      // 模拟器才加载机型和国家
      if (form.createType === 'simulator') {
        try {
          const m = await deviceFetch(deviceIP, '/android/phoneModel?page=0')
          const list = Array.isArray(m) ? m : (m?.list ?? [])
          form.setPhoneModels(cast<PhoneModel[]>(list))
        } catch { errs.push(t('host:create.errModelList')) }

        try {
          const c = await deviceFetch(deviceIP, '/android/countryCode?page=0')
          const list = Array.isArray(c) ? c : (c?.list ?? [])
          form.setCountryCodes(cast<CountryCode[]>(list))
        } catch { errs.push(t('host:create.errCountryCode')) }

        try {
          const names = await PhoneModelService.GetLocalModelNames() as string[]
          if (Array.isArray(names)) form.setLocalModelNames(names)
        } catch { errs.push(t('host:create.errLocalModel')) }
      }

      try {
        const nic = await deviceFetch(deviceIP, '/macvlan')
        const parseNic = (raw: Record<string, unknown>) => {
          if (!raw || typeof raw !== 'object') return { gw: '', subnet: '' }
          const config = (raw.IPAM as Record<string, unknown>)?.Config as Record<string, string>[] | undefined
          if (config?.[0]) return { gw: config[0].Gateway || '', subnet: config[0].Subnet || '' }
          return { gw: (raw.gw || raw.gateway) as string || '', subnet: raw.subnet as string || '' }
        }
        const info = nic as Record<string, unknown>
        if (info.macVlan) {
          const p = parseNic(info.macVlan as Record<string, unknown>)
          if (p.gw) form.setMacVlanGw(p.gw)
          if (p.subnet) form.setMacVlanSubnet(p.subnet)
        } else if (info.netWork_eth0) {
          const p = parseNic(info.netWork_eth0 as Record<string, unknown>)
          if (p.gw) form.setMacVlanGw(p.gw)
          if (p.subnet) form.setMacVlanSubnet(p.subnet)
        }
      } catch {}

      if (errs.length > 0) form.setLoadErrors(errs)
    }
    loadData()
  }, [deviceIP, isV2])

  // 私有网卡列表：模拟器/容器都加载（设备端 V1 /android 也支持 mytBridgeName）；
  // VPC 分组仍容器专属（V2 接口体系）
  useEffect(() => {
    if (isV2) return // v2：网卡/VPC 数据源不存在（8000 特有），创建表单也不展示
    if (form.createType !== 'container' && form.createType !== 'simulator') return
    const loadContainerNetwork = async () => {
      const errs: string[] = []
      try {
        const raw = await deviceFetch(deviceIP, '/mytBridge')
        const list = Array.isArray(raw) ? raw : (raw?.list ?? [])
        form.setPrivateNics(cast<PrivateNic[]>(list))
      } catch { errs.push(t('host:create.errPrivateNic')) }

      if (form.createType === 'container') {
        try {
          const raw = await deviceFetch(deviceIP, '/mytVpc/group')
          const list = Array.isArray(raw) ? raw : (raw?.list ?? [])
          form.setVpcGroups(cast<VpcGroup[]>(list))
        } catch { errs.push(t('host:create.errVpcNetwork')) }
      }

      if (errs.length > 0) {
        form.setLoadErrors(Array.from(new Set([...form.loadErrors, ...errs])))
      }
    }
    loadContainerNetwork()
  }, [deviceIP, form.createType, isV2])
}
