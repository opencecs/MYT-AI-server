import { useEffect, useRef, useState, useCallback } from 'react'
import { Events } from '@wailsio/runtime'
import * as VpcApi from '../../../api/vpcApi'
import { VpcGroup, VpcNodeRule } from '../../../types/index'
import { useActiveTab } from '../../../hooks/useNavigation'
import { useOnlineDevices } from '../../../hooks/useOnlineDevices'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getDeviceId } from '../../../utils/deviceId'

export interface UseVpcDataResult {
  selectedIP: string
  setSelectedIP: (ip: string) => void
  groups: VpcGroup[]
  rules: VpcNodeRule[]
  loading: boolean
  refresh: () => void
}

/**
 * VPC 页数据源（单源：groups/rules 在此拉取，向下传给各 section）。
 *
 * 刷新时机：
 * 1. selectedIP 变化（切设备）
 * 2. 切入 VPC tab 的切换沿（页面 keep-alive 常驻，切走不重跑；参照 useHostEvents 的
 *    prevActiveTabRef 模式在切入沿重拉，自愈切走期间的数据陈旧）
 * 3. androidlist:updated 事件（开机/关机新云机 → 规则表与分配下拉需出现新容器），
 *    activeTab==='vpc' 守卫 + 2s 防抖（开机风暴时合并为一次重拉）；只刷 rules（规则
 *    与容器强相关），groups 低频变化不跟事件
 * 4. 手动刷新按钮（refresh）
 *
 * staleRef 序号防竞态：慢请求返回时序号已过期则丢弃结果。
 */
export function useVpcData(): UseVpcDataResult {
  const { devices } = useOnlineDevices()
  const { mode } = useDeployMode()
  const [selectedIP, setSelectedIP] = useState('')
  const [groups, setGroups] = useState<VpcGroup[]>([])
  const [rules, setRules] = useState<VpcNodeRule[]>([])
  const [loading, setLoading] = useState(false)
  const staleRef = useRef(0)
  const activeTab = useActiveTab()

  // 默认选中第一台设备（公网模式设备无局域网 IP，用 device_id 作为操作标识）
  useEffect(() => {
    if (devices.length > 0 && !selectedIP) {
      setSelectedIP(getDeviceId(devices[0], mode))
    }
  }, [devices, selectedIP, mode])

  const loadGroups = useCallback(async (seq: number, ip: string) => {
    if (!ip) return
    setLoading(true)
    try {
      const result = await VpcApi.ListVpcGroups(ip)
      if (seq !== staleRef.current) return
      setGroups(result || [])
    } catch { if (seq === staleRef.current) setGroups([]) }
    finally { if (seq === staleRef.current) setLoading(false) }
  }, [])

  const loadRules = useCallback(async (seq: number, ip: string) => {
    if (!ip) return
    try {
      const result = await VpcApi.GetContainerRules(ip)
      if (seq !== staleRef.current) return
      setRules(result || [])
    } catch { if (seq === staleRef.current) setRules([]) }
  }, [])

  // 1) 切设备：全量重拉
  useEffect(() => {
    if (!selectedIP) return
    const seq = ++staleRef.current
    loadGroups(seq, selectedIP)
    loadRules(seq, selectedIP)
  }, [selectedIP, loadGroups, loadRules])

  // 2) 切入沿重拉（keep-alive 失活期间数据陈旧自愈）
  const prevActiveTabRef = useRef(activeTab)
  useEffect(() => {
    if (activeTab === prevActiveTabRef.current) return
    prevActiveTabRef.current = activeTab
    if (activeTab !== 'vpc' || !selectedIP) return
    const seq = ++staleRef.current
    loadGroups(seq, selectedIP)
    loadRules(seq, selectedIP)
  }, [activeTab, selectedIP, loadGroups, loadRules])

  // 3) 云机列表事件 → 防抖重拉 rules（开机新容器几秒内出现在规则表/下拉）
  useEffect(() => {
    const timer = { id: 0 }
    const off = Events.On('androidlist:updated', () => {
      if (prevActiveTabRef.current !== 'vpc') return
      window.clearTimeout(timer.id)
      timer.id = window.setTimeout(() => {
        if (prevActiveTabRef.current !== 'vpc' || !selectedIP) return
        const seq = ++staleRef.current
        loadRules(seq, selectedIP)
      }, 2000)
    })
    return () => { window.clearTimeout(timer.id); off() }
  }, [selectedIP, loadRules])

  const refresh = useCallback(() => {
    if (!selectedIP) return
    const seq = ++staleRef.current
    loadGroups(seq, selectedIP)
    loadRules(seq, selectedIP)
  }, [selectedIP, loadGroups, loadRules])

  return { selectedIP, setSelectedIP, groups, rules, loading, refresh }
}
