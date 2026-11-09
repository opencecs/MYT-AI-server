import { useState, useEffect, useCallback, memo } from 'react'
import { useTranslation } from 'react-i18next'
import * as MirrorService from '../../../bindings/github.com/moyunteng/myt/mirrorservicewrapper'
import * as ImageService from '../../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import { MirrorItem, SavedMirror } from '../../types/index'
import { useDownloadProgress, useUploadProgress, onDownloadComplete } from '../../hooks/useImageProgress'
import { TabType, OsFilter, SeriesFilter, MirrorTypeFilter, SERIES_OPTIONS, MIRROR_TYPE_OPTIONS, OS_VERSIONS, formatOsVer, OS_VER_ORDER, parseVersion, isPSeries, isPSeriesSaved, isEmulatorMirror, isEmulatorMirrorSaved, isContainerMirror, isContainerMirrorSaved } from './constants'
import { DownloadProgress } from './DownloadProgress'
import PushMirrorModal from './PushMirrorModal'
import ConfirmDialog from '../../components/ConfirmDialog'
import { showToast } from '../../components/Toast'
import { cast } from '../../utils/cast'

function MirrorManagement() {
  const { t } = useTranslation()
  const [tab, setTab] = useState<TabType>('online')
  const [mirrors, setMirrors] = useState<MirrorItem[]>([])
  const [savedMirrors, setSavedMirrors] = useState<SavedMirror[]>([])
  const [loading, setLoading] = useState(false)
  const [osFilter, setOsFilter] = useState<OsFilter>('all')
  const [seriesFilter, setSeriesFilter] = useState<SeriesFilter>('qrc')
  const [mirrorType, setMirrorType] = useState<MirrorTypeFilter>('emulator')
  const [pushTarget, setPushTarget] = useState<MirrorItem | SavedMirror | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const { downloads, startDownload, cancelDownload } = useDownloadProgress()
  const { uploads, clearUpload } = useUploadProgress()

  const fetchMirrors = async () => {
    setLoading(true)
    try {
      const result = await MirrorService.FetchMirrors()
      setMirrors(result as MirrorItem[])
    } catch (err) {
      console.error('Failed to fetch mirrors:', err)
    } finally {
      setLoading(false)
    }
  }

  const loadCachedMirrors = async () => {
    try {
      const cached = await MirrorService.GetCachedMirrors()
      if (cached && cached.length > 0) {
        setMirrors(cached as MirrorItem[])
      }
    } catch (_err) {
      // ignore cache read failure
    }
  }

  const fetchSavedMirrors = useCallback(async () => {
    try {
      const result = await ImageService.GetSavedMirrors()
      setSavedMirrors(cast<SavedMirror[]>(result ?? []))
    } catch (err) {
      console.error('Failed to fetch saved mirrors:', err)
    }
  }, [])

  useEffect(() => { loadCachedMirrors().then(() => fetchMirrors()) }, [])
  useEffect(() => { fetchSavedMirrors() }, [fetchSavedMirrors])

  useEffect(() => {
    const timer = setInterval(() => { loadCachedMirrors() }, 10 * 60 * 1000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    onDownloadComplete((_mirrorId, success) => {
      if (success) fetchSavedMirrors()
    })
  }, [fetchSavedMirrors])

  const sortMirrors = (list: MirrorItem[]) => [...list].sort((a, b) => {
    const osDiff = (OS_VER_ORDER[b.os_ver] ?? 0) - (OS_VER_ORDER[a.os_ver] ?? 0)
    if (osDiff !== 0) return osDiff
    return parseVersion(b.name) - parseVersion(a.name)
  })

  const sortSaved = (list: SavedMirror[]) => [...list].sort((a, b) => {
    const osDiff = (OS_VER_ORDER[b.os_ver] ?? 0) - (OS_VER_ORDER[a.os_ver] ?? 0)
    if (osDiff !== 0) return osDiff
    return parseVersion(b.name) - parseVersion(a.name)
  })

  const filteredMirrors = sortMirrors(
    mirrors
      .filter(m => mirrorType === 'emulator' ? isEmulatorMirror(m) : isContainerMirror(m))
      .filter(m => seriesFilter === 'p1' ? isPSeries(m.ttype2) : !isPSeries(m.ttype2))
      .filter(m => osFilter === 'all' || m.os_ver === osFilter)
  )

  const filteredSaved = sortSaved(
    savedMirrors
      .filter(m => mirrorType === 'emulator' ? isEmulatorMirrorSaved(m) : isContainerMirrorSaved(m))
      .filter(m => seriesFilter === 'p1' ? isPSeriesSaved(m.ttype2) : !isPSeriesSaved(m.ttype2))
      .filter(m => osFilter === 'all' || m.os_ver === osFilter)
  )

  const savedIdSet = new Set(savedMirrors.map(m => m.id))

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await ImageService.DeleteSavedMirror(deleteTarget)
      setDeleteTarget(null)
      await fetchSavedMirrors()
    } catch (err) {
      console.error('Failed to delete mirror record:', err)
      setDeleteTarget(null)
    }
  }

  const handleDownload = (m: MirrorItem) => {
    startDownload(m)
  }

  return (
    <div className="p-5">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <h1 className="text-lg font-semibold text-gray-800">{t('mirror:title')}</h1>
          <div className="flex gap-1 ml-2">
            <button onClick={() => setTab('online')} className={`px-3 py-1 text-xs rounded-lg ${tab === 'online' ? 'bg-blue-500 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}>{t('mirror:tab.online')}</button>
            <button onClick={() => { setTab('downloaded'); fetchSavedMirrors() }} className={`px-3 py-1 text-xs rounded-lg ${tab === 'downloaded' ? 'bg-blue-500 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}>{t('mirror:tab.downloaded')}</button>
            <div className="border-l border-gray-200 mx-1" />
            {SERIES_OPTIONS.map(([key, labelKey]) => (
              <button key={key} onClick={() => setSeriesFilter(key)} className={`px-3 py-1 text-xs rounded-lg ${seriesFilter === key ? 'bg-blue-500 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}>{t(`mirror:filter.${labelKey}`)}</button>
            ))}
            <div className="border-l border-gray-200 mx-1" />
            {MIRROR_TYPE_OPTIONS.map(([key, labelKey]) => (
              <button key={key} onClick={() => setMirrorType(key)} className={`px-3 py-1 text-xs rounded-lg ${mirrorType === key ? 'bg-blue-500 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}>{t(`mirror:filter.${labelKey}`)}</button>
            ))}
          </div>
        </div>
        <button onClick={fetchMirrors} disabled={loading} className="px-3 py-1 text-xs bg-blue-500 text-white rounded-lg hover:bg-blue-600 disabled:opacity-50">{loading ? t('status.loading') : t('button.refresh')}</button>
      </div>

      <div className="flex gap-1.5 mb-3 flex-wrap items-center">
        {OS_VERSIONS.map(v => (
          <button key={v} onClick={() => setOsFilter(v)} className={`px-2.5 py-0.5 text-xs rounded ${osFilter === v ? 'bg-blue-500 text-white' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'}`}>{v === 'all' ? t('mirror:filter.all') : formatOsVer(v)}</button>
        ))}
        <span className="w-px h-3 bg-gray-200 mx-1" />
        <button onClick={() => ImageService.OpenImageDirectory().catch(() => showToast(t('mirror:toast.openDirFailed'), 'error'))}
          className="flex items-center gap-1 px-2 py-0.5 text-[10px] rounded border border-amber-300 bg-amber-50 text-amber-600 hover:bg-amber-100">
          <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" /></svg>
          {t('button.openDirectory')}
        </button>
      </div>

      {tab === 'online' ? (
        <>
          <div className="text-xs text-gray-400 mb-2">{t('mirror:count.online', { count: filteredMirrors.length })}</div>
          <div className="overflow-auto">
            <table className="w-full text-sm">
              <thead className="sticky top-0 z-10 bg-white">
                <tr className="text-left text-gray-400 border-b border-gray-200">
                  <th className="py-1.5 px-3 w-64">{t('table.name')}</th>
                  <th className="py-1.5 px-5 w-32">{t('mirror:table.androidVersion')}</th>
                  <th className="py-1.5 px-3">{t('mirror:table.updateContent')}</th>
                  <th className="py-1.5 px-3 w-32">{t('table.action')}</th>
                </tr>
              </thead>
              <tbody>
                {filteredMirrors.map(m => {
                  const dl = downloads[m.id]
                  const isSaved = savedIdSet.has(m.id)
                  return (
                    <tr key={m.id} className="border-b border-gray-100 hover:bg-gray-50">
                      <td className="py-1.5 px-3 text-gray-800 break-all whitespace-normal" title={m.name}>{m.name}</td>
                      <td className="py-1.5 px-5 text-gray-600">{formatOsVer(m.os_ver)}</td>
                      <td className="py-1.5 px-3"><span className="text-xs text-gray-400 line-clamp-2" title={m.udesc}>{m.udesc || '-'}</span></td>
                      <td className="py-1.5 px-3">
                        <div className="flex flex-nowrap gap-1 items-center">
                          {dl ? (
                            <DownloadProgress progress={dl.progress} stage={dl.stage} onCancel={() => cancelDownload(m.id)} />
                          ) : isSaved ? (
                            <>
                              <button onClick={() => setPushTarget(m)} className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600">{t('button.push')}</button>
                              <button onClick={() => setDeleteTarget(m.id)} className="px-3 py-1 text-xs bg-red-500 text-white rounded hover:bg-red-600">{t('button.delete')}</button>
                            </>
                          ) : (
                            <button onClick={() => handleDownload(m)} className="px-3 py-1 text-xs bg-green-500 text-white rounded hover:bg-green-600">{t('button.download')}</button>
                          )}
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </>
      ) : (
        <>
          <div className="text-xs text-gray-400 mb-2">{t('mirror:count.downloaded', { count: filteredSaved.length })}</div>
          {filteredSaved.length === 0 ? (
            <div className="text-sm text-gray-300 py-4">{t('mirror:empty.noDownloaded')}</div>
          ) : (
            <div className="overflow-auto">
              <table className="w-full text-sm">
                <thead className="sticky top-0 z-10 bg-white">
                  <tr className="text-left text-gray-400 border-b border-gray-200">
                    <th className="py-1.5 px-3 w-64">{t('table.name')}</th>
                    <th className="py-1.5 px-5 w-32">{t('mirror:table.androidVersion')}</th>
                    <th className="py-1.5 px-3 w-28">{t('mirror:table.downloadTime')}</th>
                    <th className="py-1.5 px-3 w-32">{t('table.action')}</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredSaved.map(m => {
                    const isUploading = uploads[m.id] && !Object.values(uploads[m.id]).every(d => d.done)
                    return (
                      <tr key={m.id} className="border-b border-gray-100 hover:bg-gray-50">
                        <td className="py-1.5 px-3 text-gray-800 break-all whitespace-normal" title={m.name}>{m.name}</td>
                        <td className="py-1.5 px-5 text-gray-600">{formatOsVer(m.os_ver)}</td>
                        <td className="py-1.5 px-3 text-xs text-gray-400">{m.saved_at}</td>
                        <td className="py-1.5 px-3">
                          <div className="flex flex-nowrap gap-1 items-center">
                            <button onClick={() => setPushTarget(m)}
                              className={`px-3 py-1 text-xs rounded ${isUploading ? 'bg-blue-300 text-white' : 'bg-blue-500 text-white hover:bg-blue-600'}`}>
                              {isUploading ? t('status.pushing') : t('button.push')}
                            </button>
                            <button onClick={() => setDeleteTarget(m.id)} className="px-3 py-1 text-xs bg-red-500 text-white rounded hover:bg-red-600">{t('button.delete')}</button>
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {pushTarget && <PushMirrorModal mirror={pushTarget} onClose={() => { setPushTarget(null); clearUpload(pushTarget.id) }} />}
      {deleteTarget && <ConfirmDialog title={t('confirm.deleteTitle')} message={t('mirror:confirm.deleteMessage')} confirmLabel={t('button.delete')} onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} />}
    </div>
  )
}

export default memo(MirrorManagement)
