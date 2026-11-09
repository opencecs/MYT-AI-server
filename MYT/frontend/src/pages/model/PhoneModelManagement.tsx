import { useState, useEffect, memo } from 'react'
import { useTranslation } from 'react-i18next'
import * as ModelApi from '../../api/phoneModelApi'
import { showToast } from '../../components/Toast'
import { useConfirm } from '../../hooks/useConfirm'
import ConfirmDialog from '../../components/ConfirmDialog'
import ModelEditModal from './ModelEditModal'
import DeviceModelPanel from './DeviceModelPanel'
import CollectionToolModal from './CollectionToolModal'

type TabKey = 'online' | 'local' | 'device'

const ANDROID_VERSIONS = [
  { label: 'Android 10', value: '10' },
  { label: 'Android 11', value: '11' },
  { label: 'Android 13', value: '13' },
  { label: 'Android 14', value: '14' },
  { label: 'Android 15', value: '15' },
  { label: 'Android 16', value: '16' },
]

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)}KB`
  return `${(bytes / 1048576).toFixed(1)}MB`
}

function PhoneModelManagement() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<TabKey>('online')
  const [loading, setLoading] = useState(false)

  const [onlineModels, setOnlineModels] = useState<ModelApi.OnlineModel[]>([])
  const [androidVersion, setAndroidVersion] = useState('14')
  const [search, setSearch] = useState('')
  const [localNames, setLocalNames] = useState<Set<string>>(new Set())
  const [downloadingId, setDownloadingId] = useState<number | null>(null)

  const [editedModels, setEditedModels] = useState<ModelApi.EditedModel[]>([])
  // 盒子机型 tab 的刷新信号：顶部刷新按钮递增，DeviceModelPanel 监听后重拉
  const [reloadKey, setReloadKey] = useState(0)

  // 编辑弹窗
  const [editingName, setEditingName] = useState('')
  const [showCollectionTool, setShowCollectionTool] = useState(false)

  const { confirmAction, setConfirmAction, ask } = useConfirm()

  useEffect(() => {
    if (tab === 'online') loadOnlineModels()
    else loadEditedModels()
  }, [tab, androidVersion])

  const loadOnlineModels = async () => {
    setLoading(true)
    try {
      const [models, names] = await Promise.all([
        ModelApi.ListOnlineModels(androidVersion).catch(() => []),
        ModelApi.GetLocalModelNames().catch(() => []),
      ])
      setOnlineModels(models || [])
      setLocalNames(new Set(names || []))
    } catch { setOnlineModels([]) }
    finally { setLoading(false) }
  }

  const loadEditedModels = async () => {
    setLoading(true)
    try { setEditedModels(await ModelApi.ListEditedModels() || []) }
    catch { setEditedModels([]) }
    finally { setLoading(false) }
  }

  const handleDownload = async (m: ModelApi.OnlineModel) => {
    setDownloadingId(m.id)
    try {
      await ModelApi.DownloadPhoneModel(String(m.id))
      showToast(t('model:downloadSuccess'))
      setLocalNames(prev => new Set([...prev, m.name]))
    } catch { showToast(t('model:downloadFailed'), 'error') }
    finally { setDownloadingId(null) }
  }

  const handleDeleteEdited = async (name: string) => {
    try { await ModelApi.DeleteEditedModel(name); setConfirmAction(null); loadEditedModels() }
    catch { showToast(t('model:deleteFailed'), 'error') }
  }

  const filterBySearch = <T extends { name: string }>(list: T[]) => {
    if (!search.trim()) return list
    const kw = search.trim().toLowerCase()
    return list.filter(m => m.name.toLowerCase().includes(kw))
  }

  const filteredOnline = filterBySearch(onlineModels)
  const filteredEdited = filterBySearch(editedModels)

  const b = 'px-1.5 py-0.5 text-[10px] rounded whitespace-nowrap cursor-pointer'
  const bBlue = `${b} bg-blue-50 text-blue-600 hover:bg-blue-100`
  const bGreen = `${b} bg-green-50 text-green-600 hover:bg-green-100`
  const bRed = `${b} bg-red-50 text-red-500 hover:bg-red-100`

  const onEditDone = () => {
    setEditingName('')
    loadOnlineModels()
    loadEditedModels()
  }

  return (
    <div className="h-full flex flex-col">
        <div className="flex items-center justify-between px-4 py-2 bg-gray-50 border-b border-gray-200">
          <div className="flex items-center gap-2">
            <button onClick={() => { if (tab === 'device') setReloadKey(k => k + 1); else tab === 'online' ? loadOnlineModels() : loadEditedModels() }} disabled={loading}
              className="px-3 py-1.5 text-xs bg-white border border-gray-200 rounded hover:bg-gray-50 disabled:opacity-50">
              {loading ? t('status.loading') : t('button.refresh')}
            </button>
            <button onClick={() => setShowCollectionTool(true)}
              className="flex items-center gap-1 px-3 py-1.5 text-xs bg-purple-50 border border-purple-200 text-purple-600 rounded hover:bg-purple-100">
              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v1m6 11h2m-6 0h-2v4m0-11v3m0 0h.01M12 12h4.01M16 20h4M4 12h4m12 0h.01M5 8h2a1 1 0 001-1V5a1 1 0 00-1-1H5a1 1 0 00-1 1v2a1 1 0 001 1zm12 0h2a1 1 0 001-1V5a1 1 0 00-1-1h-2a1 1 0 00-1 1v2a1 1 0 001 1zM5 20h2a1 1 0 001-1v-2a1 1 0 00-1-1H5a1 1 0 00-1 1v2a1 1 0 001 1z" /></svg>
              {t('model:collection.tool')}
            </button>
          </div>
          <input value={search} onChange={e => setSearch(e.target.value)} placeholder={t('model:search')}
            className="text-xs px-2 py-1 border border-gray-200 rounded outline-none w-32 focus:border-blue-400" />
        </div>

      <div className="flex border-b border-gray-200 px-4">
        <button onClick={() => setTab('online')} className={`px-4 py-2 text-xs ${tab === 'online' ? 'text-blue-500 border-b-2 border-blue-500 font-medium' : 'text-gray-500'}`}>{t('model:tab.online')}</button>
        <button onClick={() => setTab('local')} className={`px-4 py-2 text-xs ${tab === 'local' ? 'text-blue-500 border-b-2 border-blue-500 font-medium' : 'text-gray-500'}`}>{t('model:tab.local')}</button>
        <button onClick={() => setTab('device')} className={`px-4 py-2 text-xs ${tab === 'device' ? 'text-blue-500 border-b-2 border-blue-500 font-medium' : 'text-gray-500'}`}>{t('model:tab.device')}</button>
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        {tab === 'online' && (
          <div className="space-y-3">
            <div className="flex items-center gap-1.5">
              {ANDROID_VERSIONS.map(v => (
                <button key={v.value} onClick={() => setAndroidVersion(v.value)}
                  className={`px-2 py-0.5 text-[10px] rounded ${androidVersion === v.value ? 'bg-blue-500 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}>
                  {v.label}
                </button>
              ))}
              <span className="w-px h-3 bg-gray-200 mx-1" />
              <button onClick={() => ModelApi.OpenModelDir()} className="flex items-center gap-1 px-2 py-0.5 text-[10px] rounded border border-amber-300 bg-amber-50 text-amber-600 hover:bg-amber-100">
                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" /></svg>
                {t('button.openDirectory')}
              </button>
            </div>
            {filteredOnline.length === 0 ? (
              <div className="text-xs text-gray-300 text-center py-6">{t('model:noOnlineModels')}</div>
            ) : (
              <div className="border border-gray-100 rounded-lg overflow-hidden">
                <div className="grid grid-cols-[60px_1fr_80px_140px] px-3 py-1.5 text-[10px] text-gray-400 bg-gray-100 font-medium">
                  <span>ID</span><span>{t('table.name')}</span><span>{t('model:version')}</span><span>{t('table.action')}</span>
                </div>
                {filteredOnline.map(m => {
                  const downloaded = localNames.has(m.name)
                  return (
                    <div key={m.id} className="grid grid-cols-[60px_1fr_80px_140px] px-3 py-1.5 border-t border-gray-50 items-center">
                      <span className="text-xs text-gray-400">{m.id}</span>
                      <span className="text-xs text-gray-700">{m.name}</span>
                      <span className="text-[10px] text-gray-400">{m.android_version}</span>
                      <div className="flex gap-1">
                        {downloaded ? (
                          <>
                            <button onClick={() => setEditingName(m.name)} className={bGreen}>{t('button.edit')}</button>
                          </>
                        ) : (
                          <button onClick={() => handleDownload(m)} disabled={downloadingId === m.id}
                            className={bBlue}>{downloadingId === m.id ? t('model:downloading') : t('button.download')}</button>
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}

        {tab === 'local' && (
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <button onClick={() => ModelApi.OpenEditedDir()}
                className="flex items-center gap-1 px-2 py-1 text-xs rounded border border-amber-300 bg-amber-50 text-amber-600 hover:bg-amber-100">
                <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" /></svg>
                {t('button.openDirectory')}
              </button>
            </div>
            {filteredEdited.length === 0 ? (
              <div className="text-xs text-gray-300 text-center py-6">{t('model:noLocalModels')}</div>
            ) : (
              <div className="border border-gray-100 rounded-lg overflow-hidden">
                <div className="grid grid-cols-[1fr_70px_140px] px-3 py-1.5 text-[10px] text-gray-400 bg-gray-100 font-medium">
                  <span>{t('table.name')}</span><span className="text-center">{t('model:size')}</span><span className="text-center">{t('table.action')}</span>
                </div>
                {filteredEdited.map(m => (
                  <div key={m.name} className="grid grid-cols-[1fr_70px_140px] px-3 py-1.5 border-t border-gray-50 items-center">
                    <span className="text-xs text-gray-700">{m.name}</span>
                    <span className="text-[10px] text-gray-400 text-center">{formatSize(m.size)}</span>
                    <div className="flex justify-center gap-1">
                      <button onClick={() => setEditingName(m.name)} className={bGreen}>{t('button.edit')}</button>
                      <button onClick={() => ask(t('model:deleteModel'), t('model:confirmDeleteModel', { name: m.name }), () => handleDeleteEdited(m.name))} className={bRed}>{t('button.delete')}</button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {tab === 'device' && <DeviceModelPanel search={search} reloadSignal={reloadKey} />}
      </div>

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={confirmAction.onConfirm} onCancel={() => setConfirmAction(null)} />}

      {editingName && (
        <ModelEditModal
          modelName={editingName}
          onClose={() => setEditingName('')}
          onDone={onEditDone}
        />
      )}

      {showCollectionTool && (
        <CollectionToolModal onClose={() => setShowCollectionTool(false)} />
      )}
    </div>
  )
}

export default memo(PhoneModelManagement)
