import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Window, Application, Browser, Events } from '@wailsio/runtime'
import { gsap } from 'gsap'
import { Monitor, Bot, RadioTower, Disc, CreditCard, Globe, Smartphone, Server, HardDrive, Archive, Settings, type LucideIcon } from 'lucide-react'
import * as AppService from '../bindings/github.com/moyunteng/myt/appservice'
import HostManagement from './pages/host/HostManagement'
import MirrorManagement from './pages/mirror/MirrorManagement'
import V2MirrorManagement from './pages/mirror/V2MirrorManagement'
import PaymentPage from './pages/payment/PaymentPage'
import { PaymentLogin } from './pages/payment/PaymentLogin'
import VpcManagement from './pages/vpc/VpcManagement'
import PhoneModelManagement from './pages/model/PhoneModelManagement'
import SettingsPage from './pages/settings/SettingsPage'
import { AuthStatusButton } from './components/AuthStatusButton'
import MaintenancePage from './pages/maintenance/MaintenancePage'
import AgentPage from './pages/agent/AgentPage'
import DisklessPage from './pages/diskless/DisklessPage'
import StreamManagement from './pages/stream/StreamManagement'
import BackupManagement from './pages/backup/BackupManagement'
import RomPage from './pages/rom/RomPage'

import Toast from './components/Toast'
import AuthDialog from './components/AuthDialog'
import TaskQueueButton from './components/taskQueue/TaskQueueButton'
import { TaskQueueProvider } from './hooks/taskQueue/useTaskQueue'
import GlobalTaskModal from './components/taskQueue/GlobalTaskModal'
import './hooks/taskQueue/registerCancels' // 注册任务取消处理器（副作用 import，模块加载时注册）
import { NavigationProvider } from './hooks/useNavigation'
import { AuthProvider, useAuthState } from './hooks/useAuth'
import { useUpdateCheck, UpdateProvider } from './hooks/useUpdateCheck'
import { DeployModeProvider, useDeployMode, useDeployModeState, type DeployMode } from './hooks/useDeployMode'
import { UpdateModals } from './pages/settings/AboutSection'
import { setAuthDialogRenderer } from './utils/authRetry'

type TabId = 'host' | 'rom' | 'backup' | 'mirror' | 'payment' | 'vpc' | 'model' | 'maintenance' | 'agent' | 'diskless' | 'stream' | 'settings'

const tabs: { id: TabId; label: string; icon: LucideIcon; beta?: boolean }[] = [
  // maintenance = 主机（设备维护：截图/坑位模式）；host = 云机（实例墙）。
  // 默认 Tab = maintenance（见 AppContent 的 useState）——因此 maintenance 必须排在 host 前面。
  { id: 'maintenance', label: 'nav.maintenance', icon: Server },
  // host = 云机：各部署模式共用同一套页面（设备侧栏 + 工具条 + 卡片/列表视图），
  // 数据源由 useHostManagement / useHostEvents 按模式切换（V4 走 v4ws + 8088 REST）
  { id: 'host', label: 'nav.host', icon: Monitor },
  // rom = 镜像管理（V4 专用，见 tabVisible）：tobox_player 8088 的 /image 分组
  { id: 'rom', label: 'nav.rom', icon: Disc },
  { id: 'backup', label: 'nav.backup', icon: Archive },
  { id: 'agent', label: 'nav.agent', icon: Bot },
  { id: 'stream', label: 'nav.stream', icon: RadioTower },
  { id: 'mirror', label: 'nav.mirror', icon: Disc },
  { id: 'payment', label: 'nav.payment', icon: CreditCard },
  { id: 'vpc', label: 'nav.vpc', icon: Globe },
  { id: 'model', label: 'nav.model', icon: Smartphone },
  { id: 'diskless', label: 'nav.diskless', icon: HardDrive },
]

/**
 * Tab 可见性（唯一来源，导航渲染与模式回落 effect 共用）：
 * - v4（tebox）：只显示云机 + ROM 两个 tab（ROM 为 V4 专用）；
 * - v2：只保留云机/主机维护/镜像/授权（与改造前一致）
 * - diskless：公网模式隐藏
 */
