import { useState, useEffect } from 'react'
import { Dialogs } from '@wailsio/runtime'
import * as AppService from '../../../bindings/github.com/moyunteng/myt/appservice'
import * as ImageService from '../../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import ConfirmDialog from '../../components/ConfirmDialog'
import { showToast } from '../../components/Toast'
import { useTranslation } from 'react-i18next'

function StorageSection() {
  const { t } = useTranslation()
  const [imagePath, setImagePath] = useState('')
  const [dataPath, setDataPath] = useState('')
  const [changing, setChanging] = useState<'image' | 'data' | null>(null)
  const [clearing, setClearing] = useState(false)
  const [confirmAction, setConfirmAction] = useState<{ title: string; message: string; onConfirm: () => void | Promise<void> } | null>(null)

  useEffect(() => {
    ImageService.GetImageStoragePath().then((p: string) => setImagePath(p || t('settings:default'))).catch(() => setImagePath(t('settings:default')))
    AppService.GetDataStoragePath().then((p: string) => setDataPath(p || t('settings:default'))).catch(() => setDataPath(t('settings:default')))
  }, [])

  const changeImagePath = async () => {
    if (changing) return
    setChanging('image')
    try {
      const dir = await Dialogs.OpenFile({ CanChooseDirectories: true, CanChooseFiles: false, Title: t('settings:selectImageDir') })
      if (!dir || dir === '') return
      await ImageService.SetImageStoragePath(dir as string)
      const p = await ImageService.GetImageStoragePath()
      setImagePath(p || t('settings:default'))
    } catch (err) { console.error('Failed to change image path:', err); showToast(t('settings:changeImagePathFailed'), 'error') }
    finally { setChanging(null) }
  }

  const changeDataPath = async () => {
    if (changing) return
    setChanging('data')
    try {
      const dir = await Dialogs.OpenFile({ CanChooseDirectories: true, CanChooseFiles: false, Title: t('settings:selectDataDir') })
      if (!dir || dir === '') return
      await AppService.SetDataStoragePath(dir as string)
      const p = await AppService.GetDataStoragePath()
      setDataPath(p || t('settings:default'))
    } catch (err) { console.error('Failed to change data path:', err); showToast(t('settings:changeDataPathFailed'), 'error') }
    finally { setChanging(null) }
  }

  const openDir = async (fn: () => Promise<void>) => {
    try { await fn() } catch (err) { console.error('Failed to open directory:', err) }
  }

  const clearCache = () => {
    if (clearing) return
    setConfirmAction({
      title: t('settings:clearCache'),
      message: t('settings:clearCacheConfirmMsg'),
      onConfirm: async () => {
        setConfirmAction(null)
        setClearing(true)
        try {
          await AppService.ClearAllCache()
          showToast(t('settings:clearCacheSuccess'))
        } catch (err) {
          console.error('Failed to clear cache:', err)
          showToast(t('settings:clearCacheFailed'), 'error')
        } finally {
          setClearing(false)
        }
      }
    })
  }

  const b = 'px-2 py-1 text-xs rounded whitespace-nowrap cursor-pointer'
  const bBlue = `${b} bg-blue-50 text-blue-600 hover:bg-blue-100`
  const bRed = `${b} bg-red-50 text-red-500 hover:bg-red-100`
  const bDisabled = `${b} bg-gray-100 text-gray-400 cursor-not-allowed`

  return (
    <div className="space-y-2 flex flex-col">
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wider">{t('settings:storage')}</h3>
        <div className="bg-gray-50 rounded-lg p-3 space-y-2.5 flex-1 flex flex-col justify-between">
          <div className="space-y-1">
            <div className="flex items-center justify-between">
              <span className="text-xs text-gray-500">{t('settings:imageStoragePath')}</span>
              <div className="flex gap-1.5">
                <button onClick={changeImagePath} disabled={!!changing} className={changing === 'image' ? bDisabled : bBlue}>{t('settings:change')}</button>
                <button onClick={() => openDir(ImageService.OpenImageDirectory)} className={bBlue}>{t('settings:open')}</button>
              </div>
            </div>
            <div className="text-xs text-gray-400 bg-white rounded px-2 py-1.5 border border-gray-100 truncate" title={imagePath}>{imagePath}</div>
          </div>
          <div className="space-y-1">
            <div className="flex items-center justify-between">
              <span className="text-xs text-gray-500">{t('settings:dataStoragePath')}</span>
              <div className="flex gap-1.5">
                <button onClick={changeDataPath} disabled={!!changing} className={changing === 'data' ? bDisabled : bBlue}>{t('settings:change')}</button>
                <button onClick={() => openDir(AppService.OpenDataDirectory)} className={bBlue}>{t('settings:open')}</button>
              </div>
            </div>
            <div className="text-xs text-gray-400 bg-white rounded px-2 py-1.5 border border-gray-100 truncate" title={dataPath}>{dataPath}</div>
          </div>
          <div className="flex items-center justify-between pt-2 border-t border-gray-200">
            <div>
              <span className="text-xs text-gray-600">{t('settings:clearCache')}</span>
              <span className="text-[10px] text-gray-400 ml-1">{t('settings:clearCacheDesc')}</span>
            </div>
            <button onClick={clearCache} disabled={clearing} className={clearing ? bDisabled : bRed}>{clearing ? t('settings:clearing') : t('settings:clearCache')}</button>
          </div>
        </div>
      {confirmAction && (
        <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
          onConfirm={confirmAction.onConfirm} onCancel={() => setConfirmAction(null)} />
      )}
    </div>
  )
}

export default StorageSection
