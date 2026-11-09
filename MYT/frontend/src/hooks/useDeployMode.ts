import { createContext, useContext, useState, useEffect, useCallback } from 'react'
import * as AppService from '../../bindings/github.com/moyunteng/myt/appservice'

export type DeployMode = 'local' | 'public' | 'v2' | 'v4'

interface DeployModeState {
  mode: DeployMode
  /** 切模式后到收到新模式首个 devicelist:updated 之前为 true（列表区域显示加载中，防残留列表闪现） */
  modeLoading: boolean
  refresh: () => void
  setModeLoading: (v: boolean) => void
}

const DeployModeContext = createContext<DeployModeState>({
  mode: 'local',
  modeLoading: false,
  refresh: () => {},
  setModeLoading: () => {},
})

export const DeployModeProvider = DeployModeContext.Provider

/** 读取部署模式（local 本地版 / public 公网版 / v2 云机主机直连 / v4 本地 V4） */
export function useDeployMode(): DeployModeState {
  return useContext(DeployModeContext)
}

/** 部署模式状态：启动时从配置读取，切换模式后由 switchDeployMode 调 refresh 刷新 */
export function useDeployModeState(): DeployModeState {
  const [mode, setMode] = useState<DeployMode>('local')
  const [modeLoading, setModeLoading] = useState(false)

  useEffect(() => {
    AppService.GetDeployMode()
      .then((m: string) => setMode(normalizeMode(m)))
      .catch(() => {})
  }, [])

  const refresh = useCallback(() => {
    AppService.GetDeployMode()
      .then((m: string) => setMode(normalizeMode(m)))
      .catch(() => {})
  }, [])

  return { mode, modeLoading, refresh, setModeLoading }
}

/** 后端模式字符串收敛为合法枚举（未知值兜底 local） */
function normalizeMode(m: string): DeployMode {
  if (m === 'public') return 'public'
  if (m === 'v2') return 'v2'
  if (m === 'v4') return 'v4'
  return 'local'
}
