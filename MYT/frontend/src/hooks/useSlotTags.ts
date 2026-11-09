import { useState, useEffect, useCallback, useMemo } from 'react'
import { Events } from '@wailsio/runtime'
import * as DiscoverService from '../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as SdkApiService from '../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as AndroidListService from '../../bindings/github.com/moyunteng/myt/androidlistservicewrapper'
import type { AndroidInfo } from '../types/index'

/** 坑位标签映射：key = "deviceId#indexNum"，value = 标签数组 */
export type SlotTagMap = Record<string, string[]>

/**
 * 坑位标签的存储键：设备 ID + 坑位号。
 * 必须与 Go 侧 config.SlotTagKey 保持一致（internal/config/slot_tag.go）。
 *
 * 绑坑位而非容器实例：云机重置 / 换镜像 / 删容器后标签仍在
 * （容器 ID 会随重建变化，坑位号不会）。
 */
export function slotTagKey(deviceId: string, indexNum: number): string {
  return `${deviceId}#${indexNum}`
}

function sanitize(raw: unknown): SlotTagMap {
  const result: SlotTagMap = {}
  if (!raw || typeof raw !== 'object') return result
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (!Array.isArray(value)) continue
    const tags = value.filter((t): t is string => typeof t === 'string' && t.trim() !== '')
    if (tags.length > 0) result[key] = tags
  }
  return result
}

/**
 * 坑位标签同步 hook：加载全部标签 + 监听 slottag:updated 事件实时刷新。
 * 所有需要标签的页面统一使用此 hook，任一 TAB 修改后其余 TAB 自动同步
 * （与 useDeviceNotes / useAndroidNotes 同款模式）。
 */
export function useSlotTags(): {
  tags: SlotTagMap
  refresh: () => void
  setTagsLocal: (deviceId: string, indexNum: number, tags: string[]) => void
} {
  const [tags, setTags] = useState<SlotTagMap>({})

  const refresh = useCallback(() => {
    DiscoverService.GetAllSlotTags().then((result: unknown) => {
      setTags(sanitize(result))
    }).catch(() => {})
  }, [])

  useEffect(() => {
    refresh()
    const off = Events.On('slottag:updated', (evt: unknown) => {
      const data = (evt as Record<string, unknown>)?.data ?? evt
      const deviceID = (data as Record<string, unknown>)?.deviceID as string
      const indexNum = Number((data as Record<string, unknown>)?.indexNum)
      if (deviceID && Number.isFinite(indexNum)) {
        // 事件携带归一化后的权威值（Go 侧已去重/去空白）
        const next = Array.isArray((data as Record<string, unknown>)?.tags)
          ? ((data as Record<string, unknown>).tags as unknown[]).filter((t): t is string => typeof t === 'string')
          : []
        setTags(prev => {
          const map = { ...prev }
          const key = slotTagKey(deviceID, indexNum)
          if (next.length > 0) map[key] = next
          else delete map[key]
          return map
        })
      } else {
        refresh()
      }
    })
    return () => { off() }
  }, [refresh])

  const setTagsLocal = useCallback((deviceId: string, indexNum: number, next: string[]) => {
    setTags(prev => {
      const map = { ...prev }
      const key = slotTagKey(deviceId, indexNum)
      if (next.length > 0) map[key] = next
      else delete map[key]
      return map
    })
  }, [])

  return { tags, refresh, setTagsLocal }
}

/**
 * 坑位标签筛选 hook：聚合全局标签 + 维护选中集合 + 自动收敛失效标签。
 *
 * 截图模式（ScreenshotView）与列表模式（MultiDeviceSlotView）共用 ——
 * 两处的筛选语义（多选「或」）、失效收敛、取消入口必须一致，各写一份必然改漏。
 */
export function useSlotTagFilter(slotTags: SlotTagMap, containerTags?: string[]): {
  /** 全局已有标签（已排序，筛选栏与编辑浮层的复用来源） */
  allTags: string[]
  /** 当前选中的标签：多选为「或」语义（命中任一即显示），空 = 不筛选 */
  selectedTags: Set<string>
  toggleTag: (tag: string) => void
  clearTags: () => void
  /** 筛选栏是否应渲染：有标签可筛，或筛选仍激活（哪怕标签已被删光） */
  hasFilterBar: boolean
} {
  const [selectedTags, setSelectedTags] = useState<Set<string>>(new Set())

  // 标签集合源（2026-10-27）：优先用调用方按「当前视图容器」聚合的 android.tags 去重集
  // （标签唯一权威来源是设备端容器，本地坑位映射 slot_tags.json 已停止消费）；
  // 未传时退回本地坑位映射聚合（单设备宿主 MaintenancePage 等无容器集合的场景）。
  // 失效收敛 / allTags 聚合 / 判定必须同源，否则选中一个仅存在于容器 tags 的标签
  // 会被下方收敛 effect 当作「失效」误删（表现为勾选后立即弹回）。
  const allTags = useMemo(() => {
    const set = new Set<string>()
    if (containerTags) {
      for (const tag of containerTags) set.add(tag)
    } else {
      for (const list of Object.values(slotTags)) for (const tag of list) set.add(tag)
    }
    return [...set].sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
  }, [slotTags, containerTags])

  // 失效标签收敛（2026-09-27 修）：某标签的最后一个引用被移除后，筛选集合里不能留着它 ——
  // 否则每台云机的「命中任一选中标签」全为 false → 列表恒空；而筛选栏又因 allTags 为空
  // 整个消失，用户连「清除筛选」都点不到，只能切视图模式让组件重建来重置筛选
  // （表现为「点一下列表才出来」）。返回 prev 时引用不变，React 直接 bail out，不会自激。
  useEffect(() => {
    setSelectedTags(prev => {
      if (prev.size === 0) return prev
      const valid = new Set(allTags)
      let changed = false
      const next = new Set<string>()
      for (const tag of prev) {
        if (valid.has(tag)) next.add(tag)
        else changed = true
      }
      return changed ? next : prev
    })
  }, [allTags])

  const toggleTag = useCallback((tag: string) => {
    setSelectedTags(prev => {
      const next = new Set(prev)
      if (next.has(tag)) next.delete(tag)
      else next.add(tag)
      return next
    })
  }, [])

  const clearTags = useCallback(() => setSelectedTags(new Set()), [])

  return {
    allTags,
    selectedTags,
    toggleTag,
    clearTags,
    hasFilterBar: allTags.length > 0 || selectedTags.size > 0,
  }
}

