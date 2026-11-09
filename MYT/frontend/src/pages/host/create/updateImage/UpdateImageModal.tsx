import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { DeviceState, AndroidInfo } from '../../../../types/index'
import {
  DNS_OPTIONS, SANDBOX_OPTIONS, RESOLUTION_OPTIONS,
  inputCls, labelCls,
} from '../lib/CreateInstanceConstants'
import { MirrorDropdown } from '../lib/MirrorDropdown'
import { useUpdateImageState } from './useUpdateImageState'
import { useModalEnter } from '../../../../hooks/useModalAnimation'
import { useAndroidNotes } from '../../../../hooks/useNoteSync'
import Select from '../../../../components/Select'

interface Props {
  device: DeviceState
  android: AndroidInfo
  onClose: () => void
  onUpdated: () => void
}

const toggleCls = 'relative w-8 h-4 rounded-full transition-colors cursor-pointer'
const toggleDotCls = 'absolute top-0.5 left-0.5 w-3 h-3 rounded-full bg-white transition-transform shadow-sm'

function UpdateImageModal({ device, android, onClose, onUpdated }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const {
    mirrorType,
    selectedImage, setSelectedImage,
    customImageUrl, setCustomImageUrl,
    modelKind, setModelKind,
    selectedModelId, setSelectedModelId,
    selectedLocalModel, setSelectedLocalModel,
    dnsType, setDnsType, customDns, setCustomDns,
    sandboxSize, setSandboxSize, resolution, setResolution,
    androidVersion,
    enforce, setEnforce,
    enableMagisk, setEnableMagisk,
    enableGMS, setEnableGMS,
    enableBridge, setEnableBridge,
    macVlanIp, setMacVlanIp,
    macVlanGw, bridgeRange,
    networkCardType, setNetworkCardType, mytBridgeName, setMytBridgeName, privateNics,
    displayMirrors, filteredModels, localModelNames,
    error, canUpdate,
    handleUpdate,
  } = useUpdateImageState({ device, android, onUpdated, onClose })

  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-lg shadow-xl w-[640px] max-h-[85vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold text-gray-800">{t('host:update.title')}</h2>
            <p className="text-xs text-gray-400">{notes[android.id] || android.name} — {device.ip}</p>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>

        <div className="flex-1 p-5 space-y-3 overflow-y-auto">
          {mirrorType === 'unknown' && (
            <div className="text-xs text-amber-600 bg-amber-50 p-2 rounded-lg">{t('host:update.unknownMirrorType')}</div>
          )}
          <div className="flex gap-3">
            <div className="flex-1">
              <label className={labelCls}>{t('host:create.androidVersion')}</label>
              {/* 版本锁定为云机当前镜像的版本（更新不提供跨版本选择），只读展示 */}
              <div className="w-full text-sm px-3 py-1.5 rounded-lg border border-gray-200 bg-gray-50 text-gray-500 select-none">安卓 {androidVersion || '-'}</div>
            </div>
            <div className="flex-1">
              <label className={labelCls}>{t('host:create.image')}</label>
              <MirrorDropdown value={selectedImage} onChange={setSelectedImage} mirrors={displayMirrors}
                allowCustom customUrl={customImageUrl} onCustomUrlChange={setCustomImageUrl} />
            </div>
          </div>

          <div className="flex gap-3">
            <div className="flex-1">
              <label className={labelCls}>{t('host:create.modelSource')}</label>
              <Select value={modelKind} onChange={v => setModelKind(v as 'online' | 'local')} options={[
                { value: 'online', label: t('host:create.onlineModel') },
                { value: 'local', label: t('host:create.localModel') },
              ]} />
            </div>
            <div className="flex-1">
              {modelKind === 'online' ? (
                <>
                  <label className={labelCls}>{t('host:create.onlineModel')}</label>
                  <Select value={selectedModelId} onChange={v => { setSelectedModelId(v); if (v !== 'random') setSelectedLocalModel('') }} options={[
                    { value: 'random', label: t('host:create.random') },
                    ...filteredModels.map(m => ({ value: m.id, label: `${m.name} (安卓 ${m.android_version})` })),
                  ]} />
                </>
              ) : (
                <>
                  <label className={labelCls}>{t('host:create.localModel')}</label>
                  <Select value={selectedLocalModel} onChange={v => { setSelectedLocalModel(v); if (v) setSelectedModelId('random') }} options={[
                    { value: '', label: t('host:create.notUsed') },
                    { value: '__random__', label: t('host:create.random') },
                    ...localModelNames.map(n => ({ value: n, label: n })),
                  ]} />
                </>
              )}
            </div>
          </div>

          <div className="flex gap-3">
            <div className="flex-1">
              <label className={labelCls}>{t('host:create.dnsAddress')}</label>
              <div className="flex gap-2">
                <Select value={dnsType} onChange={setDnsType} options={DNS_OPTIONS.map(o => ({ value: o.value, label: t(`host:create.${o.labelKey}`) }))} />
                {dnsType === 'custom' && (
                  <input type="text" value={customDns} onChange={e => setCustomDns(e.target.value)} placeholder={t('host:update.customDnsPlaceholder')} className={`flex-1 ${inputCls}`} />
                )}
              </div>
            </div>
            <div className="flex-1">
              <label className={labelCls}>{t('host:create.sandboxSize')}</label>
              <Select value={sandboxSize} onChange={setSandboxSize} options={SANDBOX_OPTIONS.map(s => ({ value: s, label: s }))} />
            </div>
          </div>

          <div className="flex gap-3">
            <div className="flex-1">
              <label className={labelCls}>{t('host:create.resolution')}</label>
              <Select value={resolution} onChange={setResolution} options={RESOLUTION_OPTIONS.map(o => ({ value: o.value, label: t(`host:create.${o.labelKey}`) }))} />
            </div>
            <div className="flex-1">
              <label className={labelCls}>{t('host:create.networkType')}</label>
              {/* 网卡类型下拉 + 子控件同一行（高度恒定，切换不跳动）：公有=桥接开关+IP；私有=网卡选择 */}
              <div className="flex items-center gap-2">
                <Select
                  value={networkCardType}
                  onChange={v => setNetworkCardType(v as 'public' | 'private')}
                  buttonClassName="py-1.5 rounded-lg bg-gray-50 border-gray-200"
                  className="w-28 shrink-0"
                  options={[
                    { value: 'public', label: t('host:create.publicNic') },
                    { value: 'private', label: t('host:create.privateNic') },
                  ]}
                />
                {networkCardType === 'public' ? (
                  <>
                    <div className={toggleCls + (enableBridge ? ' bg-blue-500' : ' bg-gray-200') + ' shrink-0'} onClick={() => setEnableBridge(!enableBridge)}>
                      <div className={toggleDotCls + (enableBridge ? ' translate-x-4' : '')} />
                    </div>
                    <input type="text" value={enableBridge ? macVlanIp : ''} onChange={e => setMacVlanIp(e.target.value)} title={macVlanGw ? `${t('host:create.gateway')} ${macVlanGw}` : undefined} placeholder={enableBridge && bridgeRange ? `${bridgeRange.start} ~ ${bridgeRange.end}` : t('host:create.bridgeDisabled')} disabled={!enableBridge} className={`flex-1 min-w-0 ${inputCls} ${!enableBridge ? 'opacity-40 cursor-not-allowed' : ''}`} />
                  </>
                ) : (
                  <Select
                    value={mytBridgeName}
                    onChange={setMytBridgeName}
                    buttonClassName="py-1.5 rounded-lg bg-gray-50 border-gray-200"
                    className="flex-1 min-w-0"
                    options={[
                      { value: '', label: t('host:create.privateNicNone') },
                      ...privateNics.map(n => ({ value: n.name, label: `${n.name} (${n.cidr})` })),
                      ...(privateNics.length === 0 ? [{ value: '', label: t('host:create.noPrivateNic'), disabled: true }] : []),
                    ]}
                  />
                )}
              </div>
            </div>
          </div>

          <div className="flex gap-6 items-center">
            <label className="flex items-center gap-2 cursor-pointer">
              <div className={toggleCls + (enableMagisk ? ' bg-blue-500' : ' bg-gray-200')} onClick={() => setEnableMagisk(!enableMagisk)}>
                <div className={toggleDotCls + (enableMagisk ? ' translate-x-4' : '')} />
              </div>
              <div className="flex flex-col leading-tight">
                <span className="text-xs text-gray-600">Magisk</span>
                <span className="text-[10px] text-gray-300">{t('host:create.magiskHint')}</span>
              </div>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <div className={toggleCls + (enableGMS ? ' bg-blue-500' : ' bg-gray-200')} onClick={() => setEnableGMS(!enableGMS)}>
                <div className={toggleDotCls + (enableGMS ? ' translate-x-4' : '')} />
              </div>
              <div className="flex flex-col leading-tight">
                <span className="text-xs text-gray-600">GMS</span>
                <span className="text-[10px] text-gray-300">{t('host:create.gmsHint')}</span>
              </div>
            </label>
            <label className="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" checked={enforce} onChange={e => setEnforce(e.target.checked)} className="w-4 h-4 rounded border-gray-300 text-blue-500 focus:ring-blue-400" />
              <span className="text-xs text-gray-600">{t('host:create.safeMode')} <span className="text-gray-300">{t('host:create.sameVersion')}</span></span>
            </label>
          </div>
        </div>

        {error && <div className="mx-6 mb-2 text-xs text-red-500 bg-red-50 p-2 rounded-lg">{error}</div>}
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={handleUpdate} disabled={!canUpdate}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50"
          >{t('host:update.confirmUpdate')}</button>
        </div>
      </div>
    </div>
  )
}

export default UpdateImageModal

