import { useState, useCallback, useRef } from 'react'
import { Search, X, Loader2, Image as ImageIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { agentC as c } from './agentTheme'
import type { HistoryRecord } from './agentTypes'

interface Props {
  onSearch: (query: string, page: number, dateFrom?: string, dateTo?: string) => Promise<{ messages: HistoryRecord[]; total: number }>
  onClose: () => void
  onSelect?: (rec: HistoryRecord) => void
}

function SearchPanel({ onSearch, onClose, onSelect }: Props) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState(() => new Date().toISOString().slice(0, 10))
  const [results, setResults] = useState<HistoryRecord[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [searching, setSearching] = useState(false)
  const [hasSearched, setHasSearched] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const doSearch = useCallback(async (p = 1) => {
    if (!query.trim() && !dateFrom && !dateTo) return
    setSearching(true)
    try {
      const res = await onSearch(query, p, dateFrom || undefined, dateTo || undefined)
      setResults(res.messages)
      setTotal(res.total)
      setPage(p)
      setHasSearched(true)
    } catch { /* */ }
    setSearching(false)
  }, [query, dateFrom, dateTo, onSearch])

  const reset = () => {
    setQuery(''); setDateFrom(''); setDateTo(new Date().toISOString().slice(0, 10))
    setResults([]); setTotal(0); setHasSearched(false)
  }

  return (
    <div className="absolute right-4 z-50 mt-1 w-96 rounded-xl border shadow-xl animate-fadeIn" style={{ top: 48, borderColor: c.line, background: c.surface }}>
      <div className="flex items-center gap-2 border-b px-3 py-2" style={{ borderColor: c.line }}>
        <Search className="size-3.5 shrink-0" style={{ color: c.textMuted }} />
        <input ref={inputRef} value={query} onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && doSearch(1)}
          placeholder={t('agent:search.placeholder')} className="flex-1 bg-transparent text-sm outline-none" style={{ color: c.text }} />
        {searching && <Loader2 className="size-3.5 animate-spin" style={{ color: c.accent }} />}
        <button onClick={reset}><X className="size-3.5" style={{ color: c.textMuted }} /></button>
      </div>

      <div className="flex items-center gap-2 border-b px-3 py-1.5" style={{ borderColor: c.line }}>
        <span className="text-[10px] shrink-0" style={{ color: c.textMuted }}>{t('agent:search.date')}</span>
        <input type="date" value={dateFrom} onChange={(e) => setDateFrom(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && doSearch(1)}
          className="flex-1 rounded border bg-transparent px-1.5 py-0.5 text-[11px] outline-none" style={{ borderColor: c.line, color: c.text }} />
        <span className="text-[10px]" style={{ color: c.textMuted }}>{t('agent:search.to')}</span>
        <input type="date" value={dateTo} onChange={(e) => setDateTo(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && doSearch(1)}
          className="flex-1 rounded border bg-transparent px-1.5 py-0.5 text-[11px] outline-none" style={{ borderColor: c.line, color: c.text }} />
        <button onClick={() => doSearch(1)} className="rounded px-2 py-0.5 text-[11px] hover:bg-black/5" style={{ color: c.accent }}>{t('agent:search.searchBtn')}</button>
      </div>

      <div className="max-h-80 overflow-y-auto">
        {!hasSearched && (
          <div className="py-8 text-center text-xs" style={{ color: c.textMuted }}>{t('agent:search.emptyHint')}</div>
        )}
        {hasSearched && results.length === 0 && !searching && (
          <div className="py-8 text-center text-xs" style={{ color: c.textMuted }}>{t('agent:search.noResults')}</div>
        )}
        {results.map((msg) => (
          <div key={msg.id} onClick={() => onSelect?.(msg)}
            className="cursor-pointer border-b px-3 py-2.5 transition-colors hover:bg-black/[0.03]" style={{ borderColor: c.line }}>
            <div className="flex items-center justify-between">
              <span className="text-[10px] font-medium" style={{ color: msg.role === 'user' ? c.accent : c.think }}>
                {msg.role === 'user' ? t('agent:chat.user') : 'Agent'}
              </span>
              <span className="text-[10px]" style={{ color: c.textMuted }}>{msg.created_at || ''}</span>
            </div>
            {msg.image && <div className="mt-1 flex items-center gap-1 text-[10px]" style={{ color: c.textMuted }}><ImageIcon className="size-2.5" />{t('agent:search.withImage')}</div>}
            {msg.content && <p className="mt-1 line-clamp-2 text-xs leading-relaxed" style={{ color: c.text }}>{msg.content}</p>}
            {msg.steps && msg.steps.filter(s => s.type === 'tool').length > 0 && (
              <div className="mt-1 flex flex-wrap gap-1">
                {msg.steps.filter(s => s.type === 'tool').slice(0, 4).map((s, i) => (
                  <span key={i} className="rounded px-1.5 py-0.5 text-[10px]" style={{ background: c.surfaceAlt, color: c.textMuted }}>{s.name}</span>
                ))}
              </div>
            )}
          </div>
        ))}
        {results.length > 0 && results.length < total && (
          <button onClick={() => doSearch(page + 1)} disabled={searching}
            className="w-full py-2 text-xs hover:bg-black/[0.02]" style={{ color: c.textMuted }}>
            {searching ? <Loader2 className="mx-auto size-3.5 animate-spin" /> : t('agent:search.loadMore')}
          </button>
        )}
      </div>
      {results.length > 0 && (
        <div className="border-t px-3 py-1.5 text-[10px]" style={{ borderColor: c.line, color: c.textMuted }}>{t('agent:search.totalResults', { count: total })}</div>
      )}
    </div>
  )
}

export default SearchPanel
