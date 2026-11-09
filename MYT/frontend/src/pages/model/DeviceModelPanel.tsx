import { useState, useEffect, useCallback, memo } from 'react'
import { useTranslation } from 'react-i18next'
import * as SdkApiService from '../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { useOnlineDevices } from '../../hooks/useOnlineDevices'
import { useConfirm } from '../../hooks/useConfirm'
import ConfirmDialog from '../../components/ConfirmDialog'
import Select from '../../components/Select'
import { showToast } from '../../components/Toast'
import { useTaskQueue } from '../../hooks/taskQueue/useTaskQueue'
import { getErrorMessage } from '../../utils/error'

// 设备端本地机型条目（GET /phoneModel，设备端扫描 /mmc/phoneModel/ 目录）
interface DeviceModelEntry {
  name: string
  mtimestamp: number
}

// mtimestamp 兼容秒/毫秒两种粒度
function formatTime(ts: number) {
  if (!ts) return '-'
  const ms = ts < 1e12 ? ts * 1000 : ts
  const d = new Date(ms)
  return isNaN(d.getTime()) ? '-' : d.toLocaleString()
}

// 盒子内本地机型管理面板：选择在线设备 → 查看/删除设备端本地机型
function DeviceModelPanel({ search, reloadSignal }: { search: string; reloadSignal: number }) {
  const { t } = useTranslation()
  const { devices } = useOnlineDevices()
  const [ip, setIp] = useState('')
  const [models, setModels] = useState<DeviceModelEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [deleting, setDeleting] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [batchDeleting, setBatchDeleting] = useState(false)
  const { confirmAction, setConfirmAction, ask } = useConfirm()
  const { addTask, updateTask } = useTaskQueue()

  const load = useCallback(async (targetIp: string) => {
    if (!targetIp) { setModels([]); return }
    setLoading(true)
    try {
      const raw = await SdkApiService.ListLocalPhoneModels(targetIp)
      const list = (raw as DeviceModelEntry[]) || []
      setModels(list)
      // 刷新后剔除已不存在的选择项（他处被删/设备端变化）
      const names = new Set(list.map(m => m.name))
      setSelected(prev => {
        const n = new Set([...prev].filter(name => names.has(name)))
        return n.size === prev.size ? prev : n
      })
    } catch {
      showToast(t('model:device.loadFailed'), 'error')
      setModels([])
    } finally { setLoading(false) }
  }, [t])

  // 选中的设备下线时自动清空选择与列表
  useEffect(() => {
    if (ip && !devices.some(d => d.ip === ip)) { setIp(''); setModels([]); setSelected(new Set()) }
  }, [devices, ip])

  // 切换设备 / 顶部刷新按钮（reloadSignal 递增）时重新加载
  useEffect(() => { load(ip) }, [ip, reloadSignal, load])

  const handleDelete = async (name: string) => {
    setDeleting(name)
    try {
      await SdkApiService.DeleteLocalPhoneModel(ip, name)
      setConfirmAction(null)
      setSelected(prev => { const n = new Set(prev); n.delete(name); return n })
      load(ip)
    } catch { showToast(t('model:deleteFailed'), 'error') }
    finally { setDeleting('') }
  }

  // 批量删除：提交任务队列（可后台观察进度），逐台串行执行
  const handleBatchDelete = async () => {
    if (!ip || selected.size === 0) return
    const names = [...selected]
    setConfirmAction(null)
    const taskId = await addTask({
      type: 'deleteDeviceModel',
      targets: names.map(n => ({ deviceIp: ip, name: n })),
      deviceIps: [ip],
      total: names.length,
    })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    setBatchDeleting(true)
    let completed = 0
    const failedTargets: Array<{ deviceIp: string; machineName: string; error: string }> = []
    for (const name of names) {
      try {
        await SdkApiService.DeleteLocalPhoneModel(ip, name)
        completed++
      } catch (err) {
        failedTargets.push({ deviceIp: ip, machineName: name, error: getErrorMessage(err) })
      }
      updateTask(taskId, {
        completed,
        failed: failedTargets.length,
        progress: Math.round(((completed + failedTargets.length) / names.length) * 100),
        failedTargets: [...failedTargets],
      })
    }
    setBatchDeleting(false)
    setSelected(new Set())
    load(ip)
    if (failedTargets.length > 0) {
      updateTask(taskId, {
        status: completed > 0 ? 'completed' : 'failed',
        progress: 100,
        endTime: Date.now(),
        error: completed > 0 ? undefined : t('model:device.batchDeletePartial', { ok: 0, failed: failedTargets.length }),
      })
    } else {
      updateTask(taskId, { status: 'completed', progress: 100, endTime: Date.now() })
    }
    if (failedTargets.length > 0) {
      showToast(t('model:device.batchDeletePartial', { ok: completed, failed: failedTargets.length }), 'error')
    } else {
      showToast(t('model:device.batchDeleteSuccess', { count: names.length }))
    }
  }

  const toggleOne = (name: string) => {
    setSelected(prev => { const n = new Set(prev); n.has(name) ? n.delete(name) : n.add(name); return n })
  }

  const kw = search.trim().toLowerCase()
  const filtered = kw ? models.filter(m => m.name.toLowerCase().includes(kw)) : models

  const allVisibleSelected = filtered.length > 0 && filtered.every(m => selected.has(m.name))
  const toggleAllVisible = () => {
    setSelected(prev => {
      const n = new Set(prev)
      if (allVisibleSelected) filtered.forEach(m => n.delete(m.name))
      else filtered.forEach(m => n.add(m.name))
      return n
    })
  }

  const bRed = 'px-1.5 py-0.5 text-[10px] rounded whitespace-nowrap cursor-pointer bg-red-50 text-red-500 hover:bg-red-100 disabled:opacity-50'
  const cb = 'w-3.5 h-3.5 accent-blue-500 cursor-pointer'

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Select value={ip} onChange={setIp} searchable searchPlaceholder={t('model:device.searchDevice')}
          placeholder={t('model:device.selectDevice')} className="min-w-[220px]" buttonClassName="h-7"
          options={devices.map(d => ({ value: d.ip, label: d.device_name ? `${d.device_name} (${d.ip})` : d.ip }))} />
        {selected.size > 0 && (
          <button onClick={() => ask(t('model:device.batchDelete'), t('model:device.confirmBatchDelete', { count: selected.size }), handleBatchDelete)}
            disabled={batchDeleting} className={bRed}>
            {batchDeleting ? t('status.loading') : `${t('model:device.batchDelete')} (${selected.size})`}
          </button>
        )}
      </div>

      {!ip ? (
        <div className="text-xs text-gray-300 text-center py-6">{t('model:device.selectDeviceFirst')}</div>
      ) : loading ? (
        <div className="text-xs text-gray-300 text-center py-6">{t('status.loading')}</div>
      ) : filtered.length === 0 ? (
        <div className="text-xs text-gray-300 text-center py-6">{t('model:device.noModels')}</div>
      ) : (
        <div className="border border-gray-100 rounded-lg overflow-hidden">
          <div className="grid grid-cols-[30px_1fr_150px_100px] px-3 py-1.5 text-[10px] text-gray-400 bg-gray-100 font-medium items-center">
            <input type="checkbox" checked={allVisibleSelected} onChange={toggleAllVisible} className={`${cb} justify-self-center`} title={t('model:device.selectAll')} />
            <span>{t('table.name')}</span><span className="text-center">{t('model:device.time')}</span><span className="text-center">{t('table.action')}</span>
          </div>
          {filtered.map(m => (
            <div key={m.name} className="grid grid-cols-[30px_1fr_150px_100px] px-3 py-1.5 border-t border-gray-50 items-center">
              <input type="checkbox" checked={selected.has(m.name)} onChange={() => toggleOne(m.name)} className={`${cb} justify-self-center`} />
              <span className="text-xs text-gray-700">{m.name}</span>
              <span className="text-[10px] text-gray-400 text-center">{formatTime(m.mtimestamp)}</span>
              <div className="flex justify-center">
                <button onClick={() => ask(t('model:deleteModel'), t('model:confirmDeleteModel', { name: m.name }), () => handleDelete(m.name))}
                  disabled={deleting === m.name || batchDeleting} className={bRed}>{deleting === m.name ? t('status.loading') : t('button.delete')}</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {confirmAction && <ConfirmDialog title={confirmAction.title} message={confirmAction.message} confirmLabel={t('button.confirm')}
        onConfirm={confirmAction.onConfirm} onCancel={() => setConfirmAction(null)} />}
    </div>
  )
}

export default memo(DeviceModelPanel)
