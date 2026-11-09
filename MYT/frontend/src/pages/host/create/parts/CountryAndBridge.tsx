import { useState, useLayoutEffect } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { useCreateInstanceForm } from '../useCreateInstanceForm'
import { inputCls, selectCls, labelCls } from '../lib/CreateInstanceConstants'
import { useDropdownEnter } from '../../../../hooks/useModalAnimation'
import Select from '../../../../components/Select'

const toggleCls = 'relative w-8 h-4 rounded-full transition-colors cursor-pointer'
const toggleDotCls = 'absolute top-0.5 left-0.5 w-3 h-3 rounded-full bg-white transition-transform shadow-sm'

type F = ReturnType<typeof useCreateInstanceForm>

export function CountryAndBridge({ form }: { form: F }) {
  const { t } = useTranslation()
  const [pos, setPos] = useState({ top: 0, left: 0, width: 0 })
  useDropdownEnter(form.countryPanelRef, form.countryOpen)

  useLayoutEffect(() => {
    if (!form.countryOpen || !form.countryRef.current) return
    const update = () => {
      const rect = form.countryRef.current!.getBoundingClientRect()
      const panelH = 192
      // 优先向上展开（原 bottom-full 行为），空间不足时向下
      const top = rect.top > panelH + 8 ? rect.top - panelH - 4 : rect.bottom + 4
      setPos({ top, left: rect.left, width: rect.width })
    }
    update()
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    return () => { window.removeEventListener('scroll', update, true); window.removeEventListener('resize', update) }
  }, [form.countryOpen])

  return (
    <div className="flex gap-3">
      <div ref={form.countryRef} className="relative flex-1 min-w-0">
        {/* label 钉死单行：列宽异常时截断省略号，绝不折行把下拉框顶歪（同排对齐） */}
        <label className={labelCls + ' truncate whitespace-nowrap'}>{t('host:create.countryRegion')} <span className="text-gray-300 font-normal">{t('host:create.countryHint')}</span></label>
        <div className={selectCls + ' h-9 cursor-pointer flex items-center justify-between'} onClick={() => { form.setCountryOpen(!form.countryOpen); form.setCountrySearch('') }}>
          <span className="truncate">{form.countryCodes.length === 0 ? t('host:create.defaultCountry') : form.selectedCountryName}</span>
          <svg className={`w-3 h-3 text-gray-400 transition-transform shrink-0 ml-1 ${form.countryOpen ? 'rotate-180' : ''}`} fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" /></svg>
        </div>
        {form.countryOpen && form.countryCodes.length > 0 && createPortal(
          <div ref={form.countryPanelRef} style={{ position: 'fixed', top: pos.top, left: pos.left, width: pos.width, zIndex: 9999 }}
            className="bg-white border border-gray-200 rounded-lg shadow-xl max-h-48 flex flex-col">
            <div className="p-1.5 border-b border-gray-100">
              <input type="text" value={form.countrySearch} onChange={e => form.setCountrySearch(e.target.value)} placeholder={t('host:create.countrySearch')} className="w-full text-xs border border-gray-200 rounded px-2 py-1 outline-none focus:border-blue-400" onClick={e => e.stopPropagation()} />
            </div>
            <div className="overflow-y-auto flex-1">
              {form.filteredCountryCodes.map(c => (
                <div key={c.countryCode} className={`px-3 py-1.5 text-xs cursor-pointer hover:bg-blue-50 ${c.countryCode === form.selectedCountry ? 'bg-blue-50 text-blue-600 font-medium' : 'text-gray-700'}`} onClick={() => { form.setSelectedCountry(c.countryCode); form.setCountryOpen(false); form.setCountrySearch('') }}>
                  {c.countryName} ({c.countryCode})
                </div>
              ))}
              {form.filteredCountryCodes.length === 0 && <div className="px-3 py-2 text-xs text-gray-400 text-center">{t('host:create.noMatch')}</div>}
            </div>
          </div>,
          document.body
        )}
      </div>
      <div className="flex-1 min-w-0">
        <label className={labelCls + ' truncate whitespace-nowrap'}>{t('host:create.networkType')}</label>
        {/* 网卡类型下拉 + 子控件同一行（高度恒定，切换不跳动）：公有=桥接开关+IP；私有=网卡选择。
            设备端 V1 /android 同样支持 mytBridgeName，模拟器/容器通用。 */}
        <div className="flex items-center gap-2">
          <Select
            value={form.networkCardType}
            onChange={v => form.setNetworkCardType(v as 'public' | 'private')}
            buttonClassName="h-9 rounded-lg bg-gray-50 border-gray-200"
            className="w-28 shrink-0"
            options={[
              { value: 'public', label: t('host:create.publicNic') },
              { value: 'private', label: t('host:create.privateNic') },
            ]}
          />
          {form.networkCardType === 'public' ? (
            <>
              <div className={toggleCls + (form.enableBridge ? ' bg-blue-500' : ' bg-gray-200') + ' shrink-0'} onClick={() => { form.setEnableBridge(!form.enableBridge); if (form.enableBridge) form.setError('') }}>
                <div className={toggleDotCls + (form.enableBridge ? ' translate-x-4' : '')} />
              </div>
              <input type="text" value={form.enableBridge ? form.macVlanIp : ''} onChange={e => form.setMacVlanIp(e.target.value)} title={form.macVlanGw ? `${t('host:create.gateway')} ${form.macVlanGw}` : undefined} placeholder={form.enableBridge && form.bridgeRange ? `${form.bridgeRange.start} ~ ${form.bridgeRange.end}` : t('host:create.bridgeDisabled')} disabled={!form.enableBridge} className={`flex-1 min-w-0 h-9 ${inputCls} ${!form.enableBridge ? 'opacity-40 cursor-not-allowed' : ''}`} />
            </>
          ) : (
            <Select
              value={form.mytBridgeName}
              onChange={form.setMytBridgeName}
              buttonClassName="h-9 rounded-lg bg-gray-50 border-gray-200"
              className="flex-1 min-w-0"
              options={[
                { value: '', label: t('host:create.privateNicNone') },
                ...form.privateNics.map(n => ({ value: n.name, label: `${n.name} (${n.cidr})` })),
                ...(form.privateNics.length === 0 ? [{ value: '', label: t('host:create.noPrivateNic'), disabled: true }] : []),
              ]}
            />
          )}
        </div>
      </div>
    </div>
  )
}
