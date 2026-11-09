import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import VpcModal from '../shared/VpcModal'
import * as VpcApi from '../../../api/vpcApi'
import { VpcGroup } from '../../../types/index'
import TooltipCell from '../../../components/TooltipCell'
import { showToast } from '../../../components/Toast'

/** 支持的代理协议列表 */
const PROTOCOLS = [
  { label: 'vmess', value: '1', rule: 'protocolRules.vmess' },
  { label: 'vless', value: '2', rule: 'protocolRules.vless' },
  { label: 'ss', value: '3', rule: 'protocolRules.ss' },
  { label: 'trojan', value: '4', rule: 'protocolRules.trojan' },
  { label: 'socks', value: '5', rule: 'protocolRules.socks' },
  { label: 'http', value: '6', rule: 'protocolRules.http' },
  { label: 'wireguard', value: '7', rule: 'protocolRules.wireguard' },
  { label: 'hysteria2', value: '8', rule: 'protocolRules.hysteria2' },
  { label: 'sstp', value: '9', rule: 'protocolRules.sstp' },
]

type ViaMode = 'none' | 'vpcId' | 'address'

/** 单条地址的中转配置 */
interface ViaConfig {
  mode: ViaMode
  vpcId: number
  address: string
}

interface Props {
  selectedIP: string
  groups: VpcGroup[]
  onClose: () => void
  onRefresh: () => void
}

