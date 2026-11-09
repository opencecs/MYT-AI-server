import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import * as ScreencastService from '../../../../bindings/github.com/moyunteng/myt/screencastservicewrapper'
import { useDeployMode } from '../../../hooks/useDeployMode'
import { getErrorMessage } from '../../../utils/error'

interface Props {
  deviceIP: string
  slot: number
  orientation: string
  /**
   * V4 投屏所需：实例所属 tebox 后端名（android.id 的前缀，可空=默认后端）与实例 id
   * （android.id 的后缀）。V4 的投屏身份是 (host, backend, instanceID)，不是 (ip, 坑位号)
   * ——坑位号在 V4 是客户端派生的序号，服务端不认识。非 v4 模式忽略这两个字段。
   */
  v4Backend?: string
  v4InstanceID?: string
  /** 投屏窗口标题（V4 用实例名；不传由 Go 侧兜底） */
  v4Title?: string
  onClose: () => void
}

function ScreenCastModal({ deviceIP, slot, orientation, v4Backend, v4InstanceID, v4Title, onClose }: Props) {
  const { t } = useTranslation()
  const { mode } = useDeployMode()
  const [error, setError] = useState('')
  const launched = useRef(false)

  useEffect(() => {
    if (launched.current) return
    launched.current = true

    ;(async () => {
      try {
        // v4（tobox_player）：投屏走 tebox 协议，身份是 (host, backend, instanceID)，
        // 端口由 Go 侧按后端推导（实例的 cast_port 是宿主机 loopback 内部端口，
        // 外部播放器必须连后端全局媒体端口）。重复点击由 Go 侧 key 去重并置顶。
        if (mode === 'v4') {
          const instanceID = v4InstanceID || ''
          if (!instanceID) throw new Error(t('host:screencast.v4InstanceMissing'))
          await ScreencastService.LaunchV4Cast(deviceIP, v4Backend || '', instanceID, v4Title || instanceID)
          onClose()
          return
        }
        // 非 v4：RTSP 单台投屏
        const running = await ScreencastService.IsProjectionRunning(deviceIP, slot)
        if (running) {
          await ScreencastService.BringProjectionToFront(deviceIP, slot)
        } else {
          // 单台投屏默认 scale=1.0（原始大小，居中显示），与多台批量投屏用滑块控制区分
          // sidebar=true 默认带侧边栏（单台投屏侧边栏有设备控制工具，默认开启）
          await ScreencastService.StartBatchIndividualProjections([{ deviceIP, indexNum: slot, orientation }], 1.0, true)
        }
      } catch (err) {
        // Wails 的 error.message 是 JSON 串，直接 String() 会带出 RuntimeError 外壳
        setError(getErrorMessage(err))
      }
      onClose()
    })()
  }, [deviceIP, slot, mode, v4Backend, v4InstanceID, v4Title, orientation, onClose, t])

  if (error) {
    return (
      <div className="fixed bottom-4 right-4 z-50">
        <div className="bg-gray-800 rounded-lg p-4 shadow-xl max-w-xs">
          <p className="text-red-400 text-xs mb-3">{error}</p>
          <button onClick={onClose} className="text-xs px-3 py-1 bg-gray-700 text-gray-300 rounded hover:bg-gray-600">
            {t('host:screencast.close')}
          </button>
        </div>
      </div>
    )
  }

  return null
}

export default ScreenCastModal
