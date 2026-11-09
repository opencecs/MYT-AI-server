import { useState, useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import * as ScreencastService from '../../../../bindings/github.com/moyunteng/myt/screencastservicewrapper'
import * as SdkApiService from '../../../../bindings/github.com/moyunteng/myt/sdkapiservicewrapper'
import { AndroidInfo, Orientation } from '../../../types/index'
import { showToast } from '../../../components/Toast'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getDeviceId } from '../../../utils/deviceId'
import { isRunnableStatus } from '../../../utils/containerStatus'
import { containerDisplayName } from '../../../utils/containerName'
import { useAndroidNotes } from '../../../hooks/useNoteSync'

interface Params {
  selectedAndroids: AndroidInfo[]
  readyKeys: Set<string>
  setBatching: (label: string) => void
  orientation: Orientation
}

// 批量投屏 / 批量控制相关逻辑（不进任务队列，用 batching 状态反馈）
export function useBatchProjection({ selectedAndroids, readyKeys, setBatching, orientation }: Params) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  // V4 投屏标题用：备注优先 + 短名（与 SlotView/CardHeader 同规则）
  const { notes } = useAndroidNotes()
  const [matrixScale] = useState(1.0) // 固定 1.0（旧客户端无 scale，排列铺满；保留给 StartMatrixProjection 等接口传参）
  const [sidebar, setSidebarState] = useState(true)  // 侧边栏开关：关了省 ~80MB/窗口 + Edge 进程。持久化到后端，所有投屏入口共用。

  // 初始化：从后端读取持久化的侧边栏开关状态
  useEffect(() => {
    ScreencastService.GetNoSidebar().then(v => setSidebarState(!v)).catch(() => {})
  }, [])

  // 开关变化：存后端，让群控/单台等所有入口都读到（不依赖前端 state 传递）
  const setSidebar = useCallback((on: boolean) => {
    setSidebarState(on)
    ScreencastService.SetNoSidebar(!on).catch(() => {})
  }, [])

  // matrixScale 固定 1.0，滑块已移除，不再监听缩放变化（旧客户端无 scale，排列铺满）

  const withBatching = async (label: string, fn: () => Promise<void>) => {
    setBatching(label)
    const t0 = Date.now()
    try {
      await fn()
    } catch (err) {
      console.error(`${label} failed:`, err)
    }
    const elapsed = Date.now() - t0
    if (elapsed < 500) await new Promise(r => setTimeout(r, 500 - elapsed))
    setBatching('')
  }

  // 批量投屏：统一走 StartBatchIndividualProjections，窗口可调大小+网格排列
  const runBatchScreenCast = () => {
    if (selectedAndroids.length === 0) return

    withBatching(t('host:actionBar.batchCast'), async () => {
      // V4（tobox_player）：投屏门槛用「可运行」而非「截图就绪」——V4 的截图是
      // 服务端抽帧（GET /android/:id/screenshot），比可投屏握手慢一拍，用截图就绪
      // 做门槛会让「刚开机」的实例被误判为不可投屏。每台走 LaunchV4Cast（Go 侧按
      // 实例所属后端推导全局媒体端口，并做同实例去重置顶）。与 V3 的差异只在数据层。
      if (mode === 'v4') {
        const runnable = selectedAndroids.filter(a => isRunnableStatus(a.status))
        const skipped = selectedAndroids.length - runnable.length
        if (runnable.length === 0) {
          showToast(t('host:toast.castNotReady'), 'error')
          return
        }
        if (skipped > 0) showToast(t('host:toast.castSkipped', { count: skipped }), 'error')
        const results = await Promise.allSettled(runnable.map(a => {
          const i = a.id.indexOf('/')
          const backend = i > 0 ? a.id.slice(0, i) : ''
          const instanceID = i > 0 ? a.id.slice(i + 1) : a.id
          return ScreencastService.LaunchV4Cast(a.ip, backend, instanceID, notes[a.id] || containerDisplayName(a.name, a.deviceId, a.indexNum))
        }))
        const failed = results.filter(r => r.status === 'rejected')
        if (failed.length > 0) {
          showToast(t('host:toast.castSkipped', { count: failed.length }), 'error')
        }
        return
      }
      // 就绪键与生成端同源（ScreenshotView 的 statusMap/screenshotMap 都以 a.ip 为键）；
      // 不能用 getDeviceId（v2 下是序列号，与 ip 键永不相交 → 恒报「未就绪」）
      const ready = selectedAndroids.filter(a => readyKeys.has(`${a.ip}_${a.indexNum}`))
      const skipped = selectedAndroids.length - ready.length
      if (ready.length === 0) {
        showToast(t('host:toast.castNotReady'), 'error')
        return
      }
      if (skipped > 0) showToast(t('host:toast.castSkipped', { count: skipped }), 'error')
      const items = ready.map(a => ({ deviceIP: getDeviceId(a, mode), indexNum: a.indexNum, orientation }))
      await ScreencastService.StartBatchIndividualProjections(items, matrixScale, sidebar)
    })
  }

  const runBatchStopCast = () => {
    withBatching(t('host:actionBar.closeCast'), async () => {
      await ScreencastService.StopAllProjections()
    })
  }

  // 重置投屏（选中实例，投屏卡死/一直加载中的自救）：
  // 1. 对选中设备 adb 执行 kill -9 $(pidof hwcodec)（设备端编码器卡死重启）
  // 2. 停止选中实例的投屏窗口（其它投屏不受影响）
  // 3. 等设备端 hwcodec 重启后按原坑位重新拉起（投屏进程重启）
  // 不做就绪校验（readyKeys）——恢复工具，截图拉不到时也要能用
  const runResetCasting = () => {
    if (selectedAndroids.length === 0) return
    // V4：投屏是 tebox 协议（无 hwcodec 编码器、无 RTSP 窗口），「重置」等价于
    // 关掉这些实例的投屏窗口再重新拉起——同样能解决卡死/黑屏。仅数据层分支。
    if (mode === 'v4') {
      withBatching(t('host:actionBar.resetCast'), async () => {
        const split = (a: { id: string }) => {
          const i = a.id.indexOf('/')
          return { backend: i > 0 ? a.id.slice(0, i) : '', instanceID: i > 0 ? a.id.slice(i + 1) : a.id }
        }
        for (const a of selectedAndroids) {
          const { backend, instanceID } = split(a)
          ScreencastService.StopV4Cast(a.ip, backend, instanceID)
        }
        await new Promise(r => setTimeout(r, 800))
        const results = await Promise.allSettled(selectedAndroids.map(a => {
          const { backend, instanceID } = split(a)
          return ScreencastService.LaunchV4Cast(a.ip, backend, instanceID, notes[a.id] || containerDisplayName(a.name, a.deviceId, a.indexNum))
        }))
        const failed = results.filter(r => r.status === 'rejected')
        if (failed.length > 0) showToast(t('host:toast.castSkipped', { count: failed.length }), 'error')
        else showToast(t('host:toast.resetCastDone'), 'success')
      })
      return
    }
    withBatching(t('host:actionBar.resetCast'), async () => {
      const items = selectedAndroids.map(a => ({ deviceIP: getDeviceId(a, mode), indexNum: a.indexNum, name: a.name }))
      // 1. kill hwcodec（三模式统一：local/public 8000 /android/exec，v2 翻译层走 81 shell）
      const ips = items.map(it => it.deviceIP)
      const names = items.map(it => it.name).filter(Boolean)
      if (names.length > 0) {
        try {
          await SdkApiService.BatchDockerExec(ips, names, 'kill -9 $(pidof hwcodec)')
        } catch (e) {
          console.warn('[resetCast] kill hwcodec 失败（继续重启窗口）:', e)
        }
      }
      // 2. 停止选中实例的投屏窗口
      await Promise.all(items.map(it =>
        ScreencastService.StopProjection(it.deviceIP, it.indexNum).catch(() => {})
      ))
      // 3. 等设备端 hwcodec 重启完成再拉起
      await new Promise(r => setTimeout(r, 2500))
      await ScreencastService.StartBatchIndividualProjections(
        items.map(it => ({ deviceIP: it.deviceIP, indexNum: it.indexNum, orientation })),
        matrixScale, sidebar
      )
      showToast(t('host:toast.resetCastDone'), 'success')
    })
  }

  return { runBatchScreenCast, runBatchStopCast, runResetCasting, matrixScale, sidebar, setSidebar }
}
