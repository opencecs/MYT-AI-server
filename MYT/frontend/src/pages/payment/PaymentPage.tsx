import { useState, useEffect, memo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as PaymentService from '../../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { DeviceState } from '../../types/index'
import { BindInfo, SlotExpireInfo, DeviceSlotInfo } from './types'
import { PackageModal } from './PackageModal'
import { QrCodeModal } from './QrCodeModal'
import { HostLicenseCard } from './HostLicenseCard'
import { getErrorMessage } from '../../utils/error'
import { useAuth } from '../../hooks/useAuth'
import { usePaymentOrder } from './usePaymentOrder'
import { RefreshCw, Search, Lock } from 'lucide-react'

// 切回 tab 时静默补拉的节流窗口：短时间内反复进出视口不重复打点授权 API
const SILENT_REFRESH_MIN_MS = 30_000

function PaymentPage() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const { loggedIn } = useAuth()

  const [devices, setDevices] = useState<DeviceState[]>([])
  const [bindStatuses, setBindStatuses] = useState<Map<string, number>>(new Map())
  // bindLoadedRef：是否已完成过一次「读缓存 / 查云端」的尝试。
  // 用 ref 而非 state：只有节流判断要用，且 loadDeviceData 会被
  // IntersectionObserver 的旧闭包持有，state 会读到过期值。
  const bindLoadedRef = useRef(false)
  const apiInFlightRef = useRef(false)
  const lastApiAtRef = useRef(0)
  const [slotExpireData, setSlotExpireData] = useState<Map<string, SlotExpireInfo[]>>(new Map())
  const [selectedSlots, setSelectedSlots] = useState<Map<string, Set<number>>>(new Map())
  const [search, setSearch] = useState('')
  const pageRef = useRef<HTMLDivElement>(null)

  const filteredDevices = search.trim()
    ? devices.filter(d => d.device_id.toLowerCase().includes(search.toLowerCase()) || d.ip.includes(search.trim()))
    : devices

  const totalSelectedCount = Array.from(selectedSlots.values()).reduce((sum, s) => sum + s.size, 0)

  const {
    packages, showPackageModal, setShowPackageModal,
    payMethod, setPayMethod, orderInfo, polling,
    handleRenew, handleBuy, resetOrder,
  } = usePaymentOrder({
    selectedSlots, totalSelectedCount, setLoading, setError,
    onPaymentSuccess: () => { setSelectedSlots(new Map()); loadDeviceData() },
  })

  useEffect(() => {
    if (loggedIn) {
      loadDeviceData()
    } else {
      resetOrder()
      setDevices([]); setBindStatuses(new Map()); setSlotExpireData(new Map()); setSelectedSlots(new Map()); setError('')
      bindLoadedRef.current = false
    }
  }, [loggedIn])

  useEffect(() => {
    if (!loggedIn) return
    // 不订阅 devicelist:updated（用户决策 2026-09-04：授权页不靠事件驱动）——
    // 数据获取只在：进页时读 Go 缓存 + 手动刷新按钮调 API + 支付成功后重查。
    // 存量数据的新鲜度由 Go 侧 30 分钟定时任务维护（落到 config 缓存）。
    const el = pageRef.current
    if (!el) return
    const io = new IntersectionObserver(entries => {
      if (!entries.some(e => e.isIntersecting)) return
      loadFromCache()
      // 切回 tab：缓存只用于「先显示」，绑定状态再后台静默补拉一次——
      // 缓存里 bind_status 可能因 omitempty 缺字段/过期而显示不准，
      // 只读缓存会让用户永久停在错误状态（必须刷新按钮才恢复）。
      loadDeviceData(true)
    })
    io.observe(el)
    return () => { io.disconnect() }
  }, [loggedIn])

  // 从 Go 缓存读取（零外网请求）：bind_status 在设备列表里，到期在 GetSlotExpiry。
  // 定时任务与添加设备查询都会更新这两处缓存，进 tab 时读即为最新。
  // 关键：缓存里 bind_status 缺失（omitempty 的 0 值不落盘）不代表「未绑定」，
  // 这类条目一律跳过，且绝不覆盖内存里已有的有效值。
  const loadFromCache = async () => {
    try {
      const list = await DiscoverService.GetDeviceList() as DeviceState[]
      const online = list.filter(d => d.state === 0)
      setDevices(online)
      const cached = new Map<string, number>()
      for (const d of online) {
        if (typeof d.bind_status === 'number') cached.set(d.device_id, d.bind_status)
      }
      if (cached.size > 0) {
        setBindStatuses(prev => {
          const next = new Map(prev)
          cached.forEach((v, k) => next.set(k, v))
          return next
        })
        bindLoadedRef.current = true
      }
      const m = await DiscoverService.GetSlotExpiry() as Record<string, { index: number; expire_time: string; state: number }[]>
      if (m) {
        const expireMap = new Map<string, SlotExpireInfo[]>()
        for (const [deviceId, slots] of Object.entries(m)) {
          if (!slots) continue
          expireMap.set(deviceId, slots.map(s => ({
            slot_index: s.index, expire_ts: s.expire_time, state: s.state,
          })))
        }
        setSlotExpireData(expireMap)
      }
    } catch { /* 缓存读取失败保持现有显示 */ }
  }

  // silent=true：切回 tab 的后台补拉——不动 loading、不清空已有数据、不覆盖错误提示；
  // 带 in-flight 守卫 + 30s 时间窗，避免反复进出视口重复打点授权 API。
  const loadDeviceData = async (silent = false) => {
    if (silent) {
      if (apiInFlightRef.current) return
      // 已有数据：30s 时间窗内不重复打点；尚无任何绑定数据（首次失败/冷启动）时放宽到 5s，
      // 让切回 tab 能尽快自愈，但不至于每次进出视口都发请求。
      const minGap = bindLoadedRef.current ? SILENT_REFRESH_MIN_MS : 5_000
      if (Date.now() - lastApiAtRef.current < minGap) return
    }
    apiInFlightRef.current = true
    lastApiAtRef.current = Date.now()
    if (!silent) { setLoading(true); setError('') }
    try {
      const devices = await DiscoverService.GetDeviceList() as DeviceState[]
      const online = devices.filter(d => d.state === 0)
      setDevices(online)
      if (online.length > 0) {
        const deviceIDs = online.map(d => d.device_id)
        const [bindResults, expireResults] = await Promise.all([
          PaymentService.GetDeviceBindStatus(deviceIDs) as Promise<BindInfo[]>,
          PaymentService.GetSlotExpireInfo(deviceIDs) as Promise<DeviceSlotInfo[]>,
        ])
        const bindMap = new Map<string, number>()
        bindResults?.forEach(b => bindMap.set(b.device_id, b.bind_status))
        setBindStatuses(bindMap)
        bindLoadedRef.current = true
        const expireMap = new Map<string, SlotExpireInfo[]>()
        expireResults?.forEach(d => expireMap.set(d.device_id, d.slots || []))
        setSlotExpireData(expireMap)
      }
    } catch (err) {
      if (!silent) setError(getErrorMessage(err))
    } finally {
      apiInFlightRef.current = false
      if (!silent) setLoading(false)
    }
  }

  const getSlotCount = (deviceID: string) => deviceID.charAt(0).toLowerCase() === 'p' ? 24 : 12

  const toggleSlot = (deviceID: string, slotIndex: number) => {
    const bindStatus = bindStatuses.get(deviceID)
    if (bindStatus !== 1) return
    const newMap = new Map(selectedSlots)
    const current = newMap.get(deviceID) || new Set<number>()
    const newSet = new Set(current)
    if (newSet.has(slotIndex)) { newSet.delete(slotIndex) } else { newSet.add(slotIndex) }
    if (newSet.size > 0) { newMap.set(deviceID, newSet) } else { newMap.delete(deviceID) }
    setSelectedSlots(newMap)
  }

  // 绑定状态语义见 bindStatus.ts：undefined = 尚未拿到（查询中），不能当作未绑定渲染。

  // 未登录：空状态引导（登录入口在导航栏）
  if (!loggedIn) {
    return (
      <div className="h-full flex flex-col items-center justify-center bg-gray-50/50">
        <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-gray-100 to-gray-200 flex items-center justify-center mb-4 shadow-sm">
          <Lock size={28} className="text-gray-400" />
        </div>
        <p className="text-sm font-medium text-gray-500 mb-1">{t('payment:pleaseLoginTitle')}</p>
        <p className="text-xs text-gray-400 mb-2">{t('payment:pleaseLoginDesc')}</p>
        <p className="text-xs text-blue-400">{t('payment:loginFromNav')}</p>
      </div>
    )
  }

  return (
    <div ref={pageRef} className="h-full flex flex-col bg-gray-50/30">
      {/* 顶部标题栏 */}
      <div className="shrink-0 px-5 py-4 flex items-center justify-between bg-white/60 backdrop-blur-sm border-b border-gray-100">
        <h1 className="text-base font-semibold text-gray-800">{t('payment:title')}</h1>
        <div className="flex items-center gap-2">
          <button onClick={() => { loadDeviceData() }} disabled={loading}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-500 bg-white border border-gray-200 rounded-lg hover:text-blue-500 hover:border-blue-300 disabled:opacity-40 transition-colors">
            <RefreshCw size={13} className={loading ? 'animate-spin' : ''} /> {t('button.refresh')}
          </button>
        </div>
      </div>

      {/* 搜索 + 设备列表 */}
      <div className="flex-1 overflow-y-auto px-5 py-4">
        {loading && <div className="text-xs text-gray-400 mb-2">{t('status.loading')}</div>}
        {error && <div className="text-xs text-red-500 mb-3 bg-red-50 rounded-lg px-3 py-2">{error}</div>}

        <div className="relative mb-4">
          <Search size={15} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
          <input value={search} onChange={e => setSearch(e.target.value)} placeholder={t('payment:searchPlaceholder')}
            className="w-full pl-10 pr-3 py-2.5 text-xs border border-gray-200 rounded-xl bg-white focus:outline-none focus:border-blue-300 placeholder:text-gray-300 transition-colors" />
        </div>

        <div className="space-y-2.5">
          {filteredDevices.map(device => (
            <HostLicenseCard key={device.device_id}
              device={device}
              slotCount={getSlotCount(device.device_id)}
              slots={slotExpireData.get(device.device_id)}
              bindStatus={bindStatuses.get(device.device_id)}
              selectedSlots={selectedSlots.get(device.device_id) || new Set<number>()}
              onToggleSlot={toggleSlot} />
          ))}
        </div>

        {filteredDevices.length === 0 && !loading && (
          <div className="text-center text-gray-300 mt-12 text-sm">{t('empty.noOnlineDevices')}</div>
        )}
      </div>

      {/* 底部续期操作栏 */}
      {totalSelectedCount > 0 && (
        <div className="shrink-0 bg-white/80 backdrop-blur-sm border-t border-gray-100 px-6 py-3.5 flex items-center justify-between">
          <span className="text-sm text-gray-600">{t('payment:selectedSlotsCount', { count: totalSelectedCount })}</span>
          <button onClick={handleRenew} disabled={loading}
            className="px-6 py-2.5 text-sm font-medium text-white bg-gradient-to-r from-blue-500 to-blue-600 rounded-xl hover:from-blue-600 hover:to-blue-700 shadow-md shadow-blue-200/50 disabled:opacity-50 transition-all">
            {t('payment:renew')}
          </button>
        </div>
      )}

      {showPackageModal && (() => {
        const slotList: { deviceID: string; slotIndex: number }[] = []
        selectedSlots.forEach((slots, deviceID) => {
          Array.from(slots).sort((a, b) => a - b).forEach(slotIndex => {
            slotList.push({ deviceID, slotIndex })
          })
        })
        return (
          <PackageModal packages={packages} selectedSlots={slotList} payMethod={payMethod} loading={loading}
            onPayMethodChange={setPayMethod} onBuy={handleBuy}
            onRemoveSlot={(idx: number) => {
              const item = slotList[idx]
              if (!item) return
              const newMap = new Map(selectedSlots)
              const current = newMap.get(item.deviceID) || new Set<number>()
              const newSet = new Set(current)
              newSet.delete(item.slotIndex)
              if (newSet.size > 0) newMap.set(item.deviceID, newSet)
              else newMap.delete(item.deviceID)
              setSelectedSlots(newMap)
            }}
            onClose={() => setShowPackageModal(false)} />
        )
      })()}
      {orderInfo && (<QrCodeModal orderInfo={orderInfo} polling={polling} onClose={resetOrder} />)}
    </div>
  )
}

export default memo(PaymentPage)
