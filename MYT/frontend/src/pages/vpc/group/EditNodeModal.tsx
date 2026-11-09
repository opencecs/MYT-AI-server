import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import VpcModal from '../shared/VpcModal'
import * as VpcApi from '../../../api/vpcApi'
import { VpcNode, VpcProfile } from '../../../types/index'
import { showToast } from '../../../components/Toast'

/** 编辑弹窗表单状态（全部为字符串，insecure 用下拉表达） */
interface EditForm {
  remarks: string
  server: string
  serverPort: string
  password: string
  security: string
  network: string
  host: string
  path: string
  headerType: string
  flow: string
  method: string
  sni: string
  alpn: string
  fingerprint: string
  insecure: string // 'true' | 'false' | ''
}

/** 从节点现有 profile JSON 解析初始表单值 */
function parseForm(node: VpcNode): EditForm {
  let p: Partial<VpcProfile> = {}
  try { p = JSON.parse(node.profile) || {} } catch { /* 无 profile 时保持空表单 */ }
  return {
    remarks: node.remarks || '',
    server: p.server || '',
    serverPort: p.serverPort || '',
    password: p.password || '',
    security: p.security || '',
    network: p.network || '',
    host: p.host || '',
    path: p.path || '',
    headerType: p.headerType || '',
    flow: p.flow || '',
    method: p.method || '',
    sni: p.sni || '',
    alpn: p.alpn || '',
    fingerprint: p.fingerprint || '',
    insecure: p.insecure === true ? 'true' : p.insecure === false ? 'false' : '',
  }
}

/** 表单 → profile（空值字段不下发，由设备端保留原值） */
function toProfile(form: EditForm): VpcProfile {
  const p: VpcProfile = {
    server: form.server.trim(),
    serverPort: form.serverPort.trim(),
  }
  if (form.password) p.password = form.password
  if (form.security) p.security = form.security
  if (form.network) p.network = form.network
  if (form.host) p.host = form.host
  if (form.path) p.path = form.path
  if (form.headerType) p.headerType = form.headerType
  if (form.flow) p.flow = form.flow
  if (form.method) p.method = form.method
  if (form.sni) p.sni = form.sni
  if (form.alpn) p.alpn = form.alpn
  if (form.fingerprint) p.fingerprint = form.fingerprint
  if (form.insecure) p.insecure = form.insecure === 'true'
  return p
}

const inputCls = 'w-full text-xs px-2 py-1.5 border border-gray-200 rounded outline-none focus:border-blue-400'
const labelCls = 'text-[10px] text-gray-400 block mb-1'

interface Props {
  selectedIP: string
  node: VpcNode
  onClose: () => void
  onRefresh: () => void
}

