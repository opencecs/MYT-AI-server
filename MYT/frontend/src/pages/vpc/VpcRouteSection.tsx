import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as VpcApi from '../../api/vpcApi'
import { VpcNodeRule, VpcGroup, DomainRouteEntry } from '../../types/index'
import { showToast } from '../../components/Toast'
import { useConfirm } from '../../hooks/useConfirm'
import ConfirmDialog from '../../components/ConfirmDialog'
import { DomainRule, buildDomains, RuleEditor, ContainerSelect, VpcNodeSelect, filterSelectableRules } from './VpcDomainShared'

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  rules: VpcNodeRule[]
}

interface RouteEdit {
  domainRules: DomainRule[]
  vpcID: string
}

const selectCls = 'text-xs px-2 py-1.5 border border-gray-200 rounded-lg outline-none focus:border-blue-400 bg-white'

function parseRouteToEdit(entry: DomainRouteEntry): RouteEdit {
  const domainRules: DomainRule[] = entry.domains.map(d => {
    const match = d.match(/^(domain:|full:|keyword:|regexp:)/)
    const prefix = match ? match[0] : 'domain:'
    const value = prefix ? d.slice(prefix.length) : d
    return { prefix, value }
  })
  return { domainRules: domainRules.length > 0 ? domainRules : [{ prefix: 'domain:', value: '' }], vpcID: String(entry.vpcID) }
}

function prefixColor(prefix: string): string {
  switch (prefix) {
    case 'full:': return 'bg-purple-50 text-purple-600'
    case 'keyword:': return 'bg-amber-50 text-amber-600'
    case 'regexp:': return 'bg-rose-50 text-rose-600'
    default: return 'bg-blue-50 text-blue-600'
  }
}

