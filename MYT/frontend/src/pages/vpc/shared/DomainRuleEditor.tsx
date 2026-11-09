import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus } from 'lucide-react'
import { showToast } from '../../../components/Toast'
import { DomainRule, rulePlaceholder, parseDomainRule, sniffIpPrefix, RulePrefixCell } from './VpcDomainShared'
import PrefixBadge from './PrefixBadge'

const inputCls = 'text-xs px-2 py-1.5 border border-gray-200 rounded-lg outline-none focus:border-blue-400 bg-white'
const btnMini = 'w-6 h-6 flex items-center justify-center rounded transition-colors shrink-0'

interface Props {
  rules: DomainRule[]
  onChange: (rules: DomainRule[]) => void
}

/**
 * 统一的域名规则编辑器（全站唯一一份，收敛 filter/direct/global 三套复制状态）：
 * - 每行 = 前缀选择 + 值输入 + 单条删除（内存副本上增删改，保存由外层全量提交）
 * - 批量粘贴导入：多行文本按行拆分，显式前缀（domain:/full:/keyword:/regexp:/ip:）拆分识别；
 *   裸 IPv4 / IPv4 CIDR 自动转 ip:；其余整行原样保留（无前缀裸串，设备端等同 domain:，
 *   未知前缀如 geoip: 不受干扰）
 */
export default function DomainRuleEditor({ rules, onChange }: Props) {
  const { t } = useTranslation()
  const [showImport, setShowImport] = useState(false)
  const [importText, setImportText] = useState('')

  const setRule = (i: number, next: Partial<DomainRule>) => {
    const copy = [...rules]
    copy[i] = { ...copy[i], ...next }
    onChange(copy)
  }

  const handleImport = () => {
    const lines = importText.split('\n').map(l => l.trim()).filter(Boolean)
    if (!lines.length) return
    const imported = lines.map(l => sniffIpPrefix(parseDomainRule(l)))
    // 与现有值去重（按 prefix+value）
    const seen = new Set(rules.map(r => r.prefix + r.value))
    const merged = [...rules]
    let added = 0
    for (const r of imported) {
      if (seen.has(r.prefix + r.value)) continue
      seen.add(r.prefix + r.value)
      merged.push(r)
      added++
    }
    if (added > 0) onChange(merged)
    showToast(t('vpc:domainShared.imported', { added, total: lines.length }))
    setImportText('')
    setShowImport(false)
  }

  // 导出当前规则为多行文本（复制到剪贴板，可直接粘回批量导入或留档）
  const handleExport = async () => {
    const text = rules.filter(r => r.value.trim()).map(r => r.prefix + r.value.trim()).join('\n')
    try {
      await navigator.clipboard.writeText(text)
      showToast(t('vpc:domainShared.exported', { count: rules.filter(r => r.value.trim()).length }))
    } catch {
      showToast(t('vpc:domainShared.exportFailed'), 'error')
    }
  }

  return (
    <div className="space-y-1.5">
      {rules.map((r, i) => (
        <div key={i} className="flex items-center gap-1.5">
          <RulePrefixCell rule={r} rules={rules} index={i} onChange={onChange} />
          <input value={r.value} onChange={e => setRule(i, { value: e.target.value })}
            placeholder={rulePlaceholder(r.prefix)} className={`flex-1 ${inputCls}`} />
          <button onClick={() => onChange(rules.filter((_, j) => j !== i))}
            title={t('button.delete')}
            className={`${btnMini} text-red-400 hover:text-red-500 hover:bg-red-50`}>
            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          </button>
        </div>
      ))}
      <div className="flex items-center gap-3">
        <button onClick={() => onChange([...rules, { prefix: 'domain:', value: '' }])}
          className="flex items-center gap-1 text-xs text-blue-500 hover:text-blue-600 transition-colors">
          <Plus size={14} />
          {t('vpc:domainShared.addRule')}
        </button>
        <button onClick={() => { setShowImport(!showImport); setImportText('') }}
          className="text-xs text-gray-400 hover:text-blue-600 transition-colors">
          {t('vpc:domainShared.batchImport')}
        </button>
        {rules.some(r => r.value.trim()) && (
          <button onClick={handleExport}
            className="text-xs text-gray-400 hover:text-blue-600 transition-colors">
            {t('vpc:domainShared.exportRules')}
          </button>
        )}
      </div>
      {showImport && (
        <div className="space-y-1.5 p-2 bg-blue-50/50 border border-blue-100 rounded-lg">
          <textarea value={importText} onChange={e => setImportText(e.target.value)}
            rows={4}
            placeholder={t('vpc:domainShared.importPlaceholder')}
            className={`w-full font-mono ${inputCls} resize-y`} />
          <p className="text-[10px] text-gray-400">{t('vpc:domainShared.importHint')}</p>
          <div className="flex justify-end gap-2">
            <button onClick={() => setShowImport(false)} className="px-2 py-1 text-[11px] text-gray-500 rounded hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
            <button onClick={handleImport} className="px-2 py-1 text-[11px] bg-blue-500 text-white rounded hover:bg-blue-600 transition-colors">{t('vpc:domainShared.import')}</button>
          </div>
        </div>
      )}
    </div>
  )
}

// 行内规则徽标渲染（总览表用）：前缀彩色徽标 + 值
export function RuleChip({ rule }: { rule: DomainRule }) {
  return (
    <span className="text-[10px] px-2 py-1 rounded-md font-mono border border-gray-100 bg-gray-50 inline-flex items-center gap-1">
      <PrefixBadge prefix={rule.prefix} />
      <span className="text-gray-700">{rule.value}</span>
    </span>
  )
}
