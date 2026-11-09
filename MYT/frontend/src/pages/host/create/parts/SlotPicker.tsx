import { useTranslation } from 'react-i18next'
import type { useCreateInstanceForm } from '../useCreateInstanceForm'
import type { useDerivedState } from '../lib/useCreateInstanceDerived'

type F = ReturnType<typeof useCreateInstanceForm>
type D = ReturnType<typeof useDerivedState>

export function SlotPicker({ form, derived }: { form: F; derived: D }) {
  const { t } = useTranslation()
  return (
    <div className="w-[240px] p-5 flex flex-col">
      <div className="flex items-center justify-between mb-1">
        <label className="text-xs text-gray-500">{t('host:slotPicker.selectSlot')}</label>
        <span className="text-[10px] text-gray-400">{t('host:slotPicker.selected', { count: form.selectedSlots.length })}</span>
      </div>
      <div className="flex gap-1.5 mb-3 text-[10px] text-gray-400 flex-wrap">
        <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-blue-400 inline-block" />{t('host:slotPicker.normal')}</span>
        <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-yellow-400 inline-block" />{t('host:slotPicker.expiring')}</span>
        <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-red-400 inline-block" />{t('host:slotPicker.expired')}</span>
        <span className="flex items-center gap-0.5"><span className="w-2 h-2 rounded-full bg-gray-300 inline-block" />{t('host:slotPicker.unsubscribed')}</span>
      </div>
      <div className="grid grid-cols-4 gap-2 flex-1 overflow-y-auto content-start">
        {derived.allSlots.map(idx => {
          const isSelected = form.selectedSlots.includes(idx)
          const slotAndroids = derived.androidsBySlot.get(idx) || []
          const hasRunning = slotAndroids.some(a => a.status === 'running')
          const hasAny = slotAndroids.length > 0
          return (
            <button key={idx} onClick={() => derived.toggleSlot(idx)}
              className={`aspect-square rounded-lg border-2 transition-all flex flex-col items-center justify-center gap-0.5 ${derived.slotBorderColor(idx)} ${isSelected ? 'bg-blue-500 text-white border-blue-500 shadow-sm' : 'bg-white hover:bg-gray-50'}`}
            >
              <span className={`w-1.5 h-1.5 rounded-full ${isSelected ? 'bg-white' : derived.slotDotColor(idx)}`} />
              <span className="text-xs font-semibold leading-none">{idx}</span>
              {hasRunning && <span className={`text-[9px] leading-none ${isSelected ? 'text-blue-100' : 'text-green-500'}`}>{t('host:slotPicker.running')}</span>}
              {hasAny && !hasRunning && <span className={`text-[9px] leading-none ${isSelected ? 'text-blue-100' : 'text-gray-400'}`}>{t('host:slotPicker.occupied')}</span>}
            </button>
          )
        })}
      </div>
    </div>
  )
}
