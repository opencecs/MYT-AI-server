import { useState, memo } from 'react'
import { gsap } from 'gsap'
import { useGSAP } from '@gsap/react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Send, Wifi, WifiOff, Loader2, Wrench, RotateCcw, Info, Eraser, X, Monitor, Search, ChevronDown, MessageSquare, Globe } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Browser } from '@wailsio/runtime'
import { agentC as c } from './agentTheme'
import { useMytAgent } from './useMytAgent'
import { useAuth } from '../../hooks/useAuth'
import AgentMessage from './AgentMessage'
import AgentInput from './AgentInput'
import DeviceToolsModal from './DeviceToolsModal'
import ScreenCastPanel from './ScreenCastPanel'
import SearchPanel from './SearchPanel'
import SkillsPage from './SkillsPage'
import TasksPage from './TasksPage'
import PlatformConfigPage from './PlatformConfigPage'
import TaskQueuePage from './TaskQueuePage'
import { AgentDialogProvider, useAgentDialog } from './useAgentDialog'
import { useTTS, useASR } from './useVoice'
import { parseUserFileAttachments, FileCard, transformAttachments, createMdComponents, buildFileHref } from './agentFileRender'
import type { HistoryRecord } from './agentTypes'
import { VIEWS, stateMeta } from './agentPageConstants'
import type { View } from './agentPageConstants'
import { useAgentPageEffects } from './agentPageEffects'
import { ApprovalDialog, FormDialog, NotificationToasts, SubAgentPanel } from './AgentEventDialogs'

