import { useState, useRef} from 'react'
import { useTranslation } from 'react-i18next'
import { useModalEnter } from '../../hooks/useModalAnimation'

interface Props {
  visible: boolean
  onClose: () => void
  onSubmit: (name: string) => void
}

/** 新建分组弹窗 */
function CreateGroupModal({ visible, onClose, onSubmit }: Props) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)

  if (!visible) return null

  const handleSubmit = () => {
    if (!name.trim()) return
    onSubmit(name.trim())
    setName('')
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef}>
      <div className="bg-white rounded-xl shadow-xl w-80 max-w-[calc(100vw-32px)] p-5 modal-content">
        <h3 className="text-sm font-medium text-gray-700 mb-3">{t('groupcontrol:createGroupTitle')}</h3>
        <input
          type="text"
          placeholder={t('groupcontrol:groupNamePlaceholder')}
          value={name}
          onChange={e => setName(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') handleSubmit() }}
          autoFocus
          className="w-full px-3 py-2 text-sm border border-gray-200 rounded-lg focus:outline-none focus:border-blue-400"
        />
        <div className="flex justify-end gap-2 mt-4">
          <button
            onClick={() => { onClose(); setName('') }}
            className="px-4 py-1.5 text-xs bg-white border border-gray-200 rounded-lg hover:bg-gray-50 text-gray-600"
          >{t('button.cancel')}</button>
          <button
            onClick={handleSubmit}
            disabled={!name.trim()}
            className="px-4 py-1.5 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50"
          >{t('button.create')}</button>
        </div>
      </div>
    </div>
  )
}

export default CreateGroupModal
