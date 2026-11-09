import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle } from 'lucide-react'
import { emitSlotStatus } from '../../../../utils/wailsEvent'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../utils/deviceId'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as ScreenshotService from '../../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import * as PhoneModelService from '../../../../../bindings/github.com/moyunteng/myt/phonemodelservice'
import type { AndroidInfo, PhoneModel, MirrorItem } from '../../../../types/index'
import { getErrorMessage } from '../../../../utils/error'
import { cast } from '../../../../utils/cast'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import Select from '../../../../components/Select'
import { deviceFetch, getAndroidVerFromImage } from './modelSwitchUtils'

// ============== 模拟器流程：SDK V3 /android/switchModel（需重启）==============

export function EmulatorSection({ androids, mirrors, onSwitched, onClose }: { androids: AndroidInfo[]; mirrors: MirrorItem[]; onSwitched?: (a: AndroidInfo) => void; onClose?: () => void }) {
  const { t } = useTranslation()
  const { mode: deployMode } = useDeployMode()
  const [phoneModels, setPhoneModels] = useState<PhoneModel[]>([])
  const [localModelNames, setLocalModelNames] = useState<string[]>([])
  const [backupModelNames, setBackupModelNames] = useState<string[]>([])
  const [modelType, setModelType] = useState<'online' | 'local' | 'backup'>('online')
  const [selectedModelId, setSelectedModelId] = useState('__random__')
  const [selectedLocalModel, setSelectedLocalModel] = useState('__random__')
  const [selectedBackupModel, setSelectedBackupModel] = useState('__random__')
  const [running, setRunning] = useState(false)
  const { addTask, updateTask } = useTaskQueue()

  const androidVersions = useMemo(() => [...new Set(
    androids.map(a => getAndroidVerFromImage(a.image, mirrors)).filter(Boolean)
  )], [androids, mirrors])

  useEffect(() => {
    const load = async () => {
      const ips = [...new Set(androids.map(a => getDeviceId(a, deployMode)))]
      if (ips.length > 0) {
        try {
          const m = await deviceFetch(ips[0], '/android/phoneModel?page=0')
          const list = Array.isArray(m) ? m : (m?.list ?? [])
          setPhoneModels(cast<PhoneModel[]>(list))
        } catch {}
        try {
          const backups = cast<{ name: string }[]>(await SdkApiService.ListPhoneModelBackups(ips[0]))
          setBackupModelNames(backups.map(b => b.name))
        } catch {}
      }
      try {
        const names = await PhoneModelService.GetLocalModelNames() as string[]
        if (Array.isArray(names)) setLocalModelNames(names)
      } catch {}
    }
    load()
  }, [])

  const filteredModels = androidVersions.length > 0
    ? phoneModels.filter(m => androidVersions.includes(m.android_version))
    : phoneModels

  const groupedModels = new Map<string, PhoneModel[]>()
  for (const m of filteredModels) {
    const ver = m.android_version || t('host:batchSwitchModel.unknownVersion')
    if (!groupedModels.has(ver)) groupedModels.set(ver, [])
    groupedModels.get(ver)!.push(m)
  }

  const currentModelOptions: { value: string; label: string }[] =
    modelType === 'online'
      ? filteredModels.map(m => ({ value: m.id, label: m.name }))
      : modelType === 'local'
        ? localModelNames.map(n => ({ value: n, label: n }))
        : backupModelNames.map(n => ({ value: n, label: n }))

  const currentModelValue =
    modelType === 'online' ? selectedModelId
      : modelType === 'local' ? selectedLocalModel
        : selectedBackupModel

  const handleModelValueChange = (v: string) => {
    if (modelType === 'online') setSelectedModelId(v)
    else if (modelType === 'local') setSelectedLocalModel(v)
    else setSelectedBackupModel(v)
  }

  const canApply = currentModelOptions.length > 0 || currentModelValue === '__random__'
  const showLocalWarning = modelType === 'local' && androidVersions.length > 0

  const handleApply = async () => {
    if (!canApply || running) return
    setRunning(true)
    const deviceIps = [...new Set(androids.map(a => getDeviceId(a, deployMode)))]
    const taskId = await addTask({
      type: 'switchModel',
      targets: androids.map(a => ({ deviceIp: getDeviceId(a, deployMode), name: a.name })),
      deviceIps,
      total: androids.length,
    })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    onClose?.() // 提交即关闭：进度在任务队列面板看（与其他批量弹窗一致）

    try {
      // 随机 = 每台云机独立挑选（对齐创建云机的机型随机语义），不再全员统一一个。
      // 本地机型 push 按设备×机型去重缓存：同一 zip 不重复上传（文件导入 5 分钟级），
      // 且每台云机用自己设备返回的注册名（此前 find(第一个非空) 把 A 设备的注册名给了所有设备）。
      const registeredLocal = new Map<string, string>() // "deviceId::modelName" → 设备注册名
      const resolveLocalModel = async (deviceId: string, modelName: string): Promise<string> => {
        const key = `${deviceId}::${modelName}`
        if (!registeredLocal.has(key)) {
          const deviceName = await PhoneModelService.PushPhoneModel(deviceId, modelName) as string
          registeredLocal.set(key, deviceName || modelName)
        }
        return registeredLocal.get(key)!
      }
      const pick = <T,>(arr: T[]): T | undefined => (arr.length > 0 ? arr[Math.floor(Math.random() * arr.length)] : undefined)

      let completed = 0, failed = 0
      const failedTargets: Array<{ deviceIp?: string; machineName?: string; slot?: number; error: string }> = []
      for (const a of androids) {
        try {
          const deviceId = getDeviceId(a, deployMode)
          let modelId = ''
          let localModel = ''
          let modelStatic = ''
          if (modelType === 'online') {
            modelId = selectedModelId
            if (modelId === '__random__') {
              const picked = pick(filteredModels)
              if (picked) modelId = picked.id
            }
          } else if (modelType === 'local') {
            let name = selectedLocalModel
            if (name === '__random__') name = pick(localModelNames) || ''
            if (name) localModel = await resolveLocalModel(deviceId, name)
          } else if (modelType === 'backup') {
            modelStatic = selectedBackupModel
            if (modelStatic === '__random__') {
              const picked = pick(backupModelNames)
              if (picked) modelStatic = picked
            }
          }
          await SdkApiService.SwitchModel(deviceId, a.name, modelId, localModel, modelStatic, 0, 0, '')
          ScreenshotService.InvalidateSlot(deviceId, a.indexNum).catch(() => {})
          emitSlotStatus(`${deviceId}_${a.indexNum}`, 'starting')
          onSwitched?.(a)
          completed++
        } catch (err) {
          failed++
          // 失败原因透出到任务详情（此前静默吞掉，失败无从排查）
          failedTargets.push({ deviceIp: getDeviceId(a, deployMode), machineName: a.name, slot: a.indexNum, error: getErrorMessage(err) })
        }
        const pct = Math.round((completed + failed) / androids.length * 100)
        updateTask(taskId, { progress: pct, completed, failed, failedTargets, status: 'running' })
      }
      updateTask(taskId, { status: failed === 0 ? 'completed' : 'failed', progress: 100, completed, failed, failedTargets, endTime: Date.now() })
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="border border-gray-200 rounded-xl p-4 space-y-4 bg-white">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-800">{t('host:batchSwitchModel.emulatorTitle')}</h3>
        <span className="text-[10px] text-gray-400">{t('host:batchSwitchModel.emulatorApi')}</span>
      </div>
      <p className="text-[11px] text-gray-500">
        {t('host:batchSwitchModel.selectedEmulators', { count: androids.length })}{androidVersions.length > 0 ? ` · Android ${androidVersions.join('/')}` : ''}
      </p>

      <div>
        <span className="text-xs text-gray-500 mb-1.5 block">{t('host:batchSwitchModel.modelSource')}</span>
        <div className="flex gap-4 text-xs">
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="radio" checked={modelType === 'online'} onChange={() => setModelType('online')} className="w-4 h-4 accent-blue-500" />
            <span className="text-gray-600">{t('host:batchSwitchModel.onlineModel')}</span>
          </label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="radio" checked={modelType === 'local'} onChange={() => setModelType('local')} className="w-4 h-4 accent-blue-500" />
            <span className="text-gray-600">{t('host:batchSwitchModel.localModel')}</span>
          </label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="radio" checked={modelType === 'backup'} onChange={() => setModelType('backup')} className="w-4 h-4 accent-blue-500" />
            <span className="text-gray-600">{t('host:batchSwitchModel.backupModel')}</span>
          </label>
        </div>
      </div>
      <div>
        <span className="text-xs text-gray-500 mb-1.5 block">{t('host:batchSwitchModel.selectModelLabel')}</span>
        <Select
          value={currentModelValue}
          onChange={handleModelValueChange}
          options={[
            { value: '__random__', label: t('host:batchSwitchModel.randomModelOption') },
            ...(modelType === 'online'
              ? [...groupedModels.entries()].map(([ver, models]) => ({ label: `Android ${ver}`, options: models.map(m => ({ value: m.id, label: m.name })) }))
              : currentModelOptions.map(o => ({ value: o.value, label: o.label }))),
          ]}
        />
      </div>
      <div className="flex items-start gap-2 text-xs text-gray-500 bg-gray-50 border border-gray-100 rounded-md p-2.5">
        <AlertTriangle size={14} className="text-amber-400 shrink-0 mt-0.5" />
        <span>
          {modelType === 'local' ? `${t('host:batchSwitchModel.localWarning')} · ` : ''}
          {modelType === 'backup' ? `${t('host:batchSwitchModel.backupHint')} · ` : ''}
          {t('host:batchSwitchModel.switchWarning')}
          {showLocalWarning && <span className="text-red-500"> · {t('host:batchSwitchModel.localWarning')}</span>}
        </span>
      </div>

      <div className="flex justify-end pt-1">
        <button onClick={handleApply} disabled={!canApply || running}
          className="h-9 px-4 text-sm rounded-md bg-blue-500 text-white hover:bg-blue-600 transition-colors disabled:opacity-40">
          {running ? t('host:batchSwitchModel.switching') : t('host:batchSwitchModel.confirmSwitchCount', { count: androids.length })}
        </button>
      </div>
    </div>
  )
}
