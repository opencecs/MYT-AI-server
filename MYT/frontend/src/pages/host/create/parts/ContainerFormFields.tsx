import { useTranslation } from 'react-i18next'
import { inputCls, labelCls, SANDBOX_OPTIONS } from '../lib/CreateInstanceConstants'
import type { PrivateNic, VpcGroup, FlatVpcOption } from '../lib/networkTypes'
import Select from '../../../../components/Select'

type F = {
  // 网卡
  networkCardType: 'private' | 'public'; setNetworkCardType: (v: 'private' | 'public') => void
  mytBridgeName: string; setMytBridgeName: (v: string) => void
  privateNics: PrivateNic[]
  // 公有网卡（批量 IP 递增提示用）
  selectedSlots: number[]; count: number
  enableBridge: boolean; setEnableBridge: (v: boolean) => void
  macVlanIp: string; setMacVlanIp: (v: string) => void
  macVlanGw: string; bridgeRange: { start: string; end: string } | null
  // VPC
  vpcGroups: VpcGroup[]; vpcGroupId: string; setVpcGroupId: (v: string) => void
  vpcNodeId: string; setVpcNodeId: (v: string) => void
  vpcSelectMode: 'specified' | 'random'; setVpcSelectMode: (v: 'specified' | 'random') => void
  // 沙盒
  sandboxMode: boolean; setSandboxMode: (v: boolean) => void
  sandboxSize: string; setSandboxSize: (v: string) => void
  // 开关
  enforce: boolean; setEnforce: (v: boolean) => void
  compatMode: boolean; setCompatMode: (v: boolean) => void
  setError: (v: string) => void
}

const toggleCls = 'relative w-8 h-4 rounded-full transition-colors cursor-pointer'
const toggleDotCls = 'absolute top-0.5 left-0.5 w-3 h-3 rounded-full bg-white transition-transform shadow-sm'

// 展平 VPC 分组 -> 节点，供节点下拉使用
function flattenVpcOptions(groups: VpcGroup[]): FlatVpcOption[] {
  const result: FlatVpcOption[] = []
  for (const g of groups) {
    for (const n of g.vpcs?.list || []) {
      result.push({ groupId: g.id, groupAlias: g.alias, nodeId: n.id, nodeRemarks: n.remarks })
    }
  }
  return result
}

// 网卡类型 + 私有网卡选择 / 公有网卡独立IP
export function NetworkSection({ form }: { form: F }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-3 border border-gray-200 rounded-xl p-4 bg-white">
      <div>
        <label className={labelCls}>{t('host:create.networkType')}</label>
        <div className="flex bg-gray-100 rounded-md overflow-hidden w-fit">
          <button
            type="button"
            onClick={() => form.setNetworkCardType('public')}
            className={`px-3 h-8 text-xs transition-colors ${form.networkCardType === 'public' ? 'bg-blue-500 text-white' : 'text-gray-600 hover:bg-gray-200'}`}
          >
            {t('host:create.publicNic')}
          </button>
          <button
            type="button"
            onClick={() => form.setNetworkCardType('private')}
            className={`px-3 h-8 text-xs transition-colors ${form.networkCardType === 'private' ? 'bg-blue-500 text-white' : 'text-gray-600 hover:bg-gray-200'}`}
          >
            {t('host:create.privateNic')}
          </button>
        </div>
        <p className="text-[11px] text-gray-400 mt-1.5">
          {form.networkCardType === 'public' ? t('host:create.publicNicHint') : t('host:create.privateNicHint')}
        </p>
      </div>

      {form.networkCardType === 'public' ? (
        <div>
          <label className={labelCls}>{t('host:create.bridgeIp')} {form.macVlanGw && <span className="text-gray-400">{t('host:create.gateway')} {form.macVlanGw}</span>} {form.selectedSlots.length * form.count > 1 && form.enableBridge && <span className="text-blue-500">{t('host:create.batchIpIncrement')}</span>}</label>
          <div className="flex items-center gap-2">
            <div className={toggleCls + (form.enableBridge ? ' bg-blue-500' : ' bg-gray-200')} onClick={() => { form.setEnableBridge(!form.enableBridge); if (form.enableBridge) form.setError('') }}>
              <div className={toggleDotCls + (form.enableBridge ? ' translate-x-4' : '')} />
            </div>
            <input type="text" value={form.enableBridge ? form.macVlanIp : ''} onChange={e => form.setMacVlanIp(e.target.value)} placeholder={form.enableBridge && form.bridgeRange ? `${form.bridgeRange.start} ~ ${form.bridgeRange.end}` : t('host:create.bridgeDisabled')} disabled={!form.enableBridge} className={`flex-1 ${inputCls} ${!form.enableBridge ? 'opacity-40 cursor-not-allowed' : ''}`} />
          </div>
        </div>
      ) : (
        <div>
          <label className={labelCls}>{t('host:create.privateNicLabel')} <span className="text-gray-400">{t('host:create.privateNicBridge')}</span></label>
          <Select value={form.mytBridgeName} onChange={form.setMytBridgeName} options={[
            { value: '', label: t('host:create.privateNicNone') },
            ...form.privateNics.map(n => ({ value: n.name, label: `${n.name} (${n.cidr})` })),
            ...(form.privateNics.length === 0 ? [{ value: '', label: t('host:create.noPrivateNic'), disabled: true }] : []),
          ]} />
        </div>
      )}
    </div>
  )
}

