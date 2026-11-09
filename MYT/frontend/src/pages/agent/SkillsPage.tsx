import { useState, useEffect, useCallback } from 'react'
import { Sparkles, Plus, Loader2, Search, Store, ShieldCheck, Brain, RefreshCw, Check, Download, Pencil, Trash2, X, FileCode } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import { skillsApi, type SkillItem } from './agentApi'
import type { AgentWsClient } from './agentWs'
import SkillForm from './SkillForm'
import SkillDetailDialog from './SkillDetailDialog'

interface Props {
  client: AgentWsClient | null
  onConfirm: (msg: string) => Promise<boolean>
  onAlert: (msg: string) => void
}

type Tab = 'official' | 'market' | 'custom' | 'auto'
const TABS: { id: Tab; labelKey: string; icon: typeof ShieldCheck }[] = [
  { id: 'official', labelKey: 'agent:skills.tabOfficial', icon: ShieldCheck },
  { id: 'market', labelKey: 'agent:skills.tabMarket', icon: Store },
  { id: 'custom', labelKey: 'agent:skills.tabCustom', icon: Sparkles },
  { id: 'auto', labelKey: 'agent:skills.tabAuto', icon: Brain },
]

const cardStyle = { borderColor: c.line, background: c.surface, boxShadow: '0 1px 3px rgba(0,0,0,0.04)' } as const

function SourceBadge({ label, color }: { label: string; color: string }) {
  return <span className="rounded-full px-2 py-0.5 text-[10px] font-medium" style={{ background: color + '15', color }}>{label}</span>
}

function ScriptBadge({ count }: { count: number }) {
  const { t } = useTranslation()
  if (count === 0) return null
  return <span className="flex items-center gap-0.5 rounded-full px-2 py-0.5 text-[10px]" style={{ background: c.surfaceAlt, color: c.textMuted }}><FileCode className="size-2.5" />{t('agent:skills.scriptCount', { count })}</span>
}

function IconBtn({ children, onClick, title, color }: { children: React.ReactNode; onClick: (e: React.MouseEvent) => void; title?: string; color?: string }) {
  return <button onClick={(e) => { e.stopPropagation(); onClick(e) }} title={title} className="flex size-7 items-center justify-center rounded-lg transition-colors hover:bg-black/5" style={{ color: color || c.textMuted }}>{children}</button>
}

