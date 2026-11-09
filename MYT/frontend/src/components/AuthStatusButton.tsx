import { useState, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { UserRound, LogOut, RefreshCw, UserX } from 'lucide-react'
import { useAuth } from '../hooks/useAuth'
import { useModalEnter, animateModalOut } from '../hooks/useModalAnimation'
import { useDeployMode } from '../hooks/useDeployMode'
import * as AuthService from '../../bindings/github.com/moyunteng/myt/authservicewrapper'
import { CancelAccountDialog } from './CancelAccountDialog'
import { showToast } from './Toast'

/**
 * 导航栏登录状态按钮：放在"设置"按钮上方。
 * - 未登录：灰色小人 + "未登录"，点击弹出登录弹窗
 * - 已登录：蓝色小人头像，点击弹出用户信息弹窗（授权同步 / 退出登录）
 *
 * 授权同步推的是**当前登录账号**的 token（Go 侧 paymentSvc.GetToken），
 * 与后台每 60 秒的自动同步（包内硬编码账号）区分开：用户点这里 = 把我的授权同步给设备。
 */
export function AuthStatusButton({ horizontal = false }: { horizontal?: boolean }) {
  const { t } = useTranslation()
  const { loggedIn, username, logout, openLoginPrompt } = useAuth()
  const { refresh: refreshDeployMode } = useDeployMode()
  const [userMenuOpen, setUserMenuOpen] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [cancelDialogOpen, setCancelDialogOpen] = useState(false)
  const userMenuRef = useRef<HTMLDivElement>(null)
  // 退场动画播放期间遮罩仍在，连点会重放动画并二次触发 onClosed——用 ref 标记防重入
  const closingRef = useRef(false)
  useModalEnter(userMenuRef)

  const openMenu = useCallback(() => {
    closingRef.current = false
    setUserMenuOpen(true)
  }, [])

  // 关闭必须走 gsap 退场（animateModalOut 播完才卸载），否则弹窗是"啪"地消失
  const closeMenu = useCallback(() => {
    if (closingRef.current) return
    closingRef.current = true
    animateModalOut(userMenuRef.current, () => setUserMenuOpen(false))
  }, [])

  const handleSyncAuth = useCallback(async () => {
    if (syncing) return
    setSyncing(true)
    try {
      const count = await AuthService.SyncNow()
      // UDP 无应答：count 只代表已发出，文案不承诺"设备已生效"
      if (count > 0) showToast(t('authSync.sent', { count }), 'success')
      else showToast(t('authSync.noDevice'), 'warning')
    } catch (err) {
      showToast(String(err), 'error')
    } finally {
      setSyncing(false)
    }
  }, [syncing, t])

  const handleLogout = async () => {
    await logout()
    setUserMenuOpen(false)
    // 后端已自动切回本地模式，同步前端部署模式状态
    refreshDeployMode()
  }

  if (!loggedIn) {
    if (horizontal) {
      return (
        <button
          onClick={() => openLoginPrompt()}
          className="h-6 px-2 flex items-center text-[11px] rounded transition-colors cursor-pointer gap-1 text-gray-400 hover:text-gray-600 hover:bg-gray-200"
          title={t('payment:login.loginBtn')}
        >
          <UserRound size={13} />
          <span>{t('nav.notLoggedIn')}</span>
        </button>
      )
    }
    return (
      <>
        <button
          onClick={() => openLoginPrompt()}
          className="w-[60px] py-2.5 rounded-lg text-xs flex flex-col items-center gap-0.5 mt-1 cursor-pointer transition-colors duration-150 text-gray-400 hover:bg-gray-100 hover:text-gray-600"
          title={t('payment:login.loginBtn')}
        >
          <UserRound size={18} />
          <span>{t('nav.notLoggedIn')}</span>
        </button>
      </>
    )
  }

  // 按钮形态二选一（顶栏横向 / 竖导航竖排），弹窗共享一份
  const button = horizontal ? (
    <button
      onClick={openMenu}
      className="h-6 px-2 flex items-center text-[11px] rounded transition-colors cursor-pointer gap-1 text-blue-500 hover:bg-blue-50"
      title={username}
    >
      <UserRound size={13} />
      <span className="max-w-[80px] truncate">{t('nav.loggedIn')}</span>
    </button>
  ) : (
    <button
      onClick={openMenu}
      className="w-[60px] py-2.5 rounded-lg text-xs flex flex-col items-center gap-0.5 mt-1 cursor-pointer transition-colors duration-150 hover:bg-blue-50"
      title={username}
    >
      <UserRound size={18} className="text-blue-500" />
      <span className="text-blue-500">{t('nav.loggedIn')}</span>
    </button>
  )

  return (
    <>
      {button}
      {userMenuOpen && (
        <div ref={userMenuRef} className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm modal-overlay"
          onClick={closeMenu}>
          <div className="bg-white rounded-3xl shadow-2xl w-[340px] p-6 modal-content" onClick={e => e.stopPropagation()}>
            <div className="flex flex-col items-center mb-5">
              <div className="w-14 h-14 rounded-full bg-gradient-to-br from-blue-400 to-blue-600 flex items-center justify-center shadow-lg shadow-blue-200/50 mb-3">
                <UserRound size={26} className="text-white" />
              </div>
              <div className="text-base font-semibold text-gray-800">{username || '—'}</div>
              <div className="text-xs text-green-500 mt-1 flex items-center gap-1">
                <span className="w-1.5 h-1.5 rounded-full bg-green-500" /> {t('nav.loggedIn')}
              </div>
            </div>
            <div className="bg-gray-50 rounded-xl px-4 py-3 mb-4">
              <div className="flex items-center justify-between text-xs">
                <span className="text-gray-400">{t('payment:login.phone')}</span>
                <span className="text-gray-700 font-medium">{username || '—'}</span>
              </div>
            </div>
            <button
              onClick={handleSyncAuth}
              disabled={syncing}
              title={t('authSync.hint')}
              className="w-full flex items-center justify-center gap-2 py-2.5 text-sm text-blue-600 bg-blue-50 rounded-xl hover:bg-blue-100 transition-colors disabled:opacity-50 mb-2"
            >
              <RefreshCw size={16} className={syncing ? 'animate-spin' : ''} />
              {syncing ? t('authSync.syncing') : t('authSync.button')}
            </button>
            <button
              onClick={handleLogout}
              className="w-full flex items-center justify-center gap-2 py-2.5 text-sm text-red-500 bg-red-50 rounded-xl hover:bg-red-100 transition-colors"
            >
              <LogOut size={16} /> {t('button.logout')}
            </button>
            <button
              onClick={() => setCancelDialogOpen(true)}
              title={t('cancelAccount.hint')}
              className="w-full flex items-center justify-center gap-2 py-2.5 text-xs text-red-400 hover:text-red-600 transition-colors"
            >
              <UserX size={14} /> {t('cancelAccount.menuItem')}
            </button>
          </div>
        </div>
      )}
      {cancelDialogOpen && (
        <CancelAccountDialog
          username={username}
          onClose={() => setCancelDialogOpen(false)}
          onSuccess={() => {
            // 后端已切回本地模式，同步前端部署模式状态
            refreshDeployMode()
            setUserMenuOpen(false)
          }}
        />
      )}
    </>
  )
}