function tabVisible(id: TabId, mode: DeployMode): boolean {
  if (mode === 'v4') return id === 'host' || id === 'rom'
  if (id === 'rom') return false
  if (mode === 'v2') return id === 'host' || id === 'maintenance' || id === 'mirror' || id === 'payment'
  if (id === 'diskless' && mode === 'public') return false
  return true
}

function AppContent() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<TabId>('maintenance')
  // 懒挂载：Tab 首次被切到时才挂载页面组件（未访问过的 Tab 完全不渲染 → 其轮询、
  // 事件订阅等副作用一律不启动）；已挂载的用 display 隐藏保留（组件状态、滚动位置、
  // WS 订阅都不丢，与改造前一致）。
  const [mountedTabs, setMountedTabs] = useState<Set<TabId>>(() => new Set<TabId>(['maintenance']))
  if (!mountedTabs.has(activeTab)) {
    // render 阶段同步加入（React 官方的「渲染期修正 state」写法，会立即重渲染）：若改成
    // useEffect，本次 render 目标 Tab 仍为 null，紧随其后的 gsap 入场动画 effect 取不到
    // [data-tab] 节点，首次切入该 Tab 的动画会静默丢失。
    setMountedTabs(new Set(mountedTabs).add(activeTab))
  }
  const [version, setVersion] = useState('')
  const pagesRef = useRef<HTMLDivElement>(null)
  const { mode: deployMode, refresh: refreshDeployMode, modeLoading, setModeLoading } = useDeployMode()
  const auth = useAuthState()
  const { loggedIn: authLoggedIn, login: authLogin, loginPromptOpen: authLoginPromptOpen, closeLoginPrompt: authCloseLoginPrompt } = auth
  const [loginLoading, setLoginLoading] = useState(false)
  const [loginError, setLoginError] = useState('')
  const updateState = useUpdateCheck()

  // 页面切换入场动画：切到目标页时，页面内容淡入 + 轻微上移
  useEffect(() => {
    const root = pagesRef.current
    if (!root) return
    const page = root.querySelector<HTMLElement>(`[data-tab="${activeTab}"]`)
    if (!page) return
    gsap.fromTo(page, { opacity: 0, y: 6 }, { opacity: 1, y: 0, duration: 0.22, ease: 'power2.out', clearProps: 'transform' })
  }, [activeTab])
  const hasNewVersion = updateState.updateResult?.hasUpdate === true

  const [authDialogs, setAuthDialogs] = useState<{ ip: string; resolve: (pwd: string) => void }[]>([])
  const [switching, setSwitching] = useState<DeployMode | null>(null)

  useEffect(() => {
    AppService.GetVersion().then((v: string) => setVersion(v)).catch(() => {})
  }, [])

  // 模式切换后若停在被隐藏的 tab，回落到该模式下第一个可见 tab
  // （tabVisible 与导航渲染同源；v4 下 maintenance 不可见，回落到 host）。
  useEffect(() => {
    if (!tabVisible(activeTab, deployMode)) {
      const firstVisible = tabs.find(tab => tabVisible(tab.id, deployMode))
      if (firstVisible) setActiveTab(firstVisible.id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deployMode])

  // 启动后未登录：自动弹出登录弹窗（可关闭，不强制）
  useEffect(() => {
    if (authLoggedIn === false) {
      const timer = setTimeout(() => auth.openLoginPrompt(), 800)
      return () => clearTimeout(timer)
    }
  }, [authLoggedIn])

  const handleGlobalLogin = async (u: string, p: string, r: boolean) => {
    setLoginLoading(true); setLoginError('')
    try {
      await authLogin(u, p, r)
      authCloseLoginPrompt()
      setLoginError('')
    } catch (err: any) {
      setLoginError(err?.message || String(err))
    } finally {
      setLoginLoading(false)
    }
  }

  const switchDeployMode = async (mode: DeployMode) => {
    if (mode === deployMode || switching) return
    // 公网模式必须登录：未登录时弹登录弹窗并拦截切换
    if (mode === 'public' && !authLoggedIn) {
      auth.openLoginPrompt()
      return
    }
    setSwitching(mode)
    const finish = () => {
      setSwitching(null)
      // 刷新部署模式 state（不再 reload 页面，需手动同步 context）
      refreshDeployMode()
    }
    // 切模式期间设备列表区域显示加载中（替代上一个模式的残留列表）：
    // 收到新模式首个**非空** devicelist:updated 即解除；15 秒兜底自动解除。
    // 不接受空列表解除：切换瞬间的残余状态上报可能 emit 空/旧列表（把已渲染
    // 数据清空并提前取消加载态，用户看到「加载很久还是空的」）——空列表语义
    // 交给 15s 兜底（缓存确实为空时多等几秒显示空态，可接受）。
    // v4 的发现已接入（UDP 7678 广播 + 8088 /health 验证，见 discover_service_core.go
    // 的 v4 分支）：空列表 = 本轮扫描没发现设备，对 v4 来说同样是「确定的结果」而不是
    // 「还没加载完」，所以 v4 接受空列表解除遮罩（否则每次切到 v4 都要等满 15s 兜底）。
    setModeLoading(true)
    const emptyListOk = mode === 'v4'
    let loadingDone = false
    let offLoading = () => {}
    let loadingTimer: ReturnType<typeof setTimeout> | undefined
    const finishLoading = () => {
      if (loadingDone) return
      loadingDone = true
      offLoading()
      if (loadingTimer !== undefined) clearTimeout(loadingTimer)
      setModeLoading(false)
    }
    offLoading = Events.On('devicelist:updated', (ev: any) => {
      const list = ev?.data ?? ev
      if (!Array.isArray(list)) return
      if (list.length === 0 && !emptyListOk) return
      finishLoading()
    })
    loadingTimer = setTimeout(finishLoading, 15000)
    try {
      // 后端已非阻塞化：SetDeployMode 只做内存切换 + 清缓存 + 事件通知，
      // 登录/首轮发现全在后台 goroutine——这里快速返回，不阻塞整体操作。
      await AppService.SetDeployMode(mode)
      finish()
    } catch (e) {
      console.error(t('deployMode.switchFailed'), e)
      finishLoading()
      finish()
    }
  }

  useEffect(() => {
    setAuthDialogRenderer((dialogs) => {
      setAuthDialogs(prev => [...prev, ...dialogs])
    })
  }, [])

  // WS 事件推送日志 → 浏览器 console（调试观察用：设备连接状态与事件流）。
  // 节流：千台规模事件流可达几十条/s，console 渲染（尤其 DevTools 开启时）
  // 会吃满渲染进程——每秒最多 3 条，超出的静默丢弃（调试观察够用）。
  // V3（eventws:log）与 V4（v4ws:log）两条通道共用同一节流器与输出格式。
  useEffect(() => {
    let lastLogAt = 0
    let dropped = 0
    const log = (ev: any) => {
      const now = performance.now()
      if (now - lastLogAt < 333) {
        dropped++
        return
      }
      lastLogAt = now
      const payload = ev?.data ?? ev
      console.log(`[ws] ${payload?.deviceId ?? ''} ${payload?.line ?? ''}${dropped > 0 ? ` (已略过${dropped}条)` : ''}`)
      dropped = 0
    }
    const off = Events.On('eventws:log', log)
    const offV4 = Events.On('v4ws:log', log)
    return () => { off(); offV4() }
  }, [])

  const show = (tab: TabId) => activeTab === tab ? '' : 'none'
  // 懒挂载的 Tab 容器：未访问过的 Tab 不渲染；已挂载的靠 display 切换（keep-alive）。
  // 注意 children 元素在此处仍会被创建，但 React 元素只是普通对象——返回 null 时不会挂载、
  // 不产生任何副作用。
  const tabPane = (id: TabId, children: React.ReactNode) => (
    mountedTabs.has(id) ? (
      <div data-tab={id} style={{ display: show(id) }} className="absolute inset-0 overflow-auto">{children}</div>
    ) : null
  )

  return (
    <NavigationProvider value={Object.assign(setActiveTab, { activeTab })}>
    <AuthProvider value={auth}>
    <UpdateProvider value={updateState}>
    <div className="flex flex-col h-screen overflow-hidden bg-white text-gray-800">
      {switching && (
        <div className="fixed inset-0 z-[100] bg-white/80 backdrop-blur-sm flex flex-col items-center justify-center gap-3">
          <div className="w-10 h-10 border-4 border-blue-100 border-t-blue-500 rounded-full animate-spin"></div>
          <div className="text-sm text-gray-500">
            {t('deployMode.switching', { mode: t(`deployMode.${switching}`) })}
          </div>
        </div>
      )}
      {/* 顶栏：横贯全宽（含左侧竖导航上方）。左 Logo/标题/版本，中空白拖拽区，右工具区 */}
      <header
        className="relative z-[70] h-8 shrink-0 flex items-center justify-between pl-4 pr-3 bg-gray-50 border-b border-gray-200 select-none"
        style={{ ['--wails-draggable' as string]: 'drag' } as React.CSSProperties}
        onMouseDown={(e) => { if (e.button === 0) AppService.StartWindowDrag() }}
      >
        <div className="flex items-center gap-2">
          <span className="text-[11px] text-gray-400 font-medium">{t('window.title')}</span>
          {version && <span className="text-[10px] text-gray-300 font-mono">{version}</span>}
        </div>
        <div className="flex items-center gap-0.5 ml-2" style={{ ['--wails-draggable' as string]: 'no-drag' } as React.CSSProperties}>
          <button
            onClick={() => switchDeployMode('local')}
            className={`h-5 px-2 text-[10px] rounded transition-colors ${deployMode === 'local' ? 'bg-blue-500 text-white' : 'text-gray-400 hover:bg-gray-200'}`}
            title={t('deployMode.localTooltip')}
          >{t('deployMode.local')}</button>
          <button
            onClick={() => switchDeployMode('public')}
            className={`h-5 px-2 text-[10px] rounded transition-colors ${deployMode === 'public' ? 'bg-blue-500 text-white' : 'text-gray-400 hover:bg-gray-200'}`}
            title={t('deployMode.publicTooltip')}
          >{t('deployMode.public')}</button>
          <button
            onClick={() => switchDeployMode('v2')}
            className={`h-5 px-2 text-[10px] rounded transition-colors ${deployMode === 'v2' ? 'bg-blue-500 text-white' : 'text-gray-400 hover:bg-gray-200'}`}
            title={t('deployMode.v2Tooltip')}
          >{t('deployMode.v2')}</button>
          <button
            onClick={() => switchDeployMode('v4')}
            className={`h-5 px-2 text-[10px] rounded transition-colors ${deployMode === 'v4' ? 'bg-blue-500 text-white' : 'text-gray-400 hover:bg-gray-200'}`}
            title={t('deployMode.v4Tooltip')}
          >{t('deployMode.v4')}</button>
        </div>
        <div className="flex items-center" style={{ ['--wails-draggable' as string]: 'no-drag' } as React.CSSProperties}>
          <TaskQueueButton />
          <button
            onClick={() => Browser.OpenURL('https://doc.opencecs.com/')}
            className="h-6 px-2 flex items-center text-[11px] text-gray-500 hover:text-blue-500 hover:bg-gray-200 rounded transition-colors cursor-pointer gap-1"
            title={t('window.feedback')}
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="10"/>
              <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/>
              <line x1="12" y1="17" x2="12.01" y2="17"/>
            </svg>
            <span>{t('window.feedback')}</span>
          </button>
          <button
            onClick={() => Window.Minimise()}
            className="w-8 h-6 flex items-center justify-center text-gray-400 hover:text-gray-600 hover:bg-gray-200 rounded transition-colors cursor-pointer"
            title={t('window.minimize')}
            aria-label={t('window.minimize')}
          >
            <svg width="10" height="1" viewBox="0 0 10 1"><rect width="10" height="1" fill="currentColor"/></svg>
          </button>
          <button
            onClick={() => Window.ToggleMaximise()}
            className="w-8 h-6 flex items-center justify-center text-gray-400 hover:text-gray-600 hover:bg-gray-200 rounded transition-colors cursor-pointer"
            title={t('window.maximize')}
            aria-label={t('window.maximize')}
          >
            <svg width="9" height="9" viewBox="0 0 9 9" fill="none" stroke="currentColor" strokeWidth="1.2"><rect x="0.6" y="0.6" width="7.8" height="7.8" rx="1"/></svg>
          </button>
          <button
            onClick={() => { try { Window.Close() } finally { Application.Quit() } }}
            className="w-8 h-6 flex items-center justify-center text-gray-400 hover:text-white hover:bg-red-500 rounded transition-colors cursor-pointer"
            title={t('window.close')}
            aria-label={t('window.close')}
          >
            <svg width="8" height="8" viewBox="0 0 8 8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"><line x1="1" y1="1" x2="7" y2="7"/><line x1="7" y1="1" x2="1" y2="7"/></svg>
          </button>
        </div>
      </header>
      <div className="flex-1 min-h-0 flex overflow-hidden">
        {/* 左侧竖导航（原样）：竖排 tabs + 底部登录状态/设置 */}
        <nav className="w-[76px] bg-gray-50 border-r border-gray-200 flex flex-col items-center shrink-0 pt-2">
          {/* v2 模式开放：云机/主机维护/镜像/授权/设置——授权走云端 API + UDP 7678
              （v2 主机同为 MYT 设备，坑位授权与授权同步均适用）；public 隐藏无盘 */}
          {tabs.filter(tab => tabVisible(tab.id, deployMode)).map(tab => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              aria-label={t(tab.label)}
              className={`w-[60px] py-2.5 rounded-lg text-xs flex flex-col items-center gap-0.5 mt-1 cursor-pointer transition-colors duration-150 ${
                activeTab === tab.id
                  ? 'bg-blue-500 text-white'
                  : 'text-gray-500 hover:bg-gray-100 hover:text-gray-700'
              }`}
            >
              <tab.icon size={18} />
              <span>{t(tab.label)}</span>
              {tab.beta && (
                <span className="text-[8px] leading-none text-amber-500">{t('beta')}</span>
              )}
            </button>
          ))}
          <div className="flex-1" />
          <AuthStatusButton />
          <button
            onClick={() => setActiveTab('settings')}
            aria-label={t('nav.settings')}
            className={`relative w-[60px] py-2.5 rounded-lg text-xs flex flex-col items-center gap-0.5 mb-3 cursor-pointer transition-colors duration-150 ${
              activeTab === 'settings'
                ? 'bg-blue-500 text-white'
                : 'text-gray-500 hover:bg-gray-100 hover:text-gray-700'
            }`}
          >
            <Settings size={18} />
            <span>{t('nav.settings')}</span>
            {hasNewVersion && (
              <span className="absolute top-1.5 right-2 w-2 h-2 bg-red-500 rounded-full animate-pulse" />
            )}
          </button>
        </nav>
        {/* 内容区：占顶栏以下、竖导航右侧 */}
        <div className="flex-1 min-w-0 overflow-hidden">
          <div ref={pagesRef} className="w-full h-full overflow-hidden relative">
            {tabPane('host', <HostManagement />)}
            {tabPane('rom', <RomPage />)}
            {tabPane('backup', <BackupManagement />)}
            {tabPane('mirror', deployMode === 'v2' ? <V2MirrorManagement /> : <MirrorManagement />)}
            {tabPane('payment', <PaymentPage />)}
            {tabPane('vpc', <VpcManagement />)}
            {tabPane('model', <PhoneModelManagement />)}
            {tabPane('maintenance', <MaintenancePage />)}
            {tabPane('agent', <AgentPage />)}
            {tabPane('diskless', <DisklessPage />)}
            {tabPane('stream', <StreamManagement />)}
            {tabPane('settings', <SettingsPage />)}
          </div>
        </div>
      </div>
      <Toast />
      <UpdateModals />
      <PaymentLogin open={authLoginPromptOpen} loading={loginLoading} error={loginError} onLogin={handleGlobalLogin} onClose={authCloseLoginPrompt} />
      {authDialogs.map((d, i) => (
        <AuthDialog key={`${d.ip}-${i}`} deviceIP={d.ip}
          onConfirm={(pwd: string) => { d.resolve(pwd); setAuthDialogs(prev => prev.filter((_, idx) => idx !== i)) }}
          onCancel={() => { d.resolve(''); setAuthDialogs(prev => prev.filter((_, idx) => idx !== i)) }} />
      ))}
    </div>
    </UpdateProvider>
    </AuthProvider>
    </NavigationProvider>
  )
}

function App() {
  const deployState = useDeployModeState()
  return (
    <TaskQueueProvider>
      <DeployModeProvider value={deployState}>
        <AppContent />
        {/* 全局任务详情弹窗：App 层渲染，不依赖队列面板开关（拟态遮罩居中） */}
        <GlobalTaskModal />
      </DeployModeProvider>
    </TaskQueueProvider>
  )
}

export default App
