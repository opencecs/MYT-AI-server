import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as VpcApi from '../../api/vpcApi'
import { VpcNodeRule } from '../../types/index'
import { showToast } from '../../components/Toast'
import { useConfirm } from '../../hooks/useConfirm'
import ConfirmDialog from '../../components/ConfirmDialog'
import { DomainRule, buildDomains, RuleEditor, ContainerSelect, filterSelectableRules } from './VpcDomainShared'

interface Props {
  selectedIP: string
  rules: VpcNodeRule[]
}

function VpcDomainSection({ selectedIP, rules: allRules }: Props) {
  // 下拉只展示可写配置的绑定：非 running（退役/已停止）与无名的孤儿绑定排除，
  // 且 value 用 containerID（真实 ID，防同名退役容器串扰）
  const rules = filterSelectableRules(allRules)
  const { t } = useTranslation()
  const [tab, setTab] = useState<'filter' | 'direct'>('filter')
  const [containerID, setContainerID] = useState('')
  const [domains, setDomains] = useState<string[]>([])
  const [, setLoading] = useState(false)
  const [globalDomains, setGlobalDomains] = useState<string[]>([])
  const [queryMode, setQueryMode] = useState<'' | 'container' | 'global'>('')
  const { confirmAction, setConfirmAction, ask } = useConfirm()

  const [showFilterDialog, setShowFilterDialog] = useState(false)
  const [filterContainer, setFilterContainer] = useState('')
  const [filterRules, setFilterRules] = useState<DomainRule[]>([{ prefix: 'domain:', value: '' }])
  const [filterSubmitting, setFilterSubmitting] = useState(false)

  const [showGlobalDialog, setShowGlobalDialog] = useState(false)
  const [globalRules, setGlobalRules] = useState<DomainRule[]>([{ prefix: 'domain:', value: '' }])
  const [globalSubmitting, setGlobalSubmitting] = useState(false)

  const [directContainer, setDirectContainer] = useState('')
  const [directDomains, setDirectDomains] = useState<string[]>([])
  const [showDirectDialog, setShowDirectDialog] = useState(false)
  const [directRules, setDirectRules] = useState<DomainRule[]>([{ prefix: 'domain:', value: '' }])
  const [directSubmitting, setDirectSubmitting] = useState(false)

  const loadFilterDomains = async (cid: string) => {
    if (!selectedIP || !cid) return
    setLoading(true)
    try {
      const result = await VpcApi.GetDomainFilters(selectedIP, cid)
      setDomains(result?.domains || [])
    } catch { setDomains([]) }
    finally { setLoading(false) }
  }

  const loadGlobalDomains = async () => {
    if (!selectedIP) return
    setLoading(true)
    setQueryMode('global')
    setContainerID('')
    try {
      const result = await VpcApi.GetGlobalDomainFilters(selectedIP)
      setGlobalDomains(result?.domains || [])
    } catch { showToast(t('vpc:toast.queryGlobalFailed'), 'error'); setGlobalDomains([]) }
    finally { setLoading(false) }
  }

  const loadDirectDomains = async (cid: string) => {
    if (!selectedIP || !cid) return
    try {
      const result = await VpcApi.GetDomainDirects(selectedIP, cid)
      setDirectDomains(result?.domains || [])
    } catch { setDirectDomains([]) }
  }

  const handleSubmitFilter = async () => {
    if (!selectedIP) return
    if (!filterContainer) { showToast(t('vpc:toast.selectContainer'), 'error'); return }
    const ds = buildDomains(filterRules)
    if (!ds.length) { showToast(t('vpc:toast.fillDomainRules'), 'error'); return }
    setFilterSubmitting(true)
    try {
      await VpcApi.SetDomainFilter(selectedIP, filterContainer, ds)
      showToast(t('vpc:toast.setSuccess'))
      setShowFilterDialog(false)
      if (containerID === filterContainer) loadFilterDomains(filterContainer)
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setFilterSubmitting(false) }
  }

  const handleSubmitGlobal = async () => {
    if (!selectedIP) return
    const ds = buildDomains(globalRules)
    if (!ds.length) { showToast(t('vpc:toast.fillDomainRules'), 'error'); return }
    setGlobalSubmitting(true)
    try {
      await VpcApi.SetGlobalDomainFilter(selectedIP, ds)
      showToast(t('vpc:toast.setSuccess'))
      setShowGlobalDialog(false)
      loadGlobalDomains()
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setGlobalSubmitting(false) }
  }

  const handleClearFilter = async (cid: string) => {
    if (!selectedIP || !cid) return
    try {
      await VpcApi.ClearDomainFilter(selectedIP, cid)
      showToast(t('vpc:toast.clearSuccess'))
      setDomains([])
    } catch { showToast(t('vpc:toast.clearFailed'), 'error') }
  }

  const handleClearGlobal = async () => {
    if (!selectedIP) return
    try {
      await VpcApi.ClearGlobalDomainFilter(selectedIP)
      showToast(t('vpc:toast.clearSuccess'))
      setGlobalDomains([])
      setQueryMode('')
    } catch { showToast(t('vpc:toast.clearFailed'), 'error') }
  }

  const handleSubmitDirect = async () => {
    if (!selectedIP) return
    if (!directContainer) { showToast(t('vpc:toast.selectContainer'), 'error'); return }
    const ds = buildDomains(directRules)
    if (!ds.length) { showToast(t('vpc:toast.fillDomain'), 'error'); return }
    setDirectSubmitting(true)
    try {
      await VpcApi.SetDomainDirect(selectedIP, directContainer, ds)
      showToast(t('vpc:toast.setSuccess'))
      setShowDirectDialog(false)
      if (directContainer) loadDirectDomains(directContainer)
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setDirectSubmitting(false) }
  }

  const handleClearDirect = async (cid: string) => {
    if (!selectedIP || !cid) return
    try {
      await VpcApi.ClearDomainDirect(selectedIP, cid)
      showToast(t('vpc:toast.clearSuccess'))
      setDirectDomains([])
    } catch { showToast(t('vpc:toast.clearFailed'), 'error') }
  }

  return (
    <div className="space-y-3">
      <div className="flex gap-1 border-b border-gray-200 pb-1">
        <button onClick={() => setTab('filter')} className={`px-3 py-1 text-xs ${tab === 'filter' ? 'text-blue-500 border-b-2 border-blue-500' : 'text-gray-500'}`}>{t('vpc:domain.tabFilter')}</button>
        <button onClick={() => setTab('direct')} className={`px-3 py-1 text-xs ${tab === 'direct' ? 'text-blue-500 border-b-2 border-blue-500' : 'text-gray-500'}`}>{t('vpc:domain.tabDirect')}</button>
      </div>

      {tab === 'filter' && (
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <button onClick={() => { setFilterContainer(''); setFilterRules([{ prefix: 'domain:', value: '' }]); setShowFilterDialog(true) }}
              className="px-2 py-1 text-xs bg-blue-50 text-blue-600 rounded hover:bg-blue-100">{t('vpc:domain.setContainerFilter')}</button>
            <button onClick={() => { setGlobalRules([{ prefix: 'domain:', value: '' }]); setShowGlobalDialog(true) }}
              className="px-2 py-1 text-xs bg-blue-50 text-blue-600 rounded hover:bg-blue-100">{t('vpc:domain.setGlobalFilter')}</button>
            <button onClick={loadGlobalDomains} className="px-2 py-1 text-xs bg-gray-50 text-gray-600 rounded hover:bg-gray-100">{t('vpc:domain.queryGlobal')}</button>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-xs text-gray-500 shrink-0">{t('vpc:domain.queryContainer')}</span>
            <select value={containerID} onChange={e => { setContainerID(e.target.value); setQueryMode('container'); e.target.value ? loadFilterDomains(e.target.value) : setDomains([]) }}
              className="flex-1 text-xs px-2 py-1 border border-gray-200 rounded outline-none">
              <option value="">{t('vpc:domain.selectContainer')}</option>
              {rules.map(r => <option key={r.containerID} value={r.containerID}>{r.containerName}</option>)}
            </select>
            {queryMode === 'global' && <span className="text-[10px] px-1.5 py-0.5 bg-amber-50 text-amber-600 rounded">{t('vpc:domain.global')}</span>}
            {queryMode === 'container' && containerID && <span className="text-[10px] px-1.5 py-0.5 bg-blue-50 text-blue-600 rounded">{containerID}</span>}
          </div>

          <div className="border border-gray-100 rounded-lg overflow-hidden">
            {(queryMode === 'container' ? domains : queryMode === 'global' ? globalDomains : []).length === 0 ? (
              <div className="text-xs text-gray-300 text-center py-4">{queryMode ? t('vpc:domain.noRules') : t('vpc:domain.selectOrQuery')}</div>
            ) : (
              (queryMode === 'container' ? domains : globalDomains).map((d, i) => (
                <div key={`${d.slice(0, 20)}-${i}`} className="text-xs text-gray-600 px-3 py-1 border-t border-gray-50 font-mono">{d}</div>
              ))
            )}
          </div>

          {queryMode === 'container' && containerID && domains.length > 0 && (
            <button onClick={() => ask(t('vpc:domain.clearFilterTitle'), t('vpc:domain.confirmClearFilter'), () => handleClearFilter(containerID))}
              className="px-2 py-1 text-xs bg-red-50 text-red-500 rounded hover:bg-red-100">{t('vpc:domain.clear')}</button>
          )}
          {queryMode === 'global' && globalDomains.length > 0 && (
            <button onClick={() => ask(t('vpc:domain.clearGlobalFilterTitle'), t('vpc:domain.confirmClearGlobal'), handleClearGlobal)}
              className="px-2 py-1 text-xs bg-red-50 text-red-500 rounded hover:bg-red-100">{t('vpc:domain.clearGlobal')}</button>
          )}
        </div>
      )}

      {tab === 'direct' && (
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <button onClick={() => { setDirectContainer(''); setDirectRules([{ prefix: 'domain:', value: '' }]); setShowDirectDialog(true) }}
              className="px-2 py-1 text-xs bg-blue-50 text-blue-600 rounded hover:bg-blue-100">{t('vpc:domain.setDirect')}</button>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-gray-500 shrink-0">{t('vpc:domain.queryContainer')}</span>
            <select value={directContainer} onChange={e => { setDirectContainer(e.target.value); e.target.value ? loadDirectDomains(e.target.value) : setDirectDomains([]) }}
              className="flex-1 text-xs px-2 py-1 border border-gray-200 rounded outline-none">
              <option value="">{t('vpc:domain.selectContainer')}</option>
              {rules.map(r => <option key={r.containerID} value={r.containerID}>{r.containerName}</option>)}
            </select>
          </div>
          {directContainer && (
            <>
              <div className="border border-gray-100 rounded-lg overflow-hidden">
                {directDomains.length === 0 ? (
                  <div className="text-xs text-gray-300 text-center py-4">{t('vpc:domain.noDirectRules')}</div>
                ) : directDomains.map((d, i) => (
                  <div key={`${d.slice(0, 20)}-${i}`} className="text-xs text-gray-600 px-3 py-1 border-t border-gray-50 font-mono">{d}</div>
                ))}
              </div>
              {directDomains.length > 0 && (
                <button onClick={() => ask(t('vpc:domain.clearDirectTitle'), t('vpc:domain.confirmClearDirect'), () => handleClearDirect(directContainer))}
                  className="px-2 py-1 text-xs bg-red-50 text-red-500 rounded hover:bg-red-100">{t('vpc:domain.clear')}</button>
              )}
            </>
          )}
        </div>
      )}

      {showFilterDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div className="bg-white rounded-lg shadow-xl p-4 w-80" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium text-gray-700">{t('vpc:domain.setContainerFilterTitle')}</h4>
              <button onClick={() => setShowFilterDialog(false)} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            </div>
            <div className="space-y-2">
              <ContainerSelect value={filterContainer} onChange={setFilterContainer} rules={rules} />
              <RuleEditor rules={filterRules} onChange={setFilterRules} />
            </div>
            <div className="flex justify-end gap-2 mt-3">
              <button onClick={() => setShowFilterDialog(false)} className="px-3 py-1 text-xs text-gray-500">{t('button.cancel')}</button>
              <button onClick={handleSubmitFilter} disabled={filterSubmitting}
                className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">
                {filterSubmitting ? t('vpc:domain.processing') : t('button.confirm')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showGlobalDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div className="bg-white rounded-lg shadow-xl p-4 w-80" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium text-gray-700">{t('vpc:domain.setGlobalFilterTitle')}</h4>
              <button onClick={() => setShowGlobalDialog(false)} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            </div>
            <RuleEditor rules={globalRules} onChange={setGlobalRules} />
            <div className="flex justify-end gap-2 mt-3">
              <button onClick={() => setShowGlobalDialog(false)} className="px-3 py-1 text-xs text-gray-500">{t('button.cancel')}</button>
              <button onClick={handleSubmitGlobal} disabled={globalSubmitting}
                className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">
                {globalSubmitting ? t('vpc:domain.processing') : t('button.confirm')}
              </button>
            </div>
          </div>
        </div>
      )}

      {showDirectDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div className="bg-white rounded-lg shadow-xl p-4 w-80" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium text-gray-700">{t('vpc:domain.setDirectTitle')}</h4>
              <button onClick={() => setShowDirectDialog(false)} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            </div>
            <div className="space-y-2">
              <ContainerSelect value={directContainer} onChange={setDirectContainer} rules={rules} />
              <RuleEditor rules={directRules} onChange={setDirectRules} />
            </div>
            <div className="flex justify-end gap-2 mt-3">
              <button onClick={() => setShowDirectDialog(false)} className="px-3 py-1 text-xs text-gray-500">{t('button.cancel')}</button>
              <button onClick={handleSubmitDirect} disabled={directSubmitting}
                className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">
                {directSubmitting ? t('vpc:domain.processing') : t('button.confirm')}
              </button>
            </div>
          </div>
        </div>
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={async () => { await confirmAction.onConfirm(); setConfirmAction(null) }} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

export default VpcDomainSection
