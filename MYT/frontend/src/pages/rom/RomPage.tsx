import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { HardDrive, Loader2, RefreshCw, Upload } from 'lucide-react'
import * as V4Service from '../../../bindings/github.com/moyunteng/myt/v4servicewrapper'
import DeviceList from '../host/components/DeviceList'
import ConfirmDialog from '../../components/ConfirmDialog'
import { useConfirm } from '../../hooks/useConfirm'
import { showToast } from '../../components/Toast'
import { useDeviceDiscovery } from '../../hooks/useDeviceDiscovery'
import RomPullDialog from './RomPullDialog'
import RomUploadDialog from './RomUploadDialog'
import RomVariantList from './RomVariantList'
import { useRomPage } from './useRomPage'
import type { RomVariantRow } from './romShared'

/**
 * ROM 管理页（V4 模式专用）
 *
 * 数据源：tobox_player 8088 的镜像管理接口（GET /image/list）。
 * 一个变体 = system.img + vendor.img + initramfs.img 三件套，ready=true 才能用于
 * 创建该变体的云机实例。
 *
 * 布局与云机页（HostManagement）一致：左侧设备列表（DeviceList，单选一台主机）
 * + 右侧 ROM 内容区。镜像目录是**每台主机各自**的，不同主机的镜像不共享——
 * 所有 ROM 操作（list/pull/upload/delete）都按左侧选中的主机执行；
 * 未选中主机时右侧显示引导空态（先添加主机）。
 */
