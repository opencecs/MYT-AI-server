import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo, AndroidProfile } from '../../../../types/index'
import { getErrorMessage } from '../../../../utils/error'
import { showToast } from '../../../../components/Toast'
import { useModalEnter } from '../../../../hooks/useModalAnimation'

interface Props {
  android: AndroidInfo
  onClose: () => void
}

const inputCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200 focus:border-blue-400 focus:outline-none'
const labelCls = 'text-xs text-gray-500 mb-1 block'

/** 空档案（WS 快照未下发 profile 时的预填兜底） */
function emptyProfile(): AndroidProfile {
  return { latitude: 0, longitude: 0, country: '', operator_numeric: '', operator_name: '', cell_id: '', phone_number: '', root: false }
}

/**
 * V4 设备档案弹窗（POST /android/configure）：
 * 经纬度 / 国家代码 / 运营商编号与名称 / 小区 ID / 手机号 / root 开关，覆盖式写入。
 * 预填取 android.profile（WS snapshot 下发，config.AndroidInfo.Profile）；
 * 保存成功后靠 androidlist:updated 事件回填新值（Go 侧乐观补丁）。
 */
export default function DeviceProfileModal({ android, onClose }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const { t } = useTranslation()
  const [profile, setProfile] = useState<AndroidProfile>(emptyProfile)
  const [loading, setLoading] = useState(false)

  // 预填：android.profile 缺失字段按零值兜底（WS snapshot 可能为部分下发）
  useEffect(() => {
    if (android.profile) setProfile({ ...emptyProfile(), ...android.profile })
  }, [android.profile])

  const set = (patch: Partial<AndroidProfile>) => setProfile(p => ({ ...p, ...patch }))

  const handleApply = async () => {
    setLoading(true)
    try {
      await SdkApiService.ConfigureAndroidProfile(android.ip, android.name, profile)
      showToast(t('host:deviceProfile.saveSuccess'), 'success')
      onClose()
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally {
      setLoading(false)
    }
  }

  const num = (v: string): number => {
    const n = parseFloat(v)
    return isNaN(n) ? 0 : n
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[440px] flex flex-col modal-content max-h-[85vh]" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-base font-semibold text-gray-800">{t('host:deviceProfile.title')}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="px-6 py-4 space-y-3 overflow-auto">
          <div className="flex gap-3">
            <div className="flex-1">
              <label className={labelCls}>{t('host:deviceProfile.latitude')}</label>
              <input type="text" value={profile.latitude || ''} onChange={e => set({ latitude: num(e.target.value) })}
                placeholder="31.2304" className={inputCls} />
            </div>
            <div className="flex-1">
              <label className={labelCls}>{t('host:deviceProfile.longitude')}</label>
              <input type="text" value={profile.longitude || ''} onChange={e => set({ longitude: num(e.target.value) })}
                placeholder="121.4737" className={inputCls} />
            </div>
          </div>
          <div>
            <label className={labelCls}>{t('host:deviceProfile.country')}</label>
            <input type="text" value={profile.country} onChange={e => set({ country: e.target.value })}
              placeholder="cn" className={inputCls} />
          </div>
          <div className="flex gap-3">
            <div className="flex-1">
              <label className={labelCls}>{t('host:deviceProfile.operatorNumeric')}</label>
              <input type="text" value={profile.operator_numeric} onChange={e => set({ operator_numeric: e.target.value })}
                placeholder="46000" className={inputCls} />
            </div>
            <div className="flex-1">
              <label className={labelCls}>{t('host:deviceProfile.operatorName')}</label>
              <input type="text" value={profile.operator_name} onChange={e => set({ operator_name: e.target.value })}
                placeholder="CMCC" className={inputCls} />
            </div>
          </div>
          <div className="flex gap-3">
            <div className="flex-1">
              <label className={labelCls}>{t('host:deviceProfile.cellId')}</label>
              <input type="text" value={profile.cell_id} onChange={e => set({ cell_id: e.target.value })}
                className={inputCls} />
            </div>
            <div className="flex-1">
              <label className={labelCls}>{t('host:deviceProfile.phoneNumber')}</label>
              <input type="text" value={profile.phone_number} onChange={e => set({ phone_number: e.target.value })}
                className={inputCls} />
            </div>
          </div>
          <label className="flex items-center gap-2 pt-1 cursor-pointer">
            <input type="checkbox" checked={profile.root} onChange={e => set({ root: e.target.checked })}
              className="w-4 h-4 accent-blue-500" />
            <span className="text-sm text-gray-700">{t('host:deviceProfile.root')}</span>
          </label>
          {!android.profile && (
            <p className="text-xs text-amber-600 bg-amber-50 border border-amber-100 rounded-lg px-3 py-2">
              {t('host:deviceProfile.noProfileHint')}
            </p>
          )}
        </div>
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={handleApply} disabled={loading}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">
            {loading ? t('host:deviceProfile.saving') : t('host:deviceProfile.apply')}
          </button>
        </div>
      </div>
    </div>
  )
}
