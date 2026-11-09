import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Globe, Search } from 'lucide-react'
import { VpcNodeRule } from '../../../types/index'

const inputCls = 'text-xs px-2 py-1.5 border border-gray-200 rounded-lg outline-none focus:border-blue-400 bg-white'

/**
 * 弹窗左列目标选择器（AddRuleModal / AddRouteModal 共用）：
 * - 固定「全局（所有云机）」项 + 可写容器列表（单选高亮）
 * - 容器多时顶部过滤搜索框；globalLock 时容器置灰不可点
 */
export default function TargetPickerList({ global, globalLock, containers, value, onChange }: {
  /** 是否显示固定全局项 */
  global?: boolean
  /** 锁定为全局项：容器置灰不可选（如「设置全局过滤」模式） */
  globalLock?: boolean
  containers: VpcNodeRule[]
  value: string
  onChange: (targetID: string) => void
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  // 输入含空格/大小写容错；空串恒匹配全部
  const needle = query.trim().toLowerCase()
  const matched = useMemo(() => needle
    ? containers.filter(r => `${r.containerName} ${r.containerIP}`.toLowerCase().includes(needle))
    : containers, [containers, needle])

  const itemCls = (active: boolean, disabled: boolean) =>
    `w-full text-left px-2.5 py-2 rounded-lg transition-colors border ${active
      ? 'bg-blue-50 border-blue-200'
      : disabled ? 'bg-gray-50/60 border-transparent opacity-50 cursor-not-allowed' : 'border-transparent hover:bg-gray-50 cursor-pointer'}`

  return (
    <div className="flex flex-col h-full min-h-0 gap-1.5">
      {containers.length > 5 && !globalLock && (
        <div className="relative shrink-0">
          <Search size={12} className="absolute left-2 top-1/2 -translate-y-1/2 text-gray-300" />
          <input value={query} onChange={e => setQuery(e.target.value)}
            placeholder={t('vpc:domainShared.searchContainer')}
            className={`${inputCls} w-full pl-7`} />
        </div>
      )}
      <div className="flex-1 min-h-0 overflow-y-auto space-y-0.5 pr-0.5">
        {global && (
          <button type="button" onClick={() => onChange('global')}
            disabled={!!globalLock}
            className={`${itemCls(value === 'global', false)} ${globalLock ? 'cursor-default' : ''}`}>
            <span className="flex items-center gap-2">
              <Globe size={13} className="text-amber-500 shrink-0" />
              <span className="text-xs text-gray-700 font-medium truncate">{t('vpc:domainShared.globalAllVMs')}</span>
            </span>
          </button>
        )}
        {matched.map(r => (
          <button key={r.containerID} type="button"
            onClick={() => !globalLock && onChange(r.containerID)}
            disabled={!!globalLock}
            className={itemCls(value === r.containerID, !!globalLock)}
            title={`${r.containerName} (${r.containerIP})`}>
            <span className="block text-xs text-gray-700 font-medium truncate">{r.containerName}</span>
            <span className="flex items-center gap-1.5 mt-0.5">
              <span className="text-[10px] text-gray-400 font-mono truncate">{r.containerIP}</span>
              {r.containerState && (
                <span className={`text-[9px] px-1.5 py-px rounded-full shrink-0 ${r.containerState === 'running' ? 'bg-emerald-50 text-emerald-600' : 'bg-gray-100 text-gray-400'}`}>
                  {r.containerState}
                </span>
              )}
            </span>
          </button>
        ))}
        {matched.length === 0 && (
          <p className="text-[11px] text-gray-400 text-center py-6">{t('vpc:domainShared.noMatchContainer')}</p>
        )}
      </div>
    </div>
  )
}
