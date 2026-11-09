import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Globe } from 'lucide-react'
import * as VpcApi from '../../../api/vpcApi'
import { VpcNodeRule } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import VpcModal from '../shared/VpcModal'
import TargetPickerList from '../shared/TargetPickerList'
import DomainRuleEditor from '../shared/DomainRuleEditor'
import { DomainRule, buildDomains, parseDomainRule, filterSelectableRules } from '../shared/VpcDomainShared'
import { DomainRow, RuleKind, rulesForContainer } from './useDomainOverview'

export type AddRuleMode = 'filter' | 'direct' | 'global'

interface Props {
  selectedIP: string
  /** 打开模式（由入口按钮决定提交的 API）：filter→SetDomainFilter、direct→SetDomainDirect、global→SetGlobalDomainFilter */
  mode: AddRuleMode
  allRules: VpcNodeRule[]
  /** 总览现有行（预填回显数据源） */
  overviewRows: DomainRow[]
  onClose: () => void
  onSaved: () => void
}

const EMPTY: DomainRule[] = [{ prefix: 'domain:', value: '' }]

/**
 * 域名规则添加弹窗（左右布局）：左列容器单选（含固定全局项），右列 DomainRuleEditor 预填回显。
 * 保存语义 = 对选中目标全量 Set（设备端 API 即全量替换语义），复用与行内编辑相同的保存分支。
 * 交互一致性约定：新增一律走本弹窗；表格内编辑已有规则仍走行内编辑器——两者共用
 * DomainRuleEditor 与「全量 Set 提交」语义，保存后统一由父组件 onChanged() 刷新总览。
 */
export default function AddRuleModal({ selectedIP, mode, allRules, overviewRows, onClose, onSaved }: Props) {
  const { t } = useTranslation()
  const containers = useMemo(() => filterSelectableRules(allRules), [allRules])
  const globalLock = mode === 'global'
  const [target, setTarget] = useState(globalLock ? 'global' : '')

  const titles = {
    filter: t('vpc:domain.addFilterRule'),
    direct: t('vpc:domain.addDirectRule'),
    global: t('vpc:domain.setGlobalFilter'),
  }

  /** 预填目标现有规则回显：全局→global filter；容器→该容器当前模式规则（空时给一条空行） */
  const prefill = useMemo((): DomainRule[] => {
    if (!target) return EMPTY
    const kind: RuleKind = mode === 'direct' ? 'direct' : 'filter'
    const scope = target === 'global' ? 'global' : 'container'
    const cid = target === 'global' ? 'global' : target
    const raws = rulesForContainer(overviewRows, kind, cid, scope)
    return raws.length ? raws.map(parseDomainRule) : EMPTY
  }, [target, mode, overviewRows])

  const [rules, setRules] = useState<DomainRule[]>(EMPTY)
  const [prefillTarget, setPrefillTarget] = useState('')
  // target 变化时重置编辑器为该目标现有规则（不做脏检查，保存才落——切换即重置是约定行为）
  if (target !== prefillTarget) {
    setPrefillTarget(target)
    setRules(prefill)
  }
  const [saving, setSaving] = useState(false)

  /** 保存 = 对选中目标全量提交（与行内编辑 handleSave 相同的 API 分支） */
  const handleSave = async () => {
    if (!selectedIP) return
    if (target !== 'global' && !target) { showToast(t('vpc:toast.selectContainer'), 'error'); return }
    const ds = buildDomains(rules)
    if (!ds.length) { showToast(t('vpc:toast.fillDomainRules'), 'error'); return }
    setSaving(true)
    try {
      if (target === 'global') {
        await VpcApi.SetGlobalDomainFilter(selectedIP, ds)
      } else if (mode === 'direct') {
        await VpcApi.SetDomainDirect(selectedIP, target, ds)
      } else {
        await VpcApi.SetDomainFilter(selectedIP, target, ds)
      }
      showToast(t('vpc:toast.setSuccess'))
      onSaved()
    } catch { showToast(t('vpc:toast.setFailed'), 'error') }
    finally { setSaving(false) }
  }

  return (
    <VpcModal title={titles[mode]} subtitle={t('vpc:domain.addRuleSubtitle')} onClose={onClose} width="w-[720px]"
      footer={<>
        <button onClick={onClose} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
        <button onClick={handleSave} disabled={saving || !target}
          className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
          {saving ? t('vpc:domain.processing') : t('vpc:domainShared.saveApply')}
        </button>
      </>}>
      <div className="grid grid-cols-[200px_1fr] gap-4 h-[52vh]">
        {/* 左列：目标单选（global 模式固定全局不可换） */}
        <div className="border-r border-gray-100 pr-3 flex flex-col min-h-0">
          <p className="text-[10px] text-gray-400 mb-1.5 shrink-0">{t('vpc:domainShared.selectTarget')}</p>
          <TargetPickerList
            global
            globalLock={globalLock}
            containers={containers}
            value={target}
            onChange={setTarget} />
        </div>
        {/* 右列：选中目标现有规则预填回显 + 编辑 */}
        <div className="flex flex-col min-h-0 overflow-y-auto">
          <div className="flex items-center gap-1.5 mb-2 shrink-0">
            {target === 'global'
              ? <><Globe size={13} className="text-amber-500" /><span className="text-xs font-medium text-gray-700">{t('vpc:domain.globalScope')}</span></>
              : <span className="text-xs font-medium text-gray-700 truncate">{containers.find(c => c.containerID === target)?.containerName}</span>}
            <span className={`text-[10px] px-2 py-0.5 rounded-full ${mode === 'direct' ? 'bg-emerald-50 text-emerald-600' : 'bg-blue-50 text-blue-600'}`}>
              {t(mode === 'direct' ? 'vpc:domain.badgeDirect' : 'vpc:domain.badgeFilter')}
            </span>
          </div>
          {!target ? (
            <p className="text-[11px] text-gray-400 py-8 text-center">{t('vpc:domainShared.selectTargetFirst')}</p>
          ) : (
            <DomainRuleEditor rules={rules} onChange={setRules} />
          )}
          {target && (
            <p className="text-[10px] text-gray-400 mt-2 shrink-0">{t('vpc:domain.fullSubmitHint')}</p>
          )}
        </div>
      </div>
    </VpcModal>
  )
}
