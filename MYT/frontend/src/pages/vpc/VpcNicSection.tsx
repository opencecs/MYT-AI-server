import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import * as VpcApi from '../../api/vpcApi'
import { PrivateNic, PublicNicData } from '../../types/index'
import { showToast } from '../../components/Toast'
import { useConfirm } from '../../hooks/useConfirm'
import ConfirmDialog from '../../components/ConfirmDialog'

interface Props {
  selectedIP: string
}

type NicTab = 'private' | 'public'

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
      <div className="flex gap-1 border-b border-gray-200 pb-1">
        <button onClick={() => setTab('private')} className={`px-3 py-1 text-xs ${tab === 'private' ? 'text-blue-500 border-b-2 border-blue-500' : 'text-gray-500'}`}>{t('vpc:nic.tabPrivate')}</button>
        <button onClick={() => setTab('public')} className={`px-3 py-1 text-xs ${tab === 'public' ? 'text-blue-500 border-b-2 border-blue-500' : 'text-gray-500'}`}>{t('vpc:nic.tabPublic')}</button>
      </div>

      {tab === 'private' && (
        <div className="space-y-2">
          <button onClick={() => setShowCreatePriv(true)} disabled={!selectedIP}
            className="px-2 py-1 text-xs bg-blue-50 text-blue-600 rounded hover:bg-blue-100 disabled:opacity-50">{t('vpc:nic.createPrivate')}</button>

          {privateNics.length === 0 ? (
            <div className="text-xs text-gray-300 text-center py-4">{selectedIP ? t('vpc:nic.noPrivate') : t('vpc:nic.selectDevice')}</div>
          ) : (
            <div className="border border-gray-100 rounded-lg overflow-hidden">
              <div className="grid grid-cols-[1fr_1fr_120px] px-3 py-1.5 text-[10px] text-gray-400 bg-gray-100 font-medium text-center">
                <span>{t('table.name')}</span><span>{t('vpc:nic.colCidr')}</span><span>{t('table.action')}</span>
              </div>
              {privateNics.map(nic => (
                <div key={nic.name} className="grid grid-cols-[1fr_1fr_120px] px-3 py-1.5 border-t border-gray-50 text-center items-center">
                  <span className="text-xs text-gray-700 font-mono">{nic.name}</span>
                  <span className="text-xs text-gray-500 font-mono">{nic.cidr}</span>
                  <div className="flex gap-1 justify-center">
                    <button onClick={() => { setEditNic(nic); setEditCidr(nic.cidr.replace('/24', '')) }} className="px-1.5 py-0.5 text-[10px] rounded bg-blue-50 text-blue-600 hover:bg-blue-100">{t('button.edit')}</button>
                    <button onClick={() => ask(t('vpc:nic.deleteNicTitle'), t('vpc:nic.confirmDeleteNic', { name: nic.name }), async () => { await handleDeletePriv(nic.name); setConfirmAction(null) })} className="px-1.5 py-0.5 text-[10px] rounded bg-red-50 text-red-500 hover:bg-red-100">{t('button.delete')}</button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {tab === 'public' && (
        <div className="space-y-3">
          {publicNics.length === 0 && !pubLoading ? (
            <div className="text-xs text-gray-300 text-center py-4">{selectedIP ? t('vpc:nic.noPublic') : t('vpc:nic.selectDevice')}</div>
          ) : publicNics.map((nic) => (
            <div key={`${nic.type}-${nic.gw}`} className="border border-gray-100 rounded-lg p-3 space-y-2">
              <div className="text-xs font-medium text-gray-700">{nic.type === 'physical' ? t('vpc:nic.physicalNic') : t('vpc:nic.macVlan')}</div>
              <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
                <span className="text-gray-400">{t('vpc:nic.gateway')}</span><span className="text-gray-600 font-mono">{nic.gw || '-'}</span>
                <span className="text-gray-400">{t('vpc:nic.subnet')}</span><span className="text-gray-600 font-mono">{nic.subnet || '-'}</span>
              </div>
              {nic.type === 'macvlan' && (
                <div className="flex items-center gap-2">
                  <button onClick={() => ask(t('vpc:nic.updateMacVlanTitle'), t('vpc:nic.updateMacVlanConfirm'), () => handleUpdateMacVlan(nic))}
                    disabled={pubSubmitting} className="px-2 py-1 text-xs bg-blue-50 text-blue-600 rounded hover:bg-blue-100 disabled:opacity-50">
                    {pubSubmitting ? t('vpc:nic.updating') : nic.isNew ? t('vpc:nic.createMacVlan') : t('vpc:nic.updateMacVlan')}
                  </button>
                  {nic.isNew && <span className="text-[10px] text-amber-500">{t('vpc:nic.updateMacVlanHint')}</span>}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* 创建私有网卡弹窗 */}
      {showCreatePriv && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div className="bg-white rounded-lg shadow-xl p-4 w-72" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium text-gray-700">{t('vpc:nic.createPrivateTitle')}</h4>
              <button onClick={() => setShowCreatePriv(false)} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            </div>
            <div className="space-y-2">
              <input value={privName} onChange={e => setPrivName(e.target.value)} placeholder={t('vpc:nic.nicNamePlaceholder')}
                className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none focus:border-blue-400" />
              <input value={privCidr} onChange={e => setPrivCidr(e.target.value)} placeholder={t('vpc:nic.ipSegmentPlaceholder')}
                className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none focus:border-blue-400" />
              <span className="text-[10px] text-gray-400">{t('vpc:nic.autoSubnetHint')}</span>
            </div>
            <div className="flex justify-end gap-2 mt-3">
              <button onClick={() => setShowCreatePriv(false)} className="px-3 py-1 text-xs text-gray-500">{t('button.cancel')}</button>
              <button onClick={handleCreatePriv} disabled={privSubmitting || !privName.trim() || !privCidr.trim()}
                className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">
                {privSubmitting ? t('vpc:nic.creating') : t('button.create')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 编辑私有网卡弹窗 */}
      {editNic && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30">
          <div className="bg-white rounded-lg shadow-xl p-4 w-72" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-sm font-medium text-gray-700">{t('vpc:nic.editTitle', { name: editNic.name })}</h4>
              <button onClick={() => setEditNic(null)} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
            </div>
            <input value={editCidr} onChange={e => setEditCidr(e.target.value)} placeholder={t('vpc:nic.ipSegment')}
              className="w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none focus:border-blue-400" />
            <div className="flex justify-end gap-2 mt-3">
              <button onClick={() => setEditNic(null)} className="px-3 py-1 text-xs text-gray-500">{t('button.cancel')}</button>
              <button onClick={handleEditPriv} disabled={privSubmitting}
                className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50">
                {privSubmitting ? t('vpc:nic.saving') : t('button.save')}
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

export default VpcNicSection
