import { createContext, useContext, useState, useEffect, useCallback } from 'react'
import * as PaymentService from '../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import { Events } from '@wailsio/runtime'

interface AuthState {
  loggedIn: boolean
  username: string
  login: (user: string, pwd: string, remember: boolean) => Promise<void>
  logout: () => Promise<void>
  loginPromptOpen: boolean
  openLoginPrompt: () => void
  closeLoginPrompt: () => void
}

const AuthContext = createContext<AuthState>({
  loggedIn: false,
  username: '',
  login: async () => {},
  logout: async () => {},
  loginPromptOpen: false,
  openLoginPrompt: () => {},
  closeLoginPrompt: () => {},
})

export const AuthProvider = AuthContext.Provider

export function useAuth() {
  return useContext(AuthContext)
}

/**
 * 判断错误是否表示"需要登录"（后端未登录时返回"请先登录魔云腾账号"）。
 * 调用方捕获错误后可用此函数决定是否弹登录弹窗。
 */
export function isLoginRequiredError(err: unknown): boolean {
  const msg = typeof err === 'string' ? err : err instanceof Error ? err.message : String(err ?? '')
  return msg.includes('请先登录') || msg.includes('未登录') || msg.includes('登录已过期') || msg.includes('请先登录魔云腾账号')
}

export function useAuthState(): AuthState {
  const [loggedIn, setLoggedIn] = useState(false)
  const [username, setUsername] = useState('')
  const [loginPromptOpen, setLoginPromptOpen] = useState(false)

  useEffect(() => {
    ;(async () => {
      const isLogged = await PaymentService.IsLoggedIn() as boolean
      setLoggedIn(isLogged)
      if (isLogged) {
        const name = await PaymentService.GetUsername() as string
        setUsername(name)
      }
    })()

    const off = Events.On('auth:status', (ev) => {
      const isLogged = !!ev.data
      setLoggedIn(isLogged)
      if (isLogged) {
        PaymentService.GetUsername().then(name => setUsername(name as string || '')).catch(() => {})
      } else {
        setUsername('')
      }
    })
    return () => { off() }
  }, [])

  const login = useCallback(async (user: string, pwd: string, remember: boolean) => {
    await PaymentService.Login(user, pwd)
    if (remember) {
      await PaymentService.SetRememberedCredentials(user, pwd)
    } else {
      await PaymentService.ClearRememberedCredentials()
    }
    setLoggedIn(true)
    setUsername(user)
  }, [])

  const logout = useCallback(async () => {
    await PaymentService.Logout()
    setLoggedIn(false)
    setUsername('')
  }, [])

  const openLoginPrompt = useCallback(() => setLoginPromptOpen(true), [])
  const closeLoginPrompt = useCallback(() => setLoginPromptOpen(false), [])

  return { loggedIn, username, login, logout, loginPromptOpen, openLoginPrompt, closeLoginPrompt }
}
