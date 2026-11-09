import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Pencil, Trash2, HardDrive } from 'lucide-react'
import * as VpcApi from '../../../api/vpcApi'
import { PrivateNic, PublicNicData } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import { useConfirm } from '../../../hooks/useConfirm'
import ConfirmDialog from '../../../components/ConfirmDialog'
import SectionCard from '../shared/SectionCard'
import EmptyState from '../shared/EmptyState'
import VpcModal from '../shared/VpcModal'
import SegmentedControl from '../shared/SegmentedControl'

interface Props {
  selectedIP: string
}

type NicTab = 'private' | 'public'

const inputCls = 'w-full text-xs px-2 py-1.5 border border-gray-200 rounded-lg outline-none focus:border-blue-400 bg-white'
const iconBtn = 'p-1 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded-md transition-colors cursor-pointer'
const iconBtnDanger = 'p-1 text-gray-400 hover:text-red-500 hover:bg-red-50 rounded-md transition-colors cursor-pointer'

function VpcNicSection({ selectedIP }: Props) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<NicTab>('private')
  const { confirmAction, setConfirmAction, ask } = useConfirm()

  // 私有网卡
  const [privateNics, setPrivateNics] = useState<PrivateNic[]>([])
  const [showCreatePriv, setShowCreatePriv] = useState(false)
  const [privName, setPrivName] = useState('')
  const [privCidr, setPrivCidr] = useState('')
  const [editNic, setEditNic] = useState<PrivateNic | null>(null)
  const [editCidr, setEditCidr] = useState('')
  const [privSubmitting, setPrivSubmitting] = useState(false)

  // 公有网卡
  const [publicNics, setPublicNics] = useState<PublicNicData[]>([])
  const [pubLoading, setPubLoading] = useState(false)
  const [pubSubmitting, setPubSubmitting] = useState(false)

  useEffect(() => {
    if (selectedIP) {
      if (tab === 'private') loadPrivateNics()
      else loadPublicNics()
    }
  }, [selectedIP, tab])

  const loadPrivateNics = async () => {
    if (!selectedIP) return
    try {
      const list = await VpcApi.ListPrivateNics(selectedIP)
      setPrivateNics(list || [])
    } catch { setPrivateNics([]) }
  }

  const loadPublicNics = async () => {
    if (!selectedIP) return
    setPubLoading(true)
    try {
      const [nicInfo, netInfo] = await Promise.all([
        VpcApi.GetPublicNicInfo(selectedIP).catch(() => null),
        VpcApi.GetNetworkInfo(selectedIP).catch(() => null),
      ])
      const list: PublicNicData[] = []
      if (nicInfo) {
        const parseNic = (raw: Record<string, unknown>) => {
          let gw = '', subnet = ''
          if (typeof raw === 'object' && raw !== null) {
            const config = (raw.IPAM as Record<string, unknown>)?.Config as Record<string, string>[] | undefined
            if (config?.[0]) { gw = config[0].Gateway || ''; subnet = config[0].Subnet || '' }
            else { gw = (raw.gw || raw.gateway) as string || ''; subnet = raw.subnet as string || '' }
          }
          return { gw, subnet }
        }
        if (nicInfo.netWork_eth0) {
          const p = parseNic(nicInfo.netWork_eth0)
          list.push({ gw: p.gw, subnet: p.subnet, type: 'physical', isNew: false })
        }
        if (nicInfo.macVlan) {
          const p = parseNic(nicInfo.macVlan)
          list.push({ gw: p.gw, subnet: p.subnet, type: 'macvlan', isNew: false })
        }
      }
      if (list.length === 0 && netInfo) {
        list.push({ gw: netInfo.gateway, subnet: netInfo.networkCIDR, type: 'macvlan', isNew: true })
      } else if (netInfo && !list.some(n => n.type === 'macvlan')) {
        list.push({ gw: netInfo.gateway, subnet: netInfo.networkCIDR, type: 'macvlan', isNew: true })
      }
      setPublicNics(list)
    } catch { setPublicNics([]) }
    finally { setPubLoading(false) }
  }

  const handleCreatePriv = async () => {
    if (!selectedIP || !privName.trim() || !privCidr.trim()) return
    setPrivSubmitting(true)
    try {
      await VpcApi.CreatePrivateNic(selectedIP, privName.trim(), privCidr.trim() + '/24')
      showToast(t('vpc:toast.createSuccess'))
      setShowCreatePriv(false)
      setPrivName(''); setPrivCidr('')
      loadPrivateNics()
    } catch { showToast(t('vpc:toast.createFailed'), 'error') }
    finally { setPrivSubmitting(false) }
  }

  const handleEditPriv = async () => {
    if (!selectedIP || !editNic || !editCidr.trim()) return
    setPrivSubmitting(true)
    try {
      await VpcApi.UpdatePrivateNic(selectedIP, editNic.name, editCidr.trim() + '/24')
      showToast(t('vpc:toast.updateSuccess'))
      setEditNic(null)
      loadPrivateNics()
    } catch { showToast(t('vpc:toast.updateFailed'), 'error') }
    finally { setPrivSubmitting(false) }
  }

  const handleDeletePriv = async (name: string) => {
    if (!selectedIP) return
    try {
      await VpcApi.DeletePrivateNic(selectedIP, name)
      showToast(t('vpc:toast.deleteSuccess'))
      loadPrivateNics()
    } catch { showToast(t('vpc:toast.deleteFailed'), 'error') }
  }

  const handleUpdateMacVlan = async (nic: PublicNicData) => {
    if (!selectedIP) return
    setPubSubmitting(true)
    try {
      if (nic.isNew) await VpcApi.CreateMacVlan(selectedIP, nic.gw, nic.subnet)
      else await VpcApi.UpdateMacVlan(selectedIP, nic.gw, nic.subnet)
      showToast(t('vpc:toast.updateSuccess'))
      loadPublicNics()
    } catch { showToast(t('vpc:toast.updateFailed'), 'error') }
    finally { setPubSubmitting(false) }
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <SegmentedControl<'private' | 'public'>
          value={tab}
          options={[
            { key: 'private', label: t('vpc:nic.tabPrivate') },
            { key: 'public', label: t('vpc:nic.tabPublic') },
          ]}
          onChange={setTab}
        />
        {tab === 'private' && (
          <button onClick={() => setShowCreatePriv(true)} disabled={!selectedIP}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
            <Plus size={13} />
            {t('vpc:nic.createPrivate')}
          </button>
        )}
      </div>

      {tab === 'private' && (
        privateNics.length === 0 ? (
          <SectionCard>
            <EmptyState icon={HardDrive} title={selectedIP ? t('vpc:nic.noPrivate') : t('vpc:nic.selectDevice')}
              actionLabel={selectedIP ? t('vpc:nic.createPrivate') : undefined}
              onAction={selectedIP ? () => setShowCreatePriv(true) : undefined} />
          </SectionCard>
        ) : (
          <SectionCard className="overflow-hidden">
            <div className="grid grid-cols-[1fr_1fr_110px] px-4 py-2 text-[11px] text-gray-400 bg-gray-50/70 font-medium text-center border-b border-gray-100">
              <span>{t('table.name')}</span><span>{t('vpc:nic.colCidr')}</span><span>{t('table.action')}</span>
            </div>
            {privateNics.map(nic => (
              <div key={nic.name} className="grid grid-cols-[1fr_1fr_110px] px-4 py-2 border-t border-gray-50 text-center items-center hover:bg-gray-50/60 transition-colors">
                <span className="text-xs text-gray-700 font-mono">{nic.name}</span>
                <span className="text-xs text-gray-500 font-mono">{nic.cidr}</span>
                <div className="flex gap-0.5 justify-center">
                  <button title={t('button.edit')} onClick={() => { setEditNic(nic); setEditCidr(nic.cidr.replace('/24', '')) }} className={iconBtn}>
                    <Pencil size={13} />
                  </button>
                  <button title={t('button.delete')}
                    onClick={() => ask(t('vpc:nic.deleteNicTitle'), t('vpc:nic.confirmDeleteNic', { name: nic.name }), async () => { await handleDeletePriv(nic.name); setConfirmAction(null) })}
                    className={iconBtnDanger}>
                    <Trash2 size={13} />
                  </button>
                </div>
              </div>
            ))}
          </SectionCard>
        )
      )}

      {tab === 'public' && (
        publicNics.length === 0 && !pubLoading ? (
          <SectionCard>
            <EmptyState icon={HardDrive} title={selectedIP ? t('vpc:nic.noPublic') : t('vpc:nic.selectDevice')} />
          </SectionCard>
        ) :
        <div className="space-y-3">
          {publicNics.map((nic) => (
            <SectionCard key={`${nic.type}-${nic.gw}`} className="p-4 space-y-2">
              <div className="flex items-center gap-2">
                <HardDrive size={14} className="text-blue-500" />
                <span className="text-xs font-medium text-gray-700">{nic.type === 'physical' ? t('vpc:nic.physicalNic') : t('vpc:nic.macVlan')}</span>
              </div>
              <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
                <span className="text-gray-400">{t('vpc:nic.gateway')}</span><span className="text-gray-600 font-mono">{nic.gw || '-'}</span>
                <span className="text-gray-400">{t('vpc:nic.subnet')}</span><span className="text-gray-600 font-mono">{nic.subnet || '-'}</span>
              </div>
              {nic.type === 'macvlan' && (
                <div className="flex items-center gap-2">
                  <button onClick={() => ask(t('vpc:nic.updateMacVlanTitle'), t('vpc:nic.updateMacVlanConfirm'), () => handleUpdateMacVlan(nic))}
                    disabled={pubSubmitting} className="px-3 py-1.5 text-xs text-blue-600 bg-blue-50 rounded-lg hover:bg-blue-100 disabled:opacity-50 transition-colors">
                    {pubSubmitting ? t('vpc:nic.updating') : nic.isNew ? t('vpc:nic.createMacVlan') : t('vpc:nic.updateMacVlan')}
                  </button>
                  {nic.isNew && <span className="text-[11px] text-amber-500">{t('vpc:nic.updateMacVlanHint')}</span>}
                </div>
              )}
            </SectionCard>
          ))}
        </div>
      )}

      {/* 创建私有网卡弹窗 */}
      {showCreatePriv && (
        <VpcModal title={t('vpc:nic.createPrivateTitle')} onClose={() => setShowCreatePriv(false)} width="w-80"
          footer={<>
            <button onClick={() => setShowCreatePriv(false)} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
            <button onClick={handleCreatePriv} disabled={privSubmitting || !privName.trim() || !privCidr.trim()}
              className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
              {privSubmitting ? t('vpc:nic.creating') : t('button.create')}
            </button>
          </>}>
          <div className="space-y-2">
            <input value={privName} onChange={e => setPrivName(e.target.value)} placeholder={t('vpc:nic.nicNamePlaceholder')} className={inputCls} />
            <input value={privCidr} onChange={e => setPrivCidr(e.target.value)} placeholder={t('vpc:nic.ipSegmentPlaceholder')} className={inputCls} />
            <span className="text-[10px] text-gray-400">{t('vpc:nic.autoSubnetHint')}</span>
          </div>
        </VpcModal>
      )}

      {/* 编辑私有网卡弹窗 */}
      {editNic && (
        <VpcModal title={t('vpc:nic.editTitle', { name: editNic.name })} onClose={() => setEditNic(null)} width="w-80"
          footer={<>
            <button onClick={() => setEditNic(null)} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
            <button onClick={handleEditPriv} disabled={privSubmitting}
              className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 transition-colors">
              {privSubmitting ? t('vpc:nic.saving') : t('button.save')}
            </button>
          </>}>
          <input value={editCidr} onChange={e => setEditCidr(e.target.value)} placeholder={t('vpc:nic.ipSegment')} className={inputCls} />
        </VpcModal>
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={async () => { await confirmAction.onConfirm(); setConfirmAction(null) }} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

export default VpcNicSection
