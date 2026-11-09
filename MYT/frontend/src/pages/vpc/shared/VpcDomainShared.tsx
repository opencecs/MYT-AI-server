import { useTranslation } from 'react-i18next'
import { Plus, X } from 'lucide-react'
import { VpcNodeRule, VpcGroup } from '../../../types/index'

interface DomainRule { prefix: string; value: string }

const PREFIXES = ['domain:', 'full:', 'keyword:', 'regexp:', 'ip:']
const rulePlaceholder = (p: string) => p === 'full:' ? 'www.example.com' : p === 'keyword:' ? 'ads' : p === 'regexp:' ? '.*\\.tiktok\\.com$' : p === 'ip:' ? '1.2.3.4' : 'example.com'

// IP/CIDR 嗅探（保守：只认明确形态的 IPv4 与 IPv4 CIDR，避免过度智能误伤普通域名）。
// IPv6 含冒号、形态多样且易与带端口写法混淆，不自动转——需要时用显式 ip: 前缀（xray 支持 ip:::1/128）。
const IPV4_RE = /^\d{1,3}(\.\d{1,3}){3}$/
const IPV4_CIDR_RE = /^\d{1,3}(\.\d{1,3}){3}\/\d{1,2}$/
const isIpOrCidr = (v: string) => IPV4_RE.test(v) || IPV4_CIDR_RE.test(v)

// parseDomainRule 把 "domain:example.com" 拆回 {prefix, value}。
// 无前缀行保持 prefix=''（设备端语义：无前缀等同 domain:）；未知前缀（geoip: 等设备端扩展）
// 同样保持原样透传——归为 domain: 会在保存时改写设备端已有规则，破坏其原始语义。
// ip: 为已知前缀（xray 的 ip: 语法，全端口命中），可正常拆分回显编辑。
const parseDomainRule = (raw: string): DomainRule => {
  const match = raw.match(/^(domain:|full:|keyword:|regexp:|ip:)/)
  if (!match) return { prefix: '', value: raw }
  return { prefix: match[0], value: raw.slice(match[0].length) }
}

// buildDomains 把编辑行拼回设备端规则串。防呆顺序：
// ① 值框里整串粘入 "domain:x.com"（会拼成 domain:domain:x.com）→ 按 parseDomainRule 拆开、以拆出的前缀为准；
// ② 裸 IPv4 / IPv4 CIDR → 自动转 ip: 前缀（嗅探覆盖所选前缀，避免 domain:1.2.3.4 这种设备端不命中的写法）；
// ③ 其余按所选前缀。
const buildDomains = (rules: DomainRule[]): string[] =>
  rules.filter(r => r.value.trim()).map(r => {
    const value = r.value.trim()
    const parsed = parseDomainRule(value)
    if (parsed.prefix) return parsed.prefix + parsed.value
    if (isIpOrCidr(value)) return 'ip:' + value
    return (r.prefix ? r.prefix + value : value)
  })

// sniffIpPrefix 导入行归一：parseDomainRule 拆分后，值命中 IPv4/CIDR 嗅探则覆盖为 ip: 前缀
// （与 buildDomains 防呆同机制：嗅探覆盖所选前缀；显式 ip: 行保持不变）。
// 未知前缀行（geoip: 等）parse 时整行落在 value、不命中 IP 正则，原样透传不受干扰。
const sniffIpPrefix = (r: DomainRule): DomainRule =>
  r.prefix !== 'ip:' && isIpOrCidr(r.value) ? { prefix: 'ip:', value: r.value } : r

// 前缀徽标配色（原 VpcRouteSection 私有实现，收编为共享，消三处重复）。
// 空前缀（无前缀/未知前缀原样行）归入 default 蓝色系。
const prefixColor = (prefix: string): string => {
  switch (prefix) {
    case 'full:': return 'bg-purple-50 text-purple-600'
    case 'keyword:': return 'bg-amber-50 text-amber-600'
    case 'regexp:': return 'bg-rose-50 text-rose-600'
    case 'ip:': return 'bg-cyan-50 text-cyan-600'
    default: return 'bg-blue-50 text-blue-600'
  }
}

export type { DomainRule }
export { PREFIXES, rulePlaceholder, buildDomains, parseDomainRule, sniffIpPrefix, prefixColor, RulePrefixCell }

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

/**
 * 规则行的前缀单元格：
 * - 空 prefix（无前缀裸串或未知前缀原样行）→ 只读灰标展示原值，防误改前缀改写设备端规则
 * - 已知五前缀（domain:/full:/keyword:/regexp:/ip:）→ 正常下拉可改
 */
function RulePrefixCell({ rule, rules, index, onChange }: { rule: DomainRule; rules: DomainRule[]; index: number; onChange: (r: DomainRule[]) => void }) {
  const { t } = useTranslation()
  if (!rule.prefix) {
    return <span className="w-24 shrink-0 text-[10px] px-1 py-1 text-center text-gray-500 bg-gray-100 rounded-lg border border-gray-200 truncate" title={rule.value}>{t('vpc:domainShared.rawPrefix')}</span>
  }
  return (
    <select value={rule.prefix} onChange={e => { const next = [...rules]; next[index] = { ...next[index], prefix: e.target.value }; onChange(next) }}
      className={`${inputCls} w-24 shrink-0`}>
      {PREFIXES.map(p => <option key={p} value={p}>{p.replace(':', '')}</option>)}
    </select>
  )
}

export function RuleEditor({ rules, onChange }: { rules: DomainRule[]; onChange: (r: DomainRule[]) => void }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-1.5">
      {rules.map((r, i) => (
        <div key={`${r.prefix}-${i}`} className="flex items-center gap-1.5">
          <RulePrefixCell rule={r} rules={rules} index={i} onChange={onChange} />
          <input value={r.value} onChange={e => { const next = [...rules]; next[i] = { ...next[i], value: e.target.value }; onChange(next) }}
            placeholder={rulePlaceholder(r.prefix)} className={`flex-1 ${inputCls}`} />
          {rules.length > 1 && (
            <button onClick={() => onChange(rules.filter((_, j) => j !== i))}
              className="w-6 h-6 flex items-center justify-center text-red-400 hover:text-red-500 hover:bg-red-50 rounded transition-colors shrink-0">
              <X size={14} />
            </button>
          )}
        </div>
      ))}
      <button onClick={() => onChange([...rules, { prefix: 'domain:', value: '' }])}
        className="flex items-center gap-1 text-xs text-blue-500 hover:text-blue-600 transition-colors">
        <Plus size={14} />
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

/** 只读容器徽标：编辑弹窗里展示当前操作目标，杜绝弹窗内切容器把 A 的规则整写到 B 的覆盖事故 */
export function ContainerBadge({ name, ip }: { name: string; ip?: string }) {
  return (
    <div className="flex items-center gap-1.5 px-2.5 py-1.5 bg-gray-50 border border-gray-100 rounded-lg">
      <span className="w-1.5 h-1.5 rounded-full bg-blue-400 shrink-0" />
      <span className="text-xs font-medium text-gray-700 truncate">{name}</span>
      {ip && <span className="text-[10px] text-gray-400 font-mono shrink-0">{ip}</span>}
    </div>
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
