import { useState, useEffect, useRef } from 'react'
import * as PaymentService from '../../../bindings/github.com/moyunteng/myt/paymentservicewrapper'
import { useTranslation } from 'react-i18next'
import { useModalEnter } from '../../hooks/useModalAnimation'
import { ShieldCheck, UserPlus, KeyRound, X, Phone, Lock, Shield } from 'lucide-react'

type AuthMode = 'login' | 'register' | 'forgot'

interface Props {
  open: boolean
  loading: boolean
  error: string
  onLogin: (username: string, password: string, remember: boolean) => Promise<void>
  onClose: () => void
}

export function PaymentLogin({ open, loading, error, onLogin, onClose }: Props) {
  const { t } = useTranslation()
  const rootRef = useRef<HTMLDivElement>(null)
  useModalEnter(rootRef)
  const [mode, setMode] = useState<AuthMode>('login')

  const [loginUser, setLoginUser] = useState('')
  const [loginPwd, setLoginPwd] = useState('')
  const [rememberMe, setRememberMe] = useState(true)

  const [regPhone, setRegPhone] = useState('')
  const [regPwd, setRegPwd] = useState('')
  const [regPwdConfirm, setRegPwdConfirm] = useState('')
  const [regVcode, setRegVcode] = useState('')
  const [regVkey, setRegVkey] = useState('')
  const [regSending, setRegSending] = useState(false)
  const [regCountdown, setRegCountdown] = useState(0)
  const [regLoading, setRegLoading] = useState(false)
  const [regError, setRegError] = useState('')

  const [fpPhone, setFpPhone] = useState('')
  const [fpPwd, setFpPwd] = useState('')
  const [fpPwdConfirm, setFpPwdConfirm] = useState('')
  const [fpVcode, setFpVcode] = useState('')
  const [fpVkey, setFpVkey] = useState('')
  const [fpSending, setFpSending] = useState(false)
  const [fpCountdown, setFpCountdown] = useState(0)
  const [fpLoading, setFpLoading] = useState(false)
  const [fpError, setFpError] = useState('')

  const regTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const fpTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    if (!open) return
    ;(async () => {
      const saved = await PaymentService.GetRememberedCredentials() as string
      if (saved) {
        const [u, p] = saved.split('\n')
        if (u) setLoginUser(u)
        if (p) setLoginPwd(p)
        setRememberMe(true)
      }
    })()
  }, [open])

  useEffect(() => {
    if (regCountdown <= 0) { if (regTimerRef.current) { clearInterval(regTimerRef.current); regTimerRef.current = null }; return }
    if (!regTimerRef.current) regTimerRef.current = setInterval(() => setRegCountdown(n => n - 1), 1000)
    return () => { if (regTimerRef.current) { clearInterval(regTimerRef.current); regTimerRef.current = null } }
  }, [regCountdown])

  useEffect(() => {
    if (fpCountdown <= 0) { if (fpTimerRef.current) { clearInterval(fpTimerRef.current); fpTimerRef.current = null }; return }
    if (!fpTimerRef.current) fpTimerRef.current = setInterval(() => setFpCountdown(n => n - 1), 1000)
    return () => { if (fpTimerRef.current) { clearInterval(fpTimerRef.current); fpTimerRef.current = null } }
  }, [fpCountdown])

  if (!open) return null

  const handleLogin = () => onLogin(loginUser, loginPwd, rememberMe)

  const handleSendRegVcode = async () => {
    if (!regPhone.trim() || !/^1[3-9]\d{9}$/.test(regPhone)) { setRegError(t('payment:login.invalidPhone')); return }
    setRegError(''); setRegSending(true)
    try {
      const vkey = await PaymentService.GetPhoneVCode(regPhone) as string
      setRegVkey(vkey); setRegCountdown(60)
    } catch (err: any) { setRegError(err?.message || t('payment:login.getVcodeFailed')) } finally { setRegSending(false) }
  }

  const handleRegister = async () => {
    setRegError('')
    if (!regPhone.trim() || !/^1[3-9]\d{9}$/.test(regPhone)) { setRegError(t('payment:login.invalidPhone')); return }
    if (!regPwd.trim()) { setRegError(t('payment:login.enterPassword')); return }
    if (regPwd !== regPwdConfirm) { setRegError(t('payment:login.passwordMismatch')); return }
    if (!regVcode.trim()) { setRegError(t('payment:login.enterVcode')); return }
    if (!regVkey) { setRegError(t('payment:login.getVcodeFirst')); return }
    setRegLoading(true)
    try {
      await PaymentService.Register(regPhone, regPwd, regVcode, regVkey)
      setLoginUser(regPhone); setLoginPwd(regPwd); setMode('login')
      setRegPhone(''); setRegPwd(''); setRegPwdConfirm(''); setRegVcode(''); setRegVkey(''); setRegCountdown(0)
    } catch (err: any) { setRegError(err?.message || t('payment:login.registerFailed')) } finally { setRegLoading(false) }
  }

  const handleSendFpVcode = async () => {
    if (!fpPhone.trim() || !/^1[3-9]\d{9}$/.test(fpPhone)) { setFpError(t('payment:login.invalidPhone')); return }
    setFpError(''); setFpSending(true)
    try {
      const vkey = await PaymentService.GetPhoneVCode(fpPhone) as string
      setFpVkey(vkey); setFpCountdown(60)
    } catch (err: any) { setFpError(err?.message || t('payment:login.getVcodeFailed')) } finally { setFpSending(false) }
  }

  const handleForgotPassword = async () => {
    setFpError('')
    if (!fpPhone.trim() || !/^1[3-9]\d{9}$/.test(fpPhone)) { setFpError(t('payment:login.invalidPhone')); return }
    if (!fpPwd.trim()) { setFpError(t('payment:login.enterNewPassword')); return }
    if (fpPwd !== fpPwdConfirm) { setFpError(t('payment:login.passwordMismatch')); return }
    if (!fpVcode.trim()) { setFpError(t('payment:login.enterVcode')); return }
    if (!fpVkey) { setFpError(t('payment:login.getVcodeFirst')); return }
    setFpLoading(true)
    try {
      await PaymentService.ResetPassword(fpPhone, fpPwd, fpVcode, fpVkey)
      setLoginUser(fpPhone); setMode('login')
      setFpPhone(''); setFpPwd(''); setFpPwdConfirm(''); setFpVcode(''); setFpVkey(''); setFpCountdown(0)
    } catch (err: any) { setFpError(err?.message || t('payment:login.resetFailed')) } finally { setFpLoading(false) }
  }

  const switchMode = (m: AuthMode) => { setMode(m); setRegError(''); setFpError('') }

  const inputCls = 'w-full pl-10 pr-3 py-2.5 text-sm border border-gray-200 rounded-xl bg-gray-50/60 focus:outline-none focus:border-blue-300 focus:bg-white transition-colors placeholder:text-gray-300'
  const fieldCls = 'w-full px-4 py-2.5 text-sm border border-gray-200 rounded-xl bg-gray-50/60 focus:outline-none focus:border-blue-300 focus:bg-white transition-colors placeholder:text-gray-300'
  const btnPrimary = 'w-full mt-5 py-2.5 text-sm font-medium text-white bg-gradient-to-r from-blue-500 to-blue-600 rounded-xl hover:from-blue-600 hover:to-blue-700 shadow-md shadow-blue-200/50 disabled:opacity-50 transition-all'
  const btnSecondary = 'shrink-0 px-4 py-2.5 text-xs text-blue-500 bg-blue-50/70 border border-blue-100 rounded-xl hover:bg-blue-100 disabled:opacity-50 whitespace-nowrap transition-colors'

  const Icon = mode === 'login' ? ShieldCheck : mode === 'register' ? UserPlus : KeyRound
  const title = mode === 'login' ? t('payment:login.loginAccount') : mode === 'register' ? t('payment:login.registerTitle') : t('payment:login.forgotTitle')

  return (
    <div ref={rootRef} className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-sm modal-overlay">
      <div className="relative bg-white rounded-3xl shadow-2xl w-[400px] p-8 modal-content" onClick={e => e.stopPropagation()}>
        {/* 关闭按钮 */}
        <button onClick={onClose} className="absolute top-5 right-5 w-8 h-8 flex items-center justify-center rounded-full text-gray-400 hover:text-gray-600 hover:bg-gray-100 transition-colors">
          <X size={18} />
        </button>

        {/* 头部图标 + 标题 */}
        <div className="flex flex-col items-center mb-6">
          <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-blue-500 to-blue-600 flex items-center justify-center shadow-lg shadow-blue-200/50 mb-3">
            <Icon size={26} className="text-white" />
          </div>
          <h2 className="text-base font-semibold text-gray-800">{title}</h2>
          <p className="text-xs text-gray-400 mt-1">{t('payment:login.subtitle')}</p>
        </div>

        {mode === 'login' && (
          <>
            <div className="space-y-3">
              <div className="relative">
                <Phone size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
                <input type="text" placeholder={t('payment:login.phonePlaceholder')} value={loginUser}
                  onChange={(e) => setLoginUser(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleLogin()}
                  className={inputCls} autoFocus />
              </div>
              <div className="relative">
                <Lock size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
                <input type="password" placeholder={t('payment:login.passwordPlaceholder')} value={loginPwd}
                  onChange={(e) => setLoginPwd(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && handleLogin()}
                  className={inputCls} />
              </div>
            </div>
            {error && <div className="text-xs text-red-500 mt-2.5 bg-red-50 rounded-lg px-3 py-2">{error}</div>}
            <label className="flex items-center gap-2 mt-3 cursor-pointer select-none">
              <input type="checkbox" checked={rememberMe} onChange={(e) => setRememberMe(e.target.checked)}
                className="w-4 h-4 rounded accent-blue-500" />
              <span className="text-xs text-gray-500">{t('payment:login.remember')}</span>
            </label>
            <button onClick={handleLogin} disabled={loading} className={btnPrimary}>
              {loading ? t('payment:login.loginLoading') : t('payment:login.loginBtn')}
            </button>
            <div className="flex items-center justify-between mt-4">
              <button onClick={() => switchMode('forgot')} className="text-xs text-blue-500 hover:underline">{t('payment:login.forgotPassword')}</button>
              <button onClick={() => switchMode('register')} className="text-xs text-blue-500 hover:underline">{t('payment:login.registerAccount')}</button>
            </div>
          </>
        )}

        {mode === 'register' && (
          <>
            <div className="space-y-3">
              <input type="text" placeholder={t('payment:login.phonePlaceholder')} value={regPhone}
                onChange={(e) => setRegPhone(e.target.value)} className={fieldCls} />
              <input type="password" placeholder={t('payment:login.loginPasswordPlaceholder')} value={regPwd}
                onChange={(e) => setRegPwd(e.target.value)} className={fieldCls} />
              <input type="password" placeholder={t('payment:login.confirmPasswordPlaceholder')} value={regPwdConfirm}
                onChange={(e) => setRegPwdConfirm(e.target.value)} className={fieldCls} />
              <div className="flex gap-2">
                <div className="relative flex-1">
                  <Shield size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
                  <input type="text" placeholder={t('payment:login.vcodePlaceholder')} value={regVcode}
                    onChange={(e) => setRegVcode(e.target.value)}
                    className={inputCls} />
                </div>
                <button onClick={handleSendRegVcode} disabled={regSending || regCountdown > 0} className={btnSecondary}>
                  {regCountdown > 0 ? `${regCountdown}s` : regSending ? t('payment:login.sending') : t('payment:login.getVcode')}
                </button>
              </div>
            </div>
            {regError && <div className="text-xs text-red-500 mt-2.5 bg-red-50 rounded-lg px-3 py-2">{regError}</div>}
            <button onClick={handleRegister} disabled={regLoading} className={btnPrimary}>
              {regLoading ? t('payment:login.registerLoading') : t('payment:login.registerBtn')}
            </button>
            <div className="text-center mt-4">
              <button onClick={() => switchMode('login')} className="text-xs text-blue-500 hover:underline">{t('payment:login.hasAccountLogin')}</button>
            </div>
          </>
        )}

        {mode === 'forgot' && (
          <>
            <div className="space-y-3">
              <input type="text" placeholder={t('payment:login.phonePlaceholder')} value={fpPhone}
                onChange={(e) => setFpPhone(e.target.value)} className={fieldCls} />
              <input type="password" placeholder={t('payment:login.newPasswordPlaceholder')} value={fpPwd}
                onChange={(e) => setFpPwd(e.target.value)} className={fieldCls} />
              <input type="password" placeholder={t('payment:login.confirmNewPasswordPlaceholder')} value={fpPwdConfirm}
                onChange={(e) => setFpPwdConfirm(e.target.value)} className={fieldCls} />
              <div className="flex gap-2">
                <div className="relative flex-1">
                  <Shield size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
                  <input type="text" placeholder={t('payment:login.vcodePlaceholder')} value={fpVcode}
                    onChange={(e) => setFpVcode(e.target.value)}
                    className={inputCls} />
                </div>
                <button onClick={handleSendFpVcode} disabled={fpSending || fpCountdown > 0} className={btnSecondary}>
                  {fpCountdown > 0 ? `${fpCountdown}s` : fpSending ? t('payment:login.sending') : t('payment:login.getVcode')}
                </button>
              </div>
            </div>
            {fpError && <div className="text-xs text-red-500 mt-2.5 bg-red-50 rounded-lg px-3 py-2">{fpError}</div>}
            <button onClick={handleForgotPassword} disabled={fpLoading} className={btnPrimary}>
              {fpLoading ? t('payment:login.resetLoading') : t('payment:login.resetBtn')}
            </button>
            <div className="text-center mt-4">
              <button onClick={() => switchMode('login')} className="text-xs text-blue-500 hover:underline">{t('payment:login.backToLogin')}</button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
