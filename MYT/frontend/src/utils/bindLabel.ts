import type { DeviceState } from '../types/index'

/** 绑定状态徽章的显示描述（文案 + 样式 + 点击语义）。 */
export interface BindBadge {
  text: string
  /** Tailwind 样式片段（颜色 + hover），与历史设备行徽章同源。 */
  cls: string
  clickable: boolean
  /** 未登录：点击跳「授权」页登录。 */
  isLogin: boolean
  /** 已绑定：点击打开解绑弹窗（短信验证码）。 */
  isUnbind: boolean
}

/**
 * 绑定状态 → 徽章描述。
 *
 * 数据源 device.bind_status（0=未绑定 / 1=已绑定当前账号 / 2=被其他账号绑定，
 * 见 types/index.ts），未登录时一律显示「未登录」（此时状态未知也无意义）。
 *
 * 历史位置：原实现为 DeviceListItem 内的私有函数，2026-09-27 设备行单行化时随
 * 第二行一起删除；提升为共享工具后一度挂在主机 Tab 的截图分组组头，现落位
 * 主机 Tab 左栏设备卡片（DeviceItem）第二行右端。
 */
export function bindLabel(
  status: number | undefined,
  loggedIn: boolean,
  t: (key: string) => string,
): BindBadge {
  if (!loggedIn) {
    return {
      text: t('host:deviceList.notLoggedIn'),
      cls: 'text-orange-500 bg-orange-50 cursor-pointer hover:bg-orange-100',
      clickable: true,
      isLogin: true,
      isUnbind: false,
    }
  }
  switch (status) {
    case 1:
      return {
        text: t('host:deviceList.bound'),
        cls: 'text-green-600 bg-green-50 cursor-pointer hover:bg-green-100',
        clickable: true,
        isLogin: false,
        isUnbind: true,
      }
    case 2:
      return {
        text: t('host:deviceList.boundByOther'),
        cls: 'text-amber-600 bg-amber-50',
        clickable: false,
        isLogin: false,
        isUnbind: false,
      }
    default:
      return {
        text: t('host:deviceList.notBound'),
        cls: 'text-blue-500 bg-blue-50 cursor-pointer hover:bg-blue-100',
        clickable: true,
        isLogin: false,
        isUnbind: false,
      }
  }
}

/**
 * 徽章点击的三个动作 + 登录态。由宿主页面（MaintenancePage）实现并透传到设备
 * 卡片，弹窗（BindDialog / UnbindDialog）挂在宿主层，避免每张卡片各挂一份。
 */
export interface BindActions {
  loggedIn: boolean
  /** 未登录时跳「授权」页。 */
  onNavigateToPayment: () => void
  /** 未绑定 → 打开绑定弹窗。 */
  onBind: (device: DeviceState) => void
  /** 已绑定 → 打开解绑弹窗。 */
  onUnbind: (device: DeviceState) => void
}
