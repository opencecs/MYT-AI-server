import { useTranslation } from 'react-i18next'
import { VpcNodeRule } from '../../../types/index'

interface Props {
  rules: VpcNodeRule[]
  androidName: string
  getNote: (containerName: string) => string
}

/** VPC 网络概况 —— 已配置的实例列表（按分组聚合） */
export default function VpcNetworkOverview({ rules, androidName, getNote }: Props) {
  const { t } = useTranslation()
  if (rules.length === 0) return null

  const groupMap = new Map<string, VpcNodeRule[]>()
  const unjoined: VpcNodeRule[] = []
  for (const r of rules) {
    if (r.status > 0 && r.groupName) {
      if (!groupMap.has(r.groupName)) groupMap.set(r.groupName, [])
      groupMap.get(r.groupName)!.push(r)
    } else {
      unjoined.push(r)
    }
  }

  return (
    <div className="mx-5 mb-4">
      <p className="text-xs text-gray-500 mb-1.5">{t('host:vpc.overview.title')}</p>
      <div className="max-h-[180px] overflow-y-auto scrollbar-thin space-y-1.5">
        {[...groupMap.entries()].map(([name, members]) => (
          <div key={name} className="border border-gray-200 rounded-lg overflow-hidden">
            <div className="flex items-center gap-1.5 px-2.5 py-1 bg-green-50/60">
              <span className="text-[10px] text-green-500">●</span>
              <span className="text-xs font-medium text-gray-700">{name}</span>
              <span className="text-[10px] text-gray-400">{members.length}{t('host:vpc.overview.unit')}</span>
            </div>
            <div className="px-2.5 py-1 space-y-0.5">
              {members.map(r => {
                const label = getNote(r.containerName) || r.containerName
                const isMe = r.containerName === androidName
                return (
                  <div key={r.id} className="flex items-center gap-1.5 text-xs">
                    <span className={`truncate ${isMe ? 'text-blue-600 font-medium' : 'text-gray-600'}`}>
                      {label}{isMe ? t('host:vpc.overview.current') : ''}
                    </span>
                    <span className="text-gray-300">→</span>
                    <span className="text-gray-400 shrink-0">{r.vpcRemarks}</span>
                    {r.enabled === false && (
                      <span className="text-[10px] px-1 py-px rounded bg-gray-100 text-gray-400 shrink-0">{t('host:vpc.overview.disabled')}</span>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        ))}
        {unjoined.length > 0 && (
          <div className="border border-gray-100 rounded-lg overflow-hidden">
            <div className="flex items-center gap-1.5 px-2.5 py-1 bg-gray-50/60">
              <span className="text-[10px] text-gray-300">●</span>
              <span className="text-xs text-gray-400">{t('host:vpc.overview.unjoined')}</span>
              <span className="text-[10px] text-gray-300">{unjoined.length}{t('host:vpc.overview.unit')}</span>
            </div>
            <div className="px-2.5 py-1 space-y-0.5">
              {unjoined.map(r => {
                const label = getNote(r.containerName) || r.containerName
                return (
                  <div key={r.id} className="text-xs text-gray-400 truncate">{label}</div>
                )
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