/** useSlotTagFilter 的返回类型（供宿主层透传给视图组件时复用） */
export type SlotTagFilterState = ReturnType<typeof useSlotTagFilter>

/**
 * 按容器集合聚合标签（去重 + zh 排序）：标签唯一权威来源是设备端 android.tags，
 * 筛选数据源与判定必须同源（useSlotTagFilter 显式传入，避免失效收敛误删容器标签）。
 * 与视图「可见容器集合」保持同一范围——筛选什么就看什么。
 */
export function collectAndroidTags(androids: AndroidInfo[]): string[] {
  const set = new Set<string>()
  for (const a of androids) for (const tag of a.tags ?? []) set.add(tag)
  return [...set].sort((x, y) => x.localeCompare(y, 'zh-Hans-CN'))
}

/**
 * 宿主层提升给视图组件的标签控制包：标签数据 + 本地写入 + 筛选状态。
 * HostManagement 把它下发给 ScreenshotView / MultiDeviceSlotView，
 * 使 effectiveAndroids 的标签收窄、全选、批量操作与视图展示同源。
 */
export interface SlotTagControl {
  slotTags: SlotTagMap
  setSlotTagsLocal: (deviceId: string, indexNum: number, tags: string[]) => void
  filter: SlotTagFilterState
  /** 拉取设备端标签字典并入聚合源（标签筛选下拉打开时调用，GET /android/tags 兜底）。 */
  refreshTagDict?: () => void
}

/**
 * 写入某坑位标签（乐观更新由调用方经 setTagsLocal 完成，此处只负责落库）。
 * 传空数组 = 清除该坑位标签。
 */
export function saveSlotTags(deviceId: string, indexNum: number, tags: string[]): Promise<void> {
  return DiscoverService.SetSlotTags(deviceId, indexNum, tags) as Promise<void>
}

/**
 * 设备端标签写入（POST /android/label 经 Go wrapper，public 模式网关透传）。
 * 标签的真实来源是设备端容器：写入后 NotifyAndroidsChanged 触发容器列表查询，
 * tags 随 androidlist:updated 权威推送收敛 UI（写入方先乐观更新本地条目，避免闪烁）。
 * 传空数组 = 清空该容器标签。注意：容器重建（创建不带 tags）会清掉同名容器旧标签。
 *
 * 写后兜底刷新（2026-10-27）：成功 2s 后主动拉一次该设备容器列表（QueryDeviceByIdent
 * 已有链路）。正常路径设备端 WS tagsChanged 事件秒级触发全量刷新，此兜底与之幂等
 * （MergeAndroidList 权威覆盖，多查一次无副作用）；老设备版本无 tagsChanged 事件、
 * local 模式操作后零查询——没有它就只能等 20s container/stats 周期，写后 UI 停留旧值。
 * 2s 也给事件到达留了窗口：事件先到则兜底查询只是空转一次。
 */
export function saveAndroidTags(deviceId: string, names: string[], tags: string[]): Promise<void> {
  return SdkApiService.SetAndroidLabels(deviceId, names, tags).then(() => {
    setTimeout(() => {
      AndroidListService.QueryDeviceByIdent(deviceId).catch(() => {})
    }, 2000)
  }) as Promise<void>
}

/**
 * 拉取设备端标签字典（GET /android/tags，扁平去重全集，不含容器归属）。
 * 与 WS tagsChanged 事件载荷同源；用途=「没有及时同步手段时」的兜底刷新：
 * 标签筛选下拉打开时拉一次（看到设备端最新全集，含其他客户端打的标签）。
 * 并行拉多台、任一失败忽略（字典是建议源，失败不阻塞筛选）。
 */
export function fetchAndroidTagDict(deviceIdents: string[]): Promise<string[]> {
  const idents = deviceIdents.filter(s => s)
  if (idents.length === 0) return Promise.resolve([])
  return Promise.allSettled(idents.map(ident => SdkApiService.GetAndroidTagDict(ident))).then(results => {
    const merged = new Set<string>()
    for (const r of results) {
      if (r.status === 'fulfilled') for (const tag of r.value) merged.add(tag)
    }
    return [...merged].sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
  })
}
