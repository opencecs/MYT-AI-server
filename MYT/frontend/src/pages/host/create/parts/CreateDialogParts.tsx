import { useTranslation } from 'react-i18next'
import type { useCreateInstanceForm } from '../useCreateInstanceForm'

type F = ReturnType<typeof useCreateInstanceForm>

// 创建进度条
export function CreateProgress({ form }: { form: F }) {
  if (!form.creating || !form.createStep) return null
  return (
    <div className="mx-6 mb-2">
      <div className="flex items-center justify-between text-xs text-gray-500 mb-1">
        <span>{form.createStep}</span>
        {form.createProgress >= 0 && <span>{form.createProgress}% {form.createProgressText && <span className="text-gray-400 ml-1">{form.createProgressText}</span>}</span>}
      </div>
      {form.createProgress >= 0 && (
        <div className="w-full bg-gray-200 rounded-full h-1.5">
          <div className="bg-blue-500 h-1.5 rounded-full transition-all duration-300" style={{ width: `${form.createProgress}%` }} />
        </div>
      )}
    </div>
  )
}

// 坑位状态确认弹窗
export function SlotConfirmDialog({ form }: { form: F }) {
  const { t } = useTranslation()
  if (!form.slotConfirm) return null
  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-[60]">
      <div className="bg-white rounded-lg shadow-xl p-6 w-80" onClick={e => e.stopPropagation()}>
        <h3 className="text-sm font-semibold text-gray-800 mb-2">{t('host:create.slotWarningTitle')}</h3>
        <p className="text-xs text-gray-500 mb-4">{form.slotConfirm.message}</p>
        <div className="flex justify-end gap-2">
          <button onClick={() => form.setSlotConfirm(null)} className="px-3 py-1 text-xs bg-gray-100 rounded hover:bg-gray-200 text-gray-700">{t('button.cancel')}</button>
          <button onClick={form.slotConfirm.onConfirm} className="px-3 py-1 text-xs bg-blue-500 rounded hover:bg-blue-600 text-white">{t('host:create.continueCreate')}</button>
        </div>
      </div>
    </div>
  )
}
