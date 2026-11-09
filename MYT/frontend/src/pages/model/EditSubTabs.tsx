import { useTranslation } from 'react-i18next'
import { OVERLAY_LABELS, type FieldDef } from './modelFields'

// --- Overlay 子Tab ---
export function OverlayTab({ keys, enabledKeys, values, onToggleKey, onChangeValue, randomizeIDs, onRandomizeChange }: {
  keys: string[]; enabledKeys: Set<string>; values: Record<string, string>
  onToggleKey: (k: string) => void; onChangeValue: (k: string, v: string) => void
  randomizeIDs: boolean; onRandomizeChange: (v: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-2">
      <label className="flex items-center gap-2 cursor-pointer">
        <input type="checkbox" checked={randomizeIDs} onChange={e => onRandomizeChange(e.target.checked)}
          className="w-3.5 h-3.5 rounded border-gray-300 text-blue-500 focus:ring-blue-400" />
        <span className="text-xs text-gray-600">{t('model:overlay.enableRandom')}</span>
      </label>
      <p className="text-[10px] text-gray-400 ml-5">{t('model:overlay.randomOffHint')}</p>
      {randomizeIDs && keys.length > 0 && (
        <div className="mt-2 space-y-1">
          <p className="text-[10px] text-gray-500 mb-1.5">{t('model:overlay.selectHint')}</p>
          {[...keys].sort().map(key => (
            <div key={key} className="flex items-center gap-2">
              <input type="checkbox" checked={enabledKeys.has(key)} onChange={() => onToggleKey(key)}
                className="w-3 h-3 rounded border-gray-300 text-blue-500 shrink-0" />
              <span className="text-[10px] text-gray-600 w-20 shrink-0">{t('model:' + (OVERLAY_LABELS[key] || key), { defaultValue: key })}</span>
              <input value={values[key] || ''} onChange={e => onChangeValue(key, e.target.value)}
                placeholder={t('model:overlay.placeholder')} className="flex-1 text-[10px] px-2 py-1 border border-gray-200 rounded outline-none focus:border-blue-400 bg-white font-mono" />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// --- 通用 dg_config 子Tab ---
export function DgConfigTab({ fields, values, onChange, hint }: { fields: FieldDef[]; values: Record<string, string>; onChange: (v: Record<string, string>) => void; hint?: string }) {
  const { t } = useTranslation()
  const update = (key: string, val: string) => onChange({ ...values, [key]: val })
  return (
    <div className="space-y-1.5">
      <p className="text-[10px] text-gray-400">{t('model:dgConfig.hint')}</p>
      {hint && <p className="text-[10px] text-amber-600 bg-amber-50 rounded px-2 py-1">{hint}</p>}
      {fields.map(f => (
        <div key={f.key} className="flex items-center gap-2">
          <span className="text-[10px] text-gray-600 w-32 shrink-0">{t('model:' + f.label, { defaultValue: f.label })}</span>
          <input value={values[f.key] || ''} onChange={e => update(f.key, e.target.value)}
            placeholder={f.placeholder ? t('model:' + f.placeholder, { defaultValue: f.key }) : f.key} className={`flex-1 text-[10px] px-2 py-1 border border-gray-200 rounded outline-none focus:border-blue-400 bg-white font-mono`} />
        </div>
      ))}
    </div>
  )
}