/** 编辑 VPC 节点弹窗（PUT /mytVpc） */
export default function EditNodeModal({ selectedIP, node, onClose, onRefresh }: Props) {
  const { t } = useTranslation()
  const [form, setForm] = useState<EditForm>(() => parseForm(node))
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [saving, setSaving] = useState(false)

  const set = (key: keyof EditForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm(prev => ({ ...prev, [key]: e.target.value }))

  const serverValid = form.server.trim() !== ''
  const portValid = form.serverPort.trim() !== '' && /^\d+$/.test(form.serverPort.trim())
  const canSave = serverValid && portValid && !saving

  const handleSave = async () => {
    if (!selectedIP) return
    if (!serverValid) { showToast(t('vpc:edit.serverRequired'), 'error'); return }
    if (!portValid) { showToast(t('vpc:edit.portInvalid'), 'error'); return }
    setSaving(true)
    try {
      await VpcApi.UpdateVpcConfig(selectedIP, node.id, form.remarks.trim(), toProfile(form))
      showToast(t('vpc:toast.updateSuccess'))
      onClose()
      onRefresh()
    } catch { showToast(t('vpc:toast.updateFailed'), 'error') }
    finally { setSaving(false) }
  }

  return (
    <VpcModal title={t('vpc:edit.title')} onClose={onClose}
      footer={<>
        <button onClick={onClose} className="px-3 py-1.5 text-xs text-gray-500 rounded-lg hover:bg-gray-100 transition-colors">{t('button.cancel')}</button>
        <button onClick={handleSave} disabled={!canSave}
          className="px-3 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50 disabled:cursor-not-allowed transition-colors">
          {saving ? t('vpc:edit.saving') : t('button.save')}
        </button>
      </>}>
      <div className="space-y-2.5">
          <div>
            <label className={labelCls}>{t('vpc:edit.remarks')}</label>
            <input value={form.remarks} onChange={set('remarks')} className={inputCls} />
          </div>
          <div className="grid grid-cols-[1fr_90px] gap-2">
            <div>
              <label className={labelCls}>{t('vpc:edit.server')}</label>
              <input value={form.server} onChange={set('server')} className={inputCls} />
            </div>
            <div>
              <label className={labelCls}>{t('vpc:edit.serverPort')}</label>
              <input value={form.serverPort} onChange={set('serverPort')} className={inputCls} />
            </div>
          </div>
          <div>
            <label className={labelCls}>{t('vpc:edit.password')}</label>
            <input value={form.password} onChange={set('password')} className={inputCls} />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className={labelCls}>{t('vpc:edit.security')}</label>
              <select value={form.security} onChange={set('security')} className={inputCls}>
                <option value="">{t('vpc:edit.keepOriginal')}</option>
                <option value="tls">tls</option>
                <option value="none">none</option>
              </select>
            </div>
            <div>
              <label className={labelCls}>{t('vpc:edit.network')}</label>
              <select value={form.network} onChange={set('network')} className={inputCls}>
                <option value="">{t('vpc:edit.keepOriginal')}</option>
                <option value="ws">ws</option>
                <option value="tcp">tcp</option>
                <option value="grpc">grpc</option>
              </select>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className={labelCls}>{t('vpc:edit.host')}</label>
              <input value={form.host} onChange={set('host')} className={inputCls} />
            </div>
            <div>
              <label className={labelCls}>{t('vpc:edit.path')}</label>
              <input value={form.path} onChange={set('path')} className={inputCls} />
            </div>
          </div>
          <div>
            <label className={labelCls}>{t('vpc:edit.headerType')}</label>
            <input value={form.headerType} onChange={set('headerType')} className={inputCls} />
          </div>

          <button type="button" onClick={() => setShowAdvanced(v => !v)}
            className="text-[10px] text-blue-500 hover:text-blue-600 flex items-center gap-1">
            <span className={`inline-block transition-transform ${showAdvanced ? 'rotate-90' : ''}`}>▶</span>
            {t('vpc:edit.advanced')}
          </button>
          {showAdvanced && (
            <div className="space-y-2.5 border border-gray-100 rounded p-2">
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className={labelCls}>{t('vpc:edit.flow')}</label>
                  <input value={form.flow} onChange={set('flow')} className={inputCls} />
                </div>
                <div>
                  <label className={labelCls}>{t('vpc:edit.method')}</label>
                  <input value={form.method} onChange={set('method')} className={inputCls} />
                </div>
                <div>
                  <label className={labelCls}>{t('vpc:edit.sni')}</label>
                  <input value={form.sni} onChange={set('sni')} className={inputCls} />
                </div>
                <div>
                  <label className={labelCls}>{t('vpc:edit.alpn')}</label>
                  <input value={form.alpn} onChange={set('alpn')} className={inputCls} />
                </div>
                <div>
                  <label className={labelCls}>{t('vpc:edit.fingerprint')}</label>
                  <input value={form.fingerprint} onChange={set('fingerprint')} className={inputCls} />
                </div>
                <div>
                  <label className={labelCls}>{t('vpc:edit.insecure')}</label>
                  <select value={form.insecure} onChange={set('insecure')} className={inputCls}>
                    <option value="">{t('vpc:edit.keepOriginal')}</option>
                    <option value="true">{t('vpc:edit.insecureTrue')}</option>
                    <option value="false">{t('vpc:edit.insecureFalse')}</option>
                  </select>
                </div>
              </div>
            </div>
          )}
      </div>
    </VpcModal>
  )
}