function VpcRouteSection({ selectedIP, groups, rules: allRules }: Props) {
  // 下拉只展示可写配置的绑定：非 running（退役/已停止）与无名的孤儿绑定排除，
  // 且 value 用 containerID（真实 ID，防同名退役容器串扰）
  const rules = filterSelectableRules(allRules)
  const { t } = useTranslation()
  const [containerID, setContainerID] = useState('')
  const [routes, setRoutes] = useState<DomainRouteEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [showDialog, setShowDialog] = useState(false)
  const [editRoutes, setEditRoutes] = useState<RouteEdit[]>([{ domainRules: [{ prefix: 'domain:', value: '' }], vpcID: '' }])
  const [submitting, setSubmitting] = useState(false)
  const { confirmAction, setConfirmAction, ask } = useConfirm()

  const loadRoutes = async (cid: string) => {
    if (!selectedIP || !cid) return
    setLoading(true)
    try {
      const result = await VpcApi.GetDomainRoutes(selectedIP, cid)
      setRoutes(result?.routes || [])
    } catch { setRoutes([]) }
    finally { setLoading(false) }
  }

  const handleOpenDialog = () => {
    if (routes.length > 0) {
      setEditRoutes(routes.map(parseRouteToEdit))
    } else {
      setEditRoutes([{ domainRules: [{ prefix: 'domain:', value: '' }], vpcID: '' }])
    }
    setShowDialog(true)
  }

  const handleSubmit = async () => {
    if (!selectedIP) return
    if (!containerID) { showToast(t('vpc:toast.selectContainer'), 'error'); return }
    const validRoutes: DomainRouteEntry[] = editRoutes
      .map(er => ({ domains: buildDomains(er.domainRules), vpcID: Number(er.vpcID) }))
      .filter(r => r.domains.length > 0 && r.vpcID > 0)
    if (validRoutes.length === 0) { showToast(t('vpc:toast.fillDomainRule'), 'error'); return }
    setSubmitting(true)
    try {
      await VpcApi.SetDomainRoutes(selectedIP, containerID, validRoutes)
      showToast(t('vpc:toast.setSuccess'))
      setShowDialog(false)
      loadRoutes(containerID)
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setSubmitting(false) }
  }

  const handleClear = async (cid: string) => {
    if (!selectedIP || !cid) return
    try {
      await VpcApi.ClearDomainRoutes(selectedIP, cid)
      showToast(t('vpc:toast.clearSuccess'))
      setRoutes([])
    } catch { showToast(t('vpc:toast.clearFailed'), 'error') }
  }

  const addEditRoute = () => {
    setEditRoutes([...editRoutes, { domainRules: [{ prefix: 'domain:', value: '' }], vpcID: '' }])
  }

  const removeEditRoute = (idx: number) => {
    setEditRoutes(editRoutes.filter((_, j) => j !== idx))
  }

  const findVpcAlias = (vpcID: number): string => {
    for (const g of groups) {
      const node = g.vpcs?.list?.find(n => n.id === vpcID)
      if (node) return `${g.alias} / ${node.remarks || node.protocol}`
    }
    return `#${vpcID}`
  }

  const findVpcGroupColor = (vpcID: number): string => {
    const colors = ['text-blue-600 bg-blue-50', 'text-emerald-600 bg-emerald-50', 'text-purple-600 bg-purple-50', 'text-amber-600 bg-amber-50', 'text-rose-600 bg-rose-50']
    const groupIdx = groups.findIndex(g => g.vpcs?.list?.some(n => n.id === vpcID))
    return colors[groupIdx >= 0 ? groupIdx % colors.length : 0]
  }

  return (
    <div className="space-y-3">
      {/* 工具栏 */}
      <div className="flex items-center gap-2">
        <select value={containerID} onChange={e => { setContainerID(e.target.value); e.target.value ? loadRoutes(e.target.value) : setRoutes([]) }}
          className={`flex-1 ${selectCls}`}>
          <option value="">{t('vpc:route.selectContainer')}</option>
          {rules.map(r => <option key={r.containerID} value={r.containerID}>{r.containerName} ({r.containerIP})</option>)}
        </select>
        <button onClick={handleOpenDialog} disabled={!containerID}
          className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors whitespace-nowrap">
          {t('vpc:route.setRoute')}
        </button>
      </div>

      {/* 路由列表 */}
      {loading ? (
        <div className="flex items-center justify-center py-12 text-gray-400">
          <svg className="w-5 h-5 animate-spin mr-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          <span className="text-xs">{t('status.loading')}</span>
        </div>
      ) : !containerID ? (
        <div className="text-center py-12">
          <svg className="w-10 h-10 text-gray-200 mx-auto mb-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg>
          <p className="text-xs text-gray-400">{t('vpc:route.selectFirst')}</p>
        </div>
      ) : routes.length === 0 ? (
        <div className="text-center py-12">
          <svg className="w-10 h-10 text-gray-200 mx-auto mb-2" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          <p className="text-xs text-gray-400 mb-3">{t('vpc:route.noRoutes')}</p>
          <button onClick={handleOpenDialog} className="px-3 py-1.5 text-xs bg-blue-50 text-blue-600 rounded-lg hover:bg-blue-100 transition-colors">
            {t('vpc:route.addRoute')}
          </button>
        </div>
      ) : (
        <>
          <div className="space-y-2">
            {routes.map((route, i) => (
              <div key={i} className="border border-gray-100 rounded-xl overflow-hidden bg-white hover:shadow-sm transition-shadow">
                {/* 路由头部：目标节点 */}
                <div className="flex items-center justify-between px-3 py-2 bg-gradient-to-r from-gray-50 to-white border-b border-gray-50">
                  <div className="flex items-center gap-2">
                    <span className="text-[10px] text-gray-300 font-mono">#{i + 1}</span>
                    <svg className="w-3.5 h-3.5 text-gray-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M5 12h14"/><polyline points="12 5 19 12 12 19"/></svg>
                    <span className={`text-xs font-medium px-2 py-0.5 rounded-md ${findVpcGroupColor(route.vpcID)}`}>
                      {findVpcAlias(route.vpcID)}
                    </span>
                  </div>
                  <span className="text-[10px] text-gray-400">{t('vpc:route.ruleCount', { count: route.domains.length })}</span>
                </div>
                {/* 域名标签 */}
                <div className="flex flex-wrap gap-1.5 px-3 py-2.5">
                  {route.domains.map((d, j) => {
                    const match = d.match(/^(domain:|full:|keyword:|regexp:)/)
                    const prefix = match ? match[0] : ''
                    const value = prefix ? d.slice(prefix.length) : d
                    return (
                      <span key={j} className="text-[10px] px-2 py-1 rounded-md font-mono border border-gray-100 bg-gray-50">
                        {prefix && <span className={prefixColor(prefix)}>{prefix.replace(':', '')}</span>}
                        <span className="text-gray-700 ml-1">{value}</span>
                      </span>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>

          {/* 底部操作 */}
          <div className="flex justify-end">
            <button onClick={() => ask(t('vpc:route.clearTitle'), t('vpc:route.confirmClear'), () => handleClear(containerID))}
              className="px-3 py-1.5 text-xs bg-red-50 text-red-500 rounded-lg hover:bg-red-100 transition-colors">
              {t('vpc:route.clearAll')}
            </button>
          </div>
        </>
      )}

      {/* 编辑弹窗 */}
      {showDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div className="bg-white rounded-2xl shadow-xl w-[520px] max-h-[85vh] flex flex-col" onClick={e => e.stopPropagation()}>
            {/* 弹窗头部 */}
            <div className="flex items-center justify-between px-5 py-3 border-b border-gray-100">
              <div>
                <h4 className="text-sm font-semibold text-gray-800">{t('vpc:route.dialogTitle')}</h4>
                <p className="text-[10px] text-gray-400 mt-0.5">{t('vpc:route.dialogSubtitle')}</p>
              </div>
              <button onClick={() => setShowDialog(false)} className="text-gray-400 hover:text-gray-600 text-lg leading-none w-7 h-7 flex items-center justify-center rounded-lg hover:bg-gray-100">&times;</button>
            </div>

            {/* 弹窗内容 */}
            <div className="flex-1 overflow-y-auto px-5 py-4 space-y-3">
              <div>
                <label className="text-[10px] text-gray-400 block mb-1">{t('vpc:route.container')}</label>
                <ContainerSelect value={containerID} onChange={setContainerID} rules={rules} />
              </div>

              {editRoutes.map((er, idx) => (
                <div key={idx} className="border border-gray-150 rounded-xl overflow-hidden">
                  {/* 路由条目头部 */}
                  <div className="flex items-center justify-between px-3 py-2 bg-gray-50 border-b border-gray-100">
                    <span className="text-[10px] text-gray-500 font-medium">{t('vpc:route.routeIndex', { index: idx + 1 })}</span>
                    {editRoutes.length > 1 && (
                      <button onClick={() => removeEditRoute(idx)} className="text-[10px] text-red-400 hover:text-red-500 px-1.5 py-0.5 rounded hover:bg-red-50 transition-colors">{t('button.delete')}</button>
                    )}
                  </div>
                  {/* 域名规则 + VPC 节点 */}
                  <div className="px-3 py-2.5 space-y-2.5">
                    <div>
                      <label className="text-[10px] text-gray-400 block mb-1.5">{t('vpc:route.domainRules')}</label>
                      <RuleEditor rules={er.domainRules} onChange={dr => { const next = [...editRoutes]; next[idx] = { ...next[idx], domainRules: dr }; setEditRoutes(next) }} />
                    </div>
                    <div>
                      <label className="text-[10px] text-gray-400 block mb-1.5">{t('vpc:route.targetVpcNode')}</label>
                      <VpcNodeSelect value={er.vpcID} onChange={v => { const next = [...editRoutes]; next[idx] = { ...next[idx], vpcID: v }; setEditRoutes(next) }} groups={groups} />
                    </div>
                  </div>
                </div>
              ))}

              <button onClick={addEditRoute} className="flex items-center gap-1 text-xs text-blue-500 hover:text-blue-600 transition-colors">
                <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                {t('vpc:route.addRouteEntry')}
              </button>
            </div>

            {/* 弹窗底部 */}
            <div className="flex justify-end gap-2 px-5 py-3 border-t border-gray-100">
              <button onClick={() => setShowDialog(false)} className="px-4 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
              <button onClick={handleSubmit} disabled={submitting}
                className="px-4 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
                {submitting ? t('vpc:route.saving') : t('button.save')}
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

export default VpcRouteSection