export default function RomPage() {
  const { t } = useTranslation()
  const { confirmAction, setConfirmAction, ask } = useConfirm()
  const rom = useRomPage()
  const {
    devices, selectedDevice, setSelectedDevice,
    host, root, visible, loading, error,
    searchKw, setSearchKw,
    busyVariant, setBusyVariant,
    pullTarget, setPullTarget,
    uploadOpen, setUploadOpen, uploadVariant, setUploadVariant,
    refresh,
  } = rom

  // 待添加设备 + 设备发现/刷新（与云机页共用 useDeviceDiscovery，保证列表行为一致）
  const {
    pendingDevices, scanning,
    refreshDevices: discoverDevices,
    addPendingDevices, addAllPendingDevices, clearPendingDevices,
  } = useDeviceDiscovery()

  const handleDelete = useCallback((v: RomVariantRow) => {
    ask(
      t('rom:confirmDelete.title'),
      t('rom:confirmDelete.message', { variant: v.variant }),
      async () => {
        setBusyVariant(v.variant)
        try {
          await V4Service.DeleteRomImage(host, v.variant)
          showToast(t('rom:toastDeleted', { variant: v.variant }))
          await refresh()
        } catch (e) {
          showToast(t('rom:toastDeleteFailed', { message: String(e) }), 'error')
        } finally {
          setBusyVariant('')
        }
      },
    )
  }, [ask, host, refresh, t, setBusyVariant])

  const handleSelect = useCallback((device: Parameters<typeof setSelectedDevice>[0]) => {
    setSelectedDevice(device)
  }, [setSelectedDevice])

  const header = (
    <div className="flex items-center gap-3 pb-3">
      <span className="flex items-center gap-1 text-xs text-gray-500">
        <HardDrive size={13} />
        {t('rom:summary', { count: visible.length })}
      </span>
      {root && <span className="text-[11px] text-gray-300 font-mono truncate max-w-[240px]" title={root}>{root}</span>}
      <div className="ml-auto flex items-center gap-2">
        <div className="relative">
          <input
            type="text"
            className="w-48 rounded-md border border-gray-300 bg-white px-2.5 py-1.5 text-sm text-gray-700 outline-none placeholder:text-gray-400 focus:border-blue-400"
            placeholder={t('rom:searchPlaceholder')}
            value={searchKw}
            onChange={e => setSearchKw(e.target.value)}
          />
        </div>
        <button
          onClick={() => { setUploadVariant(''); setUploadOpen(true) }}
          disabled={!host}
          className="flex items-center gap-1 text-xs text-blue-600 hover:text-blue-700 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
          title={t('rom:upload.title')}
        >
          <Upload size={13} />
          {t('rom:upload.entry')}
        </button>
        <button
          onClick={refresh}
          disabled={!host || loading}
          className="flex items-center gap-1 text-xs text-gray-500 hover:text-blue-600 disabled:opacity-50 cursor-pointer"
          title={t('rom:refresh')}
        >
          <RefreshCw size={13} className={loading ? 'animate-spin' : ''} />
          {t('rom:refresh')}
        </button>
      </div>
    </div>
  )

  const content = !host ? (
    // 没有选中主机：引导去添加/选中主机
    <div className="flex-1 flex flex-col items-center justify-center gap-2 text-center px-8">
      <div className="text-sm text-gray-500">{t('rom:noHostTitle')}</div>
      <div className="text-xs text-gray-400 max-w-md leading-relaxed">{t('rom:noHostHint')}</div>
    </div>
  ) : (
    <>
      {error && (
        <div className="mb-2 px-3 py-2 rounded-md bg-red-50 text-[11px] text-red-600">
          {t('rom:loadFailed', { message: error })}
        </div>
      )}
      <div className="flex-1 overflow-auto">
        {loading ? (
          <div className="flex items-center justify-center h-40 text-gray-400 text-sm gap-2">
            <Loader2 size={14} className="animate-spin" />
            {t('rom:loading')}
          </div>
        ) : visible.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-40 gap-1 text-center px-8">
            <div className="text-sm text-gray-500">
              {searchKw ? t('rom:noMatch') : t('rom:emptyTitle')}
            </div>
            {!searchKw && (
              <div className="text-xs text-gray-400 max-w-md leading-relaxed">{t('rom:emptyHint')}</div>
            )}
          </div>
        ) : (
          <RomVariantList
            host={host}
            rows={visible}
            busyVariant={busyVariant}
            onPull={setPullTarget}
            onUpload={v => { setUploadVariant(v.variant); setUploadOpen(true) }}
            onDelete={handleDelete}
          />
        )}
      </div>
    </>
  )

  return (
    <div className="flex h-full">
      <DeviceList
        devices={devices}
        selectedDevice={selectedDevice}
        selectedDeviceIds={EMPTY_SET}
        viewMode="screenshot"
        scanning={scanning}
        pendingDevices={pendingDevices}
        singleSelect
        onSelect={handleSelect}
        onToggleSelect={() => {}}
        onSelectAll={() => {}}
        onDeselectAll={() => {}}
        onDelete={() => {}}
        onDeleteDevices={() => {}}
        onAddPendingDevices={addPendingDevices}
        onAddAllPendingDevices={addAllPendingDevices}
        onClearPendingDevices={clearPendingDevices}
        onCreateInstance={() => {}}
        onRefresh={discoverDevices}
        androidGroups={EMPTY_GROUPS}
        expandedDeviceIds={EMPTY_SET}
        onToggleDeviceExpand={() => {}}
        onToggleAndroidSelect={() => {}}
        onExcludeAndroidFromDevice={() => {}}
        selectedAndroidIds={EMPTY_SET}
      />

      <div className="flex-1 flex flex-col p-3 min-w-0">
        {header}
        {content}

        {confirmAction && (
          <ConfirmDialog
            title={confirmAction.title}
            message={confirmAction.message}
            confirmLabel={t('button.confirm')}
            onConfirm={() => { confirmAction.onConfirm(); setConfirmAction(null) }}
            onCancel={() => setConfirmAction(null)}
          />
        )}

        {pullTarget && (
          <RomPullDialog
            host={host}
            variant={pullTarget}
            onClose={() => setPullTarget(null)}
            onDone={refresh}
          />
        )}

        {uploadOpen && (
          <RomUploadDialog
            host={host}
            initialVariant={uploadVariant || undefined}
            onClose={() => setUploadOpen(false)}
            onDone={refresh}
          />
        )}
      </div>
    </div>
  )
}

const EMPTY_SET = new Set<string>()
const EMPTY_GROUPS = {}