function AgentPageInner() {
  const { t } = useTranslation()
  const { loggedIn } = useAuth()
  const agent = useMytAgent()
  const { messages, connState, sending, contextInfo, hasMoreHistory, connInfo, screencastItems, setScreencastItems, connect, reconnect, sendMessage, cancel, resetSession, recall, loadMoreHistory, refreshContext, clearHistory, searchHistory, client,
    approvals, formRequest, subAgents, notifications, loadedSkills, respondApproval, respondForm, dismissNotification,
  } = agent
  const { confirm, alert: alertFn } = useAgentDialog()
  const [view, setView] = useState<View>('chat')
  const [showMenu, setShowMenu] = useState(false)
  const [showTools, setShowTools] = useState(false)
  const [showContext, setShowContext] = useState(false)
  const [showScreencast, setShowScreencast] = useState(false)
  const [showSearch, setShowSearch] = useState(false)
  const [selectedRecord, setSelectedRecord] = useState<HistoryRecord | null>(null)
  const [inputText, setInputText] = useState('')

  const { ttsEnabled, ttsLoading, toggleTTS, watchMessages } = useTTS(connInfo)
  const { recording, startRecording, stopRecording } = useASR(connInfo, sendMessage)

  // useEffect 群已拆分到 agentPageEffects.ts
  const { scrollRef, listRef, pageRef, pageVisible } = useAgentPageEffects({
    connState, loggedIn, recording, startRecording, stopRecording, connect,
    setInputText, messages, watchMessages,
  })

  useGSAP(() => {
    // 未登录/空状态渲染的是引导页，DOM 里没有 .agent-header/.agent-chat/.agent-input-wrap
    // 且 pageRef 未挂载，跳过可避免 GSAP 每帧对不存在目标报 target not found / Invalid scope
    if (!loggedIn) return
    const tl = gsap.timeline({ defaults: { ease: 'power2.out' } })
    tl.fromTo('.agent-header', { y: -16, opacity: 0 }, { y: 0, opacity: 1, duration: 0.4 })
      .fromTo('.agent-chat', { opacity: 0 }, { opacity: 1, duration: 0.5 }, '-=0.2')
      .fromTo('.agent-input-wrap', { y: 16, opacity: 0 }, { y: 0, opacity: 1, duration: 0.35 }, '-=0.3')
  }, { scope: pageRef, dependencies: [loggedIn] })

  const meta = stateMeta[connState]
  const isEmpty = messages.length === 0

  // 未登录：引导空状态（与授权页一致），登录入口在导航栏
  if (!loggedIn) {
    return (
      <div className="h-full flex flex-col items-center justify-center" style={{ background: c.bg }}>
        <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-gray-100 to-gray-200 flex items-center justify-center mb-4 shadow-sm">
          <MessageSquare size={28} className="text-gray-400" />
        </div>
        <p className="text-sm font-medium text-gray-500 mb-1">{t('payment:pleaseLoginTitle')}</p>
        <p className="text-xs text-gray-400 mb-2">{t('payment:pleaseLoginDesc')}</p>
        <p className="text-xs text-blue-400">{t('payment:loginFromNav')}</p>
      </div>
    )
  }

  return (
    <div ref={pageRef} className="h-full flex flex-col" style={{ background: c.bg }}>
      {/* 顶栏 */}
      <header className="agent-header shrink-0 relative z-20 flex items-center gap-2 border-b px-4 py-2.5" style={{ borderColor: c.line, background: c.surface }}>
        <span className="flex items-center gap-1 text-xs" style={{ color: meta.color }}>
          {connState === 'connected' ? <Wifi className="size-3.5" />
            : connState === 'connecting' || connState === 'reconnecting' ? <Loader2 className="size-3.5 animate-spin" />
            : <WifiOff className="size-3.5" />}
          {t(meta.labelKey)}
        </span>
        <button onClick={() => reconnect().catch(() => {})}
          className="flex items-center gap-0.5 rounded px-1 py-0.5 text-xs transition-colors hover:bg-black/5"
          style={{ color: c.textMuted }}>
          {t('agent:conn.reconnect')}
        </button>
        {/* 视图下拉菜单 */}
        <div className="relative">
          <button onClick={() => setShowMenu(!showMenu)} className="flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs hover:bg-black/5"
            style={{ color: c.text, background: showMenu ? `${c.accent}10` : 'transparent' }}>
            {(() => { const Icon = VIEWS.find(v => v.id === view)?.icon || MessageSquare; return <Icon className="size-3.5" style={{ color: c.accent }} /> })()}
            {t(VIEWS.find(v => v.id === view)?.labelKey || 'agent:view.chat')}
            <ChevronDown className="size-3" style={{ color: c.textMuted }} />
          </button>
          {showMenu && (
            <div className="absolute left-0 top-full z-30 w-40 rounded-xl border shadow-xl py-1 animate-fadeIn mt-1" style={{ borderColor: c.line, background: c.surface }}>
              {VIEWS.map(v => {
                const Icon = v.icon
                return <button key={v.id} onClick={() => { setView(v.id); setShowMenu(false) }}
                  className="flex items-center gap-2 w-full px-3 py-1.5 text-left text-xs hover:bg-black/[0.03]"
                  style={{ color: view === v.id ? c.accent : c.text, background: view === v.id ? `${c.accent}08` : undefined }}>
                  <Icon className="size-3.5" />{t(v.labelKey)}
                </button>
              })}
            </div>
          )}
        </div>
        <div className="ml-auto flex items-center gap-1">
          <button onClick={() => setShowSearch(!showSearch)} title={t('agent:tooltip.searchHistory')}
            className="flex size-7 items-center justify-center rounded-lg transition-colors hover:bg-black/5">
            <Search className="size-3.5" style={{ color: showSearch ? c.accent : c.textMuted }} />
          </button>
          <button onClick={() => { setShowContext(!showContext); if (!showContext) refreshContext() }} title={t('agent:tooltip.contextInfo')}
            className="flex size-7 items-center justify-center rounded-lg transition-colors hover:bg-black/5">
            <Info className="size-3.5" style={{ color: showContext ? c.accent : c.textMuted }} />
          </button>
          <button onClick={async () => {
            // 清空记录会删除本地+服务端历史（不可恢复），必须二次确认
            if (!await confirm({ title: t('agent:confirm.clearHistoryTitle'), message: t('agent:confirm.clearHistoryMessage'), confirmLabel: t('agent:confirm.clearHistoryConfirm'), danger: true })) return
            clearHistory()
          }} disabled={connState !== 'connected' || isEmpty}
            title={t('agent:tooltip.clearHistory')}
            className="flex size-7 items-center justify-center rounded-lg transition-colors disabled:opacity-40 hover:bg-black/5">
            <Eraser className="size-3.5" style={{ color: c.textMuted }} />
          </button>
          <button onClick={async () => {
            // 重置上下文会丢弃本轮会话记忆，二次确认
            if (!await confirm({ title: t('agent:confirm.resetSessionTitle'), message: t('agent:confirm.resetSessionMessage'), confirmLabel: t('agent:confirm.resetSessionConfirm'), danger: true })) return
            resetSession()
          }} disabled={connState !== 'connected' || isEmpty}
            title={t('agent:tooltip.resetSession')}
            className="flex size-7 items-center justify-center rounded-lg transition-colors disabled:opacity-40 hover:bg-black/5">
            <RotateCcw className="size-3.5" style={{ color: c.textMuted }} />
          </button>
          <button onClick={() => setShowScreencast(!showScreencast)} title={t('agent:tooltip.screencast')}
            className="relative flex size-7 items-center justify-center rounded-lg transition-colors hover:bg-black/5">
            <Monitor className="size-3.5" style={{ color: showScreencast || screencastItems.length > 0 ? c.accent : c.textMuted }} />
            {screencastItems.length > 0 && (
              <span className="absolute -right-0.5 -top-0.5 flex size-3.5 items-center justify-center rounded-full text-[8px] text-white" style={{ background: c.accent }}>{screencastItems.length}</span>
            )}
          </button>
          <button onClick={() => setShowTools(true)} title={t('agent:tooltip.agentCleanup')}
            className="flex size-7 items-center justify-center rounded-lg transition-colors hover:bg-black/5">
            <Wrench className="size-3.5" style={{ color: c.textMuted }} />
          </button>
          <button onClick={() => Browser.OpenURL('https://agent.opencecs.com/')} title={t('agent:tooltip.openWeb')}
            className="flex items-center gap-1 rounded-lg px-2 py-1 text-xs transition-colors hover:bg-black/5"
            style={{ color: c.accent }}>
            <Globe className="size-3.5" />
            <span className="hidden sm:inline">{t('agent:webClient')}</span>
          </button>
        </div>
      </header>

      {/* 上下文信息面板 */}
      {showContext && contextInfo && (
        <div className="absolute right-4 z-50 rounded-xl border shadow-lg animate-fadeIn" style={{ top: 48, borderColor: c.line, background: c.surface, maxWidth: 320 }}>
          <div className="px-4 py-3">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-[11px] font-bold uppercase tracking-wider" style={{ color: c.textMuted }}>{t('agent:ctx.title')}</span>
              <button onClick={() => setShowContext(false)} className="hover:opacity-70"><X className="size-3.5" style={{ color: c.textMuted }} /></button>
            </div>
            <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-[11px]">
              <CtxRow label={t('agent:ctx.tokens')} value={contextInfo.tokens.toLocaleString()} />
              <CtxRow label={t('agent:ctx.msgCount')} value={String(contextInfo.msg_count)} />
              <CtxRow label={t('agent:ctx.activeSessions')} value={String(contextInfo.active_sessions)} />
              <CtxRow label={t('agent:ctx.cycle')} value={String(contextInfo.cycle_num)} />
              <CtxRow label={t('agent:ctx.status')} value={contextInfo.status} />
              <CtxRow label={t('agent:ctx.maxSessions')} value={String(contextInfo.max_sessions)} />
            </div>
          </div>
        </div>
      )}

      {/* 搜索面板 */}
      {showSearch && (
        <SearchPanel onSearch={searchHistory} onClose={() => setShowSearch(false)} onSelect={setSelectedRecord} />
      )}

      {/* 搜索结果详情弹窗 */}
      {selectedRecord && (
        <div className="fixed inset-0 z-[200] flex items-center justify-center bg-black/40 animate-fadeIn" onClick={() => setSelectedRecord(null)}>
          <div className="mx-4 flex max-h-[80vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border shadow-2xl animate-fadeIn" style={{ borderColor: c.line, background: c.surface }} onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center gap-2.5 border-b px-5 py-3.5" style={{ borderColor: c.line }}>
              <span className="rounded-md px-2 py-0.5 text-[11px] font-medium" style={{ background: selectedRecord.role === 'user' ? `${c.accent}1A` : `${c.think}1A`, color: selectedRecord.role === 'user' ? c.accent : c.think }}>
                {selectedRecord.role === 'user' ? t('agent:chat.user') : 'Agent'}
              </span>
              {selectedRecord.platform && <span className="text-[11px]" style={{ color: c.textMuted }}>{selectedRecord.platform}</span>}
              <span className="text-[11px]" style={{ color: c.textMuted }}>{selectedRecord.created_at || ''}</span>
              <button onClick={() => setSelectedRecord(null)} className="ml-auto hover:opacity-70" style={{ color: c.textMuted }}><X className="size-4" /></button>
            </div>
            <div className="flex-1 overflow-y-auto px-5 py-4">
              {selectedRecord.steps && selectedRecord.steps.filter(s => s.type === 'tool').length > 0 && (
                <div className="mb-3 flex flex-wrap gap-1">
                  {selectedRecord.steps.filter(s => s.type === 'tool').map((s, i) => (
                    <span key={i} className="rounded px-1.5 py-0.5 text-[10px]" style={{ background: c.surfaceAlt, color: c.textMuted }}>{s.name}</span>
                  ))}
                </div>
              )}
              {(() => {
                const rec = selectedRecord
                const baseUrl = connInfo?.baseUrl ?? ''
                const token = connInfo?.sessionToken ?? ''
                if (rec.role === 'user') {
                  const { cleanText, files } = parseUserFileAttachments(rec.content || '')
                  return (
                    <>
                      {rec.image && (
                        <img src={`data:image/jpeg;base64,${rec.image}`} alt="" className="mb-3 max-h-64 rounded-lg object-contain" />
                      )}
                      {files.length > 0 && (
                        <div className="mb-3 flex flex-wrap gap-2">
                          {files.map((f, i) => <div key={i} className="w-full max-w-xs"><FileCard href={buildFileHref(f.path, baseUrl, token)} filename={f.name} /></div>)}
                        </div>
                      )}
                      {cleanText && <div className="text-sm leading-relaxed whitespace-pre-wrap" style={{ color: c.text }}>{cleanText}</div>}
                    </>
                  )
                }
                return (
                  <div className="prose-chat text-sm leading-relaxed" style={{ color: c.text }}>
                    <ReactMarkdown remarkPlugins={[remarkGfm]} components={createMdComponents(baseUrl, token)}>{transformAttachments(rec.content || '', baseUrl, token)}</ReactMarkdown>
                  </div>
                )
              })()}
            </div>
          </div>
        </div>
      )}

      {/* 主体 */}
      {view !== 'chat' ? (
        <div className="flex-1 overflow-hidden">
          {view === 'skills' && <SkillsPage client={client} onConfirm={confirm} onAlert={(m) => alertFn(m).catch(() => {})} />}
          {view === 'tasks' && <TasksPage client={client} onConfirm={confirm} onAlert={(m) => alertFn(m).catch(() => {})} />}
          {view === 'platforms' && <PlatformConfigPage onAlert={(m) => alertFn(m).catch(() => {})} />}
          {view === 'monitor' && connInfo && <TaskQueuePage client={client} connInfo={connInfo} />}
        </div>
      ) : (
      <div className="flex-1 flex overflow-hidden min-h-0">
        <div className="flex-1 flex flex-col overflow-hidden min-w-0">
          {/* 对话区 */}
          <div ref={scrollRef} className="agent-chat flex-1 overflow-y-auto px-8 py-6">
            {isEmpty ? (
              <div className="flex h-full flex-col items-center justify-center gap-3">
                <div className="flex size-12 items-center justify-center rounded-xl" style={{ background: `${c.accent}1A`, border: `1px solid ${c.accent}33` }}>
                  <Send className="size-5" style={{ color: c.accent }} />
                </div>
                <div className="text-center">
                  <div className="text-base font-medium" style={{ color: c.text }}>{t('agent:chat.startTitle')}</div>
                  <div className="mt-1.5 text-xs" style={{ color: c.textMuted }}>{t('agent:chat.startDesc')}</div>
                </div>
              </div>
            ) : (
              <div ref={listRef} className="relative mx-auto max-w-4xl">
                <div className="absolute bottom-0 left-4 top-0 w-px" style={{ background: c.line }} />
                {hasMoreHistory && (
                  <div className="flex justify-center pb-3">
                    <button onClick={loadMoreHistory} className="text-xs transition-colors hover:opacity-70" style={{ color: c.accent }}>
                      {t('agent:chat.loadMore')}
                    </button>
                  </div>
                )}
                <div className="ml-0">
                  {subAgents && subAgents.length > 0 && <SubAgentPanel subAgents={subAgents} />}
                  {messages.map((m) => {
                    const canRecall = m.role === 'user' && !sending
                    return <AgentMessage key={m.id} message={m} canRecall={canRecall} onRecall={() => recall(m.id)} connInfo={connInfo} />
                  })}
                </div>
              </div>
            )}
          </div>

          {/* 输入区 */}
          <div className="agent-input-wrap">
            <AgentInput onSend={sendMessage} onCancel={cancel}
              disabled={connState !== 'connected'} sending={sending}
              recording={recording} ttsEnabled={ttsEnabled} ttsLoading={ttsLoading} connInfo={connInfo}
              inputText={inputText} setInputText={setInputText}
              onStartRecording={startRecording} onStopRecording={stopRecording} onToggleTTS={toggleTTS} />
          </div>
        </div>

        {/* 投屏面板 */}
        <ScreenCastPanel visible={showScreencast && pageVisible} items={screencastItems} connInfo={connInfo}
          onClose={() => setShowScreencast(false)}
          onRemove={(id) => setScreencastItems(prev => prev.filter(i => i.id !== id))}
          onClear={() => setScreencastItems([])} />
      </div>
      )}

      {/* Agent 事件弹窗：审批、表单、通知、子Agent */}
      <ApprovalDialog approvals={approvals || []} onRespond={(id, ok) => respondApproval(id, client, ok)} />
      <FormDialog form={formRequest} onRespond={(id, data, cancelled) => respondForm(id, client, data, cancelled)} />
      <NotificationToasts notifications={notifications || []} onDismiss={dismissNotification} />
      {showTools && <DeviceToolsModal onClose={() => setShowTools(false)} />}
    </div>
  )
}

function CtxRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <span style={{ color: c.textMuted }}>{label}</span>
      <span className="font-semibold" style={{ color: c.text }}>{value}</span>
    </div>
  )
}

function AgentPage() {
  return (
    <AgentDialogProvider>
      <AgentPageInner />
    </AgentDialogProvider>
  )
}

export default memo(AgentPage)
