import { useTranslation } from 'react-i18next'
import { VpcNodeRule, VpcGroup } from '../../types/index'

interface DomainRule { prefix: string; value: string }

const PREFIXES = ['domain:', 'full:', 'keyword:', 'regexp:']
const rulePlaceholder = (p: string) => p === 'full:' ? 'www.example.com' : p === 'keyword:' ? 'ads' : p === 'regexp:' ? '.*\\.tiktok\\.com$' : 'example.com'
const buildDomains = (rules: DomainRule[]) => rules.filter(r => r.value.trim()).map(r => r.prefix + r.value.trim())

export type { DomainRule }
export { PREFIXES, rulePlaceholder, buildDomains }

const inputCls = 'text-xs px-2 py-1.5 border border-gray-200 rounded-lg outline-none focus:border-blue-400 bg-white'

/**
 * 过滤下拉可选的绑定行：
 * - ContainerName 为空的行（孤儿绑定，按名字都无法展示）排除；
 * - containerState 非 "running" 的行排除（退役/已停止容器的绑定不可选，防止域名配置写到退役容器上串扰同坑位新容器）。
 * containerState 缺省时保守放行（老固件不返回该字段，不能一刀切禁用）。
 */
export function filterSelectableRules(rules: VpcNodeRule[]): VpcNodeRule[] {
  return rules.filter(r => r.containerName && (!r.containerState || r.containerState === 'running'))
}

export function RuleEditor({ rules, onChange }: { rules: DomainRule[]; onChange: (r: DomainRule[]) => void }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-1.5">
      {rules.map((r, i) => (
        <div key={`${r.prefix}-${i}`} className="flex items-center gap-1.5">
          <select value={r.prefix} onChange={e => { const next = [...rules]; next[i] = { ...next[i], prefix: e.target.value }; onChange(next) }}
            className={`${inputCls} w-24 shrink-0`}>
            {PREFIXES.map(p => <option key={p} value={p}>{p.replace(':', '')}</option>)}
          </select>
          <input value={r.value} onChange={e => { const next = [...rules]; next[i] = { ...next[i], value: e.target.value }; onChange(next) }}
            placeholder={rulePlaceholder(r.prefix)} className={`flex-1 ${inputCls}`} />
          {rules.length > 1 && (
            <button onClick={() => onChange(rules.filter((_, j) => j !== i))}
              className="w-6 h-6 flex items-center justify-center text-red-400 hover:text-red-500 hover:bg-red-50 rounded transition-colors shrink-0">
              <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          )}
        </div>
      ))}
      <button onClick={() => onChange([...rules, { prefix: 'domain:', value: '' }])}
        className="flex items-center gap-1 text-xs text-blue-500 hover:text-blue-600 transition-colors">
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
        {t('vpc:domainShared.addRule')}
      </button>
    </div>
  )
}

// ContainerSelect 容器下拉。value 提交 containerID（inspect 归一化的真实 ID），
// 显示文本用 containerName (+IP)——避免按名字提交时撞上退役/重建的同名容器（坑位串扰）。
export function ContainerSelect({ value, onChange, rules }: { value: string; onChange: (v: string) => void; rules: VpcNodeRule[] }) {
  const { t } = useTranslation()
  const selectable = filterSelectableRules(rules)
  return (
    <select value={value} onChange={e => onChange(e.target.value)} className={`w-full ${inputCls}`}>
      <option value="">{t('vpc:domainShared.selectContainer')}</option>
      {selectable.map(r => <option key={r.containerID} value={r.containerID}>{r.containerName} ({r.containerIP})</option>)}
    </select>
  )
}

export function VpcNodeSelect({ value, onChange, groups }: { value: string; onChange: (v: string) => void; groups: VpcGroup[] }) {
  const { t } = useTranslation()
  return (
    <select value={value} onChange={e => onChange(e.target.value)} className={`w-full ${inputCls}`}>
      <option value="">{t('vpc:domainShared.selectVpcNode')}</option>
      {groups.map(g => g.vpcs?.list?.map((n) => (
        <option key={n.id} value={String(n.id)}>{g.alias} / {n.remarks || n.protocol}</option>
      )))}
    </select>
  )
}
