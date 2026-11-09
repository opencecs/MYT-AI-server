import { useState, useEffect, useCallback, memo } from 'react'
import { useTranslation } from 'react-i18next'
import { Events } from '@wailsio/runtime'
import { RefreshCw, Download, Trash2, Eraser, Loader2 } from 'lucide-react'
import * as V2Service from '../../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import * as DiscoverService from '../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import * as MirrorService from '../../../bindings/github.com/moyunteng/myt/mirrorservicewrapper'
import type { DeviceState } from '../../types/index'
import { getDeviceId } from '../../utils/deviceId'
import DeviceSidebar from '../../components/DeviceSidebar'
import ConfirmDialog from '../../components/ConfirmDialog'
import { showToast } from '../../components/Toast'
import { isEmulatorMirror } from './constants'
import type { MirrorItem } from '../../../bindings/github.com/moyunteng/myt/internal/util/models'

// V2 镜像管理页（v2 模式「镜像」tab）：按主机管理镜像。
//   镜像源 tab   = 81 /get_img_list（云端镜像源，与 V2 客户端同源）→「拉取到主机」
//   主机镜像 tab = 2375 /images/json →「删除」（2375 DELETE）+「清理悬空」（81 prune）

interface V2SourceImage { id: string; image: string; name: string }
interface HostImage { id: string; repoTags: string[]; size: number; created: number; containers: number }

type Tab = 'source' | 'host'

