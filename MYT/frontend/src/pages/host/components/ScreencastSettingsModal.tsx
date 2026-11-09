import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import Switch from '../../../components/Switch'

interface Props {
  gpuAccel: boolean
  onToggleGpuAccel: (on: boolean) => void
  sidebar: boolean
  onToggleSidebar: (on: boolean) => void
  onClose: () => void
}

// 注意事项 i18n key 列表（文案在各 locale 的 host.json actionBar.notice.* 定义）
const NOTICE_KEYS = [
  'host:actionBar.notice.needGpu',
  'host:actionBar.notice.driver',
  'host:actionBar.notice.autoFallback',
  'host:actionBar.notice.artifacts',
  'host:actionBar.notice.cpuCost',
  'host:actionBar.notice.nextCast',
]

function ScreencastSettingsModal({ gpuAccel, onToggleGpuAccel, sidebar, onToggleSidebar, onClose }: Props) {
  const { t } = useTranslation()
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/30"
      onClick={onClose}
    >
      <div
        className="w-[360px] max-w-[calc(100vw-32px)] bg-white rounded-xl shadow-2xl overflow-hidden"
        onClick={e => e.stopPropagation()}
      >
        {/* 标题栏 */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-100">
          <span className="text-sm font-semibold text-gray-800">{t('host:actionBar.screencastSettings')}</span>
          <button
            onClick={onClose}
            className="p-1 text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded transition-colors"
          >
            <X size={16} />
          </button>
        </div>

        <div className="px-5 py-4">
          {/* GPU 加速开关 */}
          <div className="flex items-center justify-between py-2">
            <div>
              <div className="text-sm font-medium text-gray-800">{t('host:actionBar.gpuAccel')}</div>
              <div className="text-xs text-gray-500 mt-0.5">
                {gpuAccel ? t('host:actionBar.gpuAccelOn') : t('host:actionBar.gpuAccelOff')}
              </div>
            </div>
            <Switch checked={gpuAccel} onChange={onToggleGpuAccel} />
          </div>

          {/* 分隔线 */}
          <div className="h-px bg-gray-100 my-3" />

          {/* 侧边栏开关（从顶栏移入投屏设置）*/}
          <div className="flex items-center justify-between py-2">
            <div>
              <div className="text-sm font-medium text-gray-800">{t('host:actionBar.sidebar')}</div>
              <div className="text-xs text-gray-500 mt-0.5">{t('host:actionBar.sidebarTitle')}</div>
            </div>
            <Switch checked={sidebar} onChange={onToggleSidebar} />
          </div>

          {/* 分隔线 */}
          <div className="h-px bg-gray-100 my-3" />

          {/* 注意事项 */}
          <div className="text-sm font-medium text-gray-800 mb-2">{t('host:actionBar.gpuAccelNotice')}</div>
          <ul className="space-y-1.5">
            {NOTICE_KEYS.map((key, i) => (
              <li key={i} className="text-xs text-gray-500 leading-relaxed flex gap-1.5">
                <span className="text-gray-400 shrink-0">•</span>
                <span>{t(key)}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  )
}

export default ScreencastSettingsModal
