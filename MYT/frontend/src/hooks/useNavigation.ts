import { createContext, useContext } from 'react'

type TabId = 'host' | 'rom' | 'mirror' | 'backup' | 'payment' | 'vpc' | 'model' | 'maintenance' | 'agent' | 'diskless' | 'stream' | 'settings'

/**
 * 导航上下文值：本身可调用（navigate 切 Tab，历史用法保持兼容），
 * 同时挂载 activeTab 属性——keep-alive 失活页面的副作用据此跳过
 * （如截图勾选集合的写入，2026-10-09 跨页面污染修复）。
 */
export type NavigateFn = ((tab: TabId) => void) & { activeTab: TabId }

const noop: NavigateFn = Object.assign(() => {}, { activeTab: 'host' as TabId })

const NavigationContext = createContext<NavigateFn>(noop)

export const NavigationProvider = NavigationContext.Provider

export function useNavigation() {
  return useContext(NavigationContext)
}

/** 当前激活 Tab（keep-alive 下失活页面的副作用据此跳过，如截图勾选集合的写入）。 */
export function useActiveTab(): TabId {
  return useContext(NavigationContext).activeTab
}