function formatSize(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(2)} GB`
  return `${mb} MB`
}

function V2MirrorManagement() {
  const { t } = useTranslation()
  const [devices, setDevices] = useState<DeviceState[]>([])
  const [selectedIP, setSelectedIP] = useState('')
  const [tab, setTab] = useState<Tab>('source')
  const [sources, setSources] = useState<V2SourceImage[]>([])
  const [hostImages, setHostImages] = useState<HostImage[]>([])
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState<string | null>(null) // 正在操作的对象标识
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [confirmPrune, setConfirmPrune] = useState(false)

  // 主机列表（在线/离线，同维护页口径）；选中默认取第一台
  useEffect(() => {
    DiscoverService.GetDeviceList().then(list => {
      const visible = (list as DeviceState[]).filter(d => d.state === 0 || d.state === 1)
      setDevices(visible)
      setSelectedIP(prev => {
        if (prev && visible.some(d => getDeviceId(d, 'v2') === prev)) return prev
        return visible.length > 0 ? getDeviceId(visible[0], 'v2') : ''
      })
    }).catch(() => {})
    const off = Events.On('devicelist:updated', (ev: unknown) => {
      const arr = (((ev as { data?: unknown })?.data ?? ev) as DeviceState[]) || []
      if (Array.isArray(arr)) setDevices(arr.filter(d => d.state === 0 || d.state === 1))
    })
    return () => { off() }
  }, [])

  const selectedDevice = devices.find(d => getDeviceId(d, 'v2') === selectedIP)
  const deviceId = selectedDevice?.device_id ?? ''

  // 镜像源 = 云端镜像中心 get_mirror_list（壳子同源：创建容器时 mirror_list[id].url
  // 就是镜像地址）。此前误调不存在的 V2Service.GetV2Images → TypeError 被静默
  // catch → 列表恒空。现走 MirrorService.FetchMirrors（V1 带 token / V2 匿名降级），
  // 过滤掉模拟器镜像（拉到主机是给容器用的）
  const loadSource = useCallback(async (id: string) => {
    setLoading(true)
    try {
      const all = (await MirrorService.FetchMirrors() ?? []) as MirrorItem[]
      setSources(all.filter(m => !isEmulatorMirror(m)).map(m => ({
        id: m.id, image: m.url, name: m.name,
      })))
    } catch (e: unknown) {
      setSources([])
      showToast(String(e).replace(/^Error:\s*/, ''), 'error')
    } finally { setLoading(false) }
  }, [])

  const loadHost = useCallback(async (id: string) => {
    setLoading(true)
    try {
      const list = (await V2Service.GetV2HostImages(id) ?? []) as HostImage[]
      setHostImages(list.map(im => ({
        id: im.id ?? '', repoTags: im.repoTags ?? [], size: Math.round((im.size ?? 0) / (1024 * 1024)),
        created: im.created ?? 0, containers: im.containers ?? 0,
      })))
    } catch { setHostImages([]) } finally { setLoading(false) }
  }, [])

  useEffect(() => {
    setSources([]); setHostImages([])
    if (!deviceId) return
    if (tab === 'source') loadSource(deviceId)
    else loadHost(deviceId)
  }, [deviceId, tab, loadSource, loadHost])

  const reload = () => { if (deviceId) (tab === 'source' ? loadSource(deviceId) : loadHost(deviceId)) }

  const pull = async (img: V2SourceImage) => {
    if (!deviceId || busy) return
    setBusy(`pull:${img.id}`)
    try {
      await V2Service.PullV2Image(deviceId, img.image)
      showToast(t('mirror:v2.pullDone', { name: img.name || img.image }), 'success')
      if (tab === 'host') await loadHost(deviceId)
    } catch (e: unknown) {
      showToast(String(e).replace(/^Error:\s*/, ''), 'error')
    } finally { setBusy(null) }
  }

  const doDelete = async () => {
    if (!deviceId || !deleteTarget) return
    const tag = deleteTarget
    setDeleteTarget(null)
    setBusy(`del:${tag}`)
    try {
      await V2Service.DeleteV2HostImage(deviceId, tag)
      showToast(t('mirror:v2.deleteDone'), 'success')
      await loadHost(deviceId)
    } catch (e: unknown) {
      showToast(String(e).replace(/^Error:\s*/, ''), 'error')
    } finally { setBusy(null) }
  }

  const prune = async () => {
    if (!deviceId) return
    setConfirmPrune(false)
    setBusy('prune')
    try {
      await V2Service.PruneV2Images(deviceId)
      showToast(t('mirror:v2.pruneDone'), 'success')
      await loadHost(deviceId)
    } catch (e: unknown) {
      showToast(String(e).replace(/^Error:\s*/, ''), 'error')
    } finally { setBusy(null) }
  }

  return (
    <div className="flex h-full">
      <DeviceSidebar selectedIP={selectedIP} onSelect={setSelectedIP} hideSearch />
      <div className="flex-1 flex flex-col overflow-hidden">
        {!selectedDevice ? (
          <div className="flex-1 flex items-center justify-center text-sm text-gray-400">{t('mirror:v2.selectHost')}</div>
        ) : (
          <>
            <div className="px-5 pt-4 flex items-center justify-between">
              <div className="flex gap-4 border-b border-gray-100 w-full">
                {(['source', 'host'] as Tab[]).map(tb => (
                  <button key={tb} onClick={() => setTab(tb)}
                    className={`pb-2 text-sm transition-colors ${tab === tb ? 'text-blue-600 border-b-2 border-blue-500 font-medium' : 'text-gray-400 hover:text-gray-600'}`}>
                    {t(`mirror:v2.tab_${tb}`)}
                  </button>
                ))}
              </div>
            </div>
            <div className="px-5 py-2 flex items-center gap-2">
              {tab === 'host' && (
                <button className="text-xs text-gray-500 hover:text-red-600 flex items-center gap-1 border border-gray-200 rounded px-2 py-1 hover:border-red-200"
                  disabled={!!busy} onClick={() => setConfirmPrune(true)}>
                  <Eraser size={12} />{t('mirror:v2.prune')}
                </button>
              )}
              <button className="text-xs text-gray-500 hover:text-blue-600 flex items-center gap-1 ml-auto" disabled={loading} onClick={reload}>
                {loading ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}{t('button.refresh')}
              </button>
            </div>
            <div className="flex-1 overflow-y-auto px-5 pb-5">
              {loading ? (
                <div className="flex items-center gap-2 text-sm text-gray-400 py-8"><Loader2 size={14} className="animate-spin" />{t('common:loading')}</div>
              ) : tab === 'source' ? (
                <SourceList sources={sources} busy={busy} onPull={pull} />
              ) : (
                <HostList images={hostImages} busy={busy} onDelete={setDeleteTarget} />
              )}
            </div>
          </>
        )}
      </div>
      {deleteTarget && (
        <ConfirmDialog
          title={t('mirror:v2.deleteTitle')}
          message={t('mirror:v2.deleteConfirm', { tag: deleteTarget })}
          confirmLabel={t('mirror:v2.delete')}
          onConfirm={doDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
      {confirmPrune && (
        <ConfirmDialog
          title={t('mirror:v2.pruneTitle')}
          message={t('mirror:v2.pruneConfirm')}
          confirmLabel={t('mirror:v2.prune')}
          onConfirm={prune}
          onCancel={() => setConfirmPrune(false)}
        />
      )}
    </div>
  )
}

function SourceList({ sources, busy, onPull }: { sources: V2SourceImage[]; busy: string | null; onPull: (img: V2SourceImage) => void }) {
  const { t } = useTranslation()
  if (sources.length === 0) return <div className="text-sm text-gray-400 py-8">{t('mirror:v2.emptySource')}</div>
  return (
    <div className="space-y-2">
      {sources.map(img => (
        <div key={img.id} className="flex items-center gap-3 border border-gray-100 rounded-lg px-3 py-2 hover:border-gray-200">
          <div className="flex-1 min-w-0">
            <div className="text-sm text-gray-700 truncate">{img.name || img.image}</div>
            <div className="text-[11px] text-gray-400 truncate">{img.image}</div>
          </div>
          <button className="text-xs text-blue-600 hover:text-blue-700 flex items-center gap-1 border border-blue-100 rounded px-2 py-1 hover:bg-blue-50 disabled:opacity-40"
            disabled={busy !== null} onClick={() => onPull(img)}>
            {busy === `pull:${img.id}` ? <Loader2 size={12} className="animate-spin" /> : <Download size={12} />}
            {busy === `pull:${img.id}` ? t('mirror:v2.pulling') : t('mirror:v2.pull')}
          </button>
        </div>
      ))}
    </div>
  )
}

function HostList({ images, busy, onDelete }: { images: HostImage[]; busy: string | null; onDelete: (tag: string) => void }) {
  const { t } = useTranslation()
  if (images.length === 0) return <div className="text-sm text-gray-400 py-8">{t('mirror:v2.emptyHost')}</div>
  return (
    <div className="space-y-2">
      {images.map(img => {
        const tag = img.repoTags[0] || `<none> (${img.id.replace('sha256:', '').slice(0, 12)})`
        return (
          <div key={img.id || tag} className="flex items-center gap-3 border border-gray-100 rounded-lg px-3 py-2 hover:border-gray-200">
            <div className="flex-1 min-w-0">
              <div className="text-sm text-gray-700 truncate">{img.repoTags.length > 0 ? img.repoTags.join(', ') : `<none> (${img.id.replace('sha256:', '').slice(0, 12)})`}</div>
              <div className="text-[11px] text-gray-400">
                {formatSize(img.size)} · {t('mirror:v2.containersUsing', { count: img.containers })} · {new Date(img.created * 1000).toLocaleDateString()}
              </div>
            </div>
            <button className="text-xs text-gray-500 hover:text-red-600 flex items-center gap-1 border border-gray-200 rounded px-2 py-1 hover:border-red-200 disabled:opacity-40"
              disabled={busy !== null} onClick={() => onDelete(tag)}>
              {busy === `del:${tag}` ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
              {t('mirror:v2.delete')}
            </button>
          </div>
        )
      })}
    </div>
  )
}

export default memo(V2MirrorManagement)
