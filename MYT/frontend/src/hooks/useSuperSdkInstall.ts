import { useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as V2Service from '../../bindings/github.com/moyunteng/myt/v2servicewrapper'
import { showToast } from '../components/Toast'
import type { DeviceState } from '../types/index'
import { useDeployMode } from './useDeployMode'
import { getDeviceId } from '../utils/deviceId'

/**
 * 超级 SDK 安装 hook（v2 模式，任务队列版）：调 InstallSuperSdk 提交
 * superSdkInstall 任务（后端立即返回 taskId），进度/终态由任务队列接管
 * （任务面板按 dp.stage 展示阶段）。就绪状态翻转经 devicelist:updated
 * 广播回流（CheckSuperSdkReady 状态变化时 emit + 任务完成落库），本 hook
 * 不再订阅进度事件。onInstalled 可选回调（如关闭弹窗让用户重进拿全新数据）。
 * 后端前置校验已就绪（InstallSuperSdk 报「已安装，无需重复安装」）时以
 * warning 提示且不触发 onInstalled（设备本来就是好的，无需刷新动作）。
 */
export function useSuperSdkInstall(onInstalled?: () => void) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const submittingRef = useRef(false)

  const install = useCallback(async (device: DeviceState | { device_id: string; ip?: string } | null) => {
    if (!device || submittingRef.current) return
    if (mode !== 'v2') return
    const did = getDeviceId(device as DeviceState, 'v2')
    if (!did) return
    submittingRef.current = true
    try {
      const taskId = await V2Service.InstallSuperSdk(did)
      showToast(t('host:superSdk.submitted', { taskId: taskId || '' }), 'success')
      onInstalled?.()
    } catch (e: unknown) {
      const msg = String(e).replace(/^Error:\s*/, '')
      if (msg.includes('无需重复安装')) {
        // 后端前置校验：已就绪设备重复点安装 → warning 提示，不当错误也不关弹窗
        showToast(t('host:superSdk.alreadyInstalled'), 'warning')
        return
      }
      showToast(msg || t('host:superSdk.installFailed'), 'error')
    } finally {
      submittingRef.current = false
    }
  }, [mode, onInstalled, t])

  return { install, installing: false, stage: 'idle' as const, stageMsg: '' }
}