/** 创建 VPC 分组弹窗（订阅地址 / 代理协议两种模式） */
export default function CreateGroupModal({ selectedIP, groups, onClose, onRefresh }: Props) {
  const { t } = useTranslation()
  const [createType, setCreateType] = useState<1 | 2>(1)
  const [createAlias, setCreateAlias] = useState('')
  const [createUrl, setCreateUrl] = useState('')
  const [createProtocol, setCreateProtocol] = useState('')
  const [createAddresses, setCreateAddresses] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // socks 中转配置：按行索引存储
  const [viaConfigs, setViaConfigs] = useState<Record<number, ViaConfig>>({})

  // 从已有 VPC 分组中提取所有节点，用于下拉选择
  const existingNodes = useMemo(() => {
    const nodes: { id: number; label: string }[] = []
    for (const g of groups) {
      for (const n of g.vpcs?.list || []) {
        nodes.push({ id: n.id, label: `${g.alias} / ${n.remarks || n.protocol}` })
      }
    }
    return nodes
  }, [groups])

  const isSocks = createProtocol === '5'

  const handleCreate = async () => {
    if (!selectedIP || !createAlias.trim()) return
    if (createType === 1 && !createUrl.trim()) { showToast(t('vpc:toast.enterSubscribeUrl'), 'error'); return }
    if (createType === 2 && !createAddresses.trim()) { showToast(t('vpc:toast.enterProxyAddress'), 'error'); return }
    if (createType === 2 && !createProtocol) { showToast(t('vpc:toast.selectConfigType'), 'error'); return }

    // SSTP/WireGuard 等不支持订阅模式，必须用代理协议模式
    if (createType === 1 && /^sstp:\/\//i.test(createUrl.trim())) {
      showToast(t('vpc:toast.sstpUseProxyMode'), 'error'); return
    }

    let addresses: string[] = []
    if (createType === 2) {
      addresses = createAddresses.split(/[，,]/).map(a => a.trim()).filter(Boolean)
      if (!addresses.length) { showToast(t('vpc:toast.enterValidAddress'), 'error'); return }

      // socks/http 格式转换
      if (createProtocol === '5' || createProtocol === '6') {
        const prefix = createProtocol === '5' ? 'socks' : 'http'
        const uriPrefix = `${prefix}://`
        // 先校验：非 URI 格式的地址必须包含 IP/端口
        for (const addr of addresses) {
          if (addr.toLowerCase().startsWith(uriPrefix)) continue
          const parts = addr.split('/').filter(Boolean)
          if (!parts[0] || !parts[1]) {
            showToast(t('vpc:toast.addressFormatError', { address: addr }), 'error')
            return
          }
        }
        addresses = addresses.map(addr => {
          if (addr.toLowerCase().startsWith(uriPrefix)) return addr
          const parts = addr.split('/').filter(Boolean)
          const ip = parts[0], port = parts[1]
          const user = parts[2] || '', pass = parts[3] || ''
          const remarks = parts[4] || `${prefix}_${Math.random().toString(36).slice(2, 8)}`
          return user && pass ? `${prefix}://${user}:${pass}@${ip}:${port}#${remarks}` : `${prefix}://${ip}:${port}#${remarks}`
        })
      } else {
        // 自动补协议前缀
        const prefixMap: Record<string, string> = { '1': 'vmess://', '2': 'vless://', '3': 'ss://', '4': 'trojan://', '7': 'wireguard://', '8': 'hysteria2://', '9': 'sstp://' }
        const prefix = prefixMap[createProtocol]
        if (prefix) {
          addresses = addresses.map(a => a.toLowerCase().startsWith(prefix) ? a : prefix + a)
        }
      }
    }

    // 组装中转参数（与 addresses 一一对应）
    const viaVpcIds: number[] = []
    const viaAddresses: string[] = []
    if (isSocks && createType === 2) {
      for (let i = 0; i < addresses.length; i++) {
        const cfg = viaConfigs[i]
        if (cfg?.mode === 'vpcId' && cfg.vpcId) {
          viaVpcIds.push(cfg.vpcId)
          viaAddresses.push('')
        } else if (cfg?.mode === 'address' && cfg.address.trim()) {
          viaVpcIds.push(0)
          viaAddresses.push(cfg.address.trim())
        } else {
          viaVpcIds.push(0)
          viaAddresses.push('')
        }
      }
    }

    setSubmitting(true)
    try {
      await VpcApi.CreateVpcGroup(selectedIP, createAlias.trim(), createUrl.trim(), createType, addresses, viaVpcIds, viaAddresses)
      showToast(t('vpc:toast.createSuccess'))
      onClose()
      onRefresh()
    } catch { showToast(t('vpc:toast.createGroupFailed'), 'error') }
    finally { setSubmitting(false) }
  }

  // 当 addresses 文本变化时，解析行数
  const addressLines = useMemo(() => {
    if (createType !== 2 || !createAddresses.trim()) return []
    return createAddresses.split(/[，,]/).map(a => a.trim()).filter(Boolean)
  }, [createType, createAddresses])

  const updateViaConfig = (index: number, patch: Partial<ViaConfig>) => {
    setViaConfigs(prev => {
      const existing = prev[index]
      const base: ViaConfig = existing || { mode: 'none', vpcId: 0, address: '' }
      return {
        ...prev,
        [index]: { mode: base.mode, vpcId: base.vpcId, address: base.address, ...patch },
      }
    })
  }

  return (
    <VpcModal title={t('vpc:create.title')} onClose={onClose} width="w-[520px]"
      footer={<>
        <button onClick={onClose} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
        <button onClick={handleCreate} disabled={submitting || !createAlias.trim()}
          className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
          {submitting ? t('vpc:create.creating') : t('button.confirm')}
        </button>
      </>}>
      <div className="space-y-3">
          <div className="flex gap-3">
            <label className="flex items-center gap-1 text-xs">
              <input type="radio" checked={createType === 1} onChange={() => setCreateType(1)} />{t('vpc:create.subscribeUrl')}
            </label>
            <label className="flex items-center gap-1 text-xs">
              <input type="radio" checked={createType === 2} onChange={() => setCreateType(2)} />{t('vpc:create.proxyProtocol')}
            </label>
          </div>
          <div>
            <label className="text-[10px] text-gray-400 block mb-1">{t('vpc:create.alias')}</label>
            <input value={createAlias} onChange={e => setCreateAlias(e.target.value)} placeholder={t('vpc:create.aliasPlaceholder')}
              className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none focus:border-blue-400" />
          </div>
          {createType === 1 && (
            <div>
              <label className="text-[10px] text-gray-400 block mb-1">{t('vpc:create.subscribeUrlLabel')}</label>
              <textarea value={createUrl} onChange={e => setCreateUrl(e.target.value)} placeholder={t('vpc:create.subscribeUrlPlaceholder')} rows={2}
                className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none focus:border-blue-400 resize-y" />
            </div>
          )}
          {createType === 2 && (
            <>
              <div>
                <label className="text-[10px] text-gray-400 block mb-1">{t('vpc:create.configType')}</label>
                <select value={createProtocol} onChange={e => setCreateProtocol(e.target.value)}
                  className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none">
                  <option value="">{t('vpc:create.selectProtocol')}</option>
                  {PROTOCOLS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
                </select>
              </div>
              {createProtocol && (
                <div>
                  <label className="text-[10px] text-gray-400 block mb-1">{PROTOCOLS.find(p => p.value === createProtocol)?.label || ''} {t('vpc:create.addressLabel')}</label>
                  <textarea value={createAddresses} onChange={e => setCreateAddresses(e.target.value)}
                    placeholder={t('vpc:create.addressPlaceholder')} rows={isSocks ? 4 : 2}
                    className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none focus:border-blue-400 resize-y" />
                  <div className="text-[10px] text-gray-500 mt-1 bg-amber-50 p-2 rounded space-y-0.5 leading-relaxed border-l-2 border-amber-300">
                    <div className="font-medium text-amber-600 mb-0.5">{t('vpc:create.formatRules')}</div>
                    {PROTOCOLS.map(p => (
                      <div key={p.value} className={`${createProtocol === p.value ? 'text-amber-700 font-medium' : 'text-gray-400'}`}>
                        <span className="font-mono">{p.label}:</span> {t('vpc:' + p.rule)}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* socks 中转配置：逐条配对 */}
              {isSocks && addressLines.length > 0 && (
                <div className="space-y-2">
                  <label className="text-[10px] text-gray-400 block">{t('vpc:create.viaConfig')}</label>
                  {addressLines.map((line, i) => {
                    const cfg = viaConfigs[i] || { mode: 'none' as ViaMode, vpcId: 0, address: '' }
                    return (
                      <div key={i} className="border border-gray-100 rounded p-2 space-y-1.5 bg-gray-50/50">
                        <TooltipCell text={line} className="text-[10px] text-gray-500">
                          <span className="text-gray-400 mr-1">#{i + 1}</span>{line}
                        </TooltipCell>
                        <div className="flex items-center gap-2">
                          <select value={cfg.mode} onChange={e => updateViaConfig(i, { mode: e.target.value as ViaMode })}
                            className="text-[10px] px-1.5 py-0.5 border border-gray-200 rounded outline-none bg-white">
                            <option value="none">{t('vpc:create.viaNone')}</option>
                            <option value="vpcId">{t('vpc:create.viaVpcId')}</option>
                            <option value="address">{t('vpc:create.viaAddress')}</option>
                          </select>
                          {cfg.mode === 'vpcId' && (
                            <select value={cfg.vpcId || ''} onChange={e => updateViaConfig(i, { vpcId: Number(e.target.value) })}
                              className="flex-1 text-[10px] px-1.5 py-0.5 border border-gray-200 rounded outline-none bg-white">
                              <option value="">{t('vpc:create.selectVpcNode')}</option>
                              {existingNodes.map(n => <option key={n.id} value={n.id}>{n.label}</option>)}
                            </select>
                          )}
                          {cfg.mode === 'address' && (
                            <input value={cfg.address} onChange={e => updateViaConfig(i, { address: e.target.value })}
                              placeholder="vless://uuid@server:port?..."
                              className="flex-1 text-[10px] px-1.5 py-0.5 border border-gray-200 rounded outline-none bg-white" />
                          )}
                        </div>
                      </div>
                    )
                  })}
                </div>
              )}
            </>
          )}
      </div>
    </VpcModal>
  )
}