// VPC 网络选择（分组 + 节点，可指定/随机）
export function VpcSection({ form }: { form: F }) {
  const { t } = useTranslation()
  const flatOptions = flattenVpcOptions(form.vpcGroups)
  return (
    <div className="flex gap-3 flex-wrap">
      <div className="flex-1 min-w-[140px]">
        <label className={labelCls}>{t('host:create.vpcGroup')} <span className="text-gray-400">{t('host:create.vpcOptional')}</span></label>
        <Select value={form.vpcGroupId} onChange={v => { form.setVpcGroupId(v); form.setVpcNodeId('') }} options={[
          { value: '', label: t('host:create.noVpc') },
          ...form.vpcGroups.map(g => ({ value: String(g.id), label: g.alias })),
        ]} />
      </div>
      {form.vpcGroupId && (
        <>
          <div className="flex-1 min-w-[140px]">
            <label className={labelCls}>{t('host:create.nodeSelect')}</label>
            <Select value={form.vpcSelectMode} onChange={v => form.setVpcSelectMode(v as 'specified' | 'random')} options={[
              { value: 'specified', label: t('host:create.specifiedNode') },
              { value: 'random', label: t('host:create.randomNode') },
            ]} />
          </div>
          {form.vpcSelectMode === 'specified' && (
            <div className="flex-1 min-w-[140px]">
              <label className={labelCls}>{t('host:create.vpcNode')}</label>
              <Select value={form.vpcNodeId} onChange={form.setVpcNodeId} options={[
                { value: '', label: t('host:create.selectPlease') },
                ...flatOptions.filter(o => String(o.groupId) === form.vpcGroupId).map(o => ({ value: String(o.nodeId), label: o.nodeRemarks || t('host:create.node', { id: o.nodeId }) })),
              ]} />
            </div>
          )}
        </>
      )}
    </div>
  )
}

// 自定义分辨率输入（分辨率选"自定义"时展开）
export function CustomResolutionFields({ form }: { form: { customResolution: { width: string; height: string; dpi: string }; setCustomResolution: (v: { width: string; height: string; dpi: string }) => void } }) {
  const { t } = useTranslation()
  const { width, height, dpi } = form.customResolution
  return (
    <div className="flex gap-3">
      <div className="flex-1">
        <label className={labelCls}>{t('host:create.width')}</label>
        <input type="number" value={width} onChange={e => form.setCustomResolution({ ...form.customResolution, width: e.target.value })} placeholder={t('host:create.widthPlaceholder')} className={inputCls} />
      </div>
      <div className="flex-1">
        <label className={labelCls}>{t('host:create.height')}</label>
        <input type="number" value={height} onChange={e => form.setCustomResolution({ ...form.customResolution, height: e.target.value })} placeholder={t('host:create.heightPlaceholder')} className={inputCls} />
      </div>
      <div className="flex-1">
        <label className={labelCls}>{t('host:create.dpi')}</label>
        <input type="number" value={dpi} onChange={e => form.setCustomResolution({ ...form.customResolution, dpi: e.target.value })} placeholder={t('host:create.dpiPlaceholder')} className={inputCls} />
      </div>
    </div>
  )
}

// 容器沙盒模式：下拉选择（关闭 / 16GB / 32GB / 64GB）
export function ContainerSandbox({ form }: { form: F }) {
  const { t } = useTranslation()
  const value = form.sandboxMode ? form.sandboxSize : ''
  return (
    <div className="w-44">
      <label className={labelCls}>{t('host:create.sandboxMode')}</label>
      <Select
        value={value}
        onChange={v => {
          if (v === '') { form.setSandboxMode(false); form.setSandboxSize('16GB') }
          else { form.setSandboxMode(true); form.setSandboxSize(v) }
        }}
        options={[
          { value: '', label: t('host:create.sandboxOff') },
          ...SANDBOX_OPTIONS.map(s => ({ value: s, label: s })),
        ]}
      />
    </div>
  )
}

// 容器模式开关（安全模式 + 兼容模式）
export function ContainerToggleOptions({ form }: { form: { enforce: boolean; setEnforce: (v: boolean) => void; compatMode: boolean; setCompatMode: (v: boolean) => void } }) {
  const { t } = useTranslation()
  return (
    <div className="flex gap-6 items-center pt-1">
      <label className="flex items-center gap-2 cursor-pointer">
        <input type="checkbox" checked={form.enforce} onChange={e => form.setEnforce(e.target.checked)} className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500 focus:ring-blue-400" />
        <span className="text-xs text-gray-600">{t('host:create.safeMode')} <span className="text-gray-300">{t('host:create.safeModeHint')}</span></span>
      </label>
      <label className="flex items-center gap-2 cursor-pointer">
        <div className={toggleCls + (form.compatMode ? ' bg-blue-500' : ' bg-gray-200')} onClick={() => form.setCompatMode(!form.compatMode)}>
          <div className={toggleDotCls + (form.compatMode ? ' translate-x-4' : '')} />
        </div>
        <div className="flex flex-col leading-tight"><span className="text-xs text-gray-600">{t('host:create.compatMode')}</span><span className="text-[10px] text-gray-300">{t('host:create.compatModeHint')}</span></div>
      </label>
    </div>
  )
}
