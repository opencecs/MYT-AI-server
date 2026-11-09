import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { emitSlotStatus } from '../../../../utils/wailsEvent'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { getDeviceId } from '../../../../utils/deviceId'
import * as SdkApiService from '../../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import * as ScreenshotService from '../../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import { ContainerRandomizeDeviceInfoOptions } from '../../../../../bindings/github.com/moyunteng/myt/models'
import type { AndroidInfo } from '../../../../types/index'
import { getErrorMessage } from '../../../../utils/error'
import { cast } from '../../../../utils/cast'
import { useTaskQueue } from '../../../../hooks/taskQueue/useTaskQueue'
import Select from '../../../../components/Select'
import { inputCls } from './modelSwitchUtils'

// ============== 容器流程：异步随机设备信息（MYTOS /modifydev?cmd=2）==============

export function ContainerSection({ androids, onSwitched, onClose }: { androids: AndroidInfo[]; onSwitched?: (a: AndroidInfo) => void; onClose?: () => void }) {
  const { t } = useTranslation()
  const { mode: deployMode } = useDeployMode()
  const [dict, setDict] = useState<{ brand: string; models: Record<string, number> }[]>([])
  const [mode, setMode] = useState<'random' | 'specified'>('random')
  const [brand, setBrand] = useState('')
  const [model, setModel] = useState('')
  const [language, setLanguage] = useState('')
  const [randomAbroad, setRandomAbroad] = useState(false)
  const [modifyMAC, setModifyMAC] = useState(false)
  const [userIP, setUserIP] = useState('')
  const [isAsync, setIsAsync] = useState(true)
  const [running, setRunning] = useState(false)
  const { addTask, updateTask } = useTaskQueue()

  useEffect(() => {
    SdkApiService.GetContainerModelDictionary().then((entries: any) => {
      const list = cast<{ brand: string; models: Record<string, number> }[]>(Array.isArray(entries) ? entries : [])
      setDict(list)
      if (list.length > 0) setBrand(list[0].brand)
    }).catch(() => {})
  }, [])

  const currentBrandModels = dict.find(d => d.brand === brand)?.models ?? {}
  const modelOptions = Object.keys(currentBrandModels).sort()
  const canApply = mode === 'random' || (mode === 'specified' && model !== '')

  const handleApply = async () => {
    if (!canApply || running) return
    setRunning(true)
    const taskId = await addTask({
      type: 'switchModel',
      targets: androids.map(a => ({ deviceIp: getDeviceId(a, deployMode), name: a.name })),
      deviceIps: [...new Set(androids.map(a => getDeviceId(a, deployMode)))],
      total: androids.length,
    })
    updateTask(taskId, { status: 'running', startTime: Date.now() })
    onClose?.() // 提交即关闭：进度在任务队列面板看（与其他批量弹窗一致）

    let completed = 0, failed = 0
    try {
      for (const a of androids) {
        const opts = new ContainerRandomizeDeviceInfoOptions({
          isAsync: isAsync,
          isSpecifiedModel: mode === 'specified',
          modelName: mode === 'specified' ? model : '',
          language,
          randomAbroad,
          modifyMAC,
          userIP,
        })
        try {
          const res = await SdkApiService.ContainerRandomizeDeviceInfo(getDeviceId(a, deployMode), a.name, opts)
          if (isAsync && res?.taskId) {
            // 轮询异步任务，最多等 90 秒
            let done = false
            for (let i = 0; i < 45; i++) {
              await new Promise(r => setTimeout(r, 2000))
              const st: any = await SdkApiService.ContainerQueryRandomizeTask(getDeviceId(a, deployMode), a.name, res.taskId)
              const code = st?.code
              const reason = st?.reason
              if (code === 200) { done = true; break }
              if (code === 202 && reason === 'task timeout') { done = true; break }
              if (code !== 202) { throw new Error(`Query task failed: ${JSON.stringify(st)}`) }
            }
            if (!done) throw new Error('Async task timed out')
          }
          ScreenshotService.InvalidateSlot(getDeviceId(a, deployMode), a.indexNum).catch(() => {})
          emitSlotStatus(`${getDeviceId(a, deployMode)}_${a.indexNum}`, 'starting')
          onSwitched?.(a)
          completed++
        } catch (err) {
          console.error('Container model switch failed', getDeviceId(a, deployMode), a.name, err)
          failed++
        }
        const pct = Math.round((completed + failed) / androids.length * 100)
        updateTask(taskId, { progress: pct, completed, failed, status: 'running' })
      }
      updateTask(taskId, { status: failed === 0 ? 'completed' : 'failed', progress: 100, completed, failed, endTime: Date.now() })
    } catch (err) {
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="border border-gray-200 rounded-xl p-4 space-y-4 bg-white">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-800">{t('host:batchSwitchModel.containerTitle')}</h3>
        <span className="text-[10px] text-gray-400">{t('host:batchSwitchModel.containerApi')}</span>
      </div>
      <p className="text-[11px] text-gray-500">{t('host:batchSwitchModel.containerDesc')}</p>

      <div className="flex gap-4 text-xs">
        <label className="flex items-center gap-2 cursor-pointer">
          <input type="radio" checked={mode === 'random'} onChange={() => setMode('random')} className="w-4 h-4 accent-blue-500" />
          <span className="text-gray-600">{t('host:batchSwitchModel.randomModel')}</span>
        </label>
        <label className="flex items-center gap-2 cursor-pointer">
          <input type="radio" checked={mode === 'specified'} onChange={() => setMode('specified')} className="w-4 h-4 accent-blue-500" />
          <span className="text-gray-600">{t('host:batchSwitchModel.specifiedModel')}</span>
        </label>
      </div>

      {mode === 'specified' && (
        <div className="grid grid-cols-2 gap-3">
          <div>
            <span className="text-xs text-gray-500 mb-1.5 block">{t('host:batchSwitchModel.brand')}</span>
            <Select value={brand} onChange={v => { setBrand(v); setModel('') }} options={dict.map(d => ({ value: d.brand, label: d.brand }))} />
          </div>
          <div>
            <span className="text-xs text-gray-500 mb-1.5 block">{t('host:batchSwitchModel.model')}</span>
            <Select value={model} onChange={setModel} options={[{ value: '', label: t('host:batchSwitchModel.selectModel') }, ...modelOptions.map(m => ({ value: m, label: m }))]} placeholder={t('host:batchSwitchModel.selectModel')} />
          </div>
        </div>
      )}

      <div className="grid grid-cols-2 gap-3">
        <label className="flex items-center gap-2 text-xs text-gray-600 cursor-pointer">
          <input type="checkbox" checked={randomAbroad} onChange={e => setRandomAbroad(e.target.checked)} className="w-4 h-4 accent-blue-500 rounded" />
          <span>{t('host:batchSwitchModel.randomOverseas')}</span>
        </label>
        <label className="flex items-center gap-2 text-xs text-gray-600 cursor-pointer">
          <input type="checkbox" checked={modifyMAC} onChange={e => setModifyMAC(e.target.checked)} className="w-4 h-4 accent-blue-500 rounded" />
          <span>{t('host:batchSwitchModel.modifyMac')}</span>
        </label>
        <label className="flex items-center gap-2 text-xs text-gray-600 cursor-pointer col-span-2">
          <input type="checkbox" checked={isAsync} onChange={e => setIsAsync(e.target.checked)} className="w-4 h-4 accent-blue-500 rounded" />
          <span>{t('host:batchSwitchModel.asyncExec')}</span>
        </label>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <span className="text-xs text-gray-500 mb-1.5 block">{t('host:batchSwitchModel.languageOptional')}</span>
          <input value={language} onChange={e => setLanguage(e.target.value)} placeholder={t('host:batchSwitchModel.languagePlaceholder')} className={`w-full ${inputCls}`} />
        </div>
        <div>
          <span className="text-xs text-gray-500 mb-1.5 block">{t('host:batchSwitchModel.regionIp')}</span>
          <input value={userIP} onChange={e => setUserIP(e.target.value)} placeholder={t('host:batchSwitchModel.regionIpPlaceholder')} className={`w-full ${inputCls}`} />
        </div>
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
