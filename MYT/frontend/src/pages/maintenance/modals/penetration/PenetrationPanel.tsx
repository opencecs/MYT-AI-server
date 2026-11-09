import { useTranslation } from 'react-i18next'
import { Globe, ShieldCheck } from 'lucide-react'
import { TunnelSection } from '../TunnelSection'
import { VpnSection } from './VpnSection'

/**
 * 公网穿透双入口面板：主机右键设备弹窗内左右并列。
 *
 * 左「frp 公网穿透」复用 TunnelSection（零改动）；右「VPN 穿透」为
 * 从智能体页迁移过来的单设备面板（原批量面板已随迁移移除）。
 *
 * 等高对齐：两栏各自 h-full flex flex-col（grid 拉伸子项到行高），
 * 卡片内容 flex-1 占满中部、按钮行 mt-auto 贴底，使左右栏底部按钮行对齐。
 * 两个 section 组件都是 space-y-3 垂直排布，条件元素从上往下排，
 * 把按钮行移到最后 + space-y 不受 mt-auto 影响即可对齐底线。
 */
export function PenetrationPanel({ deviceIP, onClose }: { deviceIP: string; onClose: () => void }) {
  const { t } = useTranslation()
  const sectionTitle = 'flex items-center gap-1.5 text-xs font-semibold text-gray-700 pb-1'
  return (
    <div className="grid grid-cols-2 gap-4 items-stretch">
      <section className="min-w-0 h-full flex flex-col">
        <h4 className={sectionTitle}>
          <Globe size={14} className="text-gray-500" />
          {t('maintenance:penetration.frpTitle')}
        </h4>
        <TunnelSection deviceIP={deviceIP} onClose={onClose} />
      </section>
      <section className="min-w-0 h-full flex flex-col">
        <h4 className={sectionTitle}>
          <ShieldCheck size={14} className="text-gray-500" />
          {t('maintenance:penetration.vpnTitle')}
        </h4>
        <VpnSection deviceIP={deviceIP} />
      </section>
    </div>
  )
}
