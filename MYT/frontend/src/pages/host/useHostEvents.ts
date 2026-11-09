import { useEffect, useRef } from 'react'
import { Events } from '@wailsio/runtime'
import { onSlotStatus } from '../../utils/wailsEvent'
import { useActiveTab } from '../../hooks/useNavigation'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as SlotService from '../../../bindings/github.com/moyunteng/myt/slotservicewrapper'
import * as ScreenshotService from '../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import * as AndroidListService from '../../../bindings/github.com/moyunteng/myt/androidlistservicewrapper'
import * as MirrorService from '../../../bindings/github.com/moyunteng/myt/mirrorservicewrapper'
import { DeviceState, AndroidInfo, SlotExpiry, SlotInfo, MirrorItem } from '../../types/index'
import { cast } from '../../utils/cast'
import { resolveStatus } from '../../utils/statusIntent'
import { jsonEqualMap, toSlotExpiryList } from './hostManagementUtils'
import { mergeDeviceList } from '../../utils/deviceMerge'
import { mergeAndroidList } from '../../utils/androidMerge'
import { useDeployMode } from '../../hooks/useDeployMode'
import { showToast } from '../../components/Toast'
import { onEvent } from '../../utils/wailsEvent'
import i18n from '../../i18n'

interface HostEventsArgs {
  selectedDeviceId: string | undefined
  viewMode: string
  selectedDeviceIds: Set<string>
  /** 侧栏展开的设备 ID（展示容器子列表）：并入安卓列表查询范围，截图轮询范围不扩大 */
  expandedDeviceIds: Set<string>
  setDevices: (updater: (prev: DeviceState[]) => DeviceState[]) => void
  setSelectedDevice: (updater: (prev: DeviceState | null) => DeviceState | null) => void
  setSelectedDeviceIds: (updater: (prev: Set<string>) => Set<string>) => void
  setAndroidList: (updater: (prev: AndroidInfo[]) => AndroidInfo[]) => void
  setSlotExpiry: (updater: (prev: Record<string, SlotExpiry[]>) => Record<string, SlotExpiry[]>) => void
  setMirrors: (m: MirrorItem[]) => void
}

/**
 * useHostEvents 抽出 useHostManagement 中 6 个事件订阅 + 镜像/坑位/待添加的初始拉取。
 * 每个 effect 自包含，只通过 setter 写回状态，主 hook 保留状态定义与操作方法。
 */
