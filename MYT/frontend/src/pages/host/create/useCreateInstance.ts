import { Events } from '@wailsio/runtime'
import { useTranslation } from 'react-i18next'
import { emitSlotStatus } from '../../../utils/wailsEvent'
import * as ImageService from '../../../../bindings/github.com/moyunteng/myt/imageservicewrapper'
import * as PhoneModelService from '../../../../bindings/github.com/moyunteng/myt/phonemodelservice'
import * as ScreenshotService from '../../../../bindings/github.com/moyunteng/myt/screenshotservicewrapper'
import * as AndroidListService from '../../../../bindings/github.com/moyunteng/myt/androidlistservicewrapper'
import * as DiscoverService from '../../../../bindings/github.com/moyunteng/myt/discoverservicewrapper'
import { deviceFetch, pullImageToDevice, incrementIp } from './lib/deviceApi'
import { getErrorMessage } from '../../../utils/error'
import { showToast } from '../../../components/Toast'
import { cast } from '../../../utils/cast'
import { slotWarningMessage, needSlotConfirm } from '../../../utils/slotHelpers'
import { useTaskQueue, registerTaskAbort, unregisterTaskAbort } from '../../../hooks/taskQueue/useTaskQueue'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { useSlotMemory, setLastCreateSlots } from '../../../hooks/useSlotMemory'
import type { FormState } from './useCreateInstanceForm'
import type { DerivedState } from './lib/useCreateInstanceDerived'
import type { DeviceState, PhoneModel } from '../../../types/index'

interface CreateDeps {
  form: FormState
  derived: DerivedState
  device: DeviceState
  deviceId: string  // local 模式为 IP，public 模式为 device_id
  onCreated: () => void
  onClose: () => void
}

// 检查镜像是否已在设备上
async function checkImageOnDevice(ip: string, imageUrl: string, taskId?: string): Promise<boolean> {
  try {
    const images = await deviceFetch(ip, '/android/image', undefined, taskId)
    const list: { imageUrl: string }[] = Array.isArray(images) ? images : ((images as { list?: { imageUrl: string }[] })?.list ?? [])
    return list.some(img => img.imageUrl === imageUrl)
  } catch { return false }
}

