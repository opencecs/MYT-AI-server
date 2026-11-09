import { useState, useRef} from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo } from '../../../../types/index'
import { getErrorMessage } from '../../../../utils/error'
import { showToast } from '../../../../components/Toast'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../utils/deviceId'
import { splitContainerName } from '../../../../utils/containerName'
import { useModalEnter } from '../../../../hooks/useModalAnimation'

interface Props {
  android: AndroidInfo
  onClose: () => void
  onRenamed?: (newName: string) => void
}

const inputCls = 'w-full bg-gray-50 text-sm px-3 py-1.5 rounded-lg border border-gray-200 focus:border-blue-400 focus:outline-none'

// 重命名云机（保留容器名前缀，只替换后面的用户可读部分）
function RenameModal({ android, onClose, onRenamed }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [prefix] = splitContainerName(android.name)
  const [newName, setNewName] = useState('')
  const [loading, setLoading] = useState(false)

  const isValid = !!newName.trim()

  const handleRename = async () => {
    if (!isValid) return
    setLoading(true)
    try {
      const finalName = prefix + newName.trim()
      await SdkApiService.RenameAndroid(getDeviceId(android, mode), android.name, finalName)
      showToast(t('host:toast.renameSuccess'), 'success')
      onRenamed?.(finalName)
      onClose()
    } catch (err) {
      showToast(getErrorMessage(err), 'error')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50 modal-overlay" ref={rootRef} onClick={onClose}>
      <div className="bg-white rounded-lg shadow-xl w-[400px] flex flex-col modal-content" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-3 border-b border-gray-100 flex items-center justify-between">
          <h2 className="text-base font-semibold text-gray-800">{t('host:rename.title')}</h2>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 text-lg leading-none">&times;</button>
        </div>
        <div className="px-6 py-4 space-y-2">
          <label className="text-xs text-gray-500">{t('host:rename.oldName')}</label>
          <input type="text" value={android.name} disabled className={inputCls + ' opacity-60'} />
          <label className="text-xs text-gray-500">{t('host:rename.newName')}</label>
          <input
            type="text" value={newName} onChange={e => setNewName(e.target.value)} autoFocus
            placeholder={t('host:rename.newNamePlaceholder')} className={inputCls}
            onKeyDown={e => { if (e.key === 'Enter') handleRename() }}
          />
          {prefix && <p className="text-xs text-gray-400">{t('host:rename.keepPrefix', { prefix })}</p>}
        </div>
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-100">
          <button onClick={onClose} className="px-4 py-1.5 text-sm bg-gray-100 text-gray-700 rounded-lg hover:bg-gray-200">{t('button.cancel')}</button>
          <button onClick={handleRename} disabled={!isValid || loading}
            className="px-4 py-1.5 text-sm bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50"
          >{loading ? t('host:rename.renaming') : t('button.confirm')}</button>
        </div>
      </div>
    </div>
  )
}

export default RenameModal