function SkillsPage({ client, onConfirm, onAlert }: Props) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<Tab>('official')
  const [skills, setSkills] = useState<SkillItem[]>([])
  const [allMySkills, setAllMySkills] = useState<SkillItem[]>([])
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [editSkill, setEditSkill] = useState<SkillItem | undefined>()
  const [detail, setDetail] = useState<SkillItem | null>(null)
  const [busyName, setBusyName] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!client) return
    setLoading(true)
    try {
      if (tab === 'custom' || tab === 'auto') {
        const all = await skillsApi.list(client) || []
        setAllMySkills(all)
      } else if (tab === 'official') {
        setSkills(await skillsApi.official(client) || [])
      } else {
        setSkills(search ? await skillsApi.market(client, search) : [])
      }
    } catch { /* */ }
    setLoading(false)
  }, [client, tab, search])

  useEffect(() => { load() }, [load])

  const displaySkills = tab === 'custom' ? allMySkills.filter(s => s.source !== 'auto')
    : tab === 'auto' ? allMySkills.filter(s => s.source === 'auto') : skills

  const handleInstall = async (name: string) => {
    setBusyName(name)
    try { await skillsApi.install(client!, name); onAlert(t('agent:skills.toast.installed', { name })); load() } catch (e) { onAlert(t('agent:skills.toast.installFailed', { error: e })) }
    setBusyName(null)
  }
  const handleUninstall = async (name: string) => {
    if (!await onConfirm(t('agent:skills.confirm.uninstall', { name }))) return
    setBusyName(name)
    try { await skillsApi.uninstall(client!, name); onAlert(t('agent:skills.toast.uninstalled', { name })); load() } catch (e) { onAlert(t('agent:skills.toast.uninstallFailed', { error: e })) }
    setBusyName(null)
  }
  const handleDelete = async (name: string) => {
    if (!await onConfirm(t('agent:skills.confirm.delete', { name }))) return
    try { await skillsApi.delete(client!, name); onAlert(t('agent:skills.toast.deleted', { name })); load() } catch (e) { onAlert(t('agent:skills.toast.deleteFailed', { error: e })) }
  }
  const handleSave = async (skill: SkillItem) => {
    if (editSkill) { await skillsApi.update(client!, skill.name, skill); onAlert(t('agent:skills.toast.saved')) }
    else { await skillsApi.create(client!, skill); onAlert(t('agent:skills.toast.created')) }
    load()
  }
  const handleToggleAuto = async (name: string, val: boolean) => {
    try { await skillsApi.setAutoUpdate(client!, name, val); load() } catch (e) { onAlert(t('agent:skills.toast.operationFailed', { error: e })) }
  }
  const handleToggle = async (sk: SkillItem) => {
    try { await skillsApi.update(client!, sk.name, { enabled: !sk.enabled }); load() } catch (e) { onAlert(t('agent:skills.toast.operationFailed', { error: e })) }
  }

  const buildActions = (s: SkillItem) => {
    const actions: { label: string; icon: React.ReactNode; onClick: () => void; danger?: boolean }[] = []
    if (tab === 'official') {
      if (!s.installed) actions.push({ label: t('button.install'), icon: <Download className="size-3" />, onClick: () => handleInstall(s.name) })
      else {
        if (s.update_available) actions.push({ label: t('agent:skills.update'), icon: <RefreshCw className="size-3" />, onClick: () => handleInstall(s.name) })
        actions.push({ label: t('button.uninstall'), icon: <Trash2 className="size-3" />, onClick: () => handleUninstall(s.name), danger: true })
        actions.push({ label: s.auto_update ? t('agent:skills.disableAutoUpdate') : t('agent:skills.autoUpdate'), icon: <Check className="size-3" />, onClick: () => handleToggleAuto(s.name, !s.auto_update) })
      }
    } else if (tab === 'market') {
      actions.push({ label: t('button.install'), icon: <Download className="size-3" />, onClick: () => handleInstall(s.name) })
    } else {
      actions.push({ label: t('button.edit'), icon: <Pencil className="size-3" />, onClick: () => { setEditSkill(s); setShowForm(true) } })
      actions.push({ label: t('button.delete'), icon: <Trash2 className="size-3" />, onClick: () => handleDelete(s.name), danger: true })
    }
    return actions
  }

  const renderCard = (s: SkillItem) => {
    const scriptCount = s.scripts ? Object.keys(s.scripts).length : 0
    if (tab === 'official') {
      const isDefault = s.is_default
      return (
        <div key={s.name} onClick={() => setDetail(s)} className="cursor-pointer overflow-hidden rounded-xl border transition-all hover:shadow-md animate-fadeIn" style={cardStyle}>
          <div className="p-4">
            <div className="mb-1 flex flex-wrap items-center gap-2">
              <SourceBadge label={isDefault ? t('agent:skills.badgeDefault') : t('agent:skills.badgeOfficial')} color={isDefault ? c.success : c.accent} />
              {s.installed && s.update_available && <SourceBadge label={t('agent:skills.badgeUpdate')} color={c.warn} />}
            </div>
            <h3 className="text-sm font-medium" style={{ color: c.text, fontFamily: 'var(--font-mono), monospace' }}>{s.name}</h3>
            {s.description && <p className="mt-1 line-clamp-2 text-xs leading-relaxed" style={{ color: c.textMuted }}>{s.description}</p>}
          </div>
          <div className="flex items-center justify-end gap-2 border-t px-3 py-2" style={{ borderColor: c.line }}>
            {!s.installed ? (
              <button onClick={(e) => { e.stopPropagation(); handleInstall(s.name) }} disabled={busyName === s.name}
                className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-opacity disabled:opacity-50" style={{ background: c.accent }}>
                {busyName === s.name ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}{t('button.install')}
              </button>
            ) : (
              <>
                <span className="flex items-center gap-1 text-xs" style={{ color: c.success }}><Check className="size-3" />{t('agent:skills.installed')}</span>
                {s.update_available && (
                  <button onClick={(e) => { e.stopPropagation(); handleInstall(s.name) }} disabled={busyName === s.name}
                    className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-opacity disabled:opacity-50" style={{ background: `${c.warn}15`, color: c.warn }}>
                    <RefreshCw className="size-3.5" />{t('agent:skills.update')}
                  </button>
                )}
                {!isDefault && (
                  <>
                    <label className="flex items-center gap-1 text-[10px] cursor-pointer" style={{ color: c.textMuted }}>
                      <input type="checkbox" checked={s.auto_update || false} onChange={(e) => { e.stopPropagation(); handleToggleAuto(s.name, e.target.checked) }} className="size-3 rounded" />
                      {t('agent:skills.autoUpdate')}
                    </label>
                    <IconBtn onClick={() => handleUninstall(s.name)} title={t('agent:tooltip.uninstallSkill')} color={c.danger}><Trash2 className="size-3.5" /></IconBtn>
                  </>
                )}
              </>
            )}
          </div>
        </div>
      )
    }
    if (tab === 'market') {
      return (
        <div key={s.name} onClick={() => setDetail(s)} className="cursor-pointer overflow-hidden rounded-xl border transition-all hover:shadow-md animate-fadeIn" style={cardStyle}>
          <div className="p-4">
            <div className="mb-1 flex flex-wrap items-center gap-2">
              {s.author && <SourceBadge label={s.author} color={c.think} />}
              <ScriptBadge count={scriptCount} />
            </div>
            <h3 className="text-sm font-medium" style={{ color: c.text, fontFamily: 'var(--font-mono), monospace' }}>{s.name}</h3>
            {s.description && <p className="mt-1 line-clamp-2 text-xs leading-relaxed" style={{ color: c.textMuted }}>{s.description}</p>}
            {s.tags && s.tags.length > 0 && (
              <div className="mt-1.5 flex flex-wrap gap-1">
                {s.tags.slice(0, 3).map(t => <span key={t} className="rounded px-1.5 py-0.5 text-[10px]" style={{ background: c.surfaceAlt, color: c.textMuted }}>{t}</span>)}
              </div>
            )}
          </div>
          <div className="flex items-center justify-end border-t px-3 py-2" style={{ borderColor: c.line }}>
            <button onClick={(e) => { e.stopPropagation(); handleInstall(s.name) }} disabled={busyName === s.name}
              className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium text-white transition-opacity disabled:opacity-50" style={{ background: c.accent }}>
              {busyName === s.name ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}{t('button.install')}
            </button>
          </div>
        </div>
      )
    }
    // custom / auto
    const isAuto = s.source === 'auto'
    const srcColor = isAuto ? c.warn : c.success
    const srcLabel = isAuto ? t('agent:skills.sourceAuto') : t('agent:skills.sourceCustom')
    return (
      <div key={s.name} onClick={() => setDetail(s)} className="cursor-pointer overflow-hidden rounded-xl border transition-all hover:shadow-md animate-fadeIn" style={cardStyle}>
        <div className="p-4">
          <div className="mb-1 flex flex-wrap items-center gap-2">
            <SourceBadge label={srcLabel} color={srcColor} />
            <span className="rounded-full px-2 py-0.5 text-[10px]" style={{ background: s.enabled !== false ? `${c.success}15` : c.surfaceAlt, color: s.enabled !== false ? c.success : c.textMuted }}>
              {s.enabled !== false ? t('agent:skills.enabled') : t('agent:skills.disabled')}
            </span>
            <ScriptBadge count={scriptCount} />
          </div>
          <h3 className="text-sm font-medium" style={{ color: c.text, fontFamily: 'var(--font-mono), monospace' }}>{s.name}</h3>
          {s.description && <p className="mt-1 line-clamp-2 text-xs leading-relaxed" style={{ color: c.textMuted }}>{s.description}</p>}
        </div>
        <div className="flex items-center justify-end gap-0.5 border-t px-2 py-1.5" style={{ borderColor: c.line }}>
          <IconBtn onClick={() => handleToggle(s)} title={s.enabled !== false ? t('agent:tooltip.disable') : t('agent:tooltip.enable')} color={s.enabled !== false ? c.success : c.textMuted}>
            {s.enabled !== false ? <Check className="size-3.5" /> : <X className="size-3.5" />}
          </IconBtn>
          <IconBtn onClick={() => { setEditSkill(s); setShowForm(true) }} title={t('agent:tooltip.edit')}><Pencil className="size-3.5" /></IconBtn>
          <IconBtn onClick={() => handleDelete(s.name)} title={t('agent:tooltip.delete')} color={c.danger}><Trash2 className="size-3.5" /></IconBtn>
        </div>
      </div>
    )
  }

  return (
    <div className="h-full flex flex-col overflow-hidden" style={{ background: c.bg }}>
      <div className="flex items-center gap-3 border-b px-5 py-3" style={{ borderColor: c.line, background: c.surface }}>
        <Sparkles className="size-4" style={{ color: c.accent }} />
        <span className="text-sm font-semibold" style={{ color: c.text }}>{t('agent:skills.title')}</span>
        <div className="flex items-center gap-1 ml-2 rounded-lg p-0.5" style={{ background: c.surfaceAlt }}>
          {TABS.map(tabItem => {
            const Icon = tabItem.icon
            return <button key={tabItem.id} onClick={() => setTab(tabItem.id)} className="flex items-center gap-1 rounded-md px-3 py-1 text-xs transition-all"
              style={{ color: tab === tabItem.id ? c.accent : c.textMuted, background: tab === tabItem.id ? c.surface : 'transparent', boxShadow: tab === tabItem.id ? '0 1px 3px rgba(0,0,0,0.08)' : 'none' }}>
              <Icon className="size-3" />{t(tabItem.labelKey)}</button>
          })}
        </div>
        {tab === 'market' && (
          <div className="relative ml-1">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5" style={{ color: c.textMuted }} />
            <input value={search} onChange={e => setSearch(e.target.value)} onKeyDown={e => e.key === 'Enter' && load()} placeholder={t('agent:skills.searchPlaceholder')}
              className="w-44 rounded-lg border pl-8 pr-3 py-1.5 text-xs outline-none transition-colors focus:border-blue-400" style={{ borderColor: c.line, background: c.surfaceAlt, color: c.text }} />
          </div>
        )}
        <div className="ml-auto flex items-center gap-2">
          {tab === 'custom' && <button onClick={() => { setEditSkill(undefined); setShowForm(true) }} className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs text-white transition-opacity hover:opacity-90" style={{ background: c.accent }}><Plus className="size-3.5" />{t('agent:skills.newSkill')}</button>}
          <button onClick={load} title={t('agent:tooltip.refresh')} className="rounded-lg p-1.5 transition-colors hover:bg-black/5" style={{ color: c.textMuted }}><RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} /></button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-5">
        {loading ? <div className="py-20 text-center"><Loader2 className="mx-auto size-6 animate-spin" style={{ color: c.accent }} /></div>
          : displaySkills.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-3">
              <div className="flex size-14 items-center justify-center rounded-2xl" style={{ background: `${c.accent}10`, border: `1px solid ${c.accent}20` }}>
                <Sparkles className="size-7" style={{ color: c.accent, opacity: 0.5 }} />
              </div>
              <div className="text-center">
                <div className="text-sm font-medium" style={{ color: c.text }}>{search ? t('agent:skills.emptySearch') : t('agent:skills.empty')}</div>
                {!search && <div className="mt-1 text-xs" style={{ color: c.textMuted }}>{tab === 'custom' ? t('agent:skills.emptyCustomHint') : tab === 'auto' ? t('agent:skills.emptyAutoHint') : t('agent:skills.emptyOtherHint')}</div>}
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
              {displaySkills.map(renderCard)}
            </div>
          )}
      </div>

      {showForm && <SkillForm initial={editSkill} onClose={() => setShowForm(false)} onSave={handleSave} />}
      {detail && <SkillDetailDialog skill={detail} actions={buildActions(detail)} onClose={() => setDetail(null)} />}
    </div>
  )
}

export default SkillsPage
