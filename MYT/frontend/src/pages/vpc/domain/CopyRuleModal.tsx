import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as VpcApi from '../../../api/vpcApi'
import { VpcNodeRule } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import VpcModal from '../shared/VpcModal'
import PrefixBadge from '../shared/PrefixBadge'
import { DomainRow, rulesForContainer } from './useDomainOverview'

interface Props {
  selectedIP: string
  /** 被复制的规则行 */
  source: DomainRow
  /** 可选目标容器（可写容器列表） */
  containers: VpcNodeRule[]
  /** 总览现有行（合并模式取目标容器现有规则） */
  overviewRows: DomainRow[]
  onClose: () => void
  onSaved: () => void
}

/** 复制规则到其他容器弹窗：目标容器多选，默认替换目标现有过滤规则（可选合并追加） */
export default function CopyRuleModal({ selectedIP, source, containers, overviewRows, onClose, onSaved }: Props) {
  const { t } = useTranslation()
  const [targets, setTargets] = useState<Set<string>>(new Set())
  const [replace, setReplace] = useState(true)
  const [copying, setCopying] = useState(false)

  const toggle = (cid: string, checked: boolean) => {
    const next = new Set(targets)
    if (checked) next.add(cid)
    else next.delete(cid)
    setTargets(next)
  }

  const handleCopy = async () => {
    if (!selectedIP || targets.size === 0) return
    setCopying(true)
    try {
      let ds: string[] = [source.raw]
      if (!replace) {
        const current = rulesForContainer(overviewRows, 'filter', source.containerID, 'container')
        ds = [...new Set([...current, source.raw])]
      }
      for (const cid of targets) {
        await VpcApi.SetDomainFilter(selectedIP, cid, ds)
      }
      showToast(t('vpc:domain.copiedTo', { count: targets.size }))
      onSaved()
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setCopying(false) }
  }

  return (
    <VpcModal title={t('vpc:domain.copyToTitle')} onClose={onClose} width="w-80"
      footer={<>
        <button onClick={onClose} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
        <button onClick={handleCopy} disabled={copying || targets.size === 0}
          className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
          {copying ? t('vpc:domain.processing') : t('button.confirm')}
        </button>
      </>}>
      <div className="space-y-2.5">
        <div className="flex items-center gap-1.5 text-xs text-gray-600">
          <PrefixBadge prefix={source.rule.prefix} />
          <span className="font-mono truncate">{source.rule.value}</span>
        </div>
        <label className="flex items-center gap-1.5 text-[11px] text-gray-500">
          <input type="checkbox" checked={replace} onChange={e => setReplace(e.target.checked)} className="accent-blue-500" />
          {t('vpc:domain.copyReplaceHint')}
        </label>
        <div className="space-y-1 max-h-56 overflow-y-auto">
          {containers.map(r => (
            <label key={r.containerID} className="flex items-center gap-2 px-2 py-1.5 rounded-lg hover:bg-gray-50 cursor-pointer transition-colors">
              <input type="checkbox" checked={targets.has(r.containerID)}
                onChange={e => toggle(r.containerID, e.target.checked)}
                className="accent-blue-500" />
              <span className="text-xs text-gray-700 truncate">{r.containerName} ({r.containerIP})</span>
            </label>
          ))}
        </div>
      </div>
    </VpcModal>
  )
}
