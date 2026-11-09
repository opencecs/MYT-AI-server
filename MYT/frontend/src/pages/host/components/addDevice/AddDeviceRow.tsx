import { memo } from 'react'
import { useTranslation } from 'react-i18next'
import type { DeviceState } from '../../../../types/index'

interface Props {
  device: DeviceState
  checked: boolean
  onToggle: (id: string) => void
  /** 显示「未装超级 SDK」标注（v2 模式待添加列表传入） */
  showSdkBadge?: boolean
}

/**
 * 添加设备弹窗「待添加列表」单行：型号作标题、编号弱化第二行、IP 等宽右对齐。
 * memo 化后，只有 checked 变化的那一行重渲染，避免勾选时全列表重渲染。
 */
function AddDeviceRow({ device, checked, onToggle, showSdkBadge }: Props) {
  const { t } = useTranslation()
  // v2 准入放宽：81 缺失的设备也进待添加，行内标注提醒用户补装超级 SDK
  const needSdk = showSdkBadge === true && device.super_sdk_ready === false
  return (
    <label
      className={`flex items-center gap-3 px-3 py-2 cursor-pointer border-b border-gray-50 last:border-b-0 transition-colors
        ${checked ? 'bg-blue-50/70' : 'hover:bg-gray-50'}`}
    >
      <input
        type="checkbox"
        checked={checked}
        onChange={() => onToggle(device.device_id)}
        className="sr-only peer"
      />
      <span
        className="w-[18px] h-[18px] shrink-0 rounded-[5px] border-[1.5px] flex items-center justify-center
          peer-checked:bg-blue-600 peer-checked:border-blue-600 border-gray-300 bg-white
          peer-focus-visible:ring-2 peer-focus-visible:ring-blue-500/30 transition-colors"
      >
        <svg
          width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="3.2"
          strokeLinecap="round" strokeLinejoin="round"
          className={checked ? 'opacity-100 scale-100' : 'opacity-0 scale-50'}
        >
          <polyline points="20 6 9 17 4 12" />
        </svg>
      </span>
      <span className="flex-1 min-w-0">
        <span className="block text-[13.5px] font-semibold text-gray-800 leading-tight">
          {device.device_type?.toUpperCase() || device.device_name}
          <span className="font-mono text-[11px] font-normal text-gray-400 ml-1.5">{device.version}</span>
          {needSdk && (
            <span className="ml-1.5 text-[10.5px] font-normal text-amber-600 bg-amber-50 px-1.5 py-px rounded align-middle">
              {t('host:superSdk.notInstalled')}
            </span>
          )}
        </span>
        <span className="block font-mono text-[11.5px] text-gray-400 truncate leading-tight mt-0.5">
          {device.device_id}
        </span>
      </span>
      <span className={`font-mono text-[13px] font-medium tabular-nums ${checked ? 'text-blue-600' : 'text-gray-700'}`}>
        {device.ip}
      </span>
    </label>
  )
}

export default memo(AddDeviceRow)
