import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import { AndroidInfo } from '../../../../types/index'
import { saveAndroidTags, useSlotTags } from '../../../../hooks/useSlotTags'
import { tagStyle } from '../../../../utils/tagColor'
import { getDeviceId } from '../../../../utils/deviceId'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { showToast } from '../../../../components/Toast'

interface Props {
  /** 批量目标云机（ActionBar 的选中集合） */
  androids: AndroidInfo[]
  onClose: () => void
}

/** 目标设备分组：该设备上参与批量的容器名 + 各容器当前标签 */
interface DeviceGroup {
  deviceId: string
  names: string[]
  /** 容器名 → 当前标签（来自设备端下发的 android.tags） */
  tagsByName: Map<string, string[]>
}

/**
 * 批量设置容器标签（设备端 POST /android/label，按容器名写入）：
 * 选中云机按 deviceId 分组，每台设备一次 wrapper 调用（该设备全部目标容器名 + 目标标签集）。
 *
 * 加/减语义：目标是「给这批容器都加上 / 都去掉某个标签」，各容器现有标签不同——
 * 按容器逐个算目标集（加 = 现有 ∪ {tag}，减 = 现有 ∖ {tag}），再按目标集分组批量写。
 * 同设备一次 API 调用只带一个 tags 集，所以同设备内目标集相同的容器合并成一次写。
 *
 * 标签真实来源是设备端容器，不再限制 running（停机容器同样可打标）；
 * 去重按容器名（同坑位多台同设备实例天然合并）。写入成功后设备端查询/WS 推送
 * 收敛 UI；部分设备失败时列出失败设备（其余已生效）。
 */
function BatchTagModal({ androids, onClose }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const { tags: localSlotTags } = useSlotTags()
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)

  // 目标设备分组：标识与 RenameModal 同款走 getDeviceId（local=IP 直连 / public=device_id），
  // 按 a.deviceId 分组在 local 模式会拿序列号当 host 拨号 → no such host。
  // 容器名去重——同容器多台只写一次。
  const groups = useMemo(() => {
    const map = new Map<string, DeviceGroup>()
    for (const a of androids) {
      if (!a.name) continue
      const ident = getDeviceId({ ip: a.ip, device_id: a.deviceId }, mode)
      let g = map.get(ident)
      if (!g) {
        g = { deviceId: ident, names: [], tagsByName: new Map() }
        map.set(ident, g)
      }
      if (!g.tagsByName.has(a.name)) {
        g.names.push(a.name)
        g.tagsByName.set(a.name, a.tags ?? [])
      }
    }
    return [...map.values()]
  }, [androids, mode])

  // 全局已有标签：设备端 android.tags 聚合为主，本地坑位标签映射兜底（老数据未迁移时仍有建议可点）
  const allTags = useMemo(() => {
    const set = new Set<string>()
    for (const a of androids) for (const tag of a.tags ?? []) set.add(tag)
    for (const list of Object.values(localSlotTags)) for (const tag of list) set.add(tag)
    return [...set].sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))
  }, [androids, localSlotTags])

  // 执行写入：按「设备 + 目标标签集」分组调用（同设备同目标集的容器合并一次写）。
  // 并行跨设备；单设备内 wrapper 本身循环单容器接口。
  const applyTag = async (tag: string, add: boolean) => {
    const name = tag.trim()
    if (!name || groups.length === 0) return
    setBusy(true)
    try {
      // 目标集 → 写入计划：deviceId → 目标集签名 → 容器名列表
      const plans = new Map<string, Map<string, string[]>>()
      for (const g of groups) {
        let bySet = plans.get(g.deviceId)
        if (!bySet) {
          bySet = new Map()
          plans.set(g.deviceId, bySet)
        }
        for (const [containerName, current] of g.tagsByName) {
          const has = current.includes(name)
          if (add === has) continue // 加已有 / 减不存在：无需写入
          // 目标集按容器现有标签计算（加 = 现有 ∪ {tag}，减 = 现有 ∖ {tag}），
          // 必须按整集覆盖写入（/android/label 是覆盖语义），同目标集的容器合并成
          // 同设备的一次调用
          const next = add ? [...current, name] : current.filter(item => item !== name)
          const key = next.join('\u0000')
          const bucket = bySet.get(key)
          if (bucket) bucket.push(containerName)
          else bySet.set(key, [containerName])
        }
      }
      const writes: Promise<void>[] = []
      for (const [deviceId, bySet] of plans) {
        for (const [key, names] of bySet) {
          writes.push(saveAndroidTags(deviceId, names, key.split('\u0000')))
        }
      }
      const results = await Promise.allSettled(writes)
      const failed = results.filter(r => r.status === 'rejected').length
      if (failed > 0) {
        showToast(t('host:tags.batchFailed') + ` (${failed})`, 'error')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 bg-black/30 flex items-center justify-center" onClick={onClose}>
      <div className="bg-white rounded-xl shadow-xl w-[340px] p-4" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm font-medium text-gray-700">{t('host:tags.batchTitle')}</span>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 cursor-pointer"><X size={14} /></button>
        </div>
        <div className="text-[11px] text-gray-500 mb-3">
          {t('host:tags.batchTarget', { containers: androids.length, devices: groups.length })}
          {busy && <span className="text-blue-600 ml-1">{t('host:tags.batchSaving')}</span>}
        </div>

        <div className="flex items-center gap-2 mb-3">
          <input
            type="text"
            value={input}
            autoFocus
            onChange={e => setInput(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') { applyTag(input, true); setInput('') } }}
            placeholder={t('host:tags.inputPlaceholder')}
            className="flex-1 text-xs px-2 py-1.5 border border-gray-200 rounded focus:outline-none focus:border-blue-400 placeholder:text-gray-300"
          />
          <button
            onClick={() => { applyTag(input, true); setInput('') }}
            disabled={!input.trim() || busy || groups.length === 0}
            className="h-[30px] px-3 text-xs rounded-md border border-blue-400 bg-blue-500 text-white hover:bg-blue-600 disabled:opacity-40 cursor-pointer"
          >
            {t('host:tags.addToAll')}
          </button>
        </div>

        {allTags.length > 0 && (
          <div>
            <div className="text-[10px] text-gray-400 mb-1">{t('host:tags.existing')}</div>
            <div className="flex flex-wrap gap-1 max-h-[160px] overflow-y-auto">
              {allTags.map(tag => {
                const style = tagStyle(tag)
                return (
                  <span key={tag} className={`inline-flex items-center rounded border overflow-hidden ${style.chip}`}>
                    <button
                      onClick={() => applyTag(tag, true)}
                      disabled={busy || groups.length === 0}
                      className="px-1.5 py-0.5 text-[11px] cursor-pointer hover:brightness-95 disabled:opacity-40"
                      title={t('host:tags.addToAll')}
                    >
                      + {tag}
                    </button>
                    <button
                      onClick={() => applyTag(tag, false)}
                      disabled={busy || groups.length === 0}
                      className="px-1 py-0.5 text-[11px] border-l border-gray-200 cursor-pointer hover:text-red-500 disabled:opacity-40"
                      title={t('host:tags.removeFromAll')}
                    >
                      <X size={10} />
                    </button>
                  </span>
                )
              })}
            </div>
          </div>
        )}

        <div className="flex justify-end mt-3">
          <button onClick={onClose} className="h-7 px-3 text-xs rounded-md border border-gray-200 bg-white text-gray-600 hover:bg-gray-50 cursor-pointer">
            {t('button.close')}
          </button>
        </div>
      </div>
    </div>
  )
}

export default BatchTagModal