export function useHostEvents(args: HostEventsArgs) {
  const {
    selectedDeviceId, viewMode, selectedDeviceIds, expandedDeviceIds,
    setDevices, setSelectedDevice, setSelectedDeviceIds,
    setAndroidList, setSlotExpiry, setMirrors,
  } = args
  const { mode } = useDeployMode()

  // 设备列表：事件驱动，Go端 devicelist:updated 事件自带数据
  useEffect(() => {
    // 按字段合并：未变化的设备复用旧对象（引用稳定）→ DeviceListItem memo
    // 生效，心跳/列表事件只重渲染真正变化的行。几百台设备时若直接 setDevices
    // (新数组)，所有行引用全变 = 每次全量重渲染（2026-09-05 卡顿修复）。
    //
    // 本函数**只合数据，不碰勾选**：勾选是用户意图，把"按 order 回灌勾选"混在
    // 数据合并里，会让 devicestats:updated（500ms 节流的指标心跳，高频频道）
    // 每秒改写用户刚做的选择 —— 全选/勾选被"自动取消"的真正来源。
    const mergeDeviceData = (list: DeviceState[]) => {
      setDevices(prev => mergeDeviceList(prev, list))
      setSelectedDevice(prev => {
        if (!prev) return null
        return list.some(d => d.device_id === prev.device_id) ? prev : null
      })
    }

    // 勾选集合以 Go 端 order 字段为唯一事实源（order>0=勾选）。
    // 不按在线状态过滤（2026-09-05）：离线时保留勾选——WS 短暂闪断会把
    // 勾选 silently 清掉，批量操作漏设备；手动勾选离线设备也是合法操作
    // （批量执行时按台失败回报，UI 红点已标明离线）。「全选」的语义由
    // Go 端 SelectAllDevices 负责，与此处一致（同样不看 State）。
    const syncSelectionFromOrder = (list: DeviceState[]) => {
      setSelectedDeviceIds(prev => {
        const orderChecked = new Set(list.filter(d => d.order > 0).map(d => d.device_id))
        if (prev.size === orderChecked.size && [...prev].every(id => orderChecked.has(id))) return prev
        return orderChecked
      })
    }

    DiscoverService.GetDeviceList().then(list => {
      const dl = cast<DeviceState[]>(list)
      if (!Array.isArray(dl)) return
      mergeDeviceData(dl)
      syncSelectionFromOrder(dl)
    }).catch(() => {})
    // local 模式不触发 UDP 发现（用户决策 2026-09-03）：列表走缓存 + WS 事件驱动；
    // public 模式需要主动拉一次网关列表（否则切模式后事件迟迟不来列表长期为空）
    if (mode === 'public') DiscoverService.RefreshNow().catch(() => {})
    const off = Events.On('devicelist:updated', (ev: any) => {
      const list = cast<DeviceState[]>(ev.data ?? ev)
      if (!Array.isArray(list)) return
      mergeDeviceData(list)
      // 只有列表真变化（增删/上下线/他处改写勾选）才回灌勾选
      syncSelectionFromOrder(list)
    })
    // 设备指标心跳（WS system/stats）：只刷磁盘/内存条，**不回灌勾选**。
    // 授权页不订阅本事件——心跳不再触发授权重查。
    const offStats = Events.On('devicestats:updated', (ev: any) => {
      const list = cast<DeviceState[]>(ev.data ?? ev)
      if (Array.isArray(list)) mergeDeviceData(list)
    })
    return () => { off(); offStats() }
  }, [])

  // 同步勾选的设备 ID 到截图服务（控制后台截图轮询范围）。
  // 截图模式：只跟随勾选集合——单击查看（selectedDeviceId）不并入，否则取消
  // 最后一个勾选时截图卡片永远残留一台（无途径清空 selectedDevice）。
  // 坑位模式：单击查看的设备并入（「单击查看 + 点刷新」时截图同步重抓该设备）。
  const screenshotIds = () => {
    // V4 也走截图服务：tobox 8088 的 GET /android/:id/screenshot（setup 注入 V4 抓图通道），
    // 设备 ID 即主机 IP，与 V3 同口径。
    const ids = new Set(selectedDeviceIds)
    if (viewMode !== 'screenshot' && selectedDeviceId) ids.add(selectedDeviceId)
    return Array.from(ids)
  }
  useEffect(() => {
    ScreenshotService.SetSelectedDevices(screenshotIds()).catch(() => {})
  }, [selectedDeviceIds, selectedDeviceId, viewMode, mode])

  // 回到云机 tab 时无条件重发一次截图勾选（自愈 keep-alive 污染）：
  // 其他页面（如主机维护页）失活期间可能改写过全局截图集合，而上面的 effect
  // 依赖不变不重跑，导致回到本页截图卡片全空，重新勾选才恢复。
  // 用 ref 只在「其他 tab → host」的切换沿触发，首次挂载不重发（上面已发过）。
  const activeTab = useActiveTab()
  const prevActiveTabRef = useRef(activeTab)
  useEffect(() => {
    if (activeTab === prevActiveTabRef.current) return
    prevActiveTabRef.current = activeTab
    if (activeTab !== 'host') return
    ScreenshotService.SetSelectedDevices(screenshotIds()).catch(() => {})
  }, [activeTab])

  // 同步设备 ID 到安卓列表服务（控制查询范围 + 新选中立即查一次）。
  // 并入范围 = 勾选设备 ∪ 单击查看设备 ∪ 侧栏展开的设备——未勾选的设备展开后
  // 也要能看到容器（androidList 默认只查勾选+单击的设备）。收起不移除（数据已在
  // 缓存，避免反复拉取）；模式切换时 Go 端清空，现有清理逻辑照旧。
  // 截图服务不在此列（上面那个 effect）：截图轮询范围不因展开而扩大。
  useEffect(() => {
    const ids = new Set(selectedDeviceIds)
    if (selectedDeviceId) ids.add(selectedDeviceId)
    for (const id of expandedDeviceIds) ids.add(id)
    AndroidListService.SetSelectedDevices(Array.from(ids)).catch(() => {})
  }, [selectedDeviceIds, selectedDeviceId, expandedDeviceIds])

  useEffect(() => {
    // v4 没有 V3 镜像 URL 体系（镜像由 tobox /image 的 variant 管理，见 ROM tab）：
    // 拉 V3 镜像只会让「更新镜像/切换机型」下拉出现一堆对 V4 无效的选项，
    // 因此不拉取 —— mirrors 保持为空，相关下拉自然为空。
    if (mode === 'v4') return
    MirrorService.FetchMirrors().then((m: any) => setMirrors(m)).catch(() => {})
  }, [mode])

  // 坑位模式下主动查询选中设备的坑位到期信息
  useEffect(() => {
    // v4 没有坑位/订阅概念：不查坑位到期（Go 侧也返回空）
    if (mode === 'v4') return
    if (viewMode !== 'slot' || !selectedDeviceId) return
    SlotService.QuerySlotInfo([selectedDeviceId]).then((results: { slots: SlotInfo[] }[]) => {
      if (results?.[0]?.slots) {
        setSlotExpiry(prev => {
          const next = { ...prev }
          next[selectedDeviceId] = toSlotExpiryList(results[0].slots)
          return jsonEqualMap(prev, next) ? prev : next
        })
      }
    }).catch(() => {})
  }, [viewMode, selectedDeviceId, mode])

  // 安卓列表：事件驱动（合并时经 resolveStatus 保护操作意图，防旧状态回灌）
  useEffect(() => {
    DiscoverService.GetAndroidList().then(al => {
      const list = cast<AndroidInfo[]>(al ?? []).map(a => ({ ...a, status: resolveStatus(a) }))
      setAndroidList(prev => mergeAndroidList(prev, list))
    }).catch(() => {})
    const off = Events.On('androidlist:updated', (ev: any) => {
      const list = cast<AndroidInfo[]>(ev.data ?? ev ?? []).map(a => ({ ...a, status: resolveStatus(a) }))
      setAndroidList(prev => mergeAndroidList(prev, list))
    })
    return () => { off() }
  }, [])

  // 云机操作后即时更新 androidList 中的 status（不等后端 3 秒轮询）
  useEffect(() => {
    const off = onSlotStatus(({ key, status }) => {
      const [id, idxStr] = key.split('_')
      const indexNum = Number(idxStr)
      if (!id || isNaN(indexNum)) return
      setAndroidList(prev => {
        const idx = prev.findIndex(a => (a.ip === id || a.deviceId === id) && a.indexNum === indexNum)
        if (idx < 0 || prev[idx].status === status) return prev
        const next = [...prev]
        next[idx] = { ...next[idx], status }
        return next
      })
    })
    return () => { off() }
  }, [])

  // V4 剪贴板回灌通知：云机侧复制了文本，Go 已写系统剪贴板，这里 toast 提示。
  // 写剪贴板本体在 Go（Wails Clipboard.SetText，见 v4_clipboard.go），前端只提示。
  useEffect(() => {
    const off = onEvent('v4:clipboard', data => {
      const d = data as { instance?: string; text?: string } | undefined
      if (!d?.text) return
      const len = [...d.text].length
      showToast(String(i18n.t('host:toast.v4ClipboardPushed', { count: len })), 'success')
    })
    return () => { off() }
  }, [])

  // 坑位到期轮询
  useEffect(() => {
    // v4 没有坑位/订阅概念：不轮询坑位到期（Go 侧 GetSlotExpiry 在 v4 下恒为空，
    // 3 秒一轮纯属空转）
    if (mode === 'v4') return
    const fetchSlotExpiry = async () => {
      try {
        const se = cast<Record<string, SlotExpiry[]>>(await DiscoverService.GetSlotExpiry())
        setSlotExpiry(prev => {
          const incoming = se ?? {}
          const merged: Record<string, SlotExpiry[]> = {}
          for (const deviceId of new Set([...Object.keys(prev), ...Object.keys(incoming)])) {
            const oldList = prev[deviceId] ?? []
            const newList = incoming[deviceId] ?? []
            const oldMap = new Map(oldList.map(s => [s.index, s]))
            const mergedList = newList.map(s => {
              if (s.state === undefined) {
                const old = oldMap.get(s.index)
                if (old && old.state !== undefined) return old
              }
              return s
            })
            for (const s of oldList) {
              if (!mergedList.some(m => m.index === s.index)) mergedList.push(s)
            }
            merged[deviceId] = mergedList
          }
          return jsonEqualMap(prev, merged) ? prev : merged
        })
      } catch {}
    }
    fetchSlotExpiry()
    const interval = setInterval(fetchSlotExpiry, 3000)
    return () => clearInterval(interval)
  }, [mode])

  // 「待添加设备」已收敛到 hooks/useDeviceDiscovery.ts（云机 tab / 主机 tab 共用），
  // 本 hook 不再维护 pendinglist:updated 订阅
}
