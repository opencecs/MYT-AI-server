import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { X, Smartphone } from 'lucide-react'
import { AndroidInfo } from '../../../types/index'
import { useModalEnter } from '../../../hooks/useModalAnimation'
import { useAndroidNotes } from '../../../hooks/useNoteSync'
import { statusBadge } from '../../host/components/slotDetail/slotViewUtils'

interface Props {
  slot: number
  machines: AndroidInfo[]
  onExport: (a: AndroidInfo) => void
  onClose: () => void
}

// 坑位下所有云机：一个坑位可挂多台云机，列出全部（名称+状态），每行可直接导出（复用外层 ExportConfirmModal）
export function SlotMachinesModal({ slot, machines, onExport, onClose }: Props) {
  const { t } = useTranslation()
  const { notes } = useAndroidNotes()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  return (
    <div className="fixed inset-0 bg-black/30 backdrop-blur-sm flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-2xl shadow-xl w-[420px] max-h-[70vh] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
          <h2 className="text-base font-semibold text-gray-800">{t('backup:android.slotMachinesTitle', { n: slot })}</h2>
          <button onClick={onClose} className="text-gray-300 hover:text-gray-500 transition-colors"><X size={18} /></button>
        </div>
        <div className="flex-1 overflow-y-auto p-4 space-y-1.5">
          {machines.map(m => (
            <div key={m.id} className="flex items-center gap-2.5 px-3 py-2.5 rounded-xl hover:bg-gray-50 transition-colors">
              <Smartphone size={16} className={m.status === 'running' ? 'text-green-500' : 'text-gray-300'} />
              <span className="text-sm text-gray-700 truncate flex-1" title={m.name}>{notes[m.id] || m.name}</span>
              <div className="shrink-0">{statusBadge(m.status)}</div>
              <button onClick={() => onExport(m)}
                className="shrink-0 flex items-center gap-1 text-xs px-2.5 py-1.5 rounded-md text-gray-500 hover:bg-gray-100 transition-colors">
                {t('backup:android.export')}
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