export function useCreateInstance({ form, derived, device, deviceId, onCreated, onClose }: CreateDeps) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const isV2 = mode === 'v2'
  const { addTask, updateTask } = useTaskQueue()
  const [slotMemoryEnabled] = useSlotMemory()

  const generateName = (slot: number, index: number) => {
    const prefix = form.name.trim() || Date.now().toString()
    return `${prefix}_${slot}_${index + 1}`
  }

  // 解析实际镜像 URL：自定义镜像时用 customImageUrl
  const imageUrl = form.selectedImage === '__custom__' ? form.customImageUrl.trim() : form.selectedImage

  const doCreate = async () => {
    if (!imageUrl || form.selectedSlots.length === 0 || !derived.resolvedDns) return
    // 仅公有网卡-启用桥接时校验桥接IP（模拟器/容器同规则）
    const needBridgeCheck = form.networkCardType === 'public' && form.enableBridge
    if (needBridgeCheck && !form.macVlanIp.trim()) { showToast(t('host:toast.bridgeIpRequired'), 'error'); return }
    if (needBridgeCheck && !form.macVlanGw) { showToast(t('host:toast.bridgeGwMissing'), 'error'); return }
    // 私有网卡-指定了私有网卡时校验（模拟器/容器同规则）
    if (form.networkCardType === 'private' && !form.mytBridgeName) {
      showToast(t('host:toast.privateNicRequired'), 'error'); return
    }

    const ac = new AbortController()
    form.abortRef.current = ac
    form.setCreating(true); form.setError(''); form.setCreateProgress(-1); form.setCreateProgressText('')
    // 加入任务队列
    const totalInstances = form.selectedSlots.reduce((sum, s) => sum + form.count, 0)
    const isLocalImage = derived.isLocalImage(imageUrl)
    const localMirror = isLocalImage ? form.savedMirrors.find(m => m.url === imageUrl) : undefined
    const taskId = await addTask({ type: 'create', targets: form.selectedSlots.map(s => ({ slot: s, deviceIp: deviceId })), deviceIps: [deviceId], total: totalInstances, currentStep: isLocalImage ? 'image' : null, imageProgress: isLocalImage ? 0 : null, mirrorId: localMirror?.id })

    // 注册 AbortController：任务面板取消按钮 → cancelTask → ac.abort() 中断编排 await 链
    registerTaskAbort(taskId, ac)
    // 立即标记为运行中
    updateTask(taskId, { status: 'running', startTime: Date.now() })

    // 提交即关闭，进度在任务队列查看
    onClose()

    // 监听本地镜像上传进度，同步到任务队列
    const offUploadProgress = Events.On('upload-progress', (evt: unknown) => {
      const { deviceIP, progress } = (evt as { data?: { mirrorId: string; deviceIP: string; progress: number } })?.data || evt as { mirrorId: string; deviceIP: string; progress: number }
      if (deviceIP !== deviceId) return
      updateTask(taskId, { imageProgress: Math.round(progress) })
    })
    const offUploadComplete = Events.On('upload-complete', (evt: unknown) => {
      const { deviceIP } = (evt as { data?: { mirrorId: string; deviceIP: string } })?.data || evt as { mirrorId: string; deviceIP: string }
      if (deviceIP !== deviceId) return
      updateTask(taskId, { imageProgress: 100 })
    })

    // 注意：onClose 会导致组件卸载，但闭包中的变量仍然有效。
    // 使用 setTimeout 确保异步逻辑在 onClose 状态更新之后执行，避免 React 状态更新竞争。
    setTimeout(async () => {
    try {
      // v2 模式：跳过镜像检查/推送与机型推送——81 create 的 image_addr 直传
      // 线上 registry 地址，主机 mytsdk 本地没有会自动拉取（V2 Web 同款语义，
      // 本地镜像推送走的 8000 上传通路在 v2 主机上不存在）
      if (!isV2) {
      // 检查镜像是否在设备上
      updateTask(taskId, { currentStep: 'image', imageProgress: 0 })
      const imageExists = await checkImageOnDevice(deviceId, imageUrl, taskId)
      if (ac.signal.aborted) return
      if (!imageExists) {
        const local = derived.isLocalImage(imageUrl)
        if (local) {
          const mirror = form.savedMirrors.find(m => m.url === imageUrl)
          if (mirror) {
            form.setCreateStep(t('host:create.stepPushingLocalImage')); form.setCreateProgress(0)
            // PushToDevices 异步执行，上传进度通过 upload-progress 事件推送到任务队列
            await ImageService.PushToDevices(mirror.id, [deviceId])
            // 等待上传完成（upload-complete 事件），上限与 Go 侧上传超时一致（10 分钟，uploader.go）
            let uploadConfirmed = false
            await new Promise<void>((resolve) => {
              const timeout = setTimeout(() => { off(); resolve() }, 600000)
              const off = Events.On('upload-complete', (evt: unknown) => {
                const { deviceIP, success } = (evt as { data?: { deviceIP: string; success: boolean } })?.data || evt as { deviceIP: string; success: boolean }
                if (deviceIP !== deviceId) return
                clearTimeout(timeout)
                if (!success) {
                  updateTask(taskId, { status: 'failed', error: t('host:create.pushImageFailed'), endTime: Date.now() })
                  ac.abort()
                } else {
                  uploadConfirmed = true
                }
                resolve()
              })
            })
            if (ac.signal.aborted) return
            // 超时未收到完成事件：回查设备确认镜像是否已到位（事件可能丢失但上传实际已完成）；
            // 确实未到位则终止任务，否则后续逐台创建会全部因镜像缺失失败
            if (!uploadConfirmed) {
              const imageArrived = await checkImageOnDevice(deviceId, imageUrl, taskId)
              if (ac.signal.aborted) return
              if (!imageArrived) {
                updateTask(taskId, { status: 'failed', error: t('host:create.pushImageTimeout'), endTime: Date.now() })
                return
              }
            }
          }
        } else {
          form.setCreateStep(t('host:create.stepPullingOnlineImage')); form.setCreateProgress(0)
          await pullImageToDevice(deviceId, imageUrl, (pct, text) => {
            form.setCreateProgress(pct); form.setCreateProgressText(text)
            updateTask(taskId, { imageProgress: pct })
          }, ac.signal, taskId)
        }
      }
      if (ac.signal.aborted) return
      } // end !isV2（镜像检查/推送跳过）

      // 推送本地机型（仅模拟器）。固定机型一次推送拿注册名；
      // 随机（'__random__'）机型每台云机独立挑选，懒推送——按机型去重缓存注册名，
      // 同一 zip 不重复上传（push 是 5 分钟长超时的文件导入）。
      const localModelRandom = form.createType === 'simulator' && form.selectedLocalModel === '__random__'
      let resolvedLocalModel = form.createType === 'simulator' && !localModelRandom ? form.selectedLocalModel : ''
      if (!isV2 && form.selectedLocalModel && !localModelRandom) {
        form.setCreateStep(t('host:create.stepPushingLocalModel'))
        const deviceName = await PhoneModelService.PushPhoneModel(deviceId, form.selectedLocalModel) as string
        if (deviceName) resolvedLocalModel = deviceName
      }
      const localRegistered = new Map<string, string>()
      const resolveRandomLocalModel = async (modelName: string): Promise<string> => {
        if (!localRegistered.has(modelName)) {
          form.setCreateStep(t('host:create.stepPushingLocalModel'))
          const deviceName = await PhoneModelService.PushPhoneModel(deviceId, modelName) as string
          localRegistered.set(modelName, deviceName || modelName)
        }
        return localRegistered.get(modelName)!
      }

      // 创建云机
      form.setCreateStep(isV2 ? t('host:create.stepCreatingV2') : t('host:create.stepCreating')); form.setCreateProgress(-1)
      updateTask(taskId, { currentStep: 'create' })

      const isSimulator = form.createType === 'simulator'
      // 容器公有网卡批量创建：macVlanIp 按实例索引递增，避免多台 IP 冲突
      let ipOffset = 0
      let models = derived.filteredModels
      if (isSimulator && models.length === 0 && form.selectedModelId === 'random') {
        try {
          const m = await deviceFetch(deviceId, '/android/phoneModel?page=0', undefined, taskId)
          const list: PhoneModel[] = cast<PhoneModel[]>(Array.isArray(m) ? m : (m?.list ?? []))
          if (list.length > 0) {
            form.setPhoneModels(list)
            models = !form.androidVersion ? list : list.filter(pm => !pm.android_version || String(pm.android_version) === form.androidVersion)
          }
        } catch {}
      }

      for (let i = 0; i < form.selectedSlots.length; i++) {
        if (ac.signal.aborted) break
        const slot = form.selectedSlots[i]
        ScreenshotService.InvalidateSlot(deviceId, slot).catch(() => {})
        emitSlotStatus(`${deviceId}_${slot}`, 'starting')
        const slotAlreadyRunning = (derived.androidsBySlot.get(slot) || []).some(a => a.status === 'running')

        for (let j = 0; j < form.count; j++) {
          if (ac.signal.aborted) break
          const containerName = generateName(slot, j)
          form.setCreateStep(t('host:create.stepCreating'))
          const slotExpireState = derived.expiryMap.get(slot)?.state
          const slotCanStart = slotExpireState === 0 || slotExpireState === 1
          const shouldStart = j === 0 && slotCanStart ? !slotAlreadyRunning : false

          if (isSimulator) {
            if (isV2) throw new Error(t('host:create.v2SimulatorUnsupported'))
            // 模拟器：POST /android
            let resolvedModelId = form.selectedModelId
            let resolvedModelName = models.find(m => m.id === resolvedModelId)?.name || ''
            // 随机本地机型：每台独立挑选（列表空则等价不使用，走线上机型）
            let slotLocalModel = resolvedLocalModel
            if (localModelRandom) {
              const picked = form.localModelNames.length > 0
                ? form.localModelNames[Math.floor(Math.random() * form.localModelNames.length)]
                : ''
              slotLocalModel = picked ? await resolveRandomLocalModel(picked) : ''
            }
            if (slotLocalModel) {
              const fallback = models[0]
              resolvedModelId = fallback?.id || form.selectedModelId
              resolvedModelName = slotLocalModel
            } else if (form.selectedModelId === 'random') {
              if (models.length > 0) { const picked = models[Math.floor(Math.random() * models.length)]; resolvedModelId = picked.id; resolvedModelName = picked.name }
              else throw new Error(t('host:create.noModelForVersion', { version: form.androidVersion }))
            }
            const body: Record<string, unknown> = {
              name: containerName, imageUrl, dns: derived.resolvedDns, indexNum: slot,
              modelId: resolvedModelId, modelName: resolvedModelName,
              localModel: slotLocalModel || undefined,
              sandboxSize: form.sandboxSize,
              // 'model'（跟随机型分辨率）：不传宽高/DPI，让设备端按机型默认走
              ...(form.resolution === 'model' ? {} : { doboxWidth: derived.resW, doboxHeight: derived.resH, doboxDpi: derived.resDpi }),
              start: shouldStart, countryCode: form.selectedCountry,
              mgenable: form.enableMagisk ? '1' : '0', gmsenable: form.enableGMS ? '1' : '0',
              enforce: form.enforce, randomFile: form.randomFile,
              compatMode: form.compatMode ? '1' : '0',
            }
            // 网卡体系（与容器路径一致）：私有走 mytBridgeName，公有走独立IP桥接
            if (form.networkCardType === 'private') {
              if (form.mytBridgeName) body.mytBridgeName = form.mytBridgeName
            } else if (form.enableBridge && form.macVlanIp && form.macVlanGw) {
              // 与容器路径一致：批量创建时按实例索引递增，避免多台 IP 冲突
              body.macVlanIp = incrementIp(form.macVlanIp, ipOffset)
              ipOffset++
            }
            const created = await deviceFetch(deviceId, '/android', { method: 'POST', body: JSON.stringify(body) }, taskId)
            if (form.remark && created?.id) DiscoverService.SetAndroidNote(created.id, form.remark).catch(() => {})
          } else {
            // 容器：POST /androidV2
            const body: Record<string, unknown> = {
              name: containerName, imageUrl, dns: derived.resolvedDns, indexNum: slot,
              sandboxSize: form.sandboxMode ? form.sandboxSize : '',
              // 'model'（跟随机型分辨率）：不传宽高/DPI，v2 翻译层宽高缺失即不下发 resolution
              ...(form.resolution === 'model' ? {} : { doboxWidth: derived.resW, doboxHeight: derived.resH, doboxDpi: derived.resDpi }),
              start: shouldStart, enforce: form.enforce,
              compatMode: form.compatMode ? '1' : '0',
            }
            // 网卡体系：公有走独立IP，私有走 mytBridgeName
            if (form.networkCardType === 'public') {
              if (form.enableBridge && form.macVlanIp && form.macVlanGw) {
                body.macVlanIp = incrementIp(form.macVlanIp, ipOffset)
                ipOffset++
              }
            } else if (form.networkCardType === 'private' && form.mytBridgeName) {
              body.mytBridgeName = form.mytBridgeName
            }
            // VPC 网络：创建时指定节点
            if (form.vpcGroupId) {
              if (form.vpcSelectMode === 'random') {
                const group = form.vpcGroups.find(g => String(g.id) === form.vpcGroupId)
                const nodes = group?.vpcs?.list || []
                if (nodes.length > 0) {
                  const picked = nodes[Math.floor(Math.random() * nodes.length)]
                  body.vpcID = picked.id
                }
              } else if (form.vpcNodeId) {
                body.vpcID = Number(form.vpcNodeId)
              }
            }
            const created = await deviceFetch(deviceId, '/androidV2', { method: 'POST', body: JSON.stringify(body) }, taskId)
            if (form.remark && created?.id) DiscoverService.SetAndroidNote(created.id, form.remark).catch(() => {})
            // 壳子同款：dc create 无 start 参数（实测被 mytsdk 忽略），创建成功后
            // 显式开机（POST /android/start → 81 dc_api/v1/run）。
            // 失败不阻断创建流程（容器已建出，用户可手动开机）
            if (shouldStart) {
              try {
                await deviceFetch(deviceId, '/android/start', { method: 'POST', body: JSON.stringify({ name: containerName }) }, taskId)
              } catch (startErr) {
                console.warn('[create] 创建后开机失败（容器已创建）:', containerName, startErr)
              }
            }
          }
          // 创建是前端直调设备 API，绕过了 Go 侧通知链路：
          // 显式触发容器列表刷新（NotifyAndroidsChanged → Go refreshAndroids：
          // local 模式立即查询 + 2s 补查一次；v2 模式 QueryDeviceByIdent 补拉），
          // 新容器逐个出现在 UI，无需手动刷新
          AndroidListService.NotifyAndroidsChanged([deviceId]).catch(() => {})
          // v2：81 create 返回后 mytsdk 的 list 视图有同步延迟（且容器 state 迁移
          // created→running 有窗口）——立即查可能拿到旧列表且无后续，表现为「一直
          // 开机中」要手动刷新。对齐壳子（创建完成回调里显式拉列表）的语义：
          // 成功路径也补 3s/8s 两次重查，确保新容器必现。
          if (isV2) {
            setTimeout(() => { AndroidListService.NotifyAndroidsChanged([deviceId]).catch(() => {}) }, 3000)
            setTimeout(() => { AndroidListService.NotifyAndroidsChanged([deviceId]).catch(() => {}) }, 8000)
          }
        }
      }
      if (ac.signal.aborted) return
      // 「坑位记忆」开启：记录本次创建选择的坑位，供下次打开表单时回填
      // （写前判断开关——关闭时不动已存的记忆值）
      if (slotMemoryEnabled) setLastCreateSlots(form.selectedSlots)
      onCreated()
      // 整批完成后立即触发截图轮询，加速新容器截图展示（列表此时已刷新）
      ScreenshotService.TriggerNow().catch(() => {})
      updateTask(taskId, { status: 'completed', progress: 100, completed: totalInstances, endTime: Date.now() })
    } catch (err) {
      if (ac.signal.aborted) return  // 取消：由 finally 统一置 canceled
      form.setError(getErrorMessage(err))
      updateTask(taskId, { status: 'failed', error: getErrorMessage(err), endTime: Date.now() })
      // v2 兜底：81 create 同步等待分钟级（首建拉镜像），请求超时/失败时容器可能
      // 已实际建出——延迟刷新容器列表，避免「创建成功但列表没反应」的观感
      if (isV2) {
        setTimeout(() => { AndroidListService.NotifyAndroidsChanged([deviceId]).catch(() => {}) }, 5000)
        setTimeout(() => { AndroidListService.NotifyAndroidsChanged([deviceId]).catch(() => {}) }, 20000)
      }
    } finally {
      // 取消（静默 return 或抛错进 catch）：统一置 canceled，避免卡在 running 或误显 completed
      if (ac.signal.aborted) updateTask(taskId, { status: 'canceled', endTime: Date.now() })
      unregisterTaskAbort(taskId)
      offUploadProgress(); offUploadComplete()
      form.setCreating(false); form.setCreateStep(''); form.setCreateProgress(-1); form.setCreateProgressText('')
      form.abortRef.current = null
    }
    }, 0)
  }

  const handleCreate = () => {
    if (!imageUrl || form.selectedSlots.length === 0 || !derived.resolvedDns) return
    const warningSlot = form.selectedSlots.find(s => needSlotConfirm(derived.expiryMap.get(s)?.state))
    if (warningSlot !== undefined) {
      const state = derived.expiryMap.get(warningSlot)?.state
      form.setSlotConfirm({ message: slotWarningMessage(state), onConfirm: () => { form.setSlotConfirm(null); doCreate() } })
      return
    }
    doCreate()
  }

  const cancelCreate = () => {
    form.abortRef.current?.abort()
    form.abortRef.current = null
    form.setCreating(false); form.setCreateStep(''); form.setCreateProgress(-1); form.setCreateProgressText('')
  }

  const handleClose = () => { cancelCreate(); onClose() }

  return { handleCreate, cancelCreate, handleClose, canCreate: derived.canCreate }
}
